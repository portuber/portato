package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:           "uninstall",
	Short:         "Remove the system autostart entry",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          uninstallRunE,
}

func uninstallRunE(cmd *cobra.Command, _ []string) error {
	opts, err := buildServiceOptions(cmd, serviceLabel)
	if err != nil {
		return err
	}
	if err := newServiceInstaller().Uninstall(opts); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Removed autostart entry.")
	return nil
}

func init() {
	uninstallCmd.Flags().StringVar(&serviceLabel, "label", "", "service label to remove (default: dev.portato.daemon)")
}
