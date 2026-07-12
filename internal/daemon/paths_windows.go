//go:build windows

package daemon

import (
	"os"
	"path/filepath"
)

// appDataDir is the per-user base directory for the discovery marker and the
// single-instance lock: %LOCALAPPDATA%\portato (the local, non-roaming app-data
// location). os.UserCacheDir() returns %LocalAppData% on Windows and is the
// fallback when LOCALAPPDATA is unset. See SPEC §6.
func appDataDir() string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		if cache, err := os.UserCacheDir(); err == nil {
			dir = cache
		}
	}
	return filepath.Join(dir, "portato")
}

// RuntimeSocketPath returns the named pipe a Windows daemon serves on. A pipe
// has no filesystem path, so this is the pipe name the transport listens/dials
// (`\\.\pipe\portato`); the discovery marker records it as the `socket` field.
func RuntimeSocketPath() (string, error) {
	return `\\.\pipe\portato`, nil
}
