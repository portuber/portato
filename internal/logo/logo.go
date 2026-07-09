// Package logo renders the Portato logo for the TUI (the empty-list splash
// and the help overlay) and for the CLI `--version` banner. Two ASCII variants
// are embedded: a compact potato and a combined "potato + PORTATO" wordmark,
// each in an outline-braille form (primary) and a solid-block form (the legacy
// Windows fallback). The ASCII glyphs are tinted with the active theme's accent
// foreground (skipped under NO_COLOR / a monochrome theme).
//
// The wordmark is shown in the empty-list splash and the `--version` banner;
// the compact potato is shown in the help overlay and as the splash fallback on
// a narrow terminal. There is no inline-PNG/image mode — iTerm2/WezTerm render
// the braille wordmark like every other terminal.
package logo

import (
	_ "embed"
	"fmt"
	"os"
	"runtime"
	"strings"

	"charm.land/lipgloss/v2"
)

// Mode selects which ASCII variant Render/RenderWordmark return.
type Mode int

const (
	// ModeBraille emits the outline-braille ASCII variant (primary mode).
	ModeBraille Mode = iota
	// ModeBlock emits the solid-block ASCII variant (legacy Windows fallback).
	ModeBlock
	// ModeOff suppresses the logo everywhere.
	ModeOff
)

// artHeight is the row count both the compact potato and the wordmark occupy.
// potatoW/wordmarkW are their cell widths. The embedded txt files are
// generated at these sizes; all lines of each file share its frame width, so
// the caller's centering keeps the art aligned.
const (
	artHeight = 12
	potatoW   = 24
	wordmarkW = 70
)

//go:embed assets/logo.braille.txt
var brailleArt string

//go:embed assets/logo-block.txt
var blockArt string

//go:embed assets/logo-portato.braille.txt
var wordmarkBraille string

//go:embed assets/logo-portato-block.txt
var wordmarkBlock string

// goos is read through a var so Detect is unit-testable across platforms
// (runtime.GOOS cannot be overridden). It defaults to the build's GOOS.
var goos = runtime.GOOS

// Detect resolves the logo mode from the environment. PORTATO_LOGO forces a
// mode (auto/braille/block/off; common truthy aliases map to auto); an empty
// value or "auto" falls through to terminal detection: GOOS=windows -> solid
// block (robust on legacy conhost, where the sparse braille reads as
// fragmented); otherwise -> braille. "image" is accepted as an alias for auto
// (the inline-PNG image mode was removed; image-capable terminals render
// braille).
func Detect() Mode {
	switch logoEnv() {
	case "braille":
		return ModeBraille
	case "block":
		return ModeBlock
	case "off":
		return ModeOff
	}
	if goos == "windows" {
		return ModeBlock
	}
	return ModeBraille
}

// logoEnv reads and normalises PORTATO_LOGO to one of auto/braille/block/off
// (lower-cased, trimmed). "image" collapses to "auto" (the image mode is gone,
// so PORTATO_LOGO=image just enables the default braille branding), and truthy
// generic values ("1", "true", "yes", "on") likewise map to "auto".
func logoEnv() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PORTATO_LOGO")))
	switch v {
	case "1", "true", "yes", "on", "image":
		return "auto"
	}
	return v
}

// EmojiEnabled reports whether the potato emoji (🥔) should mark the TUI
// header before the "Portato" title. PORTATO_LOGO_EMOJI overrides
// (on/off; truthy/falsy aliases map to on/off); otherwise the emoji is shown
// only on GOOS=darwin (where it renders cleanly at 2 cells). It is forced off
// when PORTATO_LOGO=off (branding fully suppressed). The emoji plus its
// trailing space are 3 display cells on darwin, which the header's joinRight
// accounts for via lipgloss.Width.
func EmojiEnabled() bool {
	if logoEnv() == "off" {
		return false
	}
	switch emojiEnv() {
	case "on":
		return true
	case "off":
		return false
	}
	return goos == "darwin"
}

// emojiEnv reads and normalises PORTATO_LOGO_EMOJI to on/off (lower-cased,
// trimmed). Truthy generic values map to "on", falsy ones to "off"; an empty
// or unrecognised value falls through to the GOOS-based default.
func emojiEnv() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PORTATO_LOGO_EMOJI"))) {
	case "on", "1", "true", "yes":
		return "on"
	case "off", "0", "false", "no":
		return "off"
	}
	return ""
}

// Render returns the compact potato for mode, tinted with accent unless mono is
// true (NO_COLOR or a monochrome theme), in which case the glyphs render plain.
// ModeOff (and any unknown value) returns "". The trailing newline of the
// embedded art is trimmed so the caller controls the line spacing.
func Render(mode Mode, accent lipgloss.Style, mono bool) string {
	switch mode {
	case ModeBraille:
		return tinted(brailleArt, accent, mono)
	case ModeBlock:
		return tinted(blockArt, accent, mono)
	case ModeOff:
		return ""
	}
	return ""
}

// RenderWordmark returns the combined "potato + PORTATO" wordmark for mode,
// tinted with accent unless mono. ModeOff (and any unknown value) returns "".
// The wordmark is potatoW-wide; callers gate it on terminal width and fall
// back to the compact potato (Render) when it does not fit.
func RenderWordmark(mode Mode, accent lipgloss.Style, mono bool) string {
	switch mode {
	case ModeBraille:
		return tinted(wordmarkBraille, accent, mono)
	case ModeBlock:
		return tinted(wordmarkBlock, accent, mono)
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

// Banner is the convenience for the TUI: it renders the detected mode's
// compact potato with the given accent style. Call Render directly when the
// mode is already known; call Wordmark for the splash/version wordmark.
func Banner(accent lipgloss.Style, mono bool) string {
	return Render(Detect(), accent, mono)
}

// Wordmark is the convenience for the TUI: it renders the detected mode's
// "potato + PORTATO" wordmark with the given accent style.
func Wordmark(accent lipgloss.Style, mono bool) string {
	return RenderWordmark(Detect(), accent, mono)
}

// VersionBanner renders the `--version` banner: the wordmark (via the detected
// mode) followed by a "portato <version> (<commit>, <date>)" line. It is
// inherently pipe-safe: the art is plain braille/block glyphs with no ANSI and
// no inline-image escape, so `portato --version | head` stays clean. The ASCII
// art is untinted here (the CLI banner does not load the TUI theme); the TUI
// splash tints via RenderWordmark.
func VersionBanner(version, commit, date string) string {
	var art string
	switch Detect() {
	case ModeBraille:
		art = strings.TrimRight(wordmarkBraille, "\n")
	case ModeBlock:
		art = strings.TrimRight(wordmarkBlock, "\n")
	case ModeOff:
		// PORTATO_LOGO=off: no logo, just the version line.
	}
	line := fmt.Sprintf("portato %s (%s, %s)", version, commit, date)
	if art == "" {
		return line
	}
	return art + "\n\n" + line
}
