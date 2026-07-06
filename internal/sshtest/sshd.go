// Package sshtest provides an in-process SSH server fixture for tests: a real
// golang.org/x/crypto/ssh server that serves direct-tcpip (-L) and
// tcpip-forward (-R) channels against a caller-supplied authorized public key.
// It is shared by the forward-package integration tests and by cross-package
// end-to-end tests (e.g. the Phase 16 hand-off E2E) that need a live SSH
// endpoint without standing up a real sshd.
package sshtest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// directTCPIP mirrors the wire payload of a "direct-tcpip" channel open.
type directTCPIP struct {
	Raddr string
	Rport uint32
	Laddr string
	Lport uint32
}

type connTracker struct {
	mu    sync.Mutex
	conns []*ssh.ServerConn
}

func (c *connTracker) add(s *ssh.ServerConn) {
	c.mu.Lock()
	c.conns = append(c.conns, s)
	c.mu.Unlock()
}

func (c *connTracker) remove(s *ssh.ServerConn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, x := range c.conns {
		if x == s {
			c.conns = append(c.conns[:i], c.conns[i+1:]...)
			return
		}
	}
}

func (c *connTracker) closeAll() {
	c.mu.Lock()
	old := c.conns
	c.conns = nil
	c.mu.Unlock()
	for _, s := range old {
		_ = s.Close()
	}
}

// SSHD is an in-process test SSH server. It serves direct-tcpip (-L) and
// tcpip-forward (-R) channels, authorizing only the public key passed to
// NewSSHD. The server offers BOTH an ECDSA and an ED25519 host key:
// x/crypto/ssh's default preference negotiates ECDSA first, so a client that
// records only the ED25519 key would otherwise hit a spurious "host key
// mismatch" -- the regression this dual-key setup guards against.
type SSHD struct {
	tb  testing.TB
	cfg *ssh.ServerConfig

	// Port is the localhost TCP port the server listens on (set by Start).
	Port int
	// Ed25519Pub is the server's ED25519 host public key, for a known_hosts
	// entry when accept_new_hosts is false.
	Ed25519Pub ssh.PublicKey

	listener net.Listener
	tracker  *connTracker
}

// NewSSHD builds a test SSH server that authorizes only authorizedKey. Call
// Start to bind and serve.
func NewSSHD(tb testing.TB, authorizedKey ssh.PublicKey) *SSHD {
	tb.Helper()
	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		tb.Fatalf("gen ed25519 host key: %v", err)
	}
	edSigner, err := ssh.NewSignerFromSigner(edPriv)
	if err != nil {
		tb.Fatalf("ed25519 signer: %v", err)
	}
	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatalf("gen ecdsa host key: %v", err)
	}
	ecSigner, err := ssh.NewSignerFromKey(ecPriv)
	if err != nil {
		tb.Fatalf("ecdsa signer: %v", err)
	}
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), authorizedKey.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("unknown public key")
		},
	}
	cfg.AddHostKey(edSigner)
	cfg.AddHostKey(ecSigner)
	return &SSHD{tb: tb, cfg: cfg, tracker: &connTracker{}, Ed25519Pub: edSigner.PublicKey()}
}

// Start binds the server on 127.0.0.1:0 and serves until Stop. Port is set on
// return.
func (s *SSHD) Start() {
	s.tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		s.tb.Fatalf("sshd listen: %v", err)
	}
	s.listener = ln
	s.Port = ln.Addr().(*net.TCPAddr).Port
	go s.accept()
}

func (s *SSHD) accept() {
	for {
		nConn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(nConn)
	}
}

// Addr returns the "127.0.0.1:port" the server listens on.
func (s *SSHD) Addr() string {
	return fmt.Sprintf("127.0.0.1:%d", s.Port)
}

// Stop closes the listener and every active server connection.
func (s *SSHD) Stop() {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.tracker.closeAll()
}

// Restart stops the server and rebinds on the same port (for auto-reconnect
// tests).
func (s *SSHD) Restart() {
	s.Stop()
	time.Sleep(80 * time.Millisecond)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.Port))
	if err != nil {
		s.tb.Fatalf("sshd restart listen: %v", err)
	}
	s.listener = ln
	go s.accept()
}

func (s *SSHD) handleConn(nConn net.Conn) {
	sconn, chans, reqs, err := ssh.NewServerConn(nConn, s.cfg)
	if err != nil {
		return
	}
	s.tracker.add(sconn)
	defer func() {
		s.tracker.remove(sconn)
		_ = sconn.Close()
	}()
	go s.serveForwards(sconn, reqs)
	for nch := range chans {
		if nch.ChannelType() != "direct-tcpip" {
			_ = nch.Reject(ssh.UnknownChannelType, "only direct-tcpip")
			continue
		}
		var d directTCPIP
		if err := ssh.Unmarshal(nch.ExtraData(), &d); err != nil {
			_ = nch.Reject(ssh.Prohibited, "bad payload")
			continue
		}
		ch, creqs, err := nch.Accept()
		if err != nil {
			continue
		}
		go s.serveDirect(ch, creqs, net.JoinHostPort(d.Raddr, strconv.Itoa(int(d.Rport))))
	}
}

func (s *SSHD) serveDirect(ch ssh.Channel, creqs <-chan *ssh.Request, addr string) {
	defer ch.Close()
	go ssh.DiscardRequests(creqs)
	backend, err := net.Dial("tcp", addr)
	if err != nil {
		return
	}
	defer backend.Close()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(ch, backend)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(backend, ch)
		done <- struct{}{}
	}()
	<-done
}

// forwardRequest is the wire payload of a "tcpip-forward" / "cancel-tcpip-
// forward" global request (RFC 4254 §7.1).
type forwardRequest struct {
	Addr string
	Port uint32
}

// forwardedPayload is the wire payload of a "forwarded-tcpip" channel that the
// server opens on the client when a connection arrives at the forwarded port
// (RFC 4254 §7.2). Addr/Port identify the listened address (must match what
// the client registered); OriginAddr/OriginPort identify the connecting peer.
type forwardedPayload struct {
	Addr       string
	Port       uint32
	OriginAddr string
	OriginPort uint32
}

type fwdEntry struct {
	ln   net.Listener
	host string
	port uint32
}

// serveForwards implements the server side of remote (-R) forwarding for the
// test sshd: it honors tcpip-forward (binds a real loopback port on the test
// host, modelling the "server side") and, on each accepted connection, opens a
// forwarded-tcpip channel back to the client. This is what a type=remote
// tunnel relies on via ssh.Client.Listen.
func (s *SSHD) serveForwards(sconn *ssh.ServerConn, reqs <-chan *ssh.Request) {
	var (
		mu  sync.Mutex
		fwd = make(map[string]fwdEntry)
	)
	for r := range reqs {
		switch r.Type {
		case "tcpip-forward":
			var p forwardRequest
			if err := ssh.Unmarshal(r.Payload, &p); err != nil {
				r.Reply(false, nil)
				continue
			}
			bind := net.JoinHostPort(p.Addr, strconv.FormatUint(uint64(p.Port), 10))
			ln, err := net.Listen("tcp", bind)
			if err != nil {
				r.Reply(false, nil)
				continue
			}
			port := p.Port
			if port == 0 {
				port = uint32(addrPort(ln.Addr()))
			}
			mu.Lock()
			fwd[bind] = fwdEntry{ln: ln, host: p.Addr, port: port}
			mu.Unlock()
			resp := make([]byte, 4)
			binary.BigEndian.PutUint32(resp, port)
			r.Reply(true, resp)
			go s.acceptForwarded(sconn, ln, p.Addr, port)
		case "cancel-tcpip-forward":
			var p forwardRequest
			if err := ssh.Unmarshal(r.Payload, &p); err != nil {
				r.Reply(false, nil)
				continue
			}
			bind := net.JoinHostPort(p.Addr, strconv.FormatUint(uint64(p.Port), 10))
			mu.Lock()
			e, ok := fwd[bind]
			if ok {
				delete(fwd, bind)
			}
			mu.Unlock()
			if ok {
				_ = e.ln.Close()
			}
			r.Reply(true, nil)
		default:
			if r.WantReply {
				r.Reply(false, nil)
			}
		}
	}
	mu.Lock()
	for key, e := range fwd {
		_ = e.ln.Close()
		delete(fwd, key)
	}
	mu.Unlock()
}

func (s *SSHD) acceptForwarded(sconn *ssh.ServerConn, ln net.Listener, host string, port uint32) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(conn net.Conn) {
			defer conn.Close()
			originHost := "127.0.0.1"
			originPort := uint32(0)
			if ta, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
				originHost = ta.IP.String()
				originPort = uint32(ta.Port)
			}
			payload := ssh.Marshal(&forwardedPayload{
				Addr:       host,
				Port:       port,
				OriginAddr: originHost,
				OriginPort: originPort,
			})
			ch, creqs, err := sconn.OpenChannel("forwarded-tcpip", payload)
			if err != nil {
				return
			}
			go ssh.DiscardRequests(creqs)
			done := make(chan struct{}, 2)
			go func() {
				_, _ = io.Copy(ch, conn)
				done <- struct{}{}
			}()
			go func() {
				_, _ = io.Copy(conn, ch)
				done <- struct{}{}
			}()
			<-done
			_ = ch.Close()
		}(conn)
	}
}

func addrPort(a net.Addr) int {
	if ta, ok := a.(*net.TCPAddr); ok {
		return ta.Port
	}
	return 0
}
