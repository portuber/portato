package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
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

	// filter is the Phase 13 `/` substring filter over the list. filtering is
	// true while the input is focused (typing/editing); the query stays
	// applied after `enter` until cleared. Pure view-state: the list is
	// narrowed client-side, the controller/IPC are untouched, so it works
	// identically in standalone and attach.
	filter    textinput.Model
	filtering bool

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

	// enteringPassphrase shows the identity-passphrase prompt modal (Phase 19).
	// Raised by pressing space on a tunnel whose dial is blocked on a
	// passphrase-protected identity (Status.PendingPassphrase). The input is
	// masked; enter submits via Controller.AcceptPassphrase, esc cancels.
	// passphraseAttempts counts submits so a wrong passphrase shows a retry hint.
	enteringPassphrase bool
	passphraseTarget   string
	passphraseInput    textinput.Model
	passphraseAttempts int

	// logs is the Phase 11 per-tunnel log screen sub-model (nil when inactive).
	logs *logsView

	cfgPath string

	help bool
	quit bool
}

func New(ctrl controller.Controller, opt Options) Model {
	m := Model{
		ctrl:    ctrl,
		list:    ctrl.List(),
		mode:    opt.Mode,
		attach:  strings.HasPrefix(opt.Mode, "attach"),
		cfgPath: opt.CfgPath,
	}
	m.filter = newFilterInput()
	m.passphraseInput = newPassphraseInput()
	m.clampCursor()
	return m
}

// newFilterInput builds the `/`-opened substring filter input. It has no prompt
// glyph of its own; the filter line composes "/ " + the value + a count.
func newFilterInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "filter name/type/endpoint…"
	ti.CharLimit = 64
	return ti
}

// newPassphraseInput builds the masked input for the identity-passphrase modal
// (Phase 19). EchoPassword renders the EchoCharacter mask so the typed
// passphrase is never shown in the clear.
func newPassphraseInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "passphrase: "
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 256
	return ti
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
