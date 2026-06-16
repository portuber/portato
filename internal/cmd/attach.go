package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kipkaev55/portato/internal/client"
	"github.com/kipkaev55/portato/internal/controller"
	"github.com/kipkaev55/portato/internal/daemon"
	"github.com/kipkaev55/portato/internal/tui"
)

var attachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach a TUI client to a running daemon",
	RunE:  attachRunE,
}

func attachRunE(_ *cobra.Command, _ []string) error {
	socket, err := daemon.SocketPath()
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}
	c := client.New(socket)
	if err := c.Healthz(); err != nil {
		return fmt.Errorf("daemon not running, try 'portato daemon' or 'portato install'")
	}

	ctrl := controller.NewRemote(c)
	defer ctrl.Close()
	return tui.Run(ctrl, "attach @ "+socket)
}
