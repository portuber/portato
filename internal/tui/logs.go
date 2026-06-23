package tui

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kipkaev55/portato/internal/controller"
	routelog "github.com/kipkaev55/portato/internal/log"
)

// logsView is the Phase 11 per-tunnel log screen opened with `l`. It is a
// sub-model held by the main Model (m.logs, nil when inactive), like the
// tunnel editor. It reads the controller's in-memory ring buffer and renders
// it in a scrollable viewport, refreshing on the redraw tick and on every
// state change (a state change almost always pairs with a log line).
type logsView struct {
	ctrl    controller.Controller
	name    string
	vp      viewport.Model
	debug   bool // show debug-level entries
	entries []routelog.Entry

	width, height int
	autoScroll    bool
	done          bool
}

func newLogsView(ctrl controller.Controller, name string, width, height int) *logsView {
	vp := viewport.New(viewport.WithWidth(logsWidth(width)), viewport.WithHeight(logsHeight(height)))
	lv := &logsView{
		ctrl:       ctrl,
		name:       name,
		vp:         vp,
		width:      width,
		height:     height,
		autoScroll: true,
	}
	lv.refresh()
	return lv
}

func logsWidth(w int) int {
	if w-2*sideMargin < 20 {
		return 20
	}
	return w - 2*sideMargin
}

// logsHeight reserves room for the title (2 lines) and the footer (1 line).
func logsHeight(h int) int {
	if h < 6 {
		return 3
	}
	return h - 3
}

// refresh re-reads the ring buffer and rebuilds the viewport content. When
// autoScroll is on (or the viewport was already at the bottom) it pins to the
// newest line.
func (l *logsView) refresh() {
	entries, _ := l.ctrl.Logs(l.name)
	l.entries = filterLevel(entries, l.debug)
	l.vp.SetContent(renderLogs(l.entries))
	if l.autoScroll || l.vp.AtBottom() {
		l.vp.GotoBottom()
		l.autoScroll = true
	}
}

func filterLevel(in []routelog.Entry, debug bool) []routelog.Entry {
	if debug {
		return in
	}
	out := make([]routelog.Entry, 0, len(in))
	for _, e := range in {
		if e.Level >= slog.LevelInfo {
			out = append(out, e)
		}
	}
	return out
}

func renderLogs(entries []routelog.Entry) string {
	if len(entries) == 0 {
		return dimStyle.Render("(no log entries)")
	}
	var b strings.Builder
	for _, e := range entries {
		msg := e.Msg
		if e.Attrs != "" {
			msg += " " + e.Attrs
		}
		fmt.Fprintf(&b, "%s %s %s\n", dimStyle.Render(e.Time.Format(time.TimeOnly)), levelTag(e.Level), bodyStyle.Render(msg))
	}
	return b.String()
}

func levelTag(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return errorStyle.Render("ERR")
	case l >= slog.LevelWarn:
		return warnStyle.Render("WRN")
	case l >= slog.LevelInfo:
		return dimStyle.Render("INF")
	default:
		return dimStyle.Render("DBG")
	}
}

func (l *logsView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
		l.vp.SetWidth(logsWidth(msg.Width))
		l.vp.SetHeight(logsHeight(msg.Height))
		l.refresh()
		return nil
	case tea.KeyPressMsg:
		return l.handleKey(msg)
	}
	return nil
}

func (l *logsView) handleKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q", "l":
		l.done = true
		return nil
	case "L":
		l.debug = !l.debug
		l.autoScroll = true
		l.refresh()
		return nil
	case "up", "k":
		l.vp.ScrollUp(1)
		l.autoScroll = false
	case "down", "j":
		l.vp.ScrollDown(1)
		if l.vp.AtBottom() {
			l.autoScroll = true
		}
	case "pgup":
		l.vp.PageUp()
		l.autoScroll = false
	case "pgdown":
		l.vp.PageDown()
		if l.vp.AtBottom() {
			l.autoScroll = true
		}
	case "g":
		l.vp.GotoTop()
		l.autoScroll = false
	case "G":
		l.vp.GotoBottom()
		l.autoScroll = true
	}
	return nil
}

func (l *logsView) view() string {
	var b strings.Builder
	level := dimStyle.Render("info")
	if l.debug {
		level = warnStyle.Render("debug")
	}
	b.WriteString(titleStyle.Render("Portato") + " " + dimStyle.Render("— Logs — "+l.name+"  level: "+level))
	b.WriteString("\n")
	b.WriteString(l.vp.View())
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("↑↓/jk scroll · pgup/pgdn · g/G top/bottom · L level · l/esc close"))
	return insetLines(b.String(), sideMargin)
}

// lipglossWidth returns the visible width of s, exported for tests.
func lipglossWidth(s string) int { return lipgloss.Width(s) }
