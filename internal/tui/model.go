package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/portuber/portato/internal/controller"
)

type Model struct {
	ctrl   controller.Controller
	list   []controller.Status
	cursor int
	width  int
	height int
	// cleared tracks whether tea.ClearScreen has been issued on the first
	// WindowSizeMsg, forcing a full repaint once dimensions are known (Phase 37
	// task B: the first frames render at width 0, where the surface fill is a
	// no-op, and the v2 cell-diff renderer would otherwise never repaint them).
	cleared bool
	mode    string
	attach  bool

	// pal is the resolved palette (Phase 37). Palette resolution moved off
	// package init onto the model because tea.BackgroundColorMsg (the OSC-11
	// answer) arrives after Init: a sensible default is seeded in New and
	// re-resolved in handleBackgroundColor. kind is the resolved theme kind
	// (kept for the logo's mono branch and tests).
	pal  palette
	kind themeKind

	// filter is the Phase 13 `/` substring filter over the list. filtering is
	// true while the input is focused (typing/editing); the query stays
	// applied after `enter` until cleared. Pure view-state: the list is
	// narrowed client-side, the controller/IPC are untouched, so it works
	// identically in standalone and attach.
	filter    textinput.Model
	filtering bool

	// confirmQuit shows the "leave running in background?" modal. Only raised
	// in standalone mode when there are live tubers.
	confirmQuit bool
	// handoffing marks the (brief) window after the user accepts the modal:
	// the standalone process is handing its tubers off to a spawned daemon.
	handoffing bool
	handoffErr string

	// editor is the Phase 10 tuber editor sub-model (nil when inactive).
	editor *tuberEditor
	// confirmDelete shows the "delete tuber?" modal.
	confirmDelete bool
	deleteTarget  string

	// confirmAccept shows the "accept unknown host key?" modal (Phase 11 TOFU).
	// Raised by pressing space on a tuber blocked by an unknown SSH host key.
	confirmAccept bool
	acceptTarget  string

	// enteringPassphrase shows the identity-passphrase prompt modal (Phase 19).
	// Raised by pressing `p` on (or auto-opening on) a tuber whose dial is
	// blocked on a passphrase-protected identity (Status.PendingPassphrase).
	// The input is masked; enter submits via Controller.AcceptPassphrase, esc
	// cancels. The "wrong passphrase" hint is driven by
	// Status.PassphraseAttempts (the dial's real rejection count), not a local
	// counter.
	enteringPassphrase bool
	passphraseTarget   string
	passphraseInput    textinput.Model
	// passphraseConnecting is the brief "portubbing…" state between submit and
	// the dial's verdict (passphrase accepted → modal closes; rejected → back to
	// the input). passphraseSubmitBase is the PasswordAttempts value at submit
	// time, so a tick can detect a rejection (it increments).
	passphraseConnecting bool
	passphraseSubmitBase int
	// enteringPassword shows the SSH-password prompt modal (Phase 35). Raised
	// by pressing `o` on (or auto-opening on) a tuber whose dial is blocked on
	// a password-only account (Status.PendingPassword). The input is masked;
	// enter submits via Controller.AcceptPassword, esc cancels. The "wrong
	// password" hint is driven by Status.PasswordAttempts (the dial's real
	// rejection count), not a local counter.
	enteringPassword bool
	passwordTarget   string
	passwordInput    textinput.Model
	// passwordConnecting is the brief "portubbing…" state between submit and the
	// dial's verdict (password accepted → modal closes; rejected → back to the
	// input). passwordSubmitBase is the PasswordAttempts value at submit time,
	// so a tick can detect a rejection (it increments).
	passwordConnecting bool
	passwordSubmitBase int
	// dismissedPending is the pending-prompt key (a passphrase path, a password
	// account, or a host line) the user cancelled with esc, so the auto-open on
	// tick does not re-pop the same prompt endlessly. Cleared once the cursor's
	// tuber has no pending prompt. A manual space still reopens it on demand.
	// Phase 19/35 UX.
	dismissedPending string

	// logs is the Phase 11 per-tuber log screen sub-model (nil when inactive).
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
	// Seed the palette with the environment-only resolver as the pre-message
	// default (covers the first frame and unit tests). The runtime OSC-11
	// answer, when it arrives, re-resolves via handleBackgroundColor.
	m.kind = detectKind()
	m.pal = resolvePalette(m.kind)
	m.filter = newFilterInput()
	m.passphraseInput = newPassphraseInput()
	m.passwordInput = newPasswordInput()
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

// newPasswordInput builds the masked input for the SSH-password modal (Phase
// 35), mirroring newPassphraseInput. EchoPassword renders the mask so the typed
// password is never shown in the clear.
func newPasswordInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "password: "
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
// fields (uptime) while a tuber sits in a steady state (Connected/Off) and
// produces no state-change events. This keeps the Phase 9 "no idle daemon
// load" guarantee intact: there is no per-second /tubers request, just a
// cheap local redraw.
type redrawTickMsg struct{}

const redrawInterval = time.Second

func redrawTick() tea.Cmd {
	return tea.Tick(redrawInterval, func(time.Time) tea.Msg { return redrawTickMsg{} })
}

func (m Model) Init() tea.Cmd {
	// RequestBackgroundColor asks the terminal for its background (OSC 11);
	// the answer arrives as tea.BackgroundColorMsg, handled in Update, which
	// re-resolves the palette by luminance. bubbletea v2 brackets the query
	// with a Device Attributes request so a non-answering terminal never hangs.
	return tea.Batch(waitForChange(m.ctrl.Changes()), redrawTick(), tea.RequestBackgroundColor)
}
