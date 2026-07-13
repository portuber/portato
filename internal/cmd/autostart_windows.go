//go:build windows

package cmd

import "golang.org/x/sys/windows/registry"

// runKeyPath / runValueName mirror internal/service/service_windows.go (the HKCU
// Run key and the Portato entry within it). They are duplicated rather than
// exported to keep the service package's public surface OS-agnostic.
const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "Portato"
)

// autostartInstalled reports whether the HKCU Run-key entry exists. The path
// argument (the display string from defaultAutostartArtefact) is unused — the
// registry, not the filesystem, holds the definition.
func autostartInstalled(_ string) bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	_, _, err = key.GetStringValue(runValueName)
	return err == nil
}
