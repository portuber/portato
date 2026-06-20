//go:build !darwin

package daemon

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// socketDir returns the directory shared by the IPC socket and PID file on
// non-darwin systems (Linux et al.). The preferred location is xdg.RuntimeDir
// (`/run/user/<uid>`), which systemd/logind reliably sets: it is a per-user
// tmpfs with correct permissions, cleaned on logout. The config-style fallback
// covers minimal environments where it is unset. See SPEC §6.
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
