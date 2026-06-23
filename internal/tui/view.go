package tui

import (
	"fmt"
	"image/color"
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
	sideMargin  = 1
)

func (m Model) View() tea.View {
	content := m.render()
	if surfaceBg != nil && m.width > 0 && m.height > 0 {
		content = fillBg(content, surfaceBg, m.width, m.height)
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// fillBg paints bg across the whole TUI surface: each line is padded to width
// and the content is padded to height, all with bg-coloured cells. This turns
// the light theme into a real "light mode" (a light page) instead of just
// recoloured glyphs on the terminal's own background. A no-op when bg is nil
// or the dimensions are unknown (before the first WindowSizeMsg).
//
// The implementation is reset-aware: a styled run ends with an ANSI reset, and
// the raw cells after it (spaces glued between segments, plain log text, the
// viewport's own padding) would otherwise fall back to the terminal's default
// background. fillBg re-asserts the background after every reset and paints the
// width/height padding, so no default-bg cells leak through.
func fillBg(content string, bg color.Color, width, height int) string {
	if bg == nil || width <= 0 || height <= 0 {
		return content
	}
	bgSeq := bgSequence(bg)
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		painted := paintLine(line, bgSeq)
		if pad := width - lipgloss.Width(painted); pad > 0 {
			painted += bgSeq + strings.Repeat(" ", pad)
		}
		lines[i] = painted
	}
	for len(lines) < height {
		lines = append(lines, bgSeq+strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

// bgSequence returns the profiled SGR string that sets bg as the background,
// with no trailing reset. It is obtained by rendering a single marker through a
// bg-only lipgloss style and taking the prefix in front of the marker, so the
// emitted sequence matches the active colour profile (truecolor/256/ansi).
func bgSequence(bg color.Color) string {
	const marker = "Z"
	out := lipgloss.NewStyle().Background(bg).Render(marker)
	i := strings.Index(out, marker)
	if i <= 0 {
		return ""
	}
	return strings.TrimPrefix(out[:i], "\x1b[0m")
}

// paintLine prepends the background SGR and re-asserts it after every reset in
// the line, so cells that follow a styled run keep the surface background.
func paintLine(line, bgSeq string) string {
	if bgSeq == "" {
		return line
	}
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+bgSeq)
	line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+bgSeq)
	return bgSeq + line
}

func (m Model) render() string {
	if m.logs != nil {
		return m.logs.view()
	}
	if m.editor != nil {
		return m.centered(m.editor.view())
	}
	if m.confirmDelete {
		return m.centered(m.confirmDeleteView())
	}
	if m.confirmAccept {
		return m.centered(m.confirmAcceptView())
	}
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
	return insetLines(b.String(), sideMargin)
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
	return joinRight(left, right, m.width-2*sideMargin)
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
		"    " +
			pad("NAME", colName) + gutter +
			pad("TYPE", colType) + gutter +
			pad("ENDPOINT", colEndpoint) + gutter +
			pad("STATUS", colStatus) + gutter +
			"UPTIME",
	)
}

func (m Model) row(i int, s controller.Status) string {
	selected := i == m.cursor
	endpoint := fitEndpoint(s.Endpoint(), colEndpoint)
	status := stateLabel(s.State)
	if s.Error != "" {
		status += " " + dimStyle.Render(truncate(s.Error, 18))
	}

	name, typ, ep, up := s.Name, s.Type, endpoint, uptime(s)
	if selected {
		// Selection is marked by the ❯ cursor glyph; the plain text cells are
		// bolded for emphasis. The cells are styled individually (not wrapped
		// in one outer style) because the indicator is already colour-rendered
		// and a nested ANSI reset would otherwise drop the outer styling after
		// it. Each plain cell has no inner sequences, so bolding is reliable.
		name = selectedStyle.Render(name)
		typ = selectedStyle.Render(typ)
		ep = selectedStyle.Render(ep)
		if up != "" {
			up = selectedStyle.Render(up)
		}
	}

	cells := indicator(s) + " " +
		pad(name, colName) + gutter +
		pad(typ, colType) + gutter +
		pad(ep, colEndpoint) + gutter +
		pad(status, colStatus) + gutter +
		up

	cursor := " "
	if selected {
		cursor = cursorStyle.Render("❯")
	}
	return cursor + " " + cells
}

// indicator returns the leading status glyph, coloured by state. Error uses a
// distinct ✗ so a failed tunnel cannot be mistaken for a connected one — the
// old "● for everything not Off" made an errored tunnel look live.
func indicator(s controller.Status) string {
	switch s.State {
	case controller.Off:
		return stateStyle[controller.Off].Render("○")
	case controller.Error:
		return stateStyle[controller.Error].Render("✗")
	default:
		return stateStyle[s.State].Render("●")
	}
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
	return footerStyle.Render("↑↓/jk move · space toggle · r restart · a/x all · e edit · n new · d delete · l logs · R reload · ? help · q quit")
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
		"e            edit the selected tunnel",
		"n            create a new tunnel",
		"d            delete the selected tunnel",
		"l            view the selected tunnel's logs",
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

// confirmDeleteView renders the "delete tunnel?" modal. Deleting stops an
// active tunnel (via the engine reload) and removes it from the config.
func (m Model) confirmDeleteView() string {
	line := fmt.Sprintf("Delete tunnel %q?\nThis stops it if active and removes it from the config. [y/N]", m.deleteTarget)
	return modalStyle.Render(line)
}

// confirmAcceptView renders the Phase 11 TOFU modal: the tunnel is blocked by
// an unknown SSH host key, and the user can accept it (append to known_hosts
// and restart) or cancel.
func (m Model) confirmAcceptView() string {
	host, fp := m.acceptTarget, ""
	for _, s := range m.list {
		if s.Name == m.acceptTarget {
			host = s.PendingHost
			fp = s.PendingFingerprint
			break
		}
	}
	line := fmt.Sprintf(
		"Unknown host key for %s\nhost: %s\nfingerprint: %s\n[y] accept & restart  ·  [n/esc] cancel",
		m.acceptTarget, host, fp,
	)
	return modalStyle.Render(line)
}

func joinRight(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap <= 0 {
		return left + "  " + right
	}
	return left + strings.Repeat(" ", gap) + right
}

// insetLines prefixes every line with margin cells, giving the TUI a small left
// edge so the content does not hug the terminal border. The matching right edge
// comes from the header right-aligning into width-2*margin and, for the light
// theme, fillBg painting the trailing columns.
func insetLines(content string, margin int) string {
	if margin <= 0 {
		return content
	}
	pad := strings.Repeat(" ", margin)
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

func pad(s string, n int) string {
	w := lipgloss.Width(s)
	if w > n {
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
