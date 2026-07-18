package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/controller"
	"github.com/portuber/portato/internal/daemon"
	"github.com/portuber/portato/internal/tui"
)

var attachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach a TUI client to a running daemon",
	RunE:  attachRunE,
}

func attachRunE(_ *cobra.Command, _ []string) error {
	socket, err := daemon.ResolveSocket()
	if err != nil {
		return err
	}
	if socket == "" {
		return fmt.Errorf("daemon not running, try 'portato daemon' or 'portato install'")
	}
	c := client.New(socket)
	if err := c.Healthz(); err != nil {
		return fmt.Errorf("daemon not running, try 'portato daemon' or 'portato install'")
	}

	ctrl := controller.NewRemote(c)
	defer ctrl.Close()
	return tui.Run(ctrl, tui.Options{Mode: "attach"})
}
