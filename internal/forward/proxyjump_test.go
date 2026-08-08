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
)

// setupClientKey generates an ed25519 client key, writes it to a temp identity
// file, and returns the path + a fresh empty known_hosts path + the authorized
// public key. Shared by the ProxyJump dial tests so every hop authorizes the
// one key (the shared-identity model Phase 43 starts with).
func setupClientKey(t *testing.T) (idPath, knownHosts string, authorizedKey ssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen client key: %v", err)
	}
	authorizedKey, _ = ssh.NewPublicKey(pub)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal client priv: %v", err)
	}
	dir := t.TempDir()
	idPath = filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(idPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	knownHosts = filepath.Join(dir, "known_hosts")
	return idPath, knownHosts, authorizedKey
}

// waitForConnsZero polls srv until its open-connection count reaches 0 or the
// timeout elapses. It is the ProxyJump leak guard: the leash goroutine must
// close the intermediate ssh.Clients once the final client disconnects, so each
// intermediate server's ActiveConns() drives back to 0. Returns whether it did.
func waitForConnsZero(t *testing.T, srv *sshtest.SSHD, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if srv.ActiveConns() == 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return srv.ActiveConns() == 0
}

// dialChainDials builds the key auth methods and dials cfg via dialConn (the
// Phase 43 entry that all real dial paths use). It returns the final client +
// a close func for the agent. Used by the chain tests so they exercise the same
// code path as a live tuber.
func dialChainDials(t *testing.T, ctx context.Context, cfg config.Tuber, def config.Defaults) (*ssh.Client, func() error) {
	t.Helper()
	auths, closeAgent := authMethods(ctx, cfg, def, slog.Default(), nil, nil)
	if len(auths) == 0 {
		closeAgent()
		t.Fatal("no key auth methods built")
	}
	client, err := dialConn(ctx, cfg, def, slog.Default(), nil, auths, auths)
	if err != nil {
		closeAgent()
		t.Fatalf("dialConn: %v", err)
	}
	return client, closeAgent
}

// pingThrough pipes a message through an already-dialed final client to echoAddr
// (a direct-tcpip dial on the target host) and asserts it echoes back — proving
// the -L forward carries traffic end to end through the chain.
func pingThrough(t *testing.T, client *ssh.Client, echoAddr, label string) {
	t.Helper()
	conn, err := client.Dial("tcp", echoAddr)
	if err != nil {
		t.Fatalf("%s: client.Dial(echo): %v", label, err)
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

// TestDialConnTwoHop is the Phase 43 headline E2E: a -L forward through a
// single bastion (edge -> target) carries traffic, and the intermediate's
// connection is torn down once the final client closes (leash guard).
func TestDialConnTwoHop(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	echoAddr, stopEcho := startEcho(t)
	defer stopEcho()
	idPath, knownHosts, authorizedKey := setupClientKey(t)

	edge := sshtest.NewSSHD(t, authorizedKey)
	edge.Start()
	t.Cleanup(edge.Stop)
	target := sshtest.NewSSHD(t, authorizedKey)
	target.Start()
	t.Cleanup(target.Stop)

	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}
	cfg := config.Tuber{
		Name:     "two-hop",
		Type:     "local",
		Local:    strconv.Itoa(freePort(t)),
		Remote:   echoAddr,
		SSH:      "u@" + target.Addr(),
		Identity: idPath,
		User:     "u", Host: "127.0.0.1", Port: target.Port,
		Jumps: []config.Hop{{User: "u", Host: "127.0.0.1", Port: edge.Port}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, closeAgent := dialChainDials(t, ctx, cfg, def)
	defer closeAgent()

	if edge.ActiveConns() != 1 {
		t.Errorf("edge ActiveConns = %d while connected, want 1", edge.ActiveConns())
	}
	pingThrough(t, client, echoAddr, "two-hop")

	// Close the final client; both the target and the edge (intermediate)
	// connections must drop to 0 — the latter via the leash goroutine.
	if err := client.Close(); err != nil {
		t.Fatalf("final Close: %v", err)
	}
	if !waitForConnsZero(t, target, 3*time.Second) {
		t.Errorf("target ActiveConns = %d after final close, want 0 (leak)", target.ActiveConns())
	}
	if !waitForConnsZero(t, edge, 3*time.Second) {
		t.Errorf("edge ActiveConns = %d after final close, want 0 (leash leaked the intermediate)", edge.ActiveConns())
	}
}

// TestDialConnThreeHop exercises a 2-intermediate chain (jump1 -> jump2 ->
// target), satisfying the DoD "a chain of 2+ hops works" with more than one
// intermediate, and asserts BOTH intermediates are closed by the leash.
func TestDialConnThreeHop(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	echoAddr, stopEcho := startEcho(t)
	defer stopEcho()
	idPath, knownHosts, authorizedKey := setupClientKey(t)

	jump1 := sshtest.NewSSHD(t, authorizedKey)
	jump1.Start()
	t.Cleanup(jump1.Stop)
	jump2 := sshtest.NewSSHD(t, authorizedKey)
	jump2.Start()
	t.Cleanup(jump2.Stop)
	target := sshtest.NewSSHD(t, authorizedKey)
	target.Start()
	t.Cleanup(target.Stop)

	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}
	cfg := config.Tuber{
		Name:     "three-hop",
		Type:     "local",
		Local:    strconv.Itoa(freePort(t)),
		Remote:   echoAddr,
		SSH:      "u@" + target.Addr(),
		Identity: idPath,
		User:     "u", Host: "127.0.0.1", Port: target.Port,
		Jumps: []config.Hop{
			{User: "u", Host: "127.0.0.1", Port: jump1.Port},
			{User: "u", Host: "127.0.0.1", Port: jump2.Port},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, closeAgent := dialChainDials(t, ctx, cfg, def)
	defer closeAgent()

	pingThrough(t, client, echoAddr, "three-hop")

	if err := client.Close(); err != nil {
		t.Fatalf("final Close: %v", err)
	}
	for _, srv := range []*sshtest.SSHD{target, jump2, jump1} {
		if !waitForConnsZero(t, srv, 3*time.Second) {
			t.Errorf("%p ActiveConns = %d after final close, want 0 (leash leaked an intermediate)", srv, srv.ActiveConns())
		}
	}
}

// TestDialConnNoJumpIsSingleHop is the zero-behaviour-change guard: a tuber
// with no Jumps takes dialConn's single-hop fast path (delegating to dialOnce),
// dials the host directly, carries traffic, and tears down with no
// intermediate/leash involvement.
func TestDialConnNoJumpIsSingleHop(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	echoAddr, stopEcho := startEcho(t)
	defer stopEcho()
	idPath, knownHosts, authorizedKey := setupClientKey(t)

	srv := sshtest.NewSSHD(t, authorizedKey)
	srv.Start()
	t.Cleanup(srv.Stop)

	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}
	cfg := config.Tuber{
		Name:     "no-jump",
		Type:     "local",
		Local:    strconv.Itoa(freePort(t)),
		Remote:   echoAddr,
		SSH:      "u@" + srv.Addr(),
		Identity: idPath,
		User:     "u", Host: "127.0.0.1", Port: srv.Port,
		// Jumps intentionally nil -> single-hop fast path.
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, closeAgent := dialChainDials(t, ctx, cfg, def)
	defer closeAgent()

	if srv.ActiveConns() != 1 {
		t.Errorf("ActiveConns = %d while connected, want 1", srv.ActiveConns())
	}
	pingThrough(t, client, echoAddr, "no-jump")

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !waitForConnsZero(t, srv, 3*time.Second) {
		t.Errorf("ActiveConns = %d after close, want 0 (leak)", srv.ActiveConns())
	}
}

// TestDialConnJumpBastionAuthFails confirms the documented limitation: a jump
// tuber whose bastion does NOT accept the shared key fails cleanly (no chain of
// password prompts), and the partially-built chain is torn down so nothing
// leaks.
func TestDialConnJumpBastionAuthFails(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	echoAddr, stopEcho := startEcho(t)
	defer stopEcho()
	idPath, knownHosts, clientKey := setupClientKey(t)

	// edge authorizes a DIFFERENT key -> the shared key is rejected at the
	// bastion (the first hop), so the chain never reaches the target.
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen other key: %v", err)
	}
	edgeAuthorized, _ := ssh.NewPublicKey(otherPub)
	edge := sshtest.NewSSHD(t, edgeAuthorized)
	edge.Start()
	t.Cleanup(edge.Stop)
	target := sshtest.NewSSHD(t, clientKey)
	target.Start()
	t.Cleanup(target.Stop)

	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}
	cfg := config.Tuber{
		Name:     "bastion-fail",
		Type:     "local",
		Local:    strconv.Itoa(freePort(t)),
		Remote:   echoAddr,
		SSH:      "u@" + target.Addr(),
		Identity: idPath,
		User:     "u", Host: "127.0.0.1", Port: target.Port,
		Jumps: []config.Hop{{User: "u", Host: "127.0.0.1", Port: edge.Port}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	auths, closeAgent := authMethods(ctx, cfg, def, slog.Default(), nil, nil)
	defer closeAgent()
	_, derr := dialConn(ctx, cfg, def, slog.Default(), nil, auths, auths)
	if derr == nil {
		t.Fatal("dialConn expected an auth failure at the bastion, got nil")
	}
	// The first hop failed at the handshake, so edge must have 0 open conns
	// (dialConn closed the partial chain) and target must never have been
	// reached.
	if !waitForConnsZero(t, edge, time.Second) {
		t.Errorf("edge ActiveConns = %d after bastion auth failure, want 0", edge.ActiveConns())
	}
	if target.ActiveConns() != 0 {
		t.Errorf("target ActiveConns = %d; the chain should never have reached it", target.ActiveConns())
	}
}

// TestTuberJumpEndToEnd runs a full Tuber (the reconnect loop + accept loop)
// over a two-hop chain, proving the integrated path — not just dialConn —
// reaches Connected and forwards a -L connection end to end through a bastion.
func TestTuberJumpEndToEnd(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	echoAddr, stopEcho := startEcho(t)
	defer stopEcho()
	idPath, knownHosts, authorizedKey := setupClientKey(t)

	edge := sshtest.NewSSHD(t, authorizedKey)
	edge.Start()
	t.Cleanup(edge.Stop)
	target := sshtest.NewSSHD(t, authorizedKey)
	target.Start()
	t.Cleanup(target.Stop)

	localPort := freePort(t)
	localAddr := fmt.Sprintf("127.0.0.1:%d", localPort)
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}
	cfg := config.Tuber{
		Name:     "jump-e2e",
		Type:     "local",
		Local:    strconv.Itoa(localPort),
		Remote:   echoAddr,
		SSH:      "u@" + target.Addr(),
		Identity: idPath,
		User:     "u", Host: "127.0.0.1", Port: target.Port,
		Jumps: []config.Hop{{User: "u", Host: "127.0.0.1", Port: edge.Port}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTuber(ctx, cfg, def, slog.Default(), nil, nil)
	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tun.Stop()

	if !waitForState(tun, Connected, 5*time.Second) {
		s := tun.Status()
		t.Fatalf("did not reach Connected through the bastion: state=%s err=%q", s.State, s.Error)
	}

	conn, err := net.Dial("tcp", localAddr)
	if err != nil {
		t.Fatalf("dial local: %v", err)
	}
	defer conn.Close()
	msg := []byte("via-bastion")
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
