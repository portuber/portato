package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/portuber/portato/internal/controller"
)

// hasBraille reports whether s contains a Unicode braille pattern
// (U+2800..U+28FF), the tell-tale that the braille logo art was rendered. It
// is independent of ANSI tinting, so it works under both dark and mono themes.
func hasBraille(s string) bool {
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

// maxLineWidth returns the widest display-cell line in s (ANSI stripped). It is
// used to tell the wordmark splash (~70 cells) from the compact-potato fallback
// (~24 cells) on a narrow terminal.
func maxLineWidth(s string) int {
	max := 0
	for _, l := range strings.Split(s, "\n") {
		if w := lipgloss.Width(l); w > max {
			max = w
		}
	}
	return max
}

// TestEmptyListSplashShowsLogo verifies the empty-list state renders the
// centered logo plus the hint line on a tall terminal.
func TestEmptyListSplashShowsLogo(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "braille")
	m := New(newFake(), Options{Mode: "standalone"})
	m.width, m.height = 80, 24
	out := m.render()
	if !hasBraille(out) {
		t.Errorf("empty list on a tall terminal should render the logo\n%s", out)
	}
	if !strings.Contains(out, "no tubers") {
		t.Errorf("splash should still show the hint line\n%s", out)
	}
}

// splashArt returns the logo portion of the empty-list splash (the block before
// the "\n\n" that separates it from the hint line), so a test can measure the
// art width without the hint line (which is wider than the compact potato)
// polluting the measurement.
func splashArt(table string) string {
	if i := strings.Index(table, "\n\n"); i >= 0 {
		return table[:i]
	}
	return table
}

// TestEmptyListSplashWideUsesWordmark verifies a wide terminal renders the
// "potato + PORTATO" wordmark (~70 cells) in the empty-list splash.
func TestEmptyListSplashWideUsesWordmark(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "braille")
	m := New(newFake(), Options{Mode: "standalone"})
	m.width, m.height = 80, 24
	if w := maxLineWidth(splashArt(m.table())); w < 60 {
		t.Errorf("wide terminal should render the wordmark (~70 cells), got max width %d\n%s", w, m.table())
	}
}

// TestEmptyListSplashNarrowUsesPotato verifies a narrow terminal (avail < 70)
// falls back to the compact potato (~24 cells) instead of the wordmark.
func TestEmptyListSplashNarrowUsesPotato(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "braille")
	m := New(newFake(), Options{Mode: "standalone"})
	m.width, m.height = 60, 24
	if w := maxLineWidth(splashArt(m.table())); w > 50 {
		t.Errorf("narrow terminal should fall back to the compact potato (~24 cells), got max width %d\n%s", w, m.table())
	}
}

// TestHelpShowsLogo verifies the help (?) overlay prepends the compact logo
// above the hotkey list on a tall terminal.
func TestHelpShowsLogo(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "braille")
	m := New(newFake(controller.Status{Name: "a"}), Options{Mode: "standalone"})
	m.width, m.height = 80, 24
	m.help = true
	out := m.render()
	if !hasBraille(out) {
		t.Errorf("help on a tall terminal should render the logo\n%s", out)
	}
	if !strings.Contains(out, "move cursor up") {
		t.Errorf("help should still list the hotkeys\n%s", out)
	}
}

// TestLogoOffHidesBranding verifies PORTATO_LOGO=off suppresses the logo in
// both the splash and the help overlay while leaving the hint/hotkeys intact.
func TestLogoOffHidesBranding(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "off")

	m := New(newFake(), Options{Mode: "standalone"})
	m.width, m.height = 80, 24
	if out := m.render(); hasBraille(out) {
		t.Errorf("PORTATO_LOGO=off should hide the splash logo\n%s", out)
	}

	m2 := New(newFake(controller.Status{Name: "a"}), Options{Mode: "standalone"})
	m2.width, m2.height = 80, 24
	m2.help = true
	out2 := m2.render()
	if hasBraille(out2) {
		t.Errorf("PORTATO_LOGO=off should hide the help logo\n%s", out2)
	}
	if !strings.Contains(out2, "move cursor up") {
		t.Errorf("help hotkeys should still render with logo off\n%s", out2)
	}
}

// TestSmallHeightOmitsLogo verifies the height gate: a short terminal shows
// the hint only, with no logo and no layout breakage.
func TestSmallHeightOmitsLogo(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "braille")
	m := New(newFake(), Options{Mode: "standalone"})
	m.width, m.height = 80, splashMinH-1
	out := m.render()
	if hasBraille(out) {
		t.Errorf("short terminal should omit the logo\n%s", out)
	}
	if !strings.Contains(out, "no tubers") {
		t.Errorf("short terminal should still show the hint\n%s", out)
	}
}

// TestNonEmptyListHasNoLogo guards the DoD: the working (non-empty) list must
// not show the big logo anywhere — branding lives only in the empty/help/
// version/header-mark placements.
func TestNonEmptyListHasNoLogo(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "braille")
	m := New(newFake(
		controller.Status{Name: "db", Type: "local", Local: "5432", Remote: "db:5432", State: controller.Connected},
	), Options{Mode: "standalone"})
	m.width, m.height = 80, 24
	if out := m.render(); hasBraille(out) {
		t.Errorf("non-empty list must not render the logo\n%s", out)
	}
}

// TestHeaderEmojiOverride verifies the potato emoji marks the header when
// PORTATO_LOGO_EMOJI=on and is absent when off (platform-independent: the
// override is checked directly so the test is deterministic on any host).
func TestHeaderEmojiOverride(t *testing.T) {
	check := func(t *testing.T, want bool) {
		t.Helper()
		m := New(newFake(controller.Status{Name: "a"}), Options{Mode: "standalone"})
		m.width = 80
		header := m.header()
		_, has := containsEmoji(header)
		if has != want {
			t.Errorf("emoji present=%v, want %v\nheader: %q", has, want, header)
		}
	}
	t.Run("on", func(t *testing.T) {
		t.Setenv("PORTATO_LOGO_EMOJI", "on")
		check(t, true)
	})
	t.Run("off", func(t *testing.T) {
		t.Setenv("PORTATO_LOGO_EMOJI", "off")
		check(t, false)
	})
	t.Run("PORTATO_LOGO=off hides emoji too", func(t *testing.T) {
		t.Setenv("PORTATO_LOGO", "off")
		t.Setenv("PORTATO_LOGO_EMOJI", "on")
		check(t, false)
	})
}

// containsEmoji reports whether s contains the potato emoji 🥔.
func containsEmoji(s string) (rune, bool) {
	for _, r := range s {
		if r == '🥔' {
			return r, true
		}
	}
	return 0, false
}
