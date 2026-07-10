package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/lithammer/fuzzysearch/fuzzy"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/controller"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.editor != nil {
			return m, m.editor.update(msg)
		}
		if m.logs != nil {
			return m, m.logs.update(msg)
		}
		return m, nil
	case tickMsg:
		if m.handoffing {
			return m, nil
		}
		m.list = m.ctrl.List()
		m.clampCursor()
		// Auto-open a pending prompt for the tuber under the cursor: pressing
		// space once to enable a tuber that then blocks on a passphrase / an
		// unknown host should surface the prompt without a second keypress.
		// Skipped while the user is busy (another modal/editor/filter) or after
		// they dismissed this exact prompt (esc) — otherwise it would reopen
		// on every tick. Phase 19 UX (also covers the TOFU host-key prompt).
		var openCmd tea.Cmd
		m, openCmd = m.autoOpenIfPending()
		// Auto-close the passphrase modal once the dial accepts it
		// (Status.PendingPassphrase clears). A wrong passphrase leaves it set,
		// so the modal stays open for another attempt.
		if m.enteringPassphrase && !pendingPassphraseFor(m.list, m.passphraseTarget) {
			m.enteringPassphrase = false
			m.passphraseTarget = ""
			m.passphraseAttempts = 0
			m.passphraseInput.SetValue("")
		}
		// Forget a stale dismissal once the cursor's tuber has no pending
		// prompt, so a future block on it auto-opens again.
		if m.hasCurrent() && pendingKey(m.list[m.cursor]) == "" {
			m.dismissedPending = ""
		}
		if m.logs != nil {
			m.logs.refresh()
		}
		return m, tea.Batch(openCmd, waitForChange(m.ctrl.Changes()))
	case redrawTickMsg:
		// Local re-render tick: refreshes time-based display fields (uptime)
		// without fetching from the controller. Re-arm; the change-waiter is
		// an independent pending command. See redrawTickMsg in model.go.
		// The logs screen (transient modal) does re-fetch here — acceptable:
		// it is not the idle tuber-status path Phase 9 made push-driven.
		if m.logs != nil {
			m.logs.refresh()
		}
		return m, redrawTick()
	case handoffDoneMsg:
		m.handoffing = false
		m.quit = true
		if msg.err != nil {
			m.handoffErr = msg.err.Error()
		}
		return m, tea.Quit
	case tea.KeyPressMsg:
		if m.editor != nil {
			cmd := m.editor.update(msg)
			if m.editor.done {
				m.editor = nil
			}
			return m, cmd
		}
		if m.logs != nil {
			cmd := m.logs.update(msg)
			if m.logs.done {
				m.logs = nil
			}
			return m, cmd
		}
		if m.confirmDelete {
			return m.handleDeleteConfirm(msg)
		}
		if m.confirmAccept {
			return m.handleAcceptConfirm(msg)
		}
		if m.enteringPassphrase {
			return m.handlePassphraseKey(msg)
		}
		return m.handleKey(msg)
	case tea.PasteMsg:
		// Bracketed-paste is only meaningful in the editor's text fields, the
		// `/` filter, and the passphrase modal; in the plain list view there is
		// nothing to paste into, so it is a no-op.
		if m.editor != nil {
			cmd := m.editor.update(msg)
			if m.editor.done {
				m.editor = nil
			}
			return m, cmd
		}
		if m.enteringPassphrase {
			var cmd tea.Cmd
			m.passphraseInput, cmd = m.passphraseInput.Update(msg)
			return m, cmd
		}
		if m.filtering {
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			(&m).clampCursor()
			return m, cmd
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.handleFilterKey(k)
	}
	if m.confirmQuit {
		return m.handleConfirm(k)
	}
	// A filter that has been applied (enter) but is no longer being typed:
	// `esc` clears it and restores the full list; `/` re-opens the input to
	// edit the query. Everything else (navigate, toggle, edit, …) acts on the
	// filtered view. The confirm-quit modal above takes precedence over esc.
	if m.filter.Value() != "" {
		switch k.String() {
		case "esc":
			m.filter.SetValue("")
			(&m).clampCursor()
			return m, nil
		case "/":
			m.filtering = true
			return m, m.filter.Focus()
		}
	}
	switch k.String() {
	case "q", "ctrl+c":
		if m.handoffing {
			return m, nil
		}
		if m.attach || !m.hasLiveTubers() {
			m.quit = true
			return m, tea.Quit
		}
		m.confirmQuit = true
		return m, nil
	case "esc", "?":
		m.help = !m.help
		return m, nil
	case "/":
		m.filtering = true
		return m, m.filter.Focus()
	case "up", "k":
		(&m).moveCursor(-1)
	case "down", "j":
		(&m).moveCursor(1)
	case "space":
		if m.hasCurrent() && m.list[m.cursor].PendingHost != "" {
			m.confirmAccept = true
			m.acceptTarget = m.list[m.cursor].Name
			return m, nil
		}
		(&m).toggleCurrent()
	case "p":
		if m.hasCurrent() && m.list[m.cursor].PendingPassphrase != "" {
			return m.openPassphraseModal(m.list[m.cursor].Name)
		}
	case "r":
		(&m).restartCurrent()
	case "a":
		(&m).enableAll()
	case "x":
		(&m).disableAll()
	case "R":
		_ = m.ctrl.Reload()
		m.list = m.ctrl.List()
		m.clampCursor()
	case "e":
		if m.hasCurrent() {
			ed, cmd := openEditor(m.ctrl, true, m.list[m.cursor].Name, m.width, m.height)
			m.editor = ed
			return m, cmd
		}
	case "n":
		ed, cmd := openEditor(m.ctrl, false, "", m.width, m.height)
		m.editor = ed
		return m, cmd
	case "C":
		if m.hasCurrent() {
			ed, cmd := openDuplicateEditor(m.ctrl, m.list[m.cursor].Name, m.width, m.height)
			m.editor = ed
			return m, cmd
		}
	case "d":
		if m.hasCurrent() {
			m.confirmDelete = true
			m.deleteTarget = m.list[m.cursor].Name
		}
	case "l":
		if m.hasCurrent() {
			m.logs = newLogsView(m.ctrl, m.list[m.cursor].Name, m.width, m.height)
		}
	}
	return m, nil
}

// handleDeleteConfirm dispatches the "delete tuber?" modal keys. y deletes
// (and stops the tuber via the engine reload); n/enter/esc cancel.
func (m Model) handleDeleteConfirm(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y":
		name := m.deleteTarget
		m.confirmDelete = false
		m.deleteTarget = ""
		_ = m.ctrl.DeleteTuber(name)
		m.list = m.ctrl.List()
		m.clampCursor()
	case "n", "enter", "esc":
		m.confirmDelete = false
		m.deleteTarget = ""
	}
	return m, nil
}

// handleAcceptConfirm dispatches the "accept unknown host key?" modal keys.
// y/a appends the key (Controller.AcceptHost) and restarts the tuber;
// n/enter/esc dismiss the modal without changing anything. Phase 11 TOFU.
func (m Model) handleAcceptConfirm(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y", "a":
		name := m.acceptTarget
		m.confirmAccept = false
		m.acceptTarget = ""
		_ = m.ctrl.AcceptHost(name)
		m.list = m.ctrl.List()
	case "n", "enter", "esc":
		// Record the dismissal so the tick auto-open does not re-pop the same
		// host-key prompt; a manual space still reopens it.
		m.dismissedPending = pendingKeyForName(m.list, m.acceptTarget)
		m.confirmAccept = false
		m.acceptTarget = ""
	}
	return m, nil
}

// handlePassphraseKey owns the identity-passphrase modal (Phase 19): printable
// keys edit the masked input; enter submits via Controller.AcceptPassphrase
// (the blocked dial wakes on the store; the modal auto-closes once Status.
// PendingPassphrase clears — see the tick handler — or stays open with a retry
// hint on a wrong passphrase); esc cancels.
func (m Model) handlePassphraseKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		pass := m.passphraseInput.Value()
		name := m.passphraseTarget
		_ = m.ctrl.AcceptPassphrase(name, pass)
		m.passphraseInput.SetValue("")
		m.passphraseAttempts++
		m.list = m.ctrl.List()
		// Re-arm the cursor blink in case the dial rejects it and the modal
		// stays open for another attempt.
		return m, m.passphraseInput.Focus()
	case "esc":
		// Record which prompt was dismissed so the tick auto-open does not
		// immediately re-pop it; a manual space still reopens on demand.
		m.dismissedPending = pendingKeyForName(m.list, m.passphraseTarget)
		m.enteringPassphrase = false
		m.passphraseTarget = ""
		m.passphraseAttempts = 0
		m.passphraseInput.SetValue("")
		return m, nil
	}
	var cmd tea.Cmd
	m.passphraseInput, cmd = m.passphraseInput.Update(k)
	return m, cmd
}

// pendingPassphraseFor reports whether the tuber named name currently has a
// pending passphrase need in the status snapshot. Drives the modal auto-close.
func pendingPassphraseFor(list []controller.Status, name string) bool {
	for _, s := range list {
		if s.Name == name {
			return s.PendingPassphrase != ""
		}
	}
	return false
}

// pendingKey returns a stable identifier for whatever prompt a tuber is
// blocked on (a passphrase path or a host-key line), or "" when it is not
// blocked. Used so a dismissed prompt is not auto-reopened until it changes.
func pendingKey(s controller.Status) string {
	if s.PendingPassphrase != "" {
		return "pp:" + s.PendingPassphrase
	}
	if s.PendingHostLine != "" {
		return "hk:" + s.PendingHostLine
	}
	return ""
}

// pendingKeyForName looks up pendingKey for a tuber by name in a snapshot.
func pendingKeyForName(list []controller.Status, name string) string {
	for _, s := range list {
		if s.Name == name {
			return pendingKey(s)
		}
	}
	return ""
}

// isBusy reports whether the user is mid-interaction with something that an
// auto-opened prompt would interrupt (another modal, the editor/logs screens,
// the `/` filter, the help overlay, or an in-flight daemon hand-off).
func (m Model) isBusy() bool {
	return m.editor != nil || m.logs != nil || m.filtering || m.confirmDelete ||
		m.confirmQuit || m.confirmAccept || m.enteringPassphrase || m.handoffing || m.help
}

// autoOpenIfPending surfaces a pending passphrase / unknown-host prompt for the
// tuber under the cursor without requiring a second keypress (Phase 19 UX). It
// is a no-op when the user is busy or has dismissed this exact prompt. Returns
// a command (the masked-input blink) when it opens the passphrase modal.
func (m Model) autoOpenIfPending() (Model, tea.Cmd) {
	if m.isBusy() || !m.hasCurrent() {
		return m, nil
	}
	s := m.list[m.cursor]
	key := pendingKey(s)
	if key == "" || key == m.dismissedPending {
		return m, nil
	}
	if s.PendingPassphrase != "" {
		return m.openPassphraseModal(s.Name)
	}
	m.confirmAccept = true
	m.acceptTarget = s.Name
	return m, nil
}

// openPassphraseModal arms the identity-passphrase modal for the named tuber
// (resetting the masked input and the attempt counter) and returns the
// masked-input focus command. Shared by the manual `p` affordance and the tick
// auto-open. Phase 30.
func (m Model) openPassphraseModal(name string) (Model, tea.Cmd) {
	m.enteringPassphrase = true
	m.passphraseTarget = name
	m.passphraseAttempts = 0
	m.passphraseInput.SetValue("")
	return m, m.passphraseInput.Focus()
}

// openEditor builds the tuber editor form. For edit mode the current tuber
// is fetched via Config() (the daemon owns the raw fields; Status only has the
// resolved local address). Returns a nil editor if the config can't be read.
func openEditor(ctrl controller.Controller, edit bool, selected string, width, height int) (*tuberEditor, tea.Cmd) {
	cfg, err := ctrl.Config()
	if err != nil || cfg == nil {
		return nil, nil
	}
	var names []string
	var existing config.Tuber
	for _, t := range cfg.Tubers {
		names = append(names, t.Name)
		if edit && t.Name == selected {
			existing = t
		}
	}
	mode := modeNew
	if edit {
		mode = modeEdit
	}
	e := newTuberEditor(mode, existing, names, ctrl)
	e.width, e.height = width, height
	return e, e.setFocus(fName)
}

// openDuplicateEditor opens the Phase 10 editor in create mode, prefilled from
// the selected tuber under a fresh "<name>-copy" name with Enabled=false. It
// commits via AddTuber (not UpdateTuber) when saved, so the source tuber is
// untouched — the "same SSH host, a second local port" convenience becomes a
// keystroke plus a small edit. Returns a nil editor when the config can't be
// read or the source is no longer present (mirrors openEditor's cfg-error path).
func openDuplicateEditor(ctrl controller.Controller, selected string, width, height int) (*tuberEditor, tea.Cmd) {
	cfg, err := ctrl.Config()
	if err != nil || cfg == nil {
		return nil, nil
	}
	var names []string
	var src config.Tuber
	found := false
	for _, t := range cfg.Tubers {
		names = append(names, t.Name)
		if t.Name == selected {
			src = t
			found = true
		}
	}
	if !found {
		return nil, nil
	}
	src.Name = freshName(selected, names)
	src.Enabled = false
	e := newTuberEditor(modeNew, src, names, ctrl)
	e.original = ""
	e.width, e.height = width, height
	return e, e.setFocus(fName)
}

// freshName returns a unique name for a duplicate of base: base+"-copy", or
// base+"-copy-N" (N=2,3,…) when "-copy" is already taken. The scheme keeps the
// result inside validEditorName's alphabet ([a-zA-Z0-9_-]).
func freshName(base string, existing []string) string {
	candidate := base + "-copy"
	if !containsName(existing, candidate) {
		return candidate
	}
	for i := 2; ; i++ {
		c := fmt.Sprintf("%s-copy-%d", base, i)
		if !containsName(existing, c) {
			return c
		}
	}
}

func containsName(names []string, s string) bool {
	for _, n := range names {
		if n == s {
			return true
		}
	}
	return false
}

// handleFilterKey owns the `/`-input: every key but the control keys goes to
// the text field (so letters, digits, backspace, cursor movement all edit the
// query). `esc` clears and closes; `enter` closes keeping the query applied;
// `ctrl+c` still quits.
func (m Model) handleFilterKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.filtering = false
		m.filter.SetValue("")
		m.filter.Blur()
		(&m).clampCursor()
		return m, nil
	case "enter":
		m.filtering = false
		m.filter.Blur()
		(&m).clampCursor()
		return m, nil
	case "ctrl+c":
		if m.attach || !m.hasLiveTubers() {
			m.quit = true
			return m, tea.Quit
		}
		m.confirmQuit = true
		m.filtering = false
		m.filter.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	(&m).clampCursor()
	return m, cmd
}

// handleConfirm dispatches the "leave running in background?" modal keys.
func (m Model) handleConfirm(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y":
		m.confirmQuit = false
		m.handoffing = true
		m.handoffErr = ""
		return m, m.handoffCmd()
	case "n", "enter":
		m.confirmQuit = false
		m.quit = true
		return m, tea.Quit
	case "esc":
		m.confirmQuit = false
		return m, nil
	}
	return m, nil
}

func (m Model) handoffCmd() tea.Cmd {
	ctrl := m.ctrl
	return func() tea.Msg {
		return handoffDoneMsg{err: handoffToDaemon(ctrl, m.cfgPath)}
	}
}

func (m Model) hasLiveTubers() bool {
	for _, s := range m.list {
		switch s.State {
		case controller.Connecting, controller.Connected, controller.Reconnecting:
			return true
		}
	}
	return false
}

// matches reports whether a tuber passes the `/` filter. The query is matched
// fzf-style (case-insensitive subsequence via fuzzysearch) against the name,
// type and endpoint; an exact substring still hits as a fallback so an
// unfuzzy-but-contiguous token keeps matching (Phase 20). An empty query
// matches everything.
func (m Model) matches(s controller.Status) bool {
	q := strings.ToLower(m.filter.Value())
	if q == "" {
		return true
	}
	if fuzzy.MatchFold(q, s.Name) || fuzzy.MatchFold(q, s.Type) || fuzzy.MatchFold(q, s.Endpoint()) {
		return true
	}
	// Substring fallback: every contiguous match is also a subsequence, so in
	// practice fuzzy.MatchFold already covers it — kept defensively so the
	// filter degrades to the pre-Phase-20 behaviour if the matcher ever
	// surprises us (e.g. on Unicode case-folding edge cases).
	return strings.Contains(strings.ToLower(s.Name), q) ||
		strings.Contains(strings.ToLower(s.Type), q) ||
		strings.Contains(strings.ToLower(s.Endpoint()), q)
}

// visibleCount returns how many tubers currently pass the filter, for the
// filter line's matched/total count.
func (m Model) visibleCount() int {
	n := 0
	for _, s := range m.list {
		if m.matches(s) {
			n++
		}
	}
	return n
}

// moveCursor advances the cursor by delta, skipping rows hidden by the filter.
func (m *Model) moveCursor(delta int) {
	for i := m.cursor + delta; i >= 0 && i < len(m.list); i += delta {
		if m.matches(m.list[i]) {
			m.cursor = i
			return
		}
	}
}

func (m *Model) clampCursor() {
	if len(m.list) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.list) {
		m.cursor = len(m.list) - 1
	}
	// Keep the cursor on a visible (matching) row: if it sits on a hidden one,
	// advance to the next visible, else scan backward.
	for i := m.cursor; i < len(m.list); i++ {
		if m.matches(m.list[i]) {
			m.cursor = i
			return
		}
	}
	for i := m.cursor; i >= 0; i-- {
		if m.matches(m.list[i]) {
			m.cursor = i
			return
		}
	}
}

func (m *Model) toggleCurrent() {
	if !m.hasCurrent() {
		return
	}
	s := m.list[m.cursor]
	if s.State == controller.Off {
		_ = m.ctrl.Enable(s.Name)
	} else {
		_ = m.ctrl.Disable(s.Name)
	}
	m.list = m.ctrl.List()
}

func (m *Model) restartCurrent() {
	if !m.hasCurrent() {
		return
	}
	_ = m.ctrl.Restart(m.list[m.cursor].Name)
	m.list = m.ctrl.List()
}

func (m *Model) enableAll() {
	for _, s := range m.ctrl.List() {
		if s.State == controller.Off {
			_ = m.ctrl.Enable(s.Name)
		}
	}
	m.list = m.ctrl.List()
}

func (m *Model) disableAll() {
	for _, s := range m.ctrl.List() {
		if s.State != controller.Off {
			_ = m.ctrl.Disable(s.Name)
		}
	}
	m.list = m.ctrl.List()
}

func (m *Model) hasCurrent() bool {
	return len(m.list) > 0 && m.cursor >= 0 && m.cursor < len(m.list) && m.matches(m.list[m.cursor])
}
