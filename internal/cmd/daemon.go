package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runDaemon(ctx)
}

// runDaemon prepares and runs the daemon until ctx is cancelled: it loads the
// config, sets up the rotating logger, takes the single-instance lock, adopts
// any standalone hand-off listeners, and serves. The context source is the
// only thing that differs between callers — the cobra command wires
// signal.NotifyContext, the Windows SCM handler wires the service stop signal
// — so both share this path. Pure extraction; no behaviour change.
func runDaemon(ctx context.Context) error {
	if ipcTokenFlag == "off" || os.Getenv("PORTATO_NO_IPC_TOKEN") == "1" {
		daemon.SetIpcTokenDisabled(true)
	}
	// The background update poll identifies itself as this binary's version
	// (Phase 49); set before any goroutine could fire a check.
	daemon.SetUpdateUserAgent("portato/" + version)

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

	return srv.Start(ctx)
}

// RunDaemon is the exported entry point used by the Windows SCM service handler
// (internal/service.RunAsService), which runs the daemon without the cobra
// tree. It mirrors the cobra path exactly.
func RunDaemon(ctx context.Context) error { return runDaemon(ctx) }

// ParseDaemonArgs populates the daemon's flag-backed package vars
// (--config/--ipc-token/--listen-fds/--log-level) from a raw arg slice. The
// Windows SCM launches the service binary with the recorded command line
// (e.g. `portato.exe daemon --config <abs>`), but cobra never runs under SCM,
// so the handler parses those args itself before calling RunDaemon. Unknown
// flags are ignored (the SCM command line is owned by `portato install`).
func ParseDaemonArgs(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	var cfg, ipc, lfds, lvl string
	fs.StringVar(&cfg, "config", "", "")
	fs.StringVar(&ipc, "ipc-token", "on", "")
	fs.StringVar(&lfds, "listen-fds", "", "")
	fs.StringVar(&lvl, "log-level", "info", "")
	// Drop a leading subcommand token (e.g. "daemon"): the SCM-recorded command
	// line is `portato.exe daemon --config <abs>`, and flag.Parse stops at the
	// first non-flag positional, so the leading "daemon" must be skipped or
	// --config would never be parsed.
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return
	}
	if cfg != "" {
		cfgFile = cfg
	}
	ipcTokenFlag = ipc
	listenFdsPath = lfds
	if l, err := parseLogLevel(lvl); err == nil {
		logLevel = l
	}
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
