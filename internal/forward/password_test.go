package forward

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/sshtest"
	"golang.org/x/crypto/ssh"
)

// fakePasswordProvider is a controllable PasswordProvider for the password dial
// tests, mirroring fakeProvider. Wait blocks until a value is sent on waitCh.
type fakePasswordProvider struct {
	getv   string
	getok  bool
	waitCh chan string // a value sent here unblocks Wait
	del    func()

	gets  atomic.Int32
	waits atomic.Int32
}

func newFakePasswordProvider() *fakePasswordProvider {
	return &fakePasswordProvider{waitCh: make(chan string, 4)}
}
func (f *fakePasswordProvider) Get(string) (string, bool) {
	f.gets.Add(1)
	return f.getv, f.getok
}
func (f *fakePasswordProvider) Wait(ctx context.Context, _ string) (string, bool) {
	f.waits.Add(1)
	select {
	case v := <-f.waitCh:
		f.getv, f.getok = v, true
		return v, true
	case <-ctx.Done():
		return "", false
	}
}
func (f *fakePasswordProvider) Delete(string) error {
	f.getv, f.getok = "", false
	if f.del != nil {
		f.del()
	}
	return nil
}

// passwordTestSetup starts srv and returns a cfg/def pointing at it with
// password_auth on and a per-test known_hosts file (accept_new_hosts so the
// host-key check passes without pre-seeding). dir is a fresh temp dir.
func passwordTestSetup(t *testing.T, srv *sshtest.SSHD, dir string) (config.Tuber, config.Defaults) {
	t.Helper()
	knownHosts := filepath.Join(dir, "known_hosts")
	def := config.Defaults{KnownHosts: knownHosts, AcceptNewHosts: true}
	cfg := config.Tuber{
		Name: "pw-test",
		Type: "local",
		SSH:  "u@" + srv.Addr(),
		User: "u",
		Host: "127.0.0.1",
		Port: srv.Port,
	}
	return cfg, def
}

// TestDialPassword_Success dials a password-only server: the provider supplies
// the correct password, the dial succeeds, and the pending need is cleared.
func TestDialPassword_Success(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "") // hermetic: no agent
	srv := sshtest.NewSSHDPassword(t, "secret")
	srv.Start()
	defer srv.Stop()

	cfg, def := passwordTestSetup(t, srv, t.TempDir())
	provider := newFakePasswordProvider() // getok=false -> Wait

	var surfaced atomic.Bool
	sink := func(account string) {
		if account != "" {
			surfaced.Store(true)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	var client *ssh.Client
	go func() {
		c, err := dialWithPasswordPrompt(ctx, cfg, def, slog.Default(), nil, nil, nil, provider, sink)
		client = c
		done <- err
	}()

	provider.waitCh <- "secret"
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected success; got %v", err)
		}
		if client == nil {
			t.Fatal("nil client on success")
		}
		_ = client.Close()
	case <-time.After(15 * time.Second):
		t.Fatal("dial did not return after the password was provided")
	}
	if !surfaced.Load() {
		t.Error("sink should have surfaced the password need before blocking")
	}
}

// TestDialPassword_WrongThenCorrect asserts the re-prompt loop: a wrong password
// is rejected (provider.Delete invalidates it) and the correct one succeeds on
// the next dial, with no backoff between attempts.
func TestDialPassword_WrongThenCorrect(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	srv := sshtest.NewSSHDPassword(t, "correct")
	srv.Start()
	defer srv.Stop()

	cfg, def := passwordTestSetup(t, srv, t.TempDir())
	provider := newFakePasswordProvider()

	var deleted atomic.Int32
	provider.del = func() { deleted.Add(1) }

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := dialWithPasswordPrompt(ctx, cfg, def, slog.Default(), nil, nil, nil, provider, func(string) {})
		done <- err
	}()

	provider.waitCh <- "wrong"   // first attempt: wrong -> Delete + re-prompt
	provider.waitCh <- "correct" // second attempt: correct -> success
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected success after retry; got %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("did not succeed after the correct password")
	}
	if deleted.Load() < 1 {
		t.Error("a wrong password should have triggered provider.Delete")
	}
}

// TestDialPassword_KeyPreferred asserts that a working key authenticates and the
// password provider is NEVER consulted (no prompt). The server accepts both a
// key and a password; the client has the matching identity.
func TestDialPassword_KeyPreferred(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen client key: %v", err)
	}
	authorizedKey, _ := ssh.NewPublicKey(pub)
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal priv: %v", err)
	}
	dir := t.TempDir()
	idPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(idPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}

	srv := sshtest.NewSSHDKeyAndPassword(t, authorizedKey, "secret")
	srv.Start()
	defer srv.Stop()

	cfg, def := passwordTestSetup(t, srv, dir)
	cfg.Identity = idPath // a working key -> must short-circuit before any prompt

	provider := newFakePasswordProvider()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := dialWithPasswordPrompt(ctx, cfg, def, slog.Default(), nil, nil, nil, provider, func(string) {})
	if err != nil {
		t.Fatalf("expected key auth to succeed; got %v", err)
	}
	_ = client.Close()
	if provider.gets.Load()+provider.waits.Load() != 0 {
		t.Errorf("password provider must not be consulted when a key works (gets=%d waits=%d)",
			provider.gets.Load(), provider.waits.Load())
	}
}

// TestDialPassword_ServerHasNoPasswordMethod asserts the bail-out: against a
// key-only server (no password method offered), a password dial fails and the
// loop returns a clear error instead of re-prompting forever. This empirically
// validates passwordAuthAvailable's parsing of the "remain [<methods>]" list.
func TestDialPassword_ServerHasNoPasswordMethod(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	// A key-only server that authorizes a different (unused) key.
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	authorizedKey, _ := ssh.NewPublicKey(pub)
	srv := sshtest.NewSSHD(t, authorizedKey)
	srv.Start()
	defer srv.Stop()

	cfg, def := passwordTestSetup(t, srv, t.TempDir())
	// No identity, no agent -> no key methods -> straight to the password loop.
	provider := newFakePasswordProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := dialWithPasswordPrompt(ctx, cfg, def, slog.Default(), nil, nil, nil, provider, func(string) {})
		done <- err
	}()

	provider.waitCh <- "bogus" // the only password offered; must not loop back
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a failure against a key-only server")
		}
		if !strings.Contains(err.Error(), "does not offer password authentication") {
			t.Fatalf("expected a 'does not offer password authentication' error; got %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("dial hung: the loop did not bail against a key-only server (passwordAuthAvailable parsing is wrong)")
	}
	if provider.waits.Load() > 1 {
		t.Errorf("should not re-prompt once the server rejects the password method; waits=%d", provider.waits.Load())
	}
}

// TestDialPassword_CtxCancelAbortsWait asserts that cancelling the context
// unblocks Wait (the disable/shutdown path).
func TestDialPassword_CtxCancelAbortsWait(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	srv := sshtest.NewSSHDPassword(t, "secret")
	srv.Start()
	defer srv.Stop()

	cfg, def := passwordTestSetup(t, srv, t.TempDir())
	provider := newFakePasswordProvider() // never sent a value -> blocks in Wait
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := dialWithPasswordPrompt(ctx, cfg, def, slog.Default(), nil, nil, nil, provider, func(string) {})
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled; got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dial did not return after ctx cancel")
	}
}

// TestIsAuthFailed covers the auth-failure classifier used by the loop. The
// real x/crypto/ssh auth-failure messages contain "unable to authenticate".
func TestIsAuthFailed(t *testing.T) {
	cases := []struct {
		msg    string
		expect bool
	}{
		{"ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password], no supported methods remain", true},
		{"auth failed: ssh: handshake failed: ssh: unable to authenticate, attempted methods [none], no supported methods remain", true},
		{"connect refused: dial tcp: connection refused", false},
		{"connect timeout: i/o timeout", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isAuthFailed(fmt.Errorf("%s", tc.msg)); got != tc.expect {
			t.Errorf("isAuthFailed(%q) = %v, want %v", tc.msg, got, tc.expect)
		}
	}
}

// TestPasswordAuthAvailable covers the "is password still offered" classifier,
// using the real attempted-methods strings observed from x/crypto/ssh@v0.53.0.
func TestPasswordAuthAvailable(t *testing.T) {
	cases := []struct {
		msg    string
		expect bool
	}{
		// Wrong password on a password-capable server: password was attempted.
		{"unable to authenticate, attempted methods [none password], no supported methods remain", true},
		// Key-only server: password was never offered/attempted.
		{"unable to authenticate, attempted methods [none], no supported methods remain", false},
		// No attempted-methods list -> ambiguous -> keep re-prompting.
		{"some other error", true},
	}
	for _, tc := range cases {
		if got := passwordAuthAvailable(fmt.Errorf("%s", tc.msg)); got != tc.expect {
			t.Errorf("passwordAuthAvailable(%q) = %v, want %v", tc.msg, got, tc.expect)
		}
	}
}

// pbool returns a pointer to b (Phase 35 password_auth is a *bool opt-out).
func pbool(b bool) *bool { return &b }

// TestDialSSH_AutoPromptsByDefault asserts the dispatcher routes to the password
// loop with NO password_auth set (the on-by-default behaviour): a no-key tuber
// against a password-only server connects once the password is supplied.
func TestDialSSH_AutoPromptsByDefault(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	srv := sshtest.NewSSHDPassword(t, "secret")
	srv.Start()
	defer srv.Stop()
	cfg, def := passwordTestSetup(t, srv, t.TempDir())
	// cfg.PasswordAuth left nil → on by default.
	if !cfg.ResolvedPasswordAuth(def) {
		t.Fatal("password auth should be on by default when unset")
	}
	provider := newFakePasswordProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	var client *ssh.Client
	go func() {
		c, err := dialSSH(ctx, cfg, def, slog.Default(), nil, nil, nil, provider, func(string) {})
		client = c
		done <- err
	}()
	provider.waitCh <- "secret"
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("auto-on default should connect; got %v", err)
		}
		_ = client.Close()
	case <-time.After(15 * time.Second):
		t.Fatal("dialSSH did not connect via the password loop on the default")
	}
}

// TestDialSSH_OptOutIsKeyOnly asserts an explicit password_auth: false keeps the
// tuber key-only (no password prompt): the provider is never consulted and the
// dial fails with the standard "no ssh auth method" error rather than blocking.
func TestDialSSH_OptOutIsKeyOnly(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	srv := sshtest.NewSSHDPassword(t, "secret")
	srv.Start()
	defer srv.Stop()
	cfg, def := passwordTestSetup(t, srv, t.TempDir())
	cfg.PasswordAuth = pbool(false) // opt out → key-only
	if cfg.ResolvedPasswordAuth(def) {
		t.Fatal("password auth should be off when explicitly false")
	}
	provider := newFakePasswordProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := dialSSH(ctx, cfg, def, slog.Default(), nil, nil, nil, provider, func(string) {})
	if err == nil {
		t.Fatal("expected a key-only failure (no key, password opted out)")
	}
	if !strings.Contains(err.Error(), "no ssh auth method available") {
		t.Fatalf("expected the key-only 'no ssh auth method' error; got %v", err)
	}
	if provider.gets.Load()+provider.waits.Load() != 0 {
		t.Errorf("opt-out must not consult the password provider (gets=%d waits=%d)",
			provider.gets.Load(), provider.waits.Load())
	}
}
