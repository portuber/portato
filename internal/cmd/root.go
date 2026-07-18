package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/controller"
	"github.com/portuber/portato/internal/daemon"
	routelog "github.com/portuber/portato/internal/log"
	"github.com/portuber/portato/internal/logo"
	"github.com/portuber/portato/internal/tui"
)

var (
	cfgFile         string
	forceStandalone bool
	socketFlag      string
	logLevelFlag    string
	// logLevel is parsed from --log-level in PersistentPreRunE and consumed by
	// the daemon / standalone TUI when they set up their file logger.
	logLevel = slog.LevelInfo
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
  portato daemon     background process with tubers + IPC server
  portato attach     TUI client connected to a running daemon
  portato list       list status of all tubers (stdout)
  portato enable     enable a tuber on the daemon
  portato disable    disable a tuber on the daemon
  portato restart    restart a tuber
  portato reload    reload the config on the running daemon
  portato stop       stop the running daemon
  portato install    install system autostart (launchd / systemd --user)
  portato uninstall  remove system autostart

See docs/SPEC.md for the full specification.`,
	RunE: rootRunE,
}

func rootRunE(cmd *cobra.Command, _ []string) error {
	if showVersion {
		printVersion(cmd.OutOrStdout())
		return nil
	}
	if showLicense {
		printLicense(cmd.OutOrStdout(), false)
		return nil
	}
	if !forceStandalone {
		if socket, err := daemon.ResolveSocket(); err == nil && socket != "" && probeDaemon(socket) {
			ctrl := controller.NewRemote(client.New(socket))
			defer ctrl.Close()
			return tui.Run(ctrl, tui.Options{Mode: "attach"})
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

	logger, ring, closer, err := routelog.Setup("", logLevel, logOptions(cfg))
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}
	defer closer.Close()

	ctrl := controller.NewLocal(cfg, path, logger, ring)
	defer ctrl.Close()

	// Match the daemon's boot-time StartEnabledWith (SPEC §6): launch every
	// enabled:true tuber so standalone and daemon agree on what is up, and a
	// hand-off to the daemon brings up the same set instead of surprise tubers.
	ctrl.StartEnabled()

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
	rootCmd.Flags().BoolVar(&showVersion, "version", false, "print the version banner and exit")
	rootCmd.Flags().BoolVar(&showLicense, "license", false, "print license information and exit")
	rootCmd.PersistentFlags().StringVar(&socketFlag, "socket", "",
		"override the daemon IPC socket path; the daemon binds it and clients dial it directly (also PORTATO_SOCKET)")
	rootCmd.PersistentFlags().StringVar(&logLevelFlag, "log-level", "info",
		"minimum log level for the log file (debug|info|warn|error)")
	// Push the --socket flag (if any) into the daemon package before any
	// subcommand runs, so both the daemon (bind) and clients (dial) honour it.
	// Parse --log-level once here so every subcommand's logger uses it.
	rootCmd.PersistentPreRunE = func(*cobra.Command, []string) error {
		daemon.SetSocketOverride(socketFlag)
		lvl, err := parseLogLevel(logLevelFlag)
		if err != nil {
			return err
		}
		logLevel = lvl
		return nil
	}
	// Easter egg: append the bilingual "pórtate bien" pun to the root
	// --help output only. The footer is baked into the root help template
	// once at init; Emoji is decided by logo.EmojiEnabled(). Subcommands are
	// pinned to the default template in Execute() (see defaultHelpTemplate) —
	// cobra's HelpTemplate() otherwise inherits the parent's template.
	defaultHelpTemplate = rootCmd.HelpTemplate()
	rootCmd.SetHelpTemplate(defaultHelpTemplate + "\n\n" + easterEggFooter() + "\n")
}

// defaultHelpTemplate is the cobra default help template captured before the
// root's is augmented with the easter-egg footer. Execute() pins each
// subcommand to it so `portato <sub> --help` does not inherit the footer.
var defaultHelpTemplate string

// easterEggFooter is the "And please, pórtate bien" line appended to
// `portato --help` / `portato help` — the Spanish imperative ¡pórtate bien!
// ("behave yourself!"), a near-homophone of the brand *portato*. The potato
// emoji is appended only when the terminal is emoji-capable (the Phase 24
// gate). The emoji decision is read at call time so the logic is unit-testable.
func easterEggFooter() string {
	s := "And please, pórtate bien"
	if logo.EmojiEnabled() {
		s += " 🥔"
	}
	return s
}

// parseLogLevel maps the --log-level flag value to a slog.Level. An empty value
// defaults to info so the flag is optional even when explicitly cleared.
func parseLogLevel(s string) (slog.Level, error) {
	switch s {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid --log-level %q (want debug|info|warn|error)", s)
	}
}

// logOptions translates the config's log-rotation knobs (defaults.log.*) into
// the rotating writer's LogOptions: MiB -> bytes, leaving zeros where the user
// did not configure a value so the writer's package defaults apply.
func logOptions(cfg *config.Config) routelog.LogOptions {
	lc := cfg.Defaults.Log
	return routelog.LogOptions{
		MaxSize:    int64(lc.MaxSizeMB) << 20,
		Retain:     lc.Retain,
		MaxAgeDays: lc.MaxAgeDays,
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
		reloadCmd,
		stopCmd,
		installCmd,
		uninstallCmd,
		forwardCmd,
		doctorCmd,
		addIdentityCmd,
		forgetIdentityCmd,
		versionCmd,
		licenseCmd,
	)
	// Pin every subcommand to the default help template so the root's
	// easter-egg footer does not leak via cobra's HelpTemplate() parent-walk.
	for _, sub := range rootCmd.Commands() {
		sub.SetHelpTemplate(defaultHelpTemplate)
	}
	return rootCmd.Execute()
}
