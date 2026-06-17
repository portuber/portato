package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kipkaev55/portato/internal/controller"
)

const (
	colName     = 20
	colType     = 7
	colEndpoint = 32
	colStatus   = 14
)

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.confirmQuit {
		return m.centered(m.confirmQuitView())
	}
	if m.handoffing {
		return m.centered(modeStyle.Render("Starting daemon…"))
	}
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	b.WriteString(m.table())
	b.WriteString("\n\n")
	b.WriteString(m.footer())
	if m.help {
		b.WriteString("\n\n")
		b.WriteString(m.helpBlock())
	}
	return b.String()
}

// centered overlays a single block in the middle of the screen. Width/height
// are 0 before the first WindowSizeMsg (and in unit tests), in which case the
// block is returned as-is.
func (m Model) centered(block string) string {
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, block)
	}
	return block
}

func (m Model) header() string {
	left := titleStyle.Render("Portato") + " " + dimStyle.Render("— Port Forwarding")
	right := modeStyle.Render("mode: " + m.mode)
	return joinRight(left, right, m.width)
}

func (m Model) table() string {
	if len(m.list) == 0 {
		return dimStyle.Render("no tunnels — add one to config and press R to reload")
	}
	var b strings.Builder
	b.WriteString(columnHeader())
	b.WriteString("\n")
	for i, s := range m.list {
		b.WriteString(m.row(i, s))
		b.WriteString("\n")
	}
	return b.String()
}

func columnHeader() string {
	return headerStyle.Render(
		"  " +
			pad("NAME", colName) +
			pad("TYPE", colType) +
			pad("LOCAL → REMOTE", colEndpoint) +
			pad("STATUS", colStatus) +
			"UPTIME",
	)
}

func (m Model) row(i int, s controller.Status) string {
	selected := i == m.cursor
	indicator := "○"
	if s.State != controller.Off {
		indicator = "●"
	}
	endpoint := s.Local + " → " + s.Remote
	status := stateLabel(s.State)
	if s.Error != "" {
		status += " " + dimStyle.Render(truncate(s.Error, 18))
	}
	cells := indicator + " " +
		pad(s.Name, colName) +
		pad(s.Type, colType) +
		pad(endpoint, colEndpoint) +
		pad(status, colStatus) +
		uptime(s)
	if selected {
		return selectedStyle.Render(cells)
	}
	return cells
}

func stateLabel(s controller.State) string {
	style := stateStyle[s]
	switch s {
	case controller.Off:
		return style.Render("off")
	case controller.Connecting:
		return style.Render("connecting")
	case controller.Connected:
		return style.Render("connected")
	case controller.Reconnecting:
		return style.Render("reconnecting")
	case controller.Error:
		return style.Render("error")
	default:
		return style.Render("unknown")
	}
}

func uptime(s controller.Status) string {
	if s.State != controller.Connected {
		return ""
	}
	d := s.Uptime()
	if d <= 0 {
		return ""
	}
	return formatUptime(d)
}

func formatUptime(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours()/24), int(d.Hours())%24)
	}
}

func (m Model) footer() string {
	return footerStyle.Render("↑↓/jk move · space toggle · r restart · a/x all · R reload · ? help · q quit")
}

func (m Model) helpBlock() string {
	lines := []string{
		helpTitle.Render("Help"),
		"",
		"↑ / k        move cursor up",
		"↓ / j        move cursor down",
		"space        toggle selected tunnel (on/off)",
		"r            restart selected tunnel",
		"a            enable all tunnels",
		"x            disable all tunnels",
		"R            reload config from disk",
		"? / esc      toggle this help",
		"q / ctrl+c   quit (stops all tunnels)",
	}
	return helpPanel.Render(strings.Join(lines, "\n"))
}

// confirmQuitView renders the "leave running in background?" modal shown when
// quitting a standalone TUI that still has live tunnels.
func (m Model) confirmQuitView() string {
	n := 0
	for _, s := range m.list {
		switch s.State {
		case controller.Connecting, controller.Connected, controller.Reconnecting:
			n++
		}
	}
	line := fmt.Sprintf("%d tunnel(s) active.\nLeave them running in the background? [y/N]", n)
	return modalStyle.Render(line)
}

func joinRight(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap <= 0 {
		return left + "  " + right
	}
	return left + strings.Repeat(" ", gap) + right
}

func pad(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return s + " "
	}
	return s + strings.Repeat(" ", n-w)
}

func truncate(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
