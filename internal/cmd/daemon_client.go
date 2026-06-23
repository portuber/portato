package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kipkaev55/portato/internal/client"
	"github.com/kipkaev55/portato/internal/daemon"
)

// daemonDownHint is printed to stderr when a CLI subcommand cannot reach the
// daemon. Wording matches the Phase 5 Definition of Done.
const daemonDownHint = "portato daemon is not running.\n" +
	"Start it with 'portato daemon' or set up autostart with 'portato install'."

// errDaemonDown is returned (and silenced) so cobra still yields exit code 1.
var errDaemonDown = errors.New("daemon is not running")

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
// subcommands: it dials the daemon, prints the friendly hint on failure and
// returns (nil, false) so the caller can bail out with exit code 1.
func requireDaemon(cmd *cobra.Command) (*client.Client, bool) {
	c, err := dialDaemon()
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), daemonDownHint)
		return nil, false
	}
	return c, true
}
