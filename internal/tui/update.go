package tui

import (
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
		return m.handleKey(msg)
	case tea.PasteMsg:
		// Bracketed-paste is only meaningful in the editor's text fields; in
		// the list view there is nothing to paste into, so it is a no-op.
		if m.editor != nil {
			cmd := m.editor.update(msg)
			if m.editor.done {
				m.editor = nil
			}
			return m, cmd
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.confirmQuit {
		return m.handleConfirm(k)
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
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.list)-1 {
			m.cursor++
		}
	case "space":
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
		return handoffDoneMsg{err: handoffToDaemon(ctrl, m.cfgPath, m.socketURI)}
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

func (m *Model) clampCursor() {
	if m.cursor < 0 {
		m.cursor = 0
	}
	if n := len(m.list); m.cursor >= n {
		m.cursor = n - 1
		if m.cursor < 0 {
			m.cursor = 0
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
	return len(m.list) > 0 && m.cursor >= 0 && m.cursor < len(m.list)
}
