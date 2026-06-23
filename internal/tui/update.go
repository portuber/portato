package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/controller"
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
		if m.logs != nil {
			m.logs.refresh()
		}
		return m, waitForChange(m.ctrl.Changes())
	case redrawTickMsg:
		// Local re-render tick: refreshes time-based display fields (uptime)
		// without fetching from the controller. Re-arm; the change-waiter is
		// an independent pending command. See redrawTickMsg in model.go.
		// The logs screen (transient modal) does re-fetch here — acceptable:
		// it is not the idle tunnel-status path Phase 9 made push-driven.
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
		return m.handleKey(msg)
	case tea.PasteMsg:
		// Bracketed-paste is only meaningful in the editor's text fields and
		// the `/` filter; in the plain list view there is nothing to paste
		// into, so it is a no-op.
		if m.editor != nil {
			cmd := m.editor.update(msg)
			if m.editor.done {
				m.editor = nil
			}
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
		if m.attach || !m.hasLiveTunnels() {
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

// handleDeleteConfirm dispatches the "delete tunnel?" modal keys. y deletes
// (and stops the tunnel via the engine reload); n/enter/esc cancel.
func (m Model) handleDeleteConfirm(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y":
		name := m.deleteTarget
		m.confirmDelete = false
		m.deleteTarget = ""
		_ = m.ctrl.DeleteTunnel(name)
		m.list = m.ctrl.List()
		m.clampCursor()
	case "n", "enter", "esc":
		m.confirmDelete = false
		m.deleteTarget = ""
	}
	return m, nil
}

// handleAcceptConfirm dispatches the "accept unknown host key?" modal keys.
// y/a appends the key (Controller.AcceptHost) and restarts the tunnel;
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
		m.confirmAccept = false
		m.acceptTarget = ""
	}
	return m, nil
}

// openEditor builds the tunnel editor form. For edit mode the current tunnel
// is fetched via Config() (the daemon owns the raw fields; Status only has the
// resolved local address). Returns a nil editor if the config can't be read.
func openEditor(ctrl controller.Controller, edit bool, selected string, width, height int) (*tunnelEditor, tea.Cmd) {
	cfg, err := ctrl.Config()
	if err != nil || cfg == nil {
		return nil, nil
	}
	var names []string
	var existing config.Tunnel
	for _, t := range cfg.Tunnels {
		names = append(names, t.Name)
		if edit && t.Name == selected {
			existing = t
		}
	}
	mode := modeNew
	if edit {
		mode = modeEdit
	}
	e := newTunnelEditor(mode, existing, names, ctrl)
	e.width, e.height = width, height
	return e, e.setFocus(fName)
}

// openDuplicateEditor opens the Phase 10 editor in create mode, prefilled from
// the selected tunnel under a fresh "<name>-copy" name with Enabled=false. It
// commits via AddTunnel (not UpdateTunnel) when saved, so the source tunnel is
// untouched — the "same SSH host, a second local port" convenience becomes a
// keystroke plus a small edit. Returns a nil editor when the config can't be
// read or the source is no longer present (mirrors openEditor's cfg-error path).
func openDuplicateEditor(ctrl controller.Controller, selected string, width, height int) (*tunnelEditor, tea.Cmd) {
	cfg, err := ctrl.Config()
	if err != nil || cfg == nil {
		return nil, nil
	}
	var names []string
	var src config.Tunnel
	found := false
	for _, t := range cfg.Tunnels {
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
	e := newTunnelEditor(modeNew, src, names, ctrl)
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
		if m.attach || !m.hasLiveTunnels() {
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

func (m Model) hasLiveTunnels() bool {
	for _, s := range m.list {
		switch s.State {
		case controller.Connecting, controller.Connected, controller.Reconnecting:
			return true
		}
	}
	return false
}

// matches reports whether a tunnel passes the `/` filter (case-insensitive
// substring over name / type / endpoint). An empty query matches everything.
func (m Model) matches(s controller.Status) bool {
	q := strings.ToLower(m.filter.Value())
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(s.Name), q) ||
		strings.Contains(strings.ToLower(s.Type), q) ||
		strings.Contains(strings.ToLower(s.Endpoint()), q)
}

// visibleCount returns how many tunnels currently pass the filter, for the
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
