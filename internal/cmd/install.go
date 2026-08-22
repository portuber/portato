package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/config"
)

var installCmd = &cobra.Command{
	Use:           "install",
	Short:         "Install system autostart (launchd on macOS, systemd --user on Linux, SCM on Windows)",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          installRunE,
}

func installRunE(cmd *cobra.Command, _ []string) error {
	opts, err := buildServiceOptions(cmd, serviceLabel)
	if err != nil {
		return err
	}
	if err := resolveServiceCredentials(cmd, &opts); err != nil {
		return err
	}
	path, err := newServiceInstaller().Install(opts)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\nSee: %s\n", installMessage(opts), path)

	// Phase-49: install starts the daemon that would do the background
	// polling, so it is the natural moment for the one-time consent ask
	// (TTY only; any other state — already answered, non-interactive —
	// stays silent).
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	maybeAskConsentTTY(cfgPath)
	return nil
}

func init() {
	installCmd.Flags().StringVar(&serviceLabel, "label", "", "override the service label (default: dev.portato.daemon)")
	registerWindowsInstallFlags(installCmd)
}
