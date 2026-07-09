package logo

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestEmbeddedAssets verifies the go:embed round-trip: both ASCII variants of
// each logo (compact potato + wordmark, in braille and block forms) are
// non-empty artHeight-row grids.
func TestEmbeddedAssets(t *testing.T) {
	arts := []struct{ name, val string }{
		{"brailleArt", brailleArt},
		{"blockArt", blockArt},
		{"wordmarkBraille", wordmarkBraille},
		{"wordmarkBlock", wordmarkBlock},
	}
	for _, art := range arts {
		if strings.TrimSpace(art.val) == "" {
			t.Errorf("%s embedded empty", art.name)
		}
		lines := strings.Split(strings.TrimRight(art.val, "\n"), "\n")
		if len(lines) != artHeight {
			t.Errorf("%s has %d lines, want %d", art.name, len(lines), artHeight)
		}
	}
}

// TestDetectMatrix covers PORTATO_LOGO (each value) x TERM_PROGRAM x GOOS. The
// goos var is swapped per-case so the matrix is deterministic on every host.
// (The inline-PNG image mode is gone: iTerm2/WezTerm and PORTATO_LOGO=image all
// resolve to braille.)
func TestDetectMatrix(t *testing.T) {
	prevGoos := goos
	t.Cleanup(func() { goos = prevGoos })

	cases := []struct {
		name string
		logo string
		term string
		goos string
		want Mode
	}{
		{"explicit braille", "braille", "iTerm.app", "darwin", ModeBraille},
		{"explicit block", "block", "", "darwin", ModeBlock},
		{"explicit off", "off", "WezTerm", "darwin", ModeOff},

		// "image" is now an alias for auto (image mode removed) -> braille.
		{"explicit image falls back to braille", "image", "", "linux", ModeBraille},
		{"image + iTerm.app still braille", "image", "iTerm.app", "darwin", ModeBraille},

		{"auto + iTerm.app -> braille", "auto", "iTerm.app", "linux", ModeBraille},
		{"auto + WezTerm -> braille", "auto", "WezTerm", "darwin", ModeBraille},
		{"auto + plain term + linux -> braille", "auto", "", "linux", ModeBraille},
		{"auto + plain term + darwin -> braille", "auto", "", "darwin", ModeBraille},
		{"auto + plain term + windows -> block", "auto", "", "windows", ModeBlock},

		{"unset logo + plain -> braille (linux default)", "", "", "linux", ModeBraille},
		{"unset logo + windows -> block", "", "", "windows", ModeBlock},
		{"unset logo + iTerm.app -> braille", "", "iTerm.app", "linux", ModeBraille},

		{"truthy '1' maps to auto", "1", "", "linux", ModeBraille},
		{"truthy 'on' maps to auto + image term -> braille", "on", "WezTerm", "linux", ModeBraille},
		{"uppercase OFF -> off", "OFF", "iTerm.app", "darwin", ModeOff},
		{"unknown value -> auto", "garbage", "", "linux", ModeBraille},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("PORTATO_LOGO", c.logo)
			t.Setenv("TERM_PROGRAM", c.term)
			goos = c.goos
			if got := Detect(); got != c.want {
				t.Errorf("Detect() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestEmojiEnabledMatrix covers PORTATO_LOGO_EMOJI x GOOS, plus the rule that
// PORTATO_LOGO=off forces the emoji off too.
func TestEmojiEnabledMatrix(t *testing.T) {
	prevGoos := goos
	t.Cleanup(func() { goos = prevGoos })

	cases := []struct {
		name  string
		logo  string
		emoji string
		goos  string
		want  bool
	}{
		{"default darwin -> on", "", "", "darwin", true},
		{"default linux -> off", "", "", "linux", false},
		{"default windows -> off", "", "", "windows", false},

		{"emoji=on forces on (linux)", "", "on", "linux", true},
		{"emoji=off forces off (darwin)", "", "off", "darwin", false},
		{"emoji=1 -> on", "", "1", "linux", true},
		{"emoji=true -> on", "", "true", "linux", true},
		{"emoji=0 -> off", "", "0", "darwin", false},
		{"emoji=no -> off", "", "no", "darwin", false},
		{"uppercase ON -> on", "", "ON", "linux", true},

		{"PORTATO_LOGO=off forces emoji off (darwin, no emoji var)", "off", "", "darwin", false},
		{"PORTATO_LOGO=off wins over EMOJI=on", "off", "on", "darwin", false},
		{"PORTATO_LOGO=braille leaves emoji to its own default (darwin)", "braille", "", "darwin", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("PORTATO_LOGO", c.logo)
			t.Setenv("PORTATO_LOGO_EMOJI", c.emoji)
			goos = c.goos
			if got := EmojiEnabled(); got != c.want {
				t.Errorf("EmojiEnabled() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestRenderPerMode checks Render returns non-empty for the renderable modes
// and "" for Off.
func TestRenderPerMode(t *testing.T) {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	for _, c := range []struct {
		name    string
		mode    Mode
		emptyOK bool
	}{
		{"braille non-empty", ModeBraille, false},
		{"block non-empty", ModeBlock, false},
		{"off empty", ModeOff, true},
	} {
		got := Render(c.mode, accent, false)
		if c.emptyOK {
			if got != "" {
				t.Errorf("%s: Render = %q, want empty", c.name, got)
			}
		} else {
			if got == "" {
				t.Errorf("%s: Render returned empty", c.name)
			}
		}
	}
}

// TestRenderWordmark checks RenderWordmark returns the wordmark art for the
// renderable modes and "" for Off, and that it differs from the compact
// potato (the wordmark is wider).
func TestRenderWordmark(t *testing.T) {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	if got := RenderWordmark(ModeBraille, accent, false); got == "" {
		t.Error("RenderWordmark(ModeBraille) returned empty")
	}
	if got := RenderWordmark(ModeBlock, accent, false); got == "" {
		t.Error("RenderWordmark(ModeBlock) returned empty")
	}
	if got := RenderWordmark(ModeOff, accent, false); got != "" {
		t.Errorf("RenderWordmark(ModeOff) = %q, want empty", got)
	}
	// The wordmark is wider than the compact potato: its first line has more
	// cells than the potato's whole frame.
	potato := Render(ModeBraille, accent, true)
	wm := RenderWordmark(ModeBraille, accent, true)
	if lipgloss.Width(strings.Split(potato, "\n")[0]) >= lipgloss.Width(strings.Split(wm, "\n")[0]) {
		t.Error("the wordmark should be wider than the compact potato")
	}
	// Wordmark convenience picks the detected mode.
	t.Setenv("PORTATO_LOGO", "braille")
	if Wordmark(accent, false) == "" {
		t.Error("Wordmark() returned empty for braille")
	}
}

// TestTintAppliedUnlessMono is the profile-independent guard for "the ASCII
// variant is tinted with accent unless mono": under mono the art is returned
// verbatim; otherwise it is passed through accent.Render. Both assertions hold
// regardless of the colour profile lipgloss picks for the test environment.
func TestTintAppliedUnlessMono(t *testing.T) {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	raw := strings.TrimRight(brailleArt, "\n")

	if got := Render(ModeBraille, accent, true); got != raw {
		t.Errorf("mono braille should be verbatim art\nwant %q\ngot  %q", raw, got)
	}
	wantTinted := accent.Render(raw)
	tintedOut := Render(ModeBraille, accent, false)
	if tintedOut != wantTinted {
		t.Errorf("non-mono braille should equal accent.Render(art)\nwant %q\ngot  %q", wantTinted, tintedOut)
	}
	// lipgloss styles each line individually, so every raw line must still
	// appear verbatim inside the tinted output (the art survived the styling).
	for i, line := range strings.Split(raw, "\n") {
		if !strings.Contains(tintedOut, line) {
			t.Errorf("non-mono braille line %d %q lost from tinted output: %q", i, line, tintedOut)
		}
	}
}

// TestVersionBanner covers the behaviours: the wordmark + version line under
// braille, "image" rendering braille (no inline-image escape), and
// PORTATO_LOGO=off yielding just the version line.
func TestVersionBanner(t *testing.T) {
	prevGoos := goos
	t.Cleanup(func() { goos = prevGoos })
	goos = "linux"

	t.Setenv("PORTATO_LOGO", "braille")
	out := VersionBanner("1.2.3", "abc1234", "2026-07-09")
	if !strings.Contains(out, "portato 1.2.3 (abc1234, 2026-07-09)") {
		t.Errorf("banner missing version line:\n%s", out)
	}
	if !strings.Contains(out, "\n\n") {
		t.Errorf("banner should separate the logo from the version line with a blank line:\n%s", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("braille banner must contain no ANSI/OSC escapes:\n%s", out)
	}
	// The wordmark art is present verbatim (untinted).
	if !strings.Contains(out, strings.TrimRight(wordmarkBraille, "\n")) {
		t.Errorf("braille banner should contain the wordmark art:\n%s", out)
	}

	// "image" now renders braille and never emits an inline-image escape.
	t.Setenv("PORTATO_LOGO", "image")
	img := VersionBanner("1.2.3", "abc1234", "2026-07-09")
	if strings.Contains(img, "\x1b]1337") {
		t.Errorf("image mode must not emit an OSC 1337 sequence:\n%s", img)
	}
	if !strings.Contains(img, strings.TrimRight(wordmarkBraille, "\n")) {
		t.Errorf("image mode should render the braille wordmark:\n%s", img)
	}

	t.Setenv("PORTATO_LOGO", "off")
	off := VersionBanner("dev", "none", "unknown")
	if strings.Contains(off, "\x1b") {
		t.Errorf("off banner should have no ANSI/logo:\n%s", off)
	}
	if !strings.Contains(off, "portato dev") {
		t.Errorf("off banner should still print the version line:\n%s", off)
	}
}
