package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/portuber/portato/internal/controller"
)

// Options configures a TUI run. In attach mode CfgPath is empty and unused;
// in standalone mode it drives the background hand-off (the spawned daemon is
// pointed at this config, and its socket is found via the discovery marker).
type Options struct {
	// Mode is both the routing signal ("standalone" vs "attach") and the
	// string shown in the header. The daemon's socket path is deliberately
	// not appended here (Phase 39, F12): the 60+ char temp path was permanent
	// header noise; `portato doctor` exposes it when needed.
	Mode string
	// CfgPath is passed to the spawned daemon (--config) on hand-off.
	CfgPath string
}

func Run(ctrl controller.Controller, opt Options) error {
	p := tea.NewProgram(New(ctrl, opt))
	_, err := p.Run()
	return err
}
