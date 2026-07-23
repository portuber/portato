//go:build unix

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/adrg/xdg"
)

// appDataDir is the stable, per-user base directory for the discovery marker
// and the single-instance lock. On unix this is the XDG config home; Windows
// overrides it (paths_windows.go) to %LOCALAPPDATA%.
func appDataDir() string {
	return filepath.Join(xdg.ConfigHome, "portato")
}

// RuntimeSocketPath returns where the IPC socket file lives. On Linux the
// per-user runtime dir (`/run/user/<uid>`, set by systemd/logind) is preferred
// — it is a per-user tmpfs that logind manages and never reaps while the
// session exists, so the socket file is stable. When it is unset (bare SSH
// without pam_systemd, containers, WSL, some CI) the code falls back to
// `os.TempDir()`, which carries the same reaping risk as macOS (see below).
//
// On macOS there is no reliable per-user runtime dir, and `$TMPDIR` is
// periodically reaped and rotated across sessions: a socket file placed there
// gets unlinked under a running daemon, leaving the listener fd open on an
// orphaned inode — the daemon stays alive (holding the flock and the local
// ports) but no client can connect, i.e. it is wedged (Phase 40). So on macOS
// the socket lives in a stable, owner-only dir co-located with the daemon log
// under the XDG state home. The filename is uid-scoped to avoid collisions
// between users on shared hosts. Windows overrides this (paths_windows.go) to
// return the named-pipe name instead.
func RuntimeSocketPath() (string, error) {
	name := fmt.Sprintf("portato-%d.sock", os.Getuid())
	if runtime.GOOS != "darwin" {
		if dir := xdg.RuntimeDir; dir != "" {
			return filepath.Join(dir, name), nil
		}
		return filepath.Join(os.TempDir(), name), nil
	}
	return filepath.Join(runtimeSocketDir(), name), nil
}

// runtimeSocketDir is the macOS base directory for the IPC socket. It is a
// variable so tests can redirect it: xdg.StateHome is resolved at package init
// (cached), so t.Setenv("XDG_STATE_HOME") cannot redirect RuntimeSocketPath —
// the same seam pattern discoveryPathFn / lockPathFn use. The default
// co-locates the socket with the daemon log (internal/log), in a stable,
// owner-only dir that macOS never reaps.
var runtimeSocketDir = func() string {
	return filepath.Join(xdg.StateHome, "portato")
}
