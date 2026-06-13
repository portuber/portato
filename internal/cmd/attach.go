package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var attachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach a TUI client to a running daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented yet")
	},
}
