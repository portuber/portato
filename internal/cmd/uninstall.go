package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the system autostart entry",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented yet")
	},
}
