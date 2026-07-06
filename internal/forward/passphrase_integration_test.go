package forward

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/secret"
	"github.com/kipkaev55/portato/internal/sshtest"
	"golang.org/x/crypto/ssh"
)

// writePassphraseIdentity generates an ed25519 keypair, writes the PRIVATE key
// passphrase-protected to a file, and returns (keyPath, passphrase, pub). The
// public key is what the test SSH server authorizes.
func writePassphraseIdentity(t *testing.T, passphrase string) (string, ssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen client key: %v", err)
	}
	authorizedKey, _ := ssh.NewPublicKey(pub)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	if err != nil {
		t.Fatalf("marshal passphrase priv: %v", err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(p, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	return p, authorizedKey
}

// waitForPendingPassphrase polls until the tunnel reports a pending passphrase
// need (the dial is blocked in provider.Wait), or times out.
func waitForPendingPassphrase(t *testing.T, tun *Tunnel, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if tun.Status().PendingPassphrase != "" {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestPassphraseIdentity_ConnectsNoAgent exercises the Phase 19 headline DoD:
// a passphrase-protected identity connects with NO ssh-agent, the passphrase
// supplied by the secret store. This is the full dial path — loadIdentityWith
// Passphrase -> ParsePrivateKeyWithPassphrase -> SSH handshake — not just the
// signer-construction unit tests.
func TestPassphraseIdentity_ConnectsNoAgent(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "") // no agent: only the identity path is used

	idPath, authorizedKey := writePassphraseIdentity(t, "hunter2")
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	srv := sshtest.NewSSHD(t, authorizedKey)
	srv.Start()
	defer srv.Stop()

	store := secret.NewStore(secret.NewMemBackend(), func() bool { return false })
	if err := store.Set(idPath, "hunter2"); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	cfg := config.Tunnel{
		Name: "pp", Type: "local", Local: "0", Remote: "127.0.0.1:1",
		SSH: "u@" + srv.Addr(), Identity: idPath,
		User: "u", Host: "127.0.0.1", Port: srv.Port,
	}
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTunnel(ctx, cfg, def, slog.Default(), store)
	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tun.Stop()

	if !waitForState(tun, Connected, 5*time.Second) {
		s := tun.Status()
		t.Fatalf("did not reach Connected with a passphrase key + no agent: state=%s err=%q", s.State, s.Error)
	}
}

// TestPassphraseIdentity_BlocksThenProvided asserts the blocking-dial path
// against a real handshake: with no passphrase available the dial blocks and
// surfaces PendingPassphrase; providing it via the store unblocks the dial and
// the tunnel connects (no agent).
func TestPassphraseIdentity_BlocksThenProvided(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	idPath, authorizedKey := writePassphraseIdentity(t, "secret")
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	srv := sshtest.NewSSHD(t, authorizedKey)
	srv.Start()
	defer srv.Stop()

	store := secret.NewStore(secret.NewMemBackend(), func() bool { return false }) // empty: dial must block

	cfg := config.Tunnel{
		Name: "pp", Type: "local", Local: "0", Remote: "127.0.0.1:1",
		SSH: "u@" + srv.Addr(), Identity: idPath,
		User: "u", Host: "127.0.0.1", Port: srv.Port,
	}
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTunnel(ctx, cfg, def, slog.Default(), store)
	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tun.Stop()

	if !waitForPendingPassphrase(t, tun, 5*time.Second) {
		s := tun.Status()
		t.Fatalf("dial should block and surface PendingPassphrase; state=%s err=%q pp=%q",
			s.State, s.Error, s.PendingPassphrase)
	}
	if got := tun.Status().PendingPassphrase; got != idPath {
		t.Errorf("PendingPassphrase = %q, want the identity path %q", got, idPath)
	}

	// Provide the passphrase: the blocked dial wakes and connects.
	if err := store.Set(idPath, "secret"); err != nil {
		t.Fatalf("provide passphrase: %v", err)
	}
	if !waitForState(tun, Connected, 5*time.Second) {
		s := tun.Status()
		t.Fatalf("did not reach Connected after providing the passphrase: state=%s err=%q pp=%q",
			s.State, s.Error, s.PendingPassphrase)
	}
	if tun.Status().PendingPassphrase != "" {
		t.Errorf("PendingPassphrase should clear once accepted; got %q", tun.Status().PendingPassphrase)
	}
}

// TestPassphraseIdentity_WrongThenRight asserts a wrong passphrase is rejected
// (the dial re-prompts) and the correct one then connects.
func TestPassphraseIdentity_WrongThenRight(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	idPath, authorizedKey := writePassphraseIdentity(t, "correct")
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	srv := sshtest.NewSSHD(t, authorizedKey)
	srv.Start()
	defer srv.Stop()

	store := secret.NewStore(secret.NewMemBackend(), func() bool { return false })

	cfg := config.Tunnel{
		Name: "pp", Type: "local", Local: "0", Remote: fmt.Sprintf("127.0.0.1:%d", srv.Port),
		SSH: "u@" + srv.Addr(), Identity: idPath,
		User: "u", Host: "127.0.0.1", Port: srv.Port,
	}
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tun := NewTunnel(ctx, cfg, def, slog.Default(), store)
	if err := tun.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tun.Stop()

	if !waitForPendingPassphrase(t, tun, 5*time.Second) {
		t.Fatal("dial should block awaiting a passphrase")
	}
	// Wrong passphrase: the dial rejects it, invalidates it, and re-blocks.
	if err := store.Set(idPath, "wrong"); err != nil {
		t.Fatal(err)
	}
	// After a wrong value the dial Deletes it and loops back to Wait, so the
	// pending need re-appears.
	if !waitForPendingPassphrase(t, tun, 5*time.Second) {
		t.Fatal("dial should re-block after a wrong passphrase")
	}
	// Correct passphrase: connects.
	if err := store.Set(idPath, "correct"); err != nil {
		t.Fatal(err)
	}
	if !waitForState(tun, Connected, 5*time.Second) {
		s := tun.Status()
		t.Fatalf("did not connect after the correct passphrase: state=%s err=%q", s.State, s.Error)
	}
}
