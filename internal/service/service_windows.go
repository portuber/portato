//go:build windows

package service

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// runKeyPath is the per-user autostart key Windows honours at login; the
// Portato entry within it is runValueName. SPEC §13.
const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "Portato"
)

// windowsInstaller manages a per-user autostart entry in the HKCU registry Run
// key — the MVP-equivalent of launchd/systemd on Windows (a full Service
// Control Manager service is a later refinement).
type windowsInstaller struct{}

func newInstaller() Installer { return &windowsInstaller{} }

// runCommand builds the command line the Run key launches at login:
// `"<binary>" daemon --config "<config>"`. Paths are quoted so a location with
// spaces (e.g. Program Files) survives the shell's command-line parse.
func runCommand(o Options) string {
	return fmt.Sprintf(`"%s" daemon --config "%s"`, o.BinaryPath, o.ConfigPath)
}

func (windowsInstaller) Install(o Options) (string, error) {
	// CreateKey opens the key if present, or creates it (and any missing parent)
	// if absent. A fresh user profile — e.g. a CI runner — may not have the HKCU
	// Run key yet, so a plain OpenKey fails with "file not found".
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("open HKCU Run key: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue(runValueName, runCommand(o)); err != nil {
		return "", fmt.Errorf("set Run value: %w", err)
	}
	return `HKCU\` + runKeyPath, nil
}

func (windowsInstaller) Uninstall(Options) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		// Run key absent (fresh profile or never installed) — nothing to remove;
		// uninstall is an idempotent no-op.
		return nil
	}
	defer key.Close()
	if err := key.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("remove Run value: %w", err)
	}
	return nil
}

func (windowsInstaller) Status(Options) (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return "Run key unreadable (" + err.Error() + ")", nil
	}
	defer key.Close()
	val, _, err := key.GetStringValue(runValueName)
	if err != nil {
		if err == registry.ErrNotExist {
			return "not installed", nil
		}
		return "Run value unreadable (" + err.Error() + ")", nil
	}
	return "installed: " + val, nil
}
