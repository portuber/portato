package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/portuber/portato/internal/controller"
)

// TestHelpView_AllBindingsReachable asserts the Phase 38 full-screen help view
// shows every binding at 80x24 (the audit's F4 size, where the old appended
// help block showed zero bindings). The viewport is tall enough that no scroll
// is needed at this size; the title bar and footer hint are present too.
func TestHelpView_AllBindingsReachable(t *testing.T) {
	m := New(newFake(controller.Status{Name: "a"}), Options{Mode: "standalone"})
	m.width, m.height = 80, 24
	m.help = newHelpView(m.pal, m.kind, m.width, m.height, m.attach)
	out := m.render()

	if !strings.Contains(out, "Portato") || !strings.Contains(out, "Help") {
		t.Errorf("help view missing title bar\n%s", out)
	}
	if !strings.Contains(out, "?/esc close") {
		t.Errorf("help view missing the close-hint footer\n%s", out)
	}
	for _, line := range helpLines(false) {
		if !strings.Contains(out, line) {
			t.Errorf("80x24 help missing binding line %q\n%s", line, out)
		}
	}
}

// TestHelpView_CloseKeys asserts ?/esc/q all close the help view (mirroring
// logs, which closes on the same keys). q closing help locally — instead of
// quitting the app — is the expected sub-model behaviour.
func TestHelpView_CloseKeys(t *testing.T) {
	for _, k := range []string{"esc", "?", "q"} {
		hv := newHelpView(darkPalette(), themeDark, 80, 24, false)
		hv.handleKey(tea.KeyPressMsg{Text: k})
		if !hv.done {
			t.Errorf("%q should close the help view", k)
		}
	}
}

// TestHelpView_ScrollKeysDontClose asserts the scroll keys move the viewport
// without closing (so the user can reach every binding on a short terminal).
func TestHelpView_ScrollKeysDontClose(t *testing.T) {
	for _, k := range []string{"down", "j", "pgdown", "G"} {
		hv := newHelpView(darkPalette(), themeDark, 80, 8, false) // short: 8 rows → viewport 5
		hv.handleKey(tea.KeyPressMsg{Text: k})
		if hv.done {
			t.Errorf("%q should scroll, not close, the help view", k)
		}
	}
}
