package forward

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
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

type sshd struct {
	t          *testing.T
	cfg        *ssh.ServerConfig
	port       int
	listener   net.Listener
	tracker    *connTracker
	ed25519Pub ssh.PublicKey
}

// newSSHD configures a test server that offers BOTH an ECDSA and an ED25519
// host key. x/crypto/ssh's default preference negotiates ECDSA first, so a
// client that records only the ED25519 key would otherwise hit a spurious
// "host key mismatch" — the regression this setup guards against.
func newSSHD(t *testing.T, authorizedKey ssh.PublicKey) *sshd {
	t.Helper()
	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen ed25519 host key: %v", err)
	}
	edSigner, err := ssh.NewSignerFromSigner(edPriv)
	if err != nil {
		t.Fatalf("ed25519 signer: %v", err)
	}
	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen ecdsa host key: %v", err)
	}
	ecSigner, err := ssh.NewSignerFromKey(ecPriv)
	if err != nil {
		t.Fatalf("ecdsa signer: %v", err)
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
	return &sshd{t: t, cfg: cfg, tracker: &connTracker{}, ed25519Pub: edSigner.PublicKey()}
}

func (s *sshd) start() {
	s.t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		s.t.Fatalf("sshd listen: %v", err)
	}
	s.listener = ln
	s.port = ln.Addr().(*net.TCPAddr).Port
	go s.accept()
}

func (s *sshd) accept() {
	for {
		nConn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(nConn)
	}
}

func (s *sshd) addr() string {
	return fmt.Sprintf("127.0.0.1:%d", s.port)
}

func (s *sshd) stop() {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.tracker.closeAll()
}

func (s *sshd) restart() {
	s.stop()
	time.Sleep(80 * time.Millisecond)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.port))
	if err != nil {
		s.t.Fatalf("sshd restart listen: %v", err)
	}
	s.listener = ln
	go s.accept()
}

func (s *sshd) handleConn(nConn net.Conn) {
	sconn, chans, reqs, err := ssh.NewServerConn(nConn, s.cfg)
	if err != nil {
		return
	}
	s.tracker.add(sconn)
	defer func() {
		s.tracker.remove(sconn)
		_ = sconn.Close()
	}()
	go ssh.DiscardRequests(reqs)
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

func (s *sshd) serveDirect(ch ssh.Channel, creqs <-chan *ssh.Request, addr string) {
	defer ch.Close()
	go ssh.DiscardRequests(creqs)
	backend, err := net.Dial("tcp", addr)
	if err != nil {
		return
	}
	defer backend.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(ch, backend); done <- struct{}{} }()
	go func() { _, _ = io.Copy(backend, ch); done <- struct{}{} }()
	<-done
}

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

func waitForState(t *Tunnel, want State, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if t.Status().State == want {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return t.Status().State == want
}

func waitForNotState(t *Tunnel, notWant State, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if t.Status().State != notWant {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestTunnelTrafficAndReconnect(t *testing.T) {
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

	srv := newSSHD(t, authorizedKey)
	srv.start()
	defer srv.stop()

	localPort := freePort(t)
	localAddr := fmt.Sprintf("127.0.0.1:%d", localPort)

	cfg := config.Tunnel{
		Name:     "t-test",
		Type:     "local",
		Local:    strconv.Itoa(localPort),
		Remote:   echoAddr,
		SSH:      "u@" + srv.addr(),
		Identity: idPath,
		User:     "u",
		Host:     "127.0.0.1",
		Port:     srv.port,
	}
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTunnel(ctx, cfg, def, slog.Default())
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
	// the tunnel self-heals via the reconnect loop.
	srv.stop()
	if !waitForNotState(tun, Connected, 5*time.Second) {
		t.Fatal("tunnel stayed Connected after server kill")
	}

	srv.restart()
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

// TestTunnelHonoursKnownHostKeyType guards against golang/go#36126: the server
// offers both ECDSA (preferred by x/crypto's default order) and ED25519, but
// known_hosts only has the ED25519 key. The client must negotiate the key type
// it already trusts instead of bailing out with "host key mismatch".
func TestTunnelHonoursKnownHostKeyType(t *testing.T) {
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

	srv := newSSHD(t, authorizedKey)
	srv.start()
	defer srv.stop()

	// Seed known_hosts with ONLY the server's ED25519 host key (strict mode).
	knownHosts := filepath.Join(dir, "known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize(srv.addr())}, srv.ed25519Pub)
	if err := os.WriteFile(knownHosts, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("seed known_hosts: %v", err)
	}

	localPort := freePort(t)
	localAddr := fmt.Sprintf("127.0.0.1:%d", localPort)
	cfg := config.Tunnel{
		Name:     "kh-test",
		Type:     "local",
		Local:    strconv.Itoa(localPort),
		Remote:   echoAddr,
		SSH:      "u@" + srv.addr(),
		Identity: idPath,
		User:     "u",
		Host:     "127.0.0.1",
		Port:     srv.port,
	}
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: false}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTunnel(ctx, cfg, def, slog.Default())
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

// TestTunnelAuthViaAgent exercises the ssh-agent auth path end-to-end: the
// tunnel has no identity file, so authentication must come from the in-process
// agent. Guards against the "use of closed network connection" bug where the
// agent connection was closed before the lazy signers signed during the
// handshake.
func TestTunnelAuthViaAgent(t *testing.T) {
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

	srv := newSSHD(t, authorizedKey)
	srv.start()
	defer srv.stop()

	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	localPort := freePort(t)
	localAddr := fmt.Sprintf("127.0.0.1:%d", localPort)
	cfg := config.Tunnel{
		Name:   "agent-test",
		Type:   "local",
		Local:  strconv.Itoa(localPort),
		Remote: echoAddr,
		SSH:    "u@" + srv.addr(),
		User:   "u",
		Host:   "127.0.0.1",
		Port:   srv.port,
		// no Identity -> agent is the only auth source
	}
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTunnel(ctx, cfg, def, slog.Default())
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
