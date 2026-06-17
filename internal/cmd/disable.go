package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var disableCmd = &cobra.Command{
	Use:           "disable <name>",
	Short:         "Disable a tunnel on the daemon",
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          disableRunE,
}

func disableRunE(cmd *cobra.Command, args []string) error {
	c, ok := requireDaemon(cmd)
	if !ok {
		return errDaemonDown
	}
	name := args[0]
	if err := c.Disable(name); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "disabled: %s\n", name)
	return nil
}
