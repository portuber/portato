package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:           "install",
	Short:         "Install system autostart (launchd on macOS, systemd --user on Linux)",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          installRunE,
}

func installRunE(cmd *cobra.Command, _ []string) error {
	opts, err := buildServiceOptions(cmd, serviceLabel)
	if err != nil {
		return err
	}
	path, err := newServiceInstaller().Install(opts)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Installed. Daemon will start at login.\nSee: %s\n", path)
	return nil
}

func init() {
	installCmd.Flags().StringVar(&serviceLabel, "label", "", "override the service label (default: dev.portato.daemon)")
}
