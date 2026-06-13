package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var disableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a tunnel on the daemon",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented yet")
	},
}
