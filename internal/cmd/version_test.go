package cmd

import (
	"strings"
	"testing"
)

// hasBrailleGlyph reports whether s contains a Unicode braille pattern
// (U+2800..U+28FF), the tell-tale that the braille logo was rendered.
func hasBrailleGlyph(s string) bool {
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

// TestPrintVersion_Braille verifies the banner renders the wordmark plus the
// version/commit/date line.
func TestPrintVersion_Braille(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "braille")
	var b strings.Builder
	printVersion(&b)
	out := b.String()
	if !strings.Contains(out, "portato dev (unknown, unknown)") {
		t.Errorf("version line missing: %q", out)
	}
	if !hasBrailleGlyph(out) {
		t.Errorf("braille logo missing:\n%s", out)
	}
}

// TestPrintVersion_ImageFallsBackToBraille verifies that "image" mode (the
// inline-PNG mode is gone) renders the braille wordmark and never emits an
// inline-image escape.
func TestPrintVersion_ImageFallsBackToBraille(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "image")
	var b strings.Builder
	printVersion(&b)
	out := b.String()
	if strings.Contains(out, "\x1b]1337") {
		t.Errorf("image mode must not emit an inline-image escape:\n%s", out)
	}
	if !strings.Contains(out, "portato dev") {
		t.Errorf("version line missing: %q", out)
	}
	if !hasBrailleGlyph(out) {
		t.Errorf("image mode should still carry the braille wordmark:\n%s", out)
	}
}

// TestPrintVersion_OffOmitsLogo verifies PORTATO_LOGO=off leaves only the
// version line (no logo, no ANSI).
func TestPrintVersion_OffOmitsLogo(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "off")
	var b strings.Builder
	printVersion(&b)
	out := b.String()
	if hasBrailleGlyph(out) {
		t.Errorf("off should suppress the logo:\n%s", out)
	}
	if !strings.Contains(out, "portato dev") {
		t.Errorf("version line should still print: %q", out)
	}
}

// TestVersionSubcommand verifies the `portato version` subcommand prints the
// same banner (logo + version line).
func TestVersionSubcommand(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "braille")
	c, out, errOut := captureCmd()
	if err := versionCmd.RunE(c, nil); err != nil {
		t.Fatalf("versionCmd: %v", err)
	}
	if errOut.String() != "" {
		t.Errorf("unexpected stderr: %q", errOut.String())
	}
	if !strings.Contains(out.String(), "portato dev") {
		t.Errorf("subcommand output missing version line: %q", out.String())
	}
	if !hasBrailleGlyph(out.String()) {
		t.Errorf("subcommand output missing braille logo:\n%s", out.String())
	}
}

// TestRootVersionFlagShortCircuits verifies the `--version` flag on the root
// command prints the banner and returns before any daemon/standalone work.
func TestRootVersionFlagShortCircuits(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "braille")
	prev := showVersion
	showVersion = true
	t.Cleanup(func() { showVersion = prev })

	c, out, errOut := captureCmd()
	if err := rootRunE(c, nil); err != nil {
		t.Fatalf("rootRunE: %v", err)
	}
	if errOut.String() != "" {
		t.Errorf("unexpected stderr: %q", errOut.String())
	}
	if !strings.Contains(out.String(), "portato dev") {
		t.Errorf("--version output missing version line: %q", out.String())
	}
	if !hasBrailleGlyph(out.String()) {
		t.Errorf("--version output missing braille logo:\n%s", out.String())
	}
}
