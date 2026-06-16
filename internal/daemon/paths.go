package daemon

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

const (
	socketFile = "portato.sock"
	pidFile    = "portato.pid"
)

// socketDir returns the directory shared by the unix socket and PID file.
// Preferred location is xdg.RuntimeDir; if it is empty (common on macOS
// outside of systemd/launchd sessions), the config-style fallback
// $HOME/.config/portato is used. See SPEC §6.
func socketDir() (string, error) {
	if dir := xdg.RuntimeDir; dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "portato"), nil
}

// SocketPath returns the absolute path of the IPC unix socket.
func SocketPath() (string, error) {
	dir, err := socketDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, socketFile), nil
}

// PIDPath returns the absolute path of the daemon PID file (next to the socket).
func PIDPath() (string, error) {
	dir, err := socketDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, pidFile), nil
}
