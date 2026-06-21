package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kipkaev55/portato/internal/controller"
)

type Model struct {
	ctrl   controller.Controller
	list   []controller.Status
	cursor int
	width  int
	height int
	mode   string
	attach bool

	// confirmQuit shows the "leave running in background?" modal. Only raised
	// in standalone mode when there are live tunnels.
	confirmQuit bool
	// handoffing marks the (brief) window after the user accepts the modal:
	// the standalone process is handing its tunnels off to a spawned daemon.
	handoffing bool
	handoffErr string

	// editor is the Phase 10 tunnel editor sub-model (nil when inactive).
	editor *tunnelEditor
	// confirmDelete shows the "delete tunnel?" modal.
	confirmDelete bool
	deleteTarget  string

	cfgPath   string
	socketURI string

	help bool
	quit bool
}

func New(ctrl controller.Controller, opt Options) Model {
	return Model{
		ctrl:      ctrl,
		list:      ctrl.List(),
		mode:      opt.Mode,
		attach:    strings.HasPrefix(opt.Mode, "attach"),
		cfgPath:   opt.CfgPath,
		socketURI: opt.SocketPath,
	}
}

type tickMsg struct{}

func waitForChange(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-ch
		return tickMsg{}
	}
}

func (m Model) Init() tea.Cmd {
	return waitForChange(m.ctrl.Changes())
}
