package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "portato",
	Short: "Portato — SSH port-forwarding manager with TUI",
	Long: `Portato manages a set of SSH port forwards from a single place (TUI),
like the MCP screen in opencode.

Modes:
  portato            smart launcher (attach to a running daemon, or standalone TUI)
  portato daemon     background process with tunnels + IPC server
  portato attach     TUI client connected to a running daemon
  portato list       list status of all tunnels (stdout)
  portato enable     enable a tunnel on the daemon
  portato disable    disable a tunnel on the daemon
  portato restart    restart a tunnel
  portato install    install system autostart (launchd / systemd --user)
  portato uninstall  remove system autostart

See docs/SPEC.md for the full specification.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("TUI not implemented yet")
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "path to config file (default: XDG config home)")
}

func Execute() error {
	rootCmd.AddCommand(
		daemonCmd,
		attachCmd,
		listCmd,
		enableCmd,
		disableCmd,
		restartCmd,
		installCmd,
		uninstallCmd,
	)
	return rootCmd.Execute()
}
