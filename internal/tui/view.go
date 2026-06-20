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
	colEndpoint = 48
	colStatus   = 14
	gutter      = "  "
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
			pad("NAME", colName) + gutter +
			pad("TYPE", colType) + gutter +
			pad("ENDPOINT", colEndpoint) + gutter +
			pad("STATUS", colStatus) + gutter +
			"UPTIME",
	)
}

func (m Model) row(i int, s controller.Status) string {
	selected := i == m.cursor
	indicator := "○"
	if s.State != controller.Off {
		indicator = "●"
	}
	endpoint := fitEndpoint(s.Endpoint(), colEndpoint)
	status := stateLabel(s.State)
	if s.Error != "" {
		status += " " + dimStyle.Render(truncate(s.Error, 18))
	}
	cells := indicator + " " +
		pad(s.Name, colName) + gutter +
		pad(s.Type, colType) + gutter +
		pad(endpoint, colEndpoint) + gutter +
		pad(status, colStatus) + gutter +
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

// fitEndpoint shrinks an endpoint to at most max display cells, keeping the
// local address, the direction arrow and the remote :port, and middle-
// truncating only the remote host. Endpoints without a remote host (the
// dynamic "⇄ *") and anything that still does not fit fall back to a simple
// ellipsis truncate. This keeps the ENDPOINT column a fixed width so STATUS /
// UPTIME line up across rows regardless of host length.
func fitEndpoint(s string, max int) string {
	if lipgloss.Width(s) <= max {
		return s
	}
	for _, sep := range []string{" → ", " ← "} {
		if i := strings.Index(s, sep); i >= 0 {
			left, right := s[:i+len(sep)], s[i+len(sep):]
			if budget := max - lipgloss.Width(left); budget >= 4 {
				return left + fitHostPort(right, budget)
			}
		}
	}
	return truncate(s, max)
}

// fitHostPort fits a "host:port" (or bare host) into budget cells, preserving
// the :port (splitting on the last colon, so "[::1]:3306" keeps its brackets)
// and middle-truncating the host. When there is no room for host+port, the port
// tail is kept with a leading ellipsis.
func fitHostPort(hp string, budget int) string {
	if lipgloss.Width(hp) <= budget {
		return hp
	}
	host, port := hp, ""
	if i := strings.LastIndex(hp, ":"); i >= 0 {
		host, port = hp[:i], hp[i:] // port keeps the ":"
	}
	avail := budget - lipgloss.Width(port)
	if avail <= 1 {
		return truncate("…"+port, budget)
	}
	return middleTruncate(host, avail) + port
}

// middleTruncate shrinks s to at most width cells by inserting a single "…"
// between the kept head and tail of the string.
func middleTruncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	rs := []rune(s)
	keep := width - 1 // one cell reserved for "…"
	left := keep / 2
	return string(rs[:left]) + "…" + string(rs[len(rs)-(keep-left):])
}
