package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/controller"
	routelog "github.com/kipkaev55/portato/internal/log"
	"github.com/kipkaev55/portato/internal/tui"
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
	RunE: rootRunE,
}

func rootRunE(cmd *cobra.Command, args []string) error {
	path := cfgFile
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, closer, err := routelog.Setup("")
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}
	defer closer.Close()

	ctrl := controller.NewLocal(cfg, path, logger)
	defer ctrl.Close()

	return tui.Run(ctrl, "standalone")
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
		forwardCmd,
	)
	return rootCmd.Execute()
}
