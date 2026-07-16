package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/portuber/portato/internal/controller"
	"github.com/portuber/portato/internal/logo"
)

const (
	colName      = 20
	colType      = 7
	colEndpoint  = 48
	colStatus    = 14
	gutter       = "  "
	sideMargin   = 1
	minName      = 12
	maxName      = 40
	uptimeBudget = 7
	// splashMinH is the terminal-height gate for the big logo: below it the
	// empty-list splash and the help overlay omit the logo and show text only,
	// so a short terminal never breaks the layout.
	splashMinH = 18
	// splashLogoW is the cell width of the compact potato art; splashWordmarkW
	// is the width of the "potato + PORTATO" wordmark. The splash shows the
	// wordmark when the terminal is wide enough, otherwise the compact potato.
	splashLogoW     = 28
	splashWordmarkW = 70
)

// View renders a frame. For the light theme the surface is established two ways
// (belt for non-honoring terminals, suspenders for honoring ones):
//   - fillBg cell-paints every content line with #FAFAFA (covers terminals that
//     ignore OSC 11 set, e.g. iTerm2). The v2 cell renderer strips whitespace-only
//     lines, so render() emits no internal blank separators — the content block is
//     one solid surface; the area below it to full height stays terminal-bg.
//   - View.BackgroundColor asks the renderer to set the terminal's own background
//     (OSC 11) to #FAFAFA, which covers the content block AND the below-content
//     area on terminals that honor it (e.g. Terminal.app). The bg is baked out of
//     the styles themselves, so there are no per-glyph #FAFAFA boxes when the
//     terminal's bg is not #FAFAFA.
//
// Dark/mono leave BackgroundColor nil so the user's terminal theme shows through
// (transparent). The terminal's prior background is restored on normal exit.
func (m Model) View() tea.View {
	content := m.render()
	if m.pal.surfaceBg != nil && m.width > 0 && m.height > 0 {
		content = fillBg(content, m.pal.surfaceBg, m.width, m.height)
	}
	v := tea.NewView(content)
	v.AltScreen = true
	if m.pal.surfaceBg != nil {
		v.BackgroundColor = m.pal.surfaceBg
	}
	return v
}

// fillBg paints bg across the content lines: every line is padded to width with
// bg-coloured cells. It is reset-aware — re-asserting the background after every
// ANSI reset — so the raw cells between styled runs keep the surface colour. It
// does NOT pad to full screen height: those appended lines are whitespace-only
// and the v2 cell renderer strips them anyway (render() emits no internal blank
// separators for the same reason). The area below the content block is covered
// by View.BackgroundColor on terminals that honour OSC 11 set, and stays the
// terminal's own background elsewhere (accepted).
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
	return strings.Join(lines, "\n")
}

func bgSequence(bg color.Color) string {
	const marker = "Z"
	out := lipgloss.NewStyle().Background(bg).Render(marker)
	i := strings.Index(out, marker)
	if i <= 0 {
		return ""
	}
	return strings.TrimPrefix(out[:i], "\x1b[0m")
}

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
	if m.enteringPassphrase {
		if m.passphraseConnecting {
			return m.centered(m.sproutingView())
		}
		return m.centered(m.passphraseView())
	}
	if m.enteringPassword {
		if m.passwordConnecting {
			return m.centered(m.sproutingView())
		}
		return m.centered(m.passwordView())
	}
	if m.confirmQuit {
		return m.centered(m.confirmQuitView())
	}
	if m.handoffing {
		return m.centered(m.pal.mode.Render("Starting daemon…"))
	}
	// Section separator: dark/mono (transparent surface) get a blank line for
	// breathing room — it is invisible there (the terminal's own background shows
	// through, same as the content). Light keeps sections adjacent: a blank
	// separator would render as the terminal's own background, a dark seam through
	// the card on terminals that ignore OSC 11 set, and OSC-11-set success is not
	// detectable, so light assumes the worst case.
	sep := "\n"
	if m.pal.surfaceBg == nil {
		sep = "\n\n"
	}
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString(sep)
	b.WriteString(m.table())
	b.WriteString(sep)
	if m.filtering || m.filter.Value() != "" {
		b.WriteString(m.filterLine())
		b.WriteString(sep)
	}
	b.WriteString(m.footer())
	if m.help {
		b.WriteString(sep)
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
	left := m.pal.title.Render("Portato")
	if logo.EmojiEnabled() {
		left = "🥔 " + left
	}
	left += " " + m.pal.dim.Render("— Port Forwarding")
	right := m.pal.mode.Render("mode: " + m.mode)
	return joinRight(left, right, m.width-2*sideMargin)
}

func (m Model) table() string {
	if len(m.list) == 0 {
		hint := m.pal.dim.Render("no tubers — add one to config and press R to reload")
		if m.height >= splashMinH {
			return m.splash(hint)
		}
		return hint
	}
	var rows []int
	for i, s := range m.list {
		if m.matches(s) {
			rows = append(rows, i)
		}
	}
	if len(rows) == 0 {
		return m.pal.dim.Render(fmt.Sprintf("no tubers match %q — esc clears", m.filter.Value()))
	}
	nameW := m.nameWidth()
	lines := make([]string, 0, len(rows)+1)
	lines = append(lines, columnHeader(m.pal, nameW))
	for _, i := range rows {
		lines = append(lines, m.row(i, m.list[i], nameW))
	}
	// No trailing newline: render() joins sections with "\n", and a trailing
	// "\n" here would create a whitespace-only separator line (which the v2
	// renderer strips, leaving a terminal-bg gap in the surface).
	return strings.Join(lines, "\n")
}

// splash renders the empty-list state: the centered logo with the hint line
// beneath it. It is shown only when the terminal is tall enough (table() gates
// on splashMinH); a short terminal gets the hint-only line. On a wide terminal
// the logo is the "potato + PORTATO" wordmark; on a narrow one it falls back to
// the compact potato. The logo is tinted with the title accent unless the
// theme is monochrome.
func (m Model) splash(hint string) string {
	mono := m.kind == themeMono
	avail := m.width - 2*sideMargin
	var art string
	if avail >= splashWordmarkW {
		art = logo.Wordmark(m.pal.title, mono)
	} else {
		art = logo.Banner(m.pal.title, mono)
	}
	if avail < splashLogoW {
		avail = splashLogoW
	}
	return centerBlock(art, avail) + "\n\n" + centerBlock(hint, avail)
}

// filterLine renders the `/`-input line: a prompt, the query (with a cursor
// while typing), and a matched/total count. Shown whenever the filter is open
// or has a query applied.
func (m Model) filterLine() string {
	count := m.pal.dim.Render(fmt.Sprintf("(%d/%d)", m.visibleCount(), len(m.list)))
	if m.filtering {
		return m.pal.body.Render("/ ") + m.filter.View() + "  " + count
	}
	return m.pal.dim.Render(fmt.Sprintf("/ %s  %s  — esc clears", m.filter.Value(), count))
}

func (m Model) nameWidth() int {
	longest := 0
	for _, s := range m.list {
		if w := lipgloss.Width(s.Name); w > longest {
			longest = w
		}
	}
	nameW := longest
	nameW = max(nameW, minName)
	nameW = min(nameW, maxName)
	if m.width == 0 {
		return colName
	}
	leading := sideMargin + 4
	avail := m.width - leading - 4*len(gutter) - colType - colEndpoint - colStatus - uptimeBudget
	avail = max(avail, minName)
	return min(nameW, avail)
}

func columnHeader(pal palette, nameW int) string {
	return pal.header.Render(
		"    " +
			pad("NAME", nameW) + gutter +
			pad("TYPE", colType) + gutter +
			pad("ENDPOINT", colEndpoint) + gutter +
			pad("STATUS", colStatus) + gutter +
			"UPTIME",
	)
}

func (m Model) row(i int, s controller.Status, nameW int) string {
	selected := i == m.cursor
	endpoint := fitEndpoint(s.Endpoint(), colEndpoint)
	status := stateLabel(m.pal, s.State)
	if s.Error != "" {
		status += " " + m.pal.dim.Render(truncate(s.Error, 18))
	}
	// Phase 19: a dial blocked on a passphrase-protected identity is in
	// Connecting with PendingPassphrase set; flag it with the key that opens
	// the prompt (the modal also auto-opens when this tuber is under the
	// cursor).
	if s.PendingPassphrase != "" {
		status += " " + m.pal.dim.Render("passphrase? (p)")
	}
	// Phase 35: a dial blocked on a password-only account is in Connecting
	// with PendingPassword set; flag it with the key that opens the prompt
	// (the modal also auto-opens when this tuber is under the cursor).
	if s.PendingPassword != "" {
		status += " " + m.pal.dim.Render("password? (o)")
	}

	name, typ, ep, up := fitName(s.Name, nameW), s.Type, endpoint, uptime(s)
	if selected {
		// Selection is marked by the ❯ cursor glyph; the plain text cells are
		// bolded for emphasis. The cells are styled individually (not wrapped
		// in one outer style) because the indicator is already colour-rendered
		// and a nested ANSI reset would otherwise drop the outer styling after
		// it. Each plain cell has no inner sequences, so bolding is reliable.
		name = m.pal.selected.Render(name)
		typ = m.pal.selected.Render(typ)
		ep = m.pal.selected.Render(ep)
		if up != "" {
			up = m.pal.selected.Render(up)
		}
	} else {
		name = m.pal.body.Render(name)
		typ = m.pal.body.Render(typ)
		ep = m.pal.body.Render(ep)
		if up != "" {
			up = m.pal.body.Render(up)
		}
	}

	cells := indicator(m.pal, s) + " " +
		pad(name, nameW) + gutter +
		pad(typ, colType) + gutter +
		pad(ep, colEndpoint) + gutter +
		pad(status, colStatus) + gutter +
		up

	cursor := " "
	if selected {
		cursor = m.pal.cursor.Render("❯")
	}
	return cursor + " " + cells
}

// indicator returns the leading status glyph, coloured by state. Error uses a
// distinct ✗ so a failed tuber cannot be mistaken for a connected one — the
// old "● for everything not Off" made an errored tuber look live.
func indicator(pal palette, s controller.Status) string {
	switch s.State {
	case controller.Off:
		return pal.state[controller.Off].Render("○")
	case controller.Error:
		return pal.state[controller.Error].Render("✗")
	case controller.Connecting, controller.Reconnecting:
		return pal.state[s.State].Render(pal.connectingGlyph)
	default:
		return pal.state[s.State].Render(pal.connectedGlyph)
	}
}

func stateLabel(pal palette, s controller.State) string {
	style := pal.state[s]
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
	return m.pal.footer.Render("↑↓/jk move · space toggle · p passphrase · o password · r restart · a/x all · e edit · n new · C duplicate · d delete · l logs · / filter · R reload · ? help · q quit")
}

func (m Model) helpBlock() string {
	hotkeys := []string{
		"↑ / k        move cursor up",
		"↓ / j        move cursor down",
		"space        toggle selected tuber (on/off)",
		"p            enter passphrase for the selected tuber",
		"o            enter SSH password for the selected tuber (also auto-opens)",
		"r            restart selected tuber",
		"a            enable all tubers",
		"x            disable all tubers",
		"e            edit the selected tuber",
		"n            create a new tuber",
		"C            duplicate the selected tuber",
		"d            delete the selected tuber",
		"l            view the selected tuber's logs",
		"/            filter the list (name/type/endpoint; esc clears)",
		"R            reload config from disk",
		"? / esc      toggle this help",
		"q / ctrl+c   quit (stops all tubers)",
	}
	title := m.pal.helpTitle.Render("Help")
	contentW := lipgloss.Width(title)
	for _, l := range hotkeys {
		if w := lipgloss.Width(l); w > contentW {
			contentW = w
		}
	}
	lines := make([]string, 0, len(hotkeys)+4)
	// Prepend the logo above the title when there is vertical room, centered
	// within the hotkey block width. Same height gate as the empty-state splash.
	if m.height >= splashMinH {
		lines = append(lines, centerBlock(logo.Banner(m.pal.title, m.kind == themeMono), contentW), "")
	}
	lines = append(lines, title, "")
	lines = append(lines, hotkeys...)
	return m.pal.helpPanel.Render(strings.Join(lines, "\n"))
}

// confirmQuitView renders the "leave running in background?" modal shown when
// quitting a standalone TUI that still has live tubers.
func (m Model) confirmQuitView() string {
	n := 0
	for _, s := range m.list {
		switch s.State {
		case controller.Connecting, controller.Connected, controller.Reconnecting:
			n++
		}
	}
	line := fmt.Sprintf("%d tuber(s) active.\nLeave them running in the background? [y/N]", n)
	return m.pal.modal.Render(line)
}

// confirmDeleteView renders the "delete tuber?" modal. Deleting stops an
// active tuber (via the engine reload) and removes it from the config.
func (m Model) confirmDeleteView() string {
	line := fmt.Sprintf("Delete tuber %q?\nThis stops it if active and removes it from the config. [y/N]", m.deleteTarget)
	return m.pal.modal.Render(line)
}

// confirmAcceptView renders the Phase 11 TOFU modal: the tuber is blocked by
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
	return m.pal.modal.Render(line)
}

// passphraseView renders the Phase 19 identity-passphrase modal: the tuber's
// dial is blocked on a passphrase-protected identity, and the user types the
// passphrase (masked) and submits it via Controller.AcceptPassphrase. The
// "wrong passphrase" hint is driven by Status.PassphraseAttempts (the dial's
// real rejection count), so it only appears when the dial actually rejected a
// submitted passphrase.
func (m Model) passphraseView() string {
	hint := ""
	if n := passphraseAttemptsFor(m.list, m.passphraseTarget); n > 0 {
		hint = fmt.Sprintf("\nwrong passphrase, try again (rejected %d)", n)
	}
	line := fmt.Sprintf(
		"Passphrase for %s's identity\n%s[enter] submit  ·  [esc] cancel%s",
		m.passphraseTarget, m.passphraseInput.View(), hint,
	)
	return m.pal.modal.Render(line)
}

// passphraseAttemptsFor looks up a tuber's PassphraseAttempts (dial rejections)
// in a status snapshot, for the passphrase modal's retry hint. 0 when absent.
func passphraseAttemptsFor(list []controller.Status, name string) int {
	for _, s := range list {
		if s.Name == name {
			return s.PassphraseAttempts
		}
	}
	return 0
}

// sproutingView is the brief "connecting" state shown after a password /
// passphrase submit, while the dial races to its verdict (accept → modal
// closes; reject → back to the input). An on-brand stand-in (portato = potato,
// tubers sprout) for what would otherwise be an empty input modal looking like
// "enter again".
func (m Model) sproutingView() string {
	return m.pal.modal.Render("sprouting…")
}

// passwordView renders the Phase 35 SSH-password modal: the tuber's dial is
// blocked on a password-only account, and the user types the password (masked)
// and submits it via Controller.AcceptPassword. The "wrong password" hint is
// driven by Status.PasswordAttempts (the dial's real rejection count), so it
// only appears when the server actually rejected a submitted password.
func (m Model) passwordView() string {
	hint := ""
	if n := passwordAttemptsFor(m.list, m.passwordTarget); n > 0 {
		hint = fmt.Sprintf("\nwrong password, try again (rejected %d)", n)
	}
	line := fmt.Sprintf(
		"Password for %s\n%s[enter] submit  ·  [esc] cancel%s",
		m.passwordTarget, m.passwordInput.View(), hint,
	)
	return m.pal.modal.Render(line)
}

// passwordAttemptsFor looks up a tuber's PasswordAttempts (server rejections)
// in a status snapshot, for the password modal's retry hint. 0 when absent.
func passwordAttemptsFor(list []controller.Status, name string) int {
	for _, s := range list {
		if s.Name == name {
			return s.PasswordAttempts
		}
	}
	return 0
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

// centerBlock centers a (possibly multi-line, possibly ANSI-styled) block
// within width display cells by left-padding every line equally. It is used to
// place the logo art in the empty-state splash and at the top of the help
// panel. lipgloss.Width strips ANSI when measuring, so a tinted logo still
// centers on its visible glyph width.
func centerBlock(block string, width int) string {
	pad := (width - lipgloss.Width(block)) / 2
	if pad < 0 {
		pad = 0
	}
	indent := strings.Repeat(" ", pad)
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		lines[i] = indent + l
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
func fitEndpoint(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	for _, sep := range []string{" → ", " ← "} {
		if i := strings.Index(s, sep); i >= 0 {
			left, right := s[:i+len(sep)], s[i+len(sep):]
			if budget := maxWidth - lipgloss.Width(left); budget >= 4 {
				return left + fitHostPort(right, budget)
			}
		}
	}
	return truncate(s, maxWidth)
}

func fitName(s string, maxWidth int) string {
	return middleTruncate(s, maxWidth)
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
