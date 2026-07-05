//go:build unix

package fdpass

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// maxMsg caps the single transfer message's data buffer: a 4-byte length prefix
// followed by the JSON list of Headers. A few hundred tunnels fit easily; the
// buffer bounds a single ReadMsgUnix.
const maxMsg = 16 * 1024

// maxFDs caps how many listeners one transfer may carry. It sizes the ancillary
// (SCM_RIGHTS) buffer; exceeding it is reported via the MSG_CTRUNC flag rather
// than silently dropped fds.
const maxFDs = 128

// Send hands every offer to the receiver in a single message: the JSON list of
// Headers (name/type, in offer order), length-prefixed, as the payload, and ALL
// the listener fds packed into one SCM_RIGHTS ancillary message in the same
// order. Batching into one message keeps the fd<->header association unambiguous
// on a SOCK_STREAM ("unix") socket, whose per-write boundaries are not preserved
// across reads. It closes each offer's File after sending (the kernel dup's the
// fds into the receiver) and half-closes the write side so Recv sees EOF.
func Send(c *net.UnixConn, offers []Offer) error {
	if len(offers) == 0 {
		return c.CloseWrite()
	}
	if len(offers) > maxFDs {
		for _, o := range offers {
			_ = o.File.Close()
		}
		return fmt.Errorf("fdpass: %d offers exceed max %d", len(offers), maxFDs)
	}
	headers := make([]Header, len(offers))
	fds := make([]int, 0, len(offers))
	files := make([]*os.File, 0, len(offers))
	for i, o := range offers {
		if o.File == nil {
			for _, f := range files {
				_ = f.Close()
			}
			return fmt.Errorf("fdpass: offer %q has no file", o.Name)
		}
		headers[i] = Header{Name: o.Name, Type: o.Type}
		fds = append(fds, int(o.File.Fd()))
		files = append(files, o.File)
	}
	closeAll := func() {
		for _, f := range files {
			_ = f.Close()
		}
	}
	body, err := json.Marshal(headers)
	if err != nil {
		closeAll()
		return fmt.Errorf("fdpass: marshal headers: %w", err)
	}
	if 4+len(body) > maxMsg {
		closeAll()
		return fmt.Errorf("fdpass: payload %d exceeds max %d", len(body), maxMsg-4)
	}
	payload := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(payload[:4], uint32(len(body)))
	copy(payload[4:], body)
	oob := unix.UnixRights(fds...)
	if _, _, err := c.WriteMsgUnix(payload, oob, nil); err != nil {
		closeAll()
		return fmt.Errorf("fdpass: send: %w", err)
	}
	closeAll()
	return c.CloseWrite()
}

// Recv reads the single transfer message and reconstructs each offered listener
// via net.FileListener, keyed by tunnel name (from the Headers, in order). It
// returns an empty map when the sender had nothing to pass (it half-closed
// without sending). The control message (all fds) arrives whole with the first
// ReadMsgUnix that touches the payload; the length prefix lets Recv top up a
// split payload with plain reads on the same socket.
func Recv(c *net.UnixConn) (map[string]net.Listener, error) {
	out := make(map[string]net.Listener)
	buf := make([]byte, maxMsg)
	oob := make([]byte, unix.CmsgSpace(maxFDs))
	n, oobn, flags, _, err := c.ReadMsgUnix(buf, oob)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		return out, err
	}
	if n == 0 && oobn == 0 {
		return out, nil
	}
	if flags&unix.MSG_CTRUNC != 0 {
		return out, fmt.Errorf("fdpass: control message truncated (more than %d fds)", maxFDs)
	}

	// Top the payload up to the 4-byte length prefix, then to 4+length. The oob
	// is already captured; remaining bytes are plain stream data.
	have := buf[:n]
	for len(have) < 4 {
		m, rerr := c.Read(buf[len(have):])
		if rerr != nil {
			return out, rerr
		}
		if m == 0 {
			return out, io.ErrUnexpectedEOF
		}
		have = buf[:len(have)+m]
	}
	plen := int(binary.BigEndian.Uint32(have[:4]))
	if plen > maxMsg-4 {
		return out, fmt.Errorf("fdpass: payload length %d exceeds max %d", plen, maxMsg-4)
	}
	for len(have) < 4+plen {
		m, rerr := c.Read(buf[len(have):])
		if rerr != nil {
			return out, rerr
		}
		if m == 0 {
			return out, io.ErrUnexpectedEOF
		}
		have = buf[:len(have)+m]
	}

	var headers []Header
	if err := json.Unmarshal(have[4:4+plen], &headers); err != nil {
		return out, fmt.Errorf("fdpass: parse headers: %w", err)
	}
	fds, err := fdsFromOOB(oob[:oobn])
	if err != nil {
		return out, fmt.Errorf("fdpass: %w", err)
	}
	if len(fds) != len(headers) {
		return out, fmt.Errorf("fdpass: %d headers but %d fds", len(headers), len(fds))
	}
	for i, h := range headers {
		if h.Name == "" {
			return out, errors.New("fdpass: header missing name")
		}
		f := os.NewFile(uintptr(fds[i]), "portato-adopt-"+h.Name)
		ln, err := net.FileListener(f)
		_ = f.Close()
		if err != nil {
			return out, fmt.Errorf("fdpass: %s: adopt listener: %w", h.Name, err)
		}
		out[h.Name] = ln
	}
	return out, nil
}

// fdsFromOOB extracts all fds from an SCM_RIGHTS control message.
func fdsFromOOB(oob []byte) ([]int, error) {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, fmt.Errorf("parse control message: %w", err)
	}
	var all []int
	for _, m := range msgs {
		if m.Header.Level != unix.SOL_SOCKET || m.Header.Type != unix.SCM_RIGHTS {
			continue
		}
		fds, err := unix.ParseUnixRights(&m)
		if err != nil {
			return nil, fmt.Errorf("parse scm_rights: %w", err)
		}
		all = append(all, fds...)
	}
	if len(all) == 0 {
		return nil, errors.New("no fds in ancillary data")
	}
	return all, nil
}
