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

// RuntimeSocketPath returns where the IPC socket file lives: a runtime/temp
// dir. On Linux the per-user runtime dir (`/run/user/<uid>`, set by
// systemd/logind) is preferred; on macOS there is no such dir, so `$TMPDIR`
// (via os.TempDir) is used. The filename is uid-scoped to avoid collisions
// between users on shared hosts. Windows overrides this (paths_windows.go) to
// return the named-pipe name instead.
func RuntimeSocketPath() (string, error) {
	name := fmt.Sprintf("portato-%d.sock", os.Getuid())
	if runtime.GOOS != "darwin" {
		if dir := xdg.RuntimeDir; dir != "" {
			return filepath.Join(dir, name), nil
		}
	}
	return filepath.Join(os.TempDir(), name), nil
}
