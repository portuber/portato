package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List the status of all tunnels (stdout)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented yet")
	},
}
