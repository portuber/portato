package forward

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// genPassphraseKey writes a passphrase-protected OpenSSH ed25519 key to a temp
// file and returns its path. Such a key makes ssh.ParsePrivateKey return a
// *ssh.PassphraseMissingError, the condition loadIdentityWithPassphrase detects.
func genPassphraseKey(t *testing.T, passphrase string) string {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 GenerateKey: %v", err)
	}
	_ = pub
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "test", []byte(passphrase))
	if err != nil {
		t.Fatalf("MarshalPrivateKeyWithPassphrase: %v", err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(p, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return p
}

// fakeProvider is a controllable PassphraseProvider for the dial retry tests.
// Wait blocks until value is set (via a channel), mirroring secret.Store.
type fakeProvider struct {
	getv   string
	getok  bool
	waitCh chan string // a value sent here unblocks Wait
	del    func()
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{waitCh: make(chan string, 1)}
}
func (f *fakeProvider) Get(string) (string, bool) { return f.getv, f.getok }
func (f *fakeProvider) Wait(ctx context.Context, _ string) (string, bool) {
	select {
	case v := <-f.waitCh:
		f.getv, f.getok = v, true
		return v, true
	case <-ctx.Done():
		return "", false
	}
}
func (f *fakeProvider) Delete(string) error {
	f.getv, f.getok = "", false
	if f.del != nil {
		f.del()
	}
	return nil
}

// TestLoadIdentity_PlainKey covers the no-passphrase path: a plain key returns a
// signer immediately, provider is never consulted.
func TestLoadIdentity_PlainKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "id_plain")
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = pub
	block, err := ssh.MarshalPrivateKey(priv, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	var calls int32
	provider := newFakeProvider()
	sink := func(string) { atomic.AddInt32(&calls, 1) }

	signer, err := loadIdentityWithPassphrase(context.Background(), p, provider, sink)
	if err != nil {
		t.Fatalf("plain key load: %v", err)
	}
	if signer == nil {
		t.Fatal("nil signer for plain key")
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Error("sink must not fire for a plain key")
	}
}

// TestLoadIdentity_NilProviderDegrades asserts that a passphrase-protected key
// with provider==nil yields a parse error (today's behaviour), not a block.
func TestLoadIdentity_NilProviderDegrades(t *testing.T) {
	p := genPassphraseKey(t, "secret")
	_, err := loadIdentityWithPassphrase(context.Background(), p, nil, nil)
	if err == nil {
		t.Fatal("expected a parse error for a passphrase key with nil provider")
	}
	// The wrapped error must carry the underlying PassphraseMissingError so the
	// caller sees the real reason (not a generic "parse identity" shadow).
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("error should wrap *ssh.PassphraseMissingError; got %v", err)
	}
}

// TestLoadIdentity_BlocksThenAccepts asserts the blocking path: a passphrase key
// with no cached value surfaces the need (sink), blocks in Wait, and loads once
// the correct passphrase arrives.
func TestLoadIdentity_BlocksThenAccepts(t *testing.T) {
	p := genPassphraseKey(t, "hunter2")
	provider := newFakeProvider() // getok=false → Wait

	var sawNeed atomic.Int32
	sink := func(path string) {
		if path == p {
			sawNeed.Add(1)
		}
	}

	done := make(chan error, 1)
	var signer ssh.Signer
	go func() {
		s, err := loadIdentityWithPassphrase(context.Background(), p, provider, sink)
		signer = s
		done <- err
	}()

	// Give the goroutine time to reach Wait.
	provider.waitCh <- "hunter2"
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected success after passphrase; got %v", err)
		}
		if signer == nil {
			t.Fatal("nil signer after correct passphrase")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("loadIdentity did not return after the passphrase was provided")
	}
	if sawNeed.Load() < 1 {
		t.Error("sink should have surfaced the passphrase need before blocking")
	}
}

// TestLoadIdentity_WrongPassphraseRetries asserts a wrong passphrase is rejected
// (Delete invalidates it) and a subsequent correct one is accepted.
func TestLoadIdentity_WrongPassphraseRetries(t *testing.T) {
	p := genPassphraseKey(t, "correct")
	provider := newFakeProvider()

	var deleted atomic.Int32
	provider.del = func() { deleted.Add(1) }

	done := make(chan error, 1)
	go func() {
		_, err := loadIdentityWithPassphrase(context.Background(), p, provider, func(string) {})
		done <- err
	}()

	provider.waitCh <- "wrong"   // first attempt: wrong
	provider.waitCh <- "correct" // second attempt: correct
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected success after retry; got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not succeed after the correct passphrase")
	}
	if deleted.Load() < 1 {
		t.Error("a wrong passphrase should have triggered provider.Delete")
	}
}

// TestLoadIdentity_CtxCancelAbortsWait asserts that cancelling the context
// unblocks Wait (the tuber disable/shutdown path).
func TestLoadIdentity_CtxCancelAbortsWait(t *testing.T) {
	p := genPassphraseKey(t, "secret")
	provider := newFakeProvider()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := loadIdentityWithPassphrase(ctx, p, provider, func(string) {})
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled; got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("loadIdentity did not return after ctx cancel")
	}
}
