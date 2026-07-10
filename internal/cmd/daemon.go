package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/daemon"
	"github.com/portuber/portato/internal/fdpass"
	routelog "github.com/portuber/portato/internal/log"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run as a background daemon with tubers and an IPC server",
	RunE:  daemonRunE,
}

// ipcTokenFlag mirrors the --ipc-token flag value (on|off). Default "on": the
// daemon authenticates IPC. "off" (or PORTATO_NO_IPC_TOKEN=1) is the break-glass
// escape hatch that disables the bearer token and serves openly over the 0600
// socket (Phase 18).
var ipcTokenFlag string

// listenFdsPath mirrors the --listen-fds flag: a unix-domain transfer socket
// from which the daemon pulls the standalone's live local listeners at spawn
// (Phase 16). Empty for a normal (autostart / manual) daemon start.
var listenFdsPath string

func init() {
	daemonCmd.Flags().StringVar(&ipcTokenFlag, "ipc-token", "on",
		"enable/disable IPC bearer-token auth (on|off); PORTATO_NO_IPC_TOKEN=1 forces off")
	daemonCmd.Flags().StringVar(&listenFdsPath, "listen-fds", "",
		"path to a unix-domain transfer socket to adopt live listeners from (standalone->daemon hand-off)")
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

	logger, ring, closer, err := routelog.Setup(routelog.DaemonPath(), logLevel, logOptions(cfg))
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}
	defer closer.Close()

	srv, err := daemon.New(cfg, path, logger, ring)
	if err != nil {
		// A concurrent start that lost the single-instance flock exits 0 with a
		// clear message (Phase 22), not a failure.
		if errors.Is(err, daemon.ErrAlreadyRunning) {
			fmt.Fprintln(os.Stdout, "daemon already running")
			return nil
		}
		return err
	}

	// Phase 16: if spawned with --listen-fds, pull the standalone's live local
	// listeners over the transfer socket so the ports stay up across the
	// hand-off. Any failure degrades to a normal bind (the brief MVP blip) --
	// the daemon still comes up.
	if listenFdsPath != "" {
		if adopted, aerr := adoptPassedListeners(listenFdsPath, logger); aerr != nil {
			logger.Warn("fd hand-off receive failed; starting with normal bind", "err", aerr)
		} else if len(adopted) > 0 {
			srv.SetAdopted(adopted)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Start(ctx)
}

// adoptPassedListeners dials the standalone's transfer socket and reconstructs
// the offered live listeners via fdpass. It is the daemon side of the Phase 16
// hand-off: the spawned daemon dials back the socket path it was given and reads
// the SCM_RIGHTS fds the standalone sends.
func adoptPassedListeners(socket string, log *slog.Logger) (map[string]net.Listener, error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("dial transfer socket %s: %w", socket, err)
	}
	uc := conn.(*net.UnixConn)
	defer uc.Close()
	adopted, err := fdpass.Recv(uc)
	if err != nil {
		// Release whatever we partially received so a failed hand-off does not
		// hold the ports; the daemon rebinds them normally.
		for _, ln := range adopted {
			_ = ln.Close()
		}
		return nil, err
	}
	if len(adopted) > 0 {
		names := make([]string, 0, len(adopted))
		for n := range adopted {
			names = append(names, n)
		}
		log.Info("adopted hand-off listeners", "tubers", names)
	}
	return adopted, nil
}
