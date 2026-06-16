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

func daemonRunE(_ *cobra.Command, _ []string) error {
	path := cfgFile
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, closer, err := routelog.Setup(routelog.DaemonPath())
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}
	defer closer.Close()

	srv, err := daemon.New(cfg, path, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Start(ctx)
}
