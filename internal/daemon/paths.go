package daemon

import "path/filepath"

const (
	socketFile = "portato.sock"
	pidFile    = "portato.pid"
)

// socketDir returns the directory shared by the unix socket and PID file. It
// is implemented per-OS (build-tagged paths_darwin.go / paths_unix.go) because
// the right answer differs: Linux has a reliable per-user runtime dir, macOS
// does not. See SPEC §6.

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
