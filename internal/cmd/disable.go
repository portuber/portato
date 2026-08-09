package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var disableCmd = &cobra.Command{
	Use:           "disable <name>",
	Short:         "Disable a tuber on the daemon (or every tuber with --tag)",
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          disableRunE,
}

func init() {
	registerTagFlag(disableCmd, &disableTag)
	_ = disableCmd.RegisterFlagCompletionFunc(tagFlagName, tagValueCompletion)
}

func disableRunE(cmd *cobra.Command, args []string) error {
	c, ok := requireDaemon(cmd)
	if !ok {
		return errDaemonDown
	}
	names, err := resolveTagOrName(args, disableTag, c.List)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := c.Disable(name); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "disabled: %s\n", name)
	}
	return nil
}
