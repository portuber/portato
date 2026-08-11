//go:build windows

package cmd

import (
	"github.com/portuber/portato/internal/service"
	"golang.org/x/sys/windows/registry"
)

// runKeyPath / runValueName mirror internal/service/service_windows.go (the
// HKCU Run key and the Portato entry within it). They are duplicated rather
// than exported to keep the service package's public surface OS-agnostic, and
// kept here as the --legacy-runkey fallback for autostartInstalled.
const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "Portato"
)

// autostartInstalled reports whether any Portato autostart is registered. Since
// Phase 47 the primary mechanism is the SCM service; the HKCU Run-key entry is
// the --legacy-runkey fallback. Either being present counts as installed.
func autostartInstalled(_ string) bool {
	if service.SCMInstalled() {
		return true
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	_, _, err = key.GetStringValue(runValueName)
	return err == nil
}
