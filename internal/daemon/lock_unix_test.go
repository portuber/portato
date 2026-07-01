//go:build unix

package daemon

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestAcquireInstanceLock_SecondAcquireLoses: with the lock held, a second
// acquire returns ErrAlreadyRunning. flock is associated with the open file
// description, so two separate opens (even in one process) conflict. Releasing
// the first frees the lock — it is not left wedged after close.
func TestAcquireInstanceLock_SecondAcquireLoses(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "daemon.lock") // parent dir is auto-created

	first, err := acquireInstanceLock(p)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	if _, err := acquireInstanceLock(p); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second acquire: want ErrAlreadyRunning, got %v", err)
	}

	// Releasing the first frees the lock; a fresh acquire must succeed and not
	// be rejected as stale.
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	again, err := acquireInstanceLock(p)
	if err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
	_ = again.Close()
}

// TestAcquireInstanceLock_CreatesLockFile: the lock file is created 0600 (with
// its parent dir) even when neither existed.
func TestAcquireInstanceLock_CreatesLockFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nested", "daemon.lock")
	f, err := acquireInstanceLock(p)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	_ = f.Close()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat lock: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("lock perm = %o, want 0600", perm)
	}
}

// TestNew_SecondStartExitAlreadyRunning drives the production path: the first
// New acquires the flock and the second New (same isolated lock path) is
// rejected with ErrAlreadyRunning without needing a real second process.
func TestNew_SecondStartExitAlreadyRunning(t *testing.T) {
	withIsolatedDiscovery(t)
	dir := shortDir(t)
	sock := filepath.Join(dir, "portato.sock")
	SetSocketOverride(sock)
	t.Cleanup(func() { SetSocketOverride("") })

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig()
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	first, err := New(cfg, cfgPath, slog.Default(), nil)
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	t.Cleanup(func() { _ = first.Shutdown() })

	if _, err := New(cfg, cfgPath, slog.Default(), nil); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second New: want ErrAlreadyRunning, got %v", err)
	}
}
