//go:build !windows

package cmd

import (
	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/service"
)

// registerWindowsInstallFlags / registerWindowsUninstallFlags are no-ops on
// non-Windows: the SCM / Run-key flags do not apply to launchd / systemd.
func registerWindowsInstallFlags(*cobra.Command)   {}
func registerWindowsUninstallFlags(*cobra.Command) {}

// resolveServiceCredentials is a no-op on non-Windows; launchd / systemd take
// no account / password.
func resolveServiceCredentials(*cobra.Command, *service.Options) error { return nil }

// installMessage returns the macOS/Linux message; the daemon is loaded/enabled
// (RunAtLoad / enable --now) and fires at login/boot.
func installMessage(service.Options) string {
	return "Installed. Daemon will start at login."
}
