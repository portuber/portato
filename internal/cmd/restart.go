package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:           "restart <name>",
	Short:         "Restart a tuber (down then up) on the daemon",
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          restartRunE,
}

func restartRunE(cmd *cobra.Command, args []string) error {
	c, ok := requireDaemon(cmd)
	if !ok {
		return errDaemonDown
	}
	name := args[0]
	if err := c.Restart(name); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "restarted: %s\n", name)
	return nil
}
