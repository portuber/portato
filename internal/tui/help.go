package tui

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/portuber/portato/internal/logo"
)

// helpView is the Phase 38 full-screen help overlay (? / esc). It mirrors
// logsView: a sub-model held by Model (m.help, nil when inactive) that takes
// over the whole frame via render()'s early-route, renders the key bindings in
// a scrollable viewport, and closes on ?/esc/q. This replaces the pre-Phase-38
// help block that was appended below the table+footer and was clipped
// off-screen at 80x24 (audit F4): as its own frame every binding is reachable
// (scroll is fine), and the potato logo is prepended only when it fits in
// addition to the full binding list (list-first — the inverse of F4).
type helpView struct {
	vp            viewport.Model
	pal           palette
	kind          themeKind
	width, height int
	done          bool
}

func newHelpView(pal palette, kind themeKind, width, height int) *helpView {
	vp := viewport.New(viewport.WithWidth(helpWidth(width)), viewport.WithHeight(helpHeight(height)))
	hv := &helpView{vp: vp, pal: pal, kind: kind, width: width, height: height}
	hv.refresh()
	return hv
}

func helpWidth(w int) int {
	if w-2*sideMargin < 20 {
		return 20
	}
	return w - 2*sideMargin
}

// helpHeight reserves the title line (1) + its blank separator (1) and the
// footer hint (1), mirroring logsHeight.
func helpHeight(h int) int {
	if h < 6 {
		return 3
	}
	return h - 3
}

func (h *helpView) refresh() {
	h.vp.SetContent(h.content())
	h.vp.GotoTop()
}

// content builds the viewport body. The binding list always renders; the
// potato logo is prepended only when the viewport is tall enough to hold the
// full list AND the logo above it (list-first). logo.Banner's height is
// measured at runtime so the gate holds for any detected art mode
// (braille/block) and for PORTATO_LOGO=off (empty art skips the block).
func (h *helpView) content() string {
	lines := helpLines()
	list := strings.Join(lines, "\n")
	art := logo.Banner(h.pal.title, h.kind == themeMono)
	if art == "" {
		return list
	}
	artH := strings.Count(art, "\n") + 1
	if h.vp.Height() >= len(lines)+1+artH {
		return centerBlock(art, helpWidth(h.width)) + "\n\n" + list
	}
	return list
}

func (h *helpView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width, h.height = msg.Width, msg.Height
		h.vp.SetWidth(helpWidth(msg.Width))
		h.vp.SetHeight(helpHeight(msg.Height))
		h.refresh()
		return nil
	case tea.KeyPressMsg:
		return h.handleKey(msg)
	}
	return nil
}

func (h *helpView) handleKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "?", "q":
		h.done = true
		return nil
	case "up", "k":
		h.vp.ScrollUp(1)
	case "down", "j":
		h.vp.ScrollDown(1)
	case "pgup":
		h.vp.PageUp()
	case "pgdown":
		h.vp.PageDown()
	case "g":
		h.vp.GotoTop()
	case "G":
		h.vp.GotoBottom()
	}
	return nil
}

func (h *helpView) view() string {
	var b strings.Builder
	b.WriteString(h.pal.title.Render("Portato") + " " + h.pal.dim.Render("— Help"))
	b.WriteString("\n")
	b.WriteString(h.vp.View())
	b.WriteString("\n")
	b.WriteString(h.pal.footer.Render("↑↓/jk scroll · pgup/pgdn · g/G top/bottom · ?/esc close"))
	return insetLines(b.String(), sideMargin)
}
