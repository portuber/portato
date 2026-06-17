package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/kipkaev55/portato/internal/controller"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		if m.handoffing {
			return m, nil
		}
		m.list = m.ctrl.List()
		m.clampCursor()
		return m, waitForChange(m.ctrl.Changes())
	case handoffDoneMsg:
		m.handoffing = false
		m.quit = true
		if msg.err != nil {
			m.handoffErr = msg.err.Error()
		}
		return m, tea.Quit
	case tea.KeyPressMsg:
		return m.handleKey(msg)
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
	}
	return m, nil
}

// handleConfirm dispatches the "leave running in background?" modal keys.
func (m Model) handleConfirm(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y":
		m.confirmQuit = false
		m.handoffing = true
		m.handoffErr = ""
		return m, m.handoffCmd()
	case "n", "esc", "enter":
		m.confirmQuit = false
		m.quit = true
		return m, tea.Quit
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
