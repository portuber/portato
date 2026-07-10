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

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/sshtest"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

func startEcho(t *testing.T) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// startTestAgent serves an in-process ssh-agent on a unix socket with the
// given private key loaded, so the agent-auth path can be exercised without
// touching the host's SSH_AUTH_SOCK.
func startTestAgent(t *testing.T, priv any) (sock string, cleanup func()) {
	t.Helper()
	sock = filepath.Join(t.TempDir(), "agent.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("agent listen: %v", err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatalf("agent add key: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = agent.ServeAgent(keyring, c)
			}(c)
		}
	}()
	return sock, func() { _ = ln.Close() }
}

func waitForState(t *Tuber, want State, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if t.Status().State == want {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return t.Status().State == want
}

func waitForNotState(t *Tuber, notWant State, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if t.Status().State != notWant {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// waitForPortDown polls addr until a dial fails (the port closed) or timeout.
// SSH closes a remote (-R) listener asynchronously, so the server-side port
// takes a moment to disappear after Stop.
func waitForPortDown(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return true
		}
		_ = c.Close()
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestTuberTrafficAndReconnect(t *testing.T) {
	// Hermetic: ignore the host's ssh-agent so only the identity-file auth
	// path is exercised.
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

	cfg := config.Tuber{
		Name:     "t-test",
		Type:     "local",
		Local:    strconv.Itoa(localPort),
		Remote:   echoAddr,
		SSH:      "u@" + srv.Addr(),
		Identity: idPath,
		User:     "u",
		Host:     "127.0.0.1",
		Port:     srv.Port,
	}
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTuber(ctx, cfg, def, slog.Default(), nil)
	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tun.Stop()

	if !waitForState(tun, Connected, 5*time.Second) {
		s := tun.Status()
		t.Fatalf("did not reach Connected: state=%s err=%q", s.State, s.Error)
	}

	ping := func(label string) {
		t.Helper()
		conn, err := net.Dial("tcp", localAddr)
		if err != nil {
			t.Fatalf("%s: dial local: %v", label, err)
		}
		defer conn.Close()
		msg := []byte("hello-" + label)
		if _, err := conn.Write(msg); err != nil {
			t.Fatalf("%s: write: %v", label, err)
		}
		buf := make([]byte, len(msg))
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Fatalf("%s: read: %v", label, err)
		}
		if !bytes.Equal(buf, msg) {
			t.Errorf("%s: echo %q, want %q", label, buf, msg)
		}
	}

	ping("first")

	// Kill the SSH server (drop active conns + close listener) and confirm
	// the tuber self-heals via the reconnect loop.
	srv.Stop()
	if !waitForNotState(tun, Connected, 5*time.Second) {
		t.Fatal("tuber stayed Connected after server kill")
	}

	srv.Restart()
	if !waitForState(tun, Connected, 15*time.Second) {
		s := tun.Status()
		t.Fatalf("did not reconnect after server restart: state=%s err=%q", s.State, s.Error)
	}

	ping("after-reconnect")

	// Disable must close the local port.
	if err := tun.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	c, err := net.Dial("tcp", localAddr)
	if err == nil {
		c.Close()
		t.Error("local port still open after Stop")
	}
}

// TestTuberHonoursKnownHostKeyType guards against golang/go#36126: the server
// offers both ECDSA (preferred by x/crypto's default order) and ED25519, but
// known_hosts only has the ED25519 key. The client must negotiate the key type
// it already trusts instead of bailing out with "host key mismatch".
func TestTuberHonoursKnownHostKeyType(t *testing.T) {
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

	srv := sshtest.NewSSHD(t, authorizedKey)
	srv.Start()
	defer srv.Stop()

	// Seed known_hosts with ONLY the server's ED25519 host key (strict mode).
	knownHosts := filepath.Join(dir, "known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize(srv.Addr())}, srv.Ed25519Pub)
	if err := os.WriteFile(knownHosts, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("seed known_hosts: %v", err)
	}

	localPort := freePort(t)
	localAddr := fmt.Sprintf("127.0.0.1:%d", localPort)
	cfg := config.Tuber{
		Name:     "kh-test",
		Type:     "local",
		Local:    strconv.Itoa(localPort),
		Remote:   echoAddr,
		SSH:      "u@" + srv.Addr(),
		Identity: idPath,
		User:     "u",
		Host:     "127.0.0.1",
		Port:     srv.Port,
	}
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: false}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTuber(ctx, cfg, def, slog.Default(), nil)
	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tun.Stop()

	if !waitForState(tun, Connected, 5*time.Second) {
		s := tun.Status()
		t.Fatalf("expected ED25519 to be negotiated despite ECDSA being preferred; state=%s err=%q", s.State, s.Error)
	}

	conn, err := net.Dial("tcp", localAddr)
	if err != nil {
		t.Fatalf("dial local: %v", err)
	}
	defer conn.Close()
	msg := []byte("kh")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf, msg) {
		t.Errorf("echo %q, want %q", buf, msg)
	}
}

// TestTuberAuthViaAgent exercises the ssh-agent auth path end-to-end: the
// tuber has no identity file, so authentication must come from the in-process
// agent. Guards against the "use of closed network connection" bug where the
// agent connection was closed before the lazy signers signed during the
// handshake.
func TestTuberAuthViaAgent(t *testing.T) {
	echoAddr, stopEcho := startEcho(t)
	defer stopEcho()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen client key: %v", err)
	}
	authorizedKey, _ := ssh.NewPublicKey(pub)

	sock, stopAgent := startTestAgent(t, priv)
	defer stopAgent()
	t.Setenv("SSH_AUTH_SOCK", sock)

	srv := sshtest.NewSSHD(t, authorizedKey)
	srv.Start()
	defer srv.Stop()

	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	localPort := freePort(t)
	localAddr := fmt.Sprintf("127.0.0.1:%d", localPort)
	cfg := config.Tuber{
		Name:   "agent-test",
		Type:   "local",
		Local:  strconv.Itoa(localPort),
		Remote: echoAddr,
		SSH:    "u@" + srv.Addr(),
		User:   "u",
		Host:   "127.0.0.1",
		Port:   srv.Port,
		// no Identity -> agent is the only auth source
	}
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTuber(ctx, cfg, def, slog.Default(), nil)
	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tun.Stop()

	if !waitForState(tun, Connected, 5*time.Second) {
		s := tun.Status()
		t.Fatalf("agent auth did not reach Connected: state=%s err=%q", s.State, s.Error)
	}

	conn, err := net.Dial("tcp", localAddr)
	if err != nil {
		t.Fatalf("dial local: %v", err)
	}
	defer conn.Close()
	msg := []byte("via-agent")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf, msg) {
		t.Errorf("echo %q, want %q", buf, msg)
	}
}

// TestTuberRemoteTrafficAndReconnect exercises a type=remote (-R) tuber end
// to end: the port is listened on the server side (via ssh.Client.Listen), and
// traffic is forwarded back to a local echo server. It also confirms the
// remote listener is re-established after an sshd drop/restart.
func TestTuberRemoteTrafficAndReconnect(t *testing.T) {
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

	// The port the remote tuber will bind on the server side (loopback): the
	// test sshd is a Go listener that cannot bind the "*" wildcard a bare port
	// now expands to, so the test requests loopback explicitly.
	remotePort := freePort(t)
	remoteBind := fmt.Sprintf("127.0.0.1:%d", remotePort)

	cfg := config.Tuber{
		Name:     "r-test",
		Type:     "remote",
		Local:    echoAddr,                                // forward server-side conns to the local echo
		Remote:   fmt.Sprintf("127.0.0.1:%d", remotePort), // server-side listen (explicit loopback)
		SSH:      "u@" + srv.Addr(),
		Identity: idPath,
		User:     "u",
		Host:     "127.0.0.1",
		Port:     srv.Port,
	}
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTuber(ctx, cfg, def, slog.Default(), nil)
	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tun.Stop()

	if !waitForState(tun, Connected, 5*time.Second) {
		s := tun.Status()
		t.Fatalf("did not reach Connected: state=%s err=%q", s.State, s.Error)
	}

	ping := func(label string) {
		t.Helper()
		// Dial the SERVER-SIDE port, modelling a client connecting on the host.
		conn, err := net.Dial("tcp", remoteBind)
		if err != nil {
			t.Fatalf("%s: dial server-side port: %v", label, err)
		}
		defer conn.Close()
		msg := []byte("hello-" + label)
		if _, err := conn.Write(msg); err != nil {
			t.Fatalf("%s: write: %v", label, err)
		}
		buf := make([]byte, len(msg))
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Fatalf("%s: read: %v", label, err)
		}
		if !bytes.Equal(buf, msg) {
			t.Errorf("%s: echo %q, want %q", label, buf, msg)
		}
	}

	ping("first")

	// Kill the SSH server and confirm the remote listener is re-bound after
	// the tuber self-heals.
	srv.Stop()
	if !waitForNotState(tun, Connected, 5*time.Second) {
		t.Fatal("tuber stayed Connected after server kill")
	}
	srv.Restart()
	if !waitForState(tun, Connected, 15*time.Second) {
		s := tun.Status()
		t.Fatalf("did not reconnect after server restart: state=%s err=%q", s.State, s.Error)
	}

	ping("after-reconnect")

	// Disable must tear the remote listener down: the server-side port closes
	// (async over SSH — poll until it's down).
	if err := tun.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !waitForPortDown(remoteBind, 5*time.Second) {
		t.Error("server-side port still reachable after Stop")
	}
}

// socks5Dial performs a minimal SOCKS5 no-auth CONNECT handshake against proxy
// and returns the established connection tubered to dst (IPv4 host:port). It
// avoids pulling in a SOCKS5 client dependency just for this test.
func socks5Dial(t *testing.T, proxy, dst string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", proxy)
	if err != nil {
		t.Fatalf("socks5: dial proxy %s: %v", proxy, err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Greeting: ver=5, 1 method offered, no-auth (0x00).
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("socks5: write greeting: %v", err)
	}
	gr := make([]byte, 2)
	if _, err := io.ReadFull(conn, gr); err != nil {
		t.Fatalf("socks5: read greeting resp: %v", err)
	}
	if gr[0] != 0x05 || gr[1] != 0x00 {
		t.Fatalf("socks5: greeting resp = %x, want method 00 (no-auth)", gr)
	}

	host, portStr, err := net.SplitHostPort(dst)
	if err != nil {
		t.Fatalf("socks5: bad dst %q: %v", dst, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("socks5: bad port in %q: %v", dst, err)
	}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		t.Fatalf("socks5: dst %q is not IPv4", dst)
	}
	// CONNECT: ver=5, cmd=1, rsv=0, atyp=1(ipv4), addr(4), port(2).
	req := []byte{0x05, 0x01, 0x00, 0x01, ip[0], ip[1], ip[2], ip[3], byte(port >> 8), byte(port)}
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("socks5: write connect: %v", err)
	}
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		t.Fatalf("socks5: read reply hdr: %v", err)
	}
	if hdr[0] != 0x05 || hdr[1] != 0x00 {
		t.Fatalf("socks5: connect reply = %x%x..., want rep=00 (succeeded)", hdr[:2], "")
	}
	// Consume the bound addr+port according to atyp.
	switch hdr[3] {
	case 0x01:
		_, _ = io.ReadFull(conn, make([]byte, 4+2))
	case 0x04:
		_, _ = io.ReadFull(conn, make([]byte, 16+2))
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			t.Fatalf("socks5: read bnd domain len: %v", err)
		}
		_, _ = io.ReadFull(conn, make([]byte, int(l[0])+2))
	}
	_ = conn.SetDeadline(time.Time{})
	return conn
}

// TestTuberDynamicTrafficAndReconnect exercises a type=dynamic (-D) tuber end
// to end: a SOCKS5 proxy on local whose per-connection dial is routed through
// the SSH client. A hand-rolled SOCKS5 client CONNECTs to an echo server via
// the proxy; the connection is re-established after an sshd drop/restart.
func TestTuberDynamicTrafficAndReconnect(t *testing.T) {
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

	cfg := config.Tuber{
		Name:     "d-test",
		Type:     "dynamic",
		Local:    strconv.Itoa(localPort),
		SSH:      "u@" + srv.Addr(),
		Identity: idPath,
		User:     "u",
		Host:     "127.0.0.1",
		Port:     srv.Port,
		// no Remote: each SOCKS request carries its own destination.
	}
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTuber(ctx, cfg, def, slog.Default(), nil)
	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tun.Stop()

	if !waitForState(tun, Connected, 5*time.Second) {
		s := tun.Status()
		t.Fatalf("did not reach Connected: state=%s err=%q", s.State, s.Error)
	}

	ping := func(label string) {
		t.Helper()
		conn := socks5Dial(t, localAddr, echoAddr)
		defer conn.Close()
		msg := []byte("hello-" + label)
		if _, err := conn.Write(msg); err != nil {
			t.Fatalf("%s: write: %v", label, err)
		}
		buf := make([]byte, len(msg))
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Fatalf("%s: read: %v", label, err)
		}
		if !bytes.Equal(buf, msg) {
			t.Errorf("%s: echo %q, want %q", label, buf, msg)
		}
	}

	ping("first")

	// Kill the SSH server and confirm the SOCKS proxy self-heals.
	srv.Stop()
	if !waitForNotState(tun, Connected, 5*time.Second) {
		t.Fatal("tuber stayed Connected after server kill")
	}
	srv.Restart()
	if !waitForState(tun, Connected, 15*time.Second) {
		s := tun.Status()
		t.Fatalf("did not reconnect after server restart: state=%s err=%q", s.State, s.Error)
	}

	ping("after-reconnect")

	// Stop must close the local SOCKS port.
	if err := tun.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	c, err := net.Dial("tcp", localAddr)
	if err == nil {
		c.Close()
		t.Error("local port still open after Stop")
	}
}
