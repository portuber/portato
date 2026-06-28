package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/daemon"
	routelog "github.com/kipkaev55/portato/internal/log"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run as a background daemon with tunnels and an IPC server",
	RunE:  daemonRunE,
}

// ipcTokenFlag mirrors the --ipc-token flag value (on|off). Default "on": the
// daemon authenticates IPC. "off" (or PORTATO_NO_IPC_TOKEN=1) is the break-glass
// escape hatch that disables the bearer token and serves openly over the 0600
// socket (Phase 18).
var ipcTokenFlag string

func init() {
	daemonCmd.Flags().StringVar(&ipcTokenFlag, "ipc-token", "on",
		"enable/disable IPC bearer-token auth (on|off); PORTATO_NO_IPC_TOKEN=1 forces off")
}

func daemonRunE(_ *cobra.Command, _ []string) error {
	if ipcTokenFlag == "off" || os.Getenv("PORTATO_NO_IPC_TOKEN") == "1" {
		daemon.SetIpcTokenDisabled(true)
	}

	path := cfgFile
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, ring, closer, err := routelog.Setup(routelog.DaemonPath())
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}
	defer closer.Close()

	srv, err := daemon.New(cfg, path, logger, ring)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Start(ctx)
}
