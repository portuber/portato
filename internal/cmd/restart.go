package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:   "restart <name>",
	Short: "Restart a tunnel (down then up) on the daemon",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented yet")
	},
}
