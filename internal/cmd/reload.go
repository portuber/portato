package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var reloadCmd = &cobra.Command{
	Use:           "reload",
	Short:         "Reload the config on the running daemon (re-read config.yaml)",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          reloadRunE,
}

// reloadRunE makes the daemon re-read config.yaml from disk via POST /reload,
// so an edit takes effect without restarting the daemon. The daemon's own
// file watcher (Phase 28) usually applies edits automatically; this is the
// manual knob.
func reloadRunE(cmd *cobra.Command, _ []string) error {
	c, ok := requireDaemon(cmd)
	if !ok {
		return errDaemonDown
	}
	if err := c.Reload(); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "config reloaded")
	return nil
}
