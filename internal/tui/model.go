package tui

import (
	"strings"
	"time"

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

	// confirmAccept shows the "accept unknown host key?" modal (Phase 11 TOFU).
	// Raised by pressing space on a tunnel blocked by an unknown SSH host key.
	confirmAccept bool
	acceptTarget  string

	// logs is the Phase 11 per-tunnel log screen sub-model (nil when inactive).
	logs *logsView

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

// redrawTickMsg drives a purely local re-render every second. It does NOT fetch
// from the controller — its only purpose is to refresh time-based display
// fields (uptime) while a tunnel sits in a steady state (Connected/Off) and
// produces no state-change events. This keeps the Phase 9 "no idle daemon
// load" guarantee intact: there is no per-second /tunnels request, just a
// cheap local redraw.
type redrawTickMsg struct{}

const redrawInterval = time.Second

func redrawTick() tea.Cmd {
	return tea.Tick(redrawInterval, func(time.Time) tea.Msg { return redrawTickMsg{} })
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitForChange(m.ctrl.Changes()), redrawTick())
}
