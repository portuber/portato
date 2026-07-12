//go:build windows

package ipctoken

import (
	"os"
	"path/filepath"
)

// TokenPath returns the token file path. On Windows the IPC transport is a
// named pipe (`\\.\pipe\portato`), which has no sibling directory, so the token
// lives in the local app-data dir alongside the discovery marker. The pipe-name
// argument is ignored: the daemon and every client resolve the same path.
func TokenPath(socketPath string) string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		if cache, err := os.UserCacheDir(); err == nil {
			dir = cache
		}
	}
	return filepath.Join(dir, "portato", tokenFile)
}
