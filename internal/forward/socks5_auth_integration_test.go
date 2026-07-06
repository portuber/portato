package forward

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/sshtest"
	"golang.org/x/crypto/ssh"
)

// socks5DialUserPass performs a SOCKS5 CONNECT handshake offering ONLY the
// UserPass auth method (0x02). On success it returns the established connection
// tunneled to dst; on auth failure (or method rejection) it returns an error so
// the caller can distinguish a rejected credential from a transport failure.
// Mirrors socks5Dial (no-auth) in tunnel_integration_test.go (Phase 20).
func socks5DialUserPass(t *testing.T, proxy, dst, user, pass string) (net.Conn, error) {
	t.Helper()
	conn, err := net.Dial("tcp", proxy)
	if err != nil {
		return nil, fmt.Errorf("dial proxy %s: %w", proxy, err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Greeting: offer only UserPass (0x02).
	if _, err := conn.Write([]byte{0x05, 0x01, 0x02}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write greeting: %w", err)
	}
	gr := make([]byte, 2)
	if _, err := io.ReadFull(conn, gr); err != nil {
		conn.Close()
		return nil, fmt.Errorf("read greeting resp: %w", err)
	}
	if gr[0] != 0x05 {
		conn.Close()
		return nil, fmt.Errorf("greeting resp ver=%x, want 05", gr[0])
	}
	if gr[1] != 0x02 {
		conn.Close()
		return nil, fmt.Errorf("server selected method %x, want 02 (UserPass)", gr[1])
	}

	// RFC 1929 user/pass sub-negotiation: VER=0x01, ULEN, UNAME, PLEN, PASSWD.
	ub := []byte(user)
	pb := []byte(pass)
	if len(ub) > 255 || len(pb) > 255 {
		conn.Close()
		return nil, fmt.Errorf("user/pass too long")
	}
	authReq := make([]byte, 0, 3+len(ub)+len(pb))
	authReq = append(authReq, 0x01, byte(len(ub)))
	authReq = append(authReq, ub...)
	authReq = append(authReq, byte(len(pb)))
	authReq = append(authReq, pb...)
	if _, err := conn.Write(authReq); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write auth: %w", err)
	}
	ar := make([]byte, 2)
	if _, err := io.ReadFull(conn, ar); err != nil {
		conn.Close()
		return nil, fmt.Errorf("read auth resp: %w", err)
	}
	if ar[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("auth rejected (status %x)", ar[1])
	}

	host, portStr, err := net.SplitHostPort(dst)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("bad dst %q: %w", dst, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("bad port in %q: %w", dst, err)
	}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		conn.Close()
		return nil, fmt.Errorf("dst %q is not IPv4", dst)
	}
	req := []byte{0x05, 0x01, 0x00, 0x01, ip[0], ip[1], ip[2], ip[3], byte(port >> 8), byte(port)}
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write connect: %w", err)
	}
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		conn.Close()
		return nil, fmt.Errorf("read reply hdr: %w", err)
	}
	if hdr[0] != 0x05 || hdr[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("connect reply rep=%x, want 00", hdr[1])
	}
	switch hdr[3] {
	case 0x01:
		_, _ = io.ReadFull(conn, make([]byte, 4+2))
	case 0x04:
		_, _ = io.ReadFull(conn, make([]byte, 16+2))
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			conn.Close()
			return nil, fmt.Errorf("read bnd domain len: %w", err)
		}
		_, _ = io.ReadFull(conn, make([]byte, int(l[0])+2))
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// TestTunnelDynamicSocks5Auth is the Phase 20 SOCKS5 user/pass end-to-end test.
// A type=dynamic tunnel configured with socks5_user/socks5_password must:
//   - accept a SOCKS5 client with the correct creds and forward traffic;
//   - reject a client offering wrong creds at the auth step;
//   - reject a client offering NoAuth only (the server requires UserPass).
func TestTunnelDynamicSocks5Auth(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	echoAddr, stopEcho := startEcho(t)
	defer stopEcho()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen client key: %v", err)
	}
	authorizedKey, _ := ssh.NewPublicKey(pub)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal client priv: %v", err)
	}
	dir := t.TempDir()
	idPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(idPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	knownHosts := filepath.Join(dir, "known_hosts")

	srv := sshtest.NewSSHD(t, authorizedKey)
	srv.Start()
	defer srv.Stop()

	localPort := freePort(t)
	localAddr := fmt.Sprintf("127.0.0.1:%d", localPort)

	cfg := config.Tunnel{
		Name: "d-auth", Type: "dynamic",
		Local:    strconv.Itoa(localPort),
		SSH:      "u@" + srv.Addr(),
		Identity: idPath,
		User:     "u", Host: "127.0.0.1", Port: srv.Port,
		Socks5User: "alice", Socks5Password: "wonderland",
	}
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTunnel(ctx, cfg, def, slog.Default(), nil)
	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tun.Stop()

	if !waitForState(tun, Connected, 5*time.Second) {
		s := tun.Status()
		t.Fatalf("did not reach Connected: state=%s err=%q", s.State, s.Error)
	}

	// Correct creds → CONNECT succeeds and echo round-trips.
	conn, err := socks5DialUserPass(t, localAddr, echoAddr, "alice", "wonderland")
	if err != nil {
		t.Fatalf("correct creds: %v", err)
	}
	msg := []byte("hello-auth")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.Equal(buf, msg) {
		t.Errorf("echo = %q, want %q", buf, msg)
	}
	conn.Close()

	// Wrong password → rejected at the auth step.
	if _, err := socks5DialUserPass(t, localAddr, echoAddr, "alice", "WRONG"); err == nil {
		t.Error("wrong password: expected auth rejection, got success")
	}

	// Wrong user → rejected at the auth step.
	if _, err := socks5DialUserPass(t, localAddr, echoAddr, "bob", "wonderland"); err == nil {
		t.Error("wrong user: expected auth rejection, got success")
	}

	// NoAuth only → the server must reject the greeting (it requires UserPass).
	c, err := net.Dial("tcp", localAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil { // offer NoAuth only
		t.Fatalf("write greeting: %v", err)
	}
	gr := make([]byte, 2)
	if _, err := io.ReadFull(c, gr); err != nil {
		t.Fatalf("read greeting resp: %v", err)
	}
	c.Close()
	if gr[1] != 0xFF {
		t.Errorf("NoAuth greeting: server selected method %x, want FF (no acceptable methods)", gr[1])
	}
}
