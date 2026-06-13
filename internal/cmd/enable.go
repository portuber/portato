package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var enableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable a tunnel on the daemon",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented yet")
	},
}
