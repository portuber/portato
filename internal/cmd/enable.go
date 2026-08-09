package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var enableCmd = &cobra.Command{
	Use:           "enable <name>",
	Short:         "Enable a tuber on the daemon (or every tuber with --tag)",
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          enableRunE,
}

func init() {
	registerTagFlag(enableCmd, &enableTag)
	_ = enableCmd.RegisterFlagCompletionFunc(tagFlagName, tagValueCompletion)
}

func enableRunE(cmd *cobra.Command, args []string) error {
	c, ok := requireDaemon(cmd)
	if !ok {
		return errDaemonDown
	}
	names, err := resolveTagOrName(args, enableTag, c.List)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := c.Enable(name); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "enabled: %s\n", name)
	}
	return nil
}
