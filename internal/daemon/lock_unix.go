//go:build unix

package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// acquireInstanceLock takes an exclusive, non-blocking flock on path so that a
// second daemon started at the same instant (before either has written a
// discovery marker) detects the first and exits cleanly. The lock is held for
// the caller's lifetime by keeping the *os.File open; the kernel releases it
// when the process exits, so it is crash-safe — unlike the PID-file check,
// which races on simultaneous start. Returns ErrAlreadyRunning (wrapping the
// path) when another daemon holds the lock. unix-only; the non-unix stub is a
// no-op (Windows falls back to the marker/PID check until Phase 17).
func acquireInstanceLock(path string) (*os.File, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create lock dir: %w", err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: lock held at %s", ErrAlreadyRunning, path)
		}
		return nil, fmt.Errorf("flock %s: %w", path, err)
	}
	return f, nil
}
