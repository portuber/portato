package logo

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// pngSig is the 8-byte PNG magic every PNG file starts with.
var pngSig = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

// TestEmbeddedAssets verifies the go:embed round-trip: both ASCII variants are
// non-empty 28x12 grids and the PNG is a real PNG whose bytes decode back to
// the embedded slice.
func TestEmbeddedAssets(t *testing.T) {
	if strings.TrimSpace(brailleArt) == "" {
		t.Error("brailleArt embedded empty")
	}
	if strings.TrimSpace(blockArt) == "" {
		t.Error("blockArt embedded empty")
	}
	if len(pngBytes) == 0 {
		t.Fatal("pngBytes embedded empty")
	}
	if !bytes.HasPrefix(pngBytes, pngSig) {
		t.Errorf("pngBytes does not start with the PNG signature: % x", pngBytes[:min(8, len(pngBytes))])
	}
	// The ASCII variants are generated at 28x12 cells: each line is 12 rows.
	for _, art := range []struct{ name, val string }{
		{"braille", brailleArt},
		{"block", blockArt},
	} {
		lines := strings.Split(strings.TrimRight(art.val, "\n"), "\n")
		if len(lines) != logoHeight {
			t.Errorf("%s art has %d lines, want %d", art.name, len(lines), logoHeight)
		}
	}
}

// TestDetectMatrix covers PORTATO_LOGO (each value) x TERM_PROGRAM x GOOS. The
// goos var is swapped per-case so the matrix is deterministic on every host.
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
		{"explicit image overrides everything", "image", "", "linux", ModeImage},
		{"explicit braille", "braille", "iTerm.app", "darwin", ModeBraille},
		{"explicit block", "block", "", "darwin", ModeBlock},
		{"explicit off", "off", "WezTerm", "darwin", ModeOff},

		{"auto + iTerm.app -> image", "auto", "iTerm.app", "linux", ModeImage},
		{"auto + WezTerm -> image", "auto", "WezTerm", "darwin", ModeImage},
		{"auto + plain term + linux -> braille", "auto", "", "linux", ModeBraille},
		{"auto + plain term + darwin -> braille", "auto", "", "darwin", ModeBraille},
		{"auto + plain term + windows -> block", "auto", "", "windows", ModeBlock},
		{"auto + image term + windows still image (explicit term wins)", "auto", "iTerm.app", "windows", ModeImage},

		{"unset logo + plain -> braille (linux default)", "", "", "linux", ModeBraille},
		{"unset logo + windows -> block", "", "", "windows", ModeBlock},
		{"unset logo + iTerm.app -> image", "", "iTerm.app", "linux", ModeImage},

		{"truthy '1' maps to auto", "1", "", "linux", ModeBraille},
		{"truthy 'on' maps to auto + image term", "on", "WezTerm", "linux", ModeImage},
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

// TestRenderPerMode checks Render returns non-empty for the renderable modes
// and "" for Off.
func TestRenderPerMode(t *testing.T) {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	for _, c := range []struct {
		name    string
		mode    Mode
		emptyOK bool
	}{
		{"image non-empty", ModeImage, false},
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

// TestInlineImageWellFormed asserts the OSC 1337 sequence wraps the base64 PNG
// with the right prefix (sized in cells) and a trailing BEL.
func TestInlineImageWellFormed(t *testing.T) {
	out := inlineImage(pngBytes)

	prefix := "\x1b]1337;File=inline=1;width=28cells;height=12cells;preserveAspectRatio=1:"
	if !strings.HasPrefix(out, prefix) {
		t.Errorf("OSC 1337 missing prefix:\nwant prefix %q\ngot %q", prefix, out[:min(len(prefix), len(out))])
	}
	if !strings.HasSuffix(out, "\x07") {
		t.Error("OSC 1337 must end with BEL (\\x07)")
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(out, prefix), "\x07")
	dec, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Errorf("OSC 1337 payload is not valid base64: %v", err)
	}
	if !bytes.Equal(dec, pngBytes) {
		t.Errorf("OSC 1337 payload decodes to %d bytes, want %d (the embedded PNG)", len(dec), len(pngBytes))
	}
}

// TestRenderImageViaMode checks the image mode Render path produces the same
// OSC sequence as inlineImage (the public Render delegates to it).
func TestRenderImageViaMode(t *testing.T) {
	got := Render(ModeImage, lipgloss.NewStyle(), false)
	want := inlineImage(pngBytes)
	if got != want {
		t.Error("Render(ModeImage,...) should equal inlineImage(pngBytes)")
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

// TestVersionBanner covers the three behaviours: logo + version line on a TTY,
// braille fallback (no OSC) when piped, and PORTATO_LOGO=off yields just the
// version line.
func TestVersionBanner(t *testing.T) {
	prevGoos := goos
	t.Cleanup(func() { goos = prevGoos })
	goos = "linux"

	t.Setenv("PORTATO_LOGO", "braille")
	tty := VersionBanner("1.2.3", "abc1234", "2026-07-07", true)
	if !strings.Contains(tty, "portato 1.2.3 (abc1234, 2026-07-07)") {
		t.Errorf("tty banner missing version line:\n%s", tty)
	}
	if !strings.Contains(tty, "\n\n") {
		t.Errorf("tty banner should separate the logo from the version line with a blank line:\n%s", tty)
	}
	if strings.Contains(tty, "\x1b]1337") {
		t.Errorf("braille banner must not contain an OSC 1337 sequence:\n%s", tty)
	}

	t.Setenv("PORTATO_LOGO", "image")
	// Piped (tty=false): inline image suppressed, braille used instead.
	piped := VersionBanner("1.2.3", "abc1234", "2026-07-07", false)
	if strings.Contains(piped, "\x1b]1337") {
		t.Errorf("piped banner must not emit an inline image:\n%s", piped)
	}
	if !strings.Contains(piped, "portato 1.2.3") {
		t.Errorf("piped banner missing version line:\n%s", piped)
	}
	// On a TTY the image mode emits the OSC sequence.
	onTTY := VersionBanner("1.2.3", "abc1234", "2026-07-07", true)
	if !strings.Contains(onTTY, "\x1b]1337") {
		t.Errorf("image+TTY banner should emit OSC 1337:\n%s", onTTY)
	}

	t.Setenv("PORTATO_LOGO", "off")
	off := VersionBanner("dev", "none", "unknown", true)
	if strings.Contains(off, "\x1b") {
		t.Errorf("off banner should have no ANSI/logo:\n%s", off)
	}
	if !strings.Contains(off, "portato dev") {
		t.Errorf("off banner should still print the version line:\n%s", off)
	}
}
