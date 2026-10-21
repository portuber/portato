package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/daemon"
)

// daemonDownHint is printed to stderr when a CLI subcommand cannot reach the
// daemon. Wording matches the Phase 5 Definition of Done.
const daemonDownHint = "portato daemon is not running.\n" +
	"Start it with 'portato daemon' or set up autostart with 'portato install'."

// errDaemonDown is returned when a CLI subcommand cannot reach the daemon.
// It carries the full Phase 5 hint text so the centralized silenced-error
// printing (see printSilencedError) yields a self-explanatory message.
var errDaemonDown = errors.New(daemonDownHint)

// dialDaemon resolves the socket and confirms the daemon is alive. Overridable
// in tests; production wiring lives in defaultDialDaemon.
var dialDaemon = defaultDialDaemon

func defaultDialDaemon() (*client.Client, error) {
	socket, err := daemon.ResolveSocket()
	if err != nil {
		return nil, err
	}
	if socket == "" {
		return nil, errDaemonDown
	}
	c := client.New(socket)
	if err := c.Healthz(); err != nil {
		return nil, errDaemonDown
	}
	return c, nil
}

// requireDaemon is the shared preamble of the list/enable/disable/restart
// subcommands: it dials the daemon and returns (nil, false) on failure so the
// caller can bail out with errDaemonDown (whose text is the friendly hint).
func requireDaemon(_ *cobra.Command) (*client.Client, bool) {
	c, err := dialDaemon()
	if err != nil {
		return nil, false
	}
	return c, true
}
