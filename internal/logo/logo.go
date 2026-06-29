// Package logo renders the Portato potato logo for the TUI (the empty-list
// splash and the help overlay) and for the CLI `--version` banner. It picks
// the best rendering the active terminal supports — an inline PNG via the
// iTerm2/WezTerm OSC 1337 sequence, the outline-braille ASCII variant, or the
// solid-block fallback for legacy Windows consoles — and tints the ASCII
// variants with the active theme's accent foreground (skipped under NO_COLOR
// / a monochrome theme).
package logo

import (
	_ "embed"
	"fmt"
	"os"
	"runtime"
	"strings"

	"charm.land/lipgloss/v2"
)

// Mode selects which logo rendering Render returns.
type Mode int

const (
	// ModeImage emits the inline PNG via the iTerm2/WezTerm OSC 1337 sequence.
	ModeImage Mode = iota
	// ModeBraille emits the outline-braille ASCII variant (primary ASCII mode).
	ModeBraille
	// ModeBlock emits the solid-block ASCII variant (legacy Windows fallback).
	ModeBlock
	// ModeOff suppresses the big logo everywhere.
	ModeOff
)

// logoWidth/logoHeight are the footprint both ASCII variants and the inline
// PNG occupy, in terminal cells. The embedded txt files are generated at this
// size and the OSC 1337 sequence sizes the PNG to the same grid.
const (
	logoWidth  = 28
	logoHeight = 12
)

//go:embed assets/logo.braille.txt
var brailleArt string

//go:embed assets/logo-block.txt
var blockArt string

//go:embed assets/logo.png
var pngBytes []byte

// goos is read through a var so Detect is unit-testable across platforms
// (runtime.GOOS cannot be overridden). It defaults to the build's GOOS.
var goos = runtime.GOOS

// Detect resolves the logo mode from the environment. PORTATO_LOGO forces a
// mode (auto/image/braille/block/off; common truthy aliases map to auto); an
// empty value or "auto" falls through to terminal detection: iTerm2 or
// WezTerm (TERM_PROGRAM) -> inline PNG; GOOS=windows -> solid block (robust
// on legacy conhost, where the sparse outline-block reads as fragmented);
// otherwise -> braille.
func Detect() Mode {
	switch logoEnv() {
	case "image":
		return ModeImage
	case "braille":
		return ModeBraille
	case "block":
		return ModeBlock
	case "off":
		return ModeOff
	}
	if isImageTerm() {
		return ModeImage
	}
	if goos == "windows" {
		return ModeBlock
	}
	return ModeBraille
}

// logoEnv reads and normalises PORTATO_LOGO to one of auto/image/braille/
// block/off (lower-cased, trimmed). Truthy generic values ("1", "true",
// "yes", "on") collapse to "auto" so `PORTATO_LOGO=1` just enables branding.
func logoEnv() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PORTATO_LOGO")))
	switch v {
	case "1", "true", "yes", "on":
		return "auto"
	}
	return v
}

// isImageTerm reports whether the terminal advertises iTerm2 or WezTerm,
// both of which honour the OSC 1337 inline-image sequence.
func isImageTerm() bool {
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm":
		return true
	}
	return false
}

// Render returns the logo string for mode. The ASCII variants are tinted with
// accent (the theme's title/accent foreground) unless mono is true (NO_COLOR
// or a monochrome theme), in which case the glyphs render plain. ModeOff (and
// any unknown value) returns "". The trailing newline of the embedded art is
// trimmed so the caller controls the line spacing.
func Render(mode Mode, accent lipgloss.Style, mono bool) string {
	switch mode {
	case ModeImage:
		return inlineImage(pngBytes)
	case ModeBraille:
		return tinted(brailleArt, accent, mono)
	case ModeBlock:
		return tinted(blockArt, accent, mono)
	case ModeOff:
		return ""
	}
	return ""
}

// tinted renders art (with its trailing newline trimmed) via accent when
// colour is enabled, or verbatim under mono. The mono path returns the raw
// glyphs so NO_COLOR stays clean (no bold/colour drift).
func tinted(art string, accent lipgloss.Style, mono bool) string {
	art = strings.TrimRight(art, "\n")
	if mono {
		return art
	}
	return accent.Render(art)
}

// Banner is the convenience for the TUI: it renders the detected mode with
// the given accent style. Call Render directly when the mode is already known.
func Banner(accent lipgloss.Style, mono bool) string {
	return Render(Detect(), accent, mono)
}

// VersionBanner renders the `--version` banner: the 28x12 logo (via the
// detected mode) followed by a "portato <version> (<commit>, <date>)" line.
// It is pipe-safe: when tty is false (stdout is not a terminal) the inline
// image and all ANSI are suppressed and the braille variant is used, so
// `portato --version | head` stays clean. The ASCII art is untinted here (the
// CLI banner does not load the TUI theme); the TUI splash/help tint via Render.
func VersionBanner(version, commit, date string, tty bool) string {
	art := ""
	switch Detect() {
	case ModeImage:
		if tty {
			art = inlineImage(pngBytes)
		} else {
			art = strings.TrimRight(brailleArt, "\n")
		}
	case ModeBraille:
		art = strings.TrimRight(brailleArt, "\n")
	case ModeBlock:
		art = strings.TrimRight(blockArt, "\n")
	case ModeOff:
		// PORTATO_LOGO=off: no logo, just the version line.
	}
	line := fmt.Sprintf("portato %s (%s, %s)", version, commit, date)
	if art == "" {
		return line
	}
	return art + "\n\n" + line
}
