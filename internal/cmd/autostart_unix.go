//go:build unix

package cmd

// autostartInstalled reports whether the autostart definition file is present
// (the launchd plist on macOS, the systemd unit on Linux). Windows overrides
// this (autostart_windows.go) with a registry query — its artefact is not a
// file.
func autostartInstalled(path string) bool {
	return fileExists(path)
}
