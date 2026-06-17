package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var enableCmd = &cobra.Command{
	Use:           "enable <name>",
	Short:         "Enable a tunnel on the daemon",
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          enableRunE,
}

func enableRunE(cmd *cobra.Command, args []string) error {
	c, ok := requireDaemon(cmd)
	if !ok {
		return errDaemonDown
	}
	name := args[0]
	if err := c.Enable(name); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "enabled: %s\n", name)
	return nil
}
