package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var restartCmd = &cobra.Command{
	Use:           "restart <name>",
	Short:         "Restart a tuber (down then up) on the daemon (or every tuber with --tag)",
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          restartRunE,
}

func init() {
	registerTagFlag(restartCmd, &restartTag)
	_ = restartCmd.RegisterFlagCompletionFunc(tagFlagName, tagValueCompletion)
}

func restartRunE(cmd *cobra.Command, args []string) error {
	c, ok := requireDaemon(cmd)
	if !ok {
		return errDaemonDown
	}
	names, err := resolveTagOrName(args, restartTag, c.List)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := c.Restart(name); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "restarted: %s\n", name)
	}
	return nil
}
