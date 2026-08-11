//go:build windows

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/service"
	"golang.org/x/term"
)

// registerWindowsInstallFlags adds the Phase-47 SCM flags to `portato install`:
// the account the service runs as, where to read its password from, and the
// legacy Run-key escape hatch.
func registerWindowsInstallFlags(c *cobra.Command) {
	c.Flags().StringVar(&serviceAccount, "service-account", "",
		"Windows account the SCM service runs as (default: current user; 'LocalSystem' / 'NT AUTHORITY\\SYSTEM' for the system account)")
	c.Flags().StringVar(&passwordFile, "password-file", "",
		"read the Windows account password from this file (CI / automation; skips the interactive prompt)")
	c.Flags().BoolVar(&legacyRunKey, "legacy-runkey", false,
		"use the Phase-17 HKCU Run-key autostart instead of a Service Control Manager service (locked-down environments)")
}

// registerWindowsUninstallFlags only adds --legacy-runkey to uninstall (the
// account / password flags are install-only).
func registerWindowsUninstallFlags(c *cobra.Command) {
	c.Flags().BoolVar(&legacyRunKey, "legacy-runkey", false,
		"remove the Phase-17 HKCU Run-key autostart instead of the SCM service")
}

// resolveServiceCredentials fills opts.Account / opts.Password for an SCM
// install. The default account is the installing user (`DOMAIN\user`); an
// explicit `LocalSystem` / `NT AUTHORITY\SYSTEM` skips the password (the system
// account has no password). For a real user, the password comes from
// --password-file or an interactive no-echo prompt. The password is handed
// straight to SCM and never persisted by portato (SCM keeps it as an LSA
// secret).
func resolveServiceCredentials(cmd *cobra.Command, opts *service.Options) error {
	if opts.Legacy {
		return nil
	}
	if opts.Account == "" {
		opts.Account = currentUserAccount()
	}
	if service.IsLocalSystemAccount(opts.Account) {
		return nil
	}
	if opts.Password != "" {
		return nil
	}
	if passwordFile != "" {
		b, err := os.ReadFile(passwordFile)
		if err != nil {
			return fmt.Errorf("read password file %s: %w", passwordFile, err)
		}
		opts.Password = strings.TrimRight(string(b), "\r\n")
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("a Windows account password is required to install the SCM service; pass --password-file, or use --service-account LocalSystem, or --legacy-runkey")
	}
	fmt.Fprint(cmd.OutOrStdout(), "Enter Windows account password: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(cmd.OutOrStdout())
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	opts.Password = string(b)
	return nil
}

// installMessage formats the post-install line. SCM installs the daemon
// immediately (it is running) and starts at boot; a Run-key / legacy install
// only fires at login. A user-account install also reminds that a Windows
// password change requires a fresh `portato install` (SCM's LSA secret goes
// stale).
func installMessage(opts service.Options) string {
	if opts.Legacy {
		return "Installed. Daemon will start at login (Run key)."
	}
	msg := "Installed. Daemon is running and will start at boot."
	if !service.IsLocalSystemAccount(opts.Account) {
		msg += "\nNote: re-run `portato install` after changing your Windows account password (SCM keeps its own copy)."
	}
	return msg
}

// currentUserAccount returns the `DOMAIN\user` form of the installing user, the
// default SCM service account.
func currentUserAccount() string {
	user := os.Getenv("USERNAME")
	domain := os.Getenv("USERDOMAIN")
	if user == "" {
		return ""
	}
	if domain != "" {
		return domain + `\` + user
	}
	return user
}
