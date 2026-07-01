package daemon

import (
	"errors"
	"path/filepath"

	"github.com/adrg/xdg"
)

// ErrAlreadyRunning is returned by New when another daemon already holds the
// single-instance lock. The daemon command maps it to a clean exit 0 with a
// friendly "already running" message rather than treating it as a failure
// (Phase 22 concurrent-start hardening).
var ErrAlreadyRunning = errors.New("daemon already running")

// lockFile is the stable single-instance lock under the config home. It is a
// dedicated file (not the discovery marker) so the marker's atomic
// tmp+rename writes never contend with the lock.
const lockFile = "daemon.lock"

// LockPath returns the path of the single-instance lock under the config home.
func LockPath() (string, error) { return lockPathFn() }

// lockPathFn resolves the lock path. It is a variable so tests can redirect it
// to a temp dir: adrg/xdg caches xdg.ConfigHome at package init, so
// t.Setenv("XDG_CONFIG_HOME", ...) would not affect LockPath (the same seam
// pattern discoveryPathFn uses).
var lockPathFn = func() (string, error) {
	return filepath.Join(xdg.ConfigHome, "portato", lockFile), nil
}
