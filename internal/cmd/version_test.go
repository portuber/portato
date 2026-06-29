package cmd

import (
	"os"
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

// TestPrintVersion_TTYBraille verifies the banner renders the logo plus the
// version/commit/date line.
func TestPrintVersion_TTYBraille(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "braille")
	var b strings.Builder
	printVersion(&b, true)
	out := b.String()
	if !strings.Contains(out, "portato dev (unknown, unknown)") {
		t.Errorf("version line missing: %q", out)
	}
	if !hasBrailleGlyph(out) {
		t.Errorf("braille logo missing:\n%s", out)
	}
}

// TestPrintVersion_PipedImageNoOSC verifies pipe-safety: with image mode on a
// non-TTY the inline image (and all ANSI) is suppressed and braille is used.
func TestPrintVersion_PipedImageNoOSC(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "image")
	var b strings.Builder
	printVersion(&b, false)
	out := b.String()
	if strings.Contains(out, "\x1b]1337") {
		t.Errorf("piped output must not contain an inline image:\n%s", out)
	}
	if !strings.Contains(out, "portato dev") {
		t.Errorf("version line missing: %q", out)
	}
	if !hasBrailleGlyph(out) {
		t.Errorf("piped output should still carry the braille logo:\n%s", out)
	}
}

// TestPrintVersion_OffOmitsLogo verifies PORTATO_LOGO=off leaves only the
// version line (no logo, no ANSI).
func TestPrintVersion_OffOmitsLogo(t *testing.T) {
	t.Setenv("PORTATO_LOGO", "off")
	var b strings.Builder
	printVersion(&b, true)
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

// TestIsTerminal covers the pipe-safety guard: a regular file is not a
// terminal; the check is nil-safe.
func TestIsTerminal(t *testing.T) {
	if isTerminal(nil) {
		t.Error("isTerminal(nil) should be false")
	}
	f, err := os.CreateTemp(t.TempDir(), "notatty")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Error("a regular file must not be reported as a terminal")
	}
}
