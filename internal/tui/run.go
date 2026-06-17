package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/kipkaev55/portato/internal/controller"
)

// Options configures a TUI run. In attach mode CfgPath/SocketPath are empty
// and unused; in standalone mode they drive the background hand-off.
type Options struct {
	// Mode is both the routing signal ("standalone" vs "attach @ <socket>")
	// and the string shown in the header.
	Mode string
	// CfgPath is passed to the spawned daemon (--config) on hand-off.
	CfgPath string
	// SocketPath is the unix socket awaited on hand-off.
	SocketPath string
}

func Run(ctrl controller.Controller, opt Options) error {
	p := tea.NewProgram(New(ctrl, opt))
	_, err := p.Run()
	return err
}
