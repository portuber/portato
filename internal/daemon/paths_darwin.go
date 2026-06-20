//go:build darwin

package daemon

import (
	"os"
	"path/filepath"
)

// socketDir returns a stable, session-independent location for the IPC socket
// and PID file on macOS. macOS has no reliable per-user runtime directory —
// `XDG_RUNTIME_DIR` is not set by the OS and varies across terminal/tmux
// sessions, so relying on it makes the daemon and its clients disagree on the
// socket path (one binds at path A, another probes path B). A fixed
// subdirectory under Application Support avoids that and stays consistent
// regardless of which shell launched the process. See SPEC §6.
func socketDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "portato"), nil
}
