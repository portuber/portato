package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kipkaev55/portato/internal/client"
	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/controller"
	"github.com/kipkaev55/portato/internal/daemon"
	routelog "github.com/kipkaev55/portato/internal/log"
	"github.com/kipkaev55/portato/internal/tui"
)

var (
	cfgFile         string
	forceStandalone bool
	socketFlag      string
)

// probeTimeout caps the smart-launcher daemon probe: a live daemon (or
// connection-refused) responds in well under this.
const probeTimeout = 200 * time.Millisecond

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

func rootRunE(_ *cobra.Command, _ []string) error {
	if !forceStandalone {
		if socket, err := daemon.ResolveSocket(); err == nil && socket != "" && probeDaemon(socket) {
			ctrl := controller.NewRemote(client.New(socket))
			defer ctrl.Close()
			return tui.Run(ctrl, tui.Options{Mode: "attach @ " + socket})
		}
	}
	return runStandalone()
}

// runStandalone loads the config, builds a local controller and runs the TUI
// without a daemon. This is the fallback when no daemon answers, and the
// forced path under --force-standalone. The hand-off reads the daemon's
// socket from the discovery marker once the spawned daemon advertises it.
func runStandalone() error {
	path := cfgFile
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, ring, closer, err := routelog.Setup("")
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}
	defer closer.Close()

	ctrl := controller.NewLocal(cfg, path, logger, ring)
	defer ctrl.Close()

	return tui.Run(ctrl, tui.Options{Mode: "standalone", CfgPath: path})
}

// probeDaemon reports whether a live daemon is answering on the socket within
// probeTimeout. Any error (refused, timeout, bad response) means "not alive".
func probeDaemon(socket string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return client.New(socket).HealthzCtx(ctx) == nil
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "path to config file (default: XDG config home)")
	rootCmd.Flags().BoolVar(&forceStandalone, "force-standalone", false, "skip daemon auto-detection and run a standalone TUI")
	rootCmd.PersistentFlags().StringVar(&socketFlag, "socket", "",
		"override the daemon IPC socket path; the daemon binds it and clients dial it directly (also PORTATO_SOCKET)")
	// Push the --socket flag (if any) into the daemon package before any
	// subcommand runs, so both the daemon (bind) and clients (dial) honour it.
	rootCmd.PersistentPreRunE = func(*cobra.Command, []string) error {
		daemon.SetSocketOverride(socketFlag)
		return nil
	}
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
		doctorCmd,
		versionCmd,
	)
	return rootCmd.Execute()
}
