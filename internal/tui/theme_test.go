package tui

import (
	"math"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/portuber/portato/internal/controller"
)

func TestDetectKind(t *testing.T) {
	// Neutralise both vars by default; each case re-sets exactly what it needs.
	clearThemeEnv := func(t *testing.T) {
		t.Setenv("PORTATO_THEME", "")
		t.Setenv("NO_COLOR", "")
		t.Setenv("COLORFGBG", "")
	}
	cases := []struct {
		name string
		set  func(t *testing.T)
		want themeKind
	}{
		{"default dark", clearThemeEnv, themeDark},
		{"PORTATO_THEME light", func(t *testing.T) {
			clearThemeEnv(t)
			t.Setenv("PORTATO_THEME", "light")
		}, themeLight},
		{"PORTATO_THEME dark", func(t *testing.T) {
			clearThemeEnv(t)
			t.Setenv("PORTATO_THEME", "dark")
		}, themeDark},
		{"PORTATO_THEME mono", func(t *testing.T) {
			clearThemeEnv(t)
			t.Setenv("PORTATO_THEME", "mono")
		}, themeMono},
		{"PORTATO_THEME wins over NO_COLOR", func(t *testing.T) {
			clearThemeEnv(t)
			t.Setenv("NO_COLOR", "1")
			t.Setenv("PORTATO_THEME", "light")
		}, themeLight},
		{"PORTATO_THEME auto falls through to NO_COLOR", func(t *testing.T) {
			clearThemeEnv(t)
			t.Setenv("PORTATO_THEME", "auto")
			t.Setenv("NO_COLOR", "1")
		}, themeMono},
		{"NO_COLOR -> mono", func(t *testing.T) {
			clearThemeEnv(t)
			t.Setenv("NO_COLOR", "1")
		}, themeMono},
		{"COLORFGBG light bg -> light", func(t *testing.T) {
			clearThemeEnv(t)
			t.Setenv("COLORFGBG", "0;15")
		}, themeLight},
		{"COLORFGBG dark bg -> dark", func(t *testing.T) {
			clearThemeEnv(t)
			t.Setenv("COLORFGBG", "15;0")
		}, themeDark},
		{"COLORFGBG malformed -> default dark", func(t *testing.T) {
			clearThemeEnv(t)
			t.Setenv("COLORFGBG", "nope")
		}, themeDark},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.set(t)
			if got := detectKind(); got != c.want {
				t.Errorf("detectKind() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestResolvePaletteAllKinds(t *testing.T) {
	states := []controller.State{
		controller.Off, controller.Connecting, controller.Connected,
		controller.Reconnecting, controller.Error,
	}
	for _, kind := range []themeKind{themeDark, themeLight, themeMono} {
		p := resolvePalette(kind)
		for _, s := range states {
			if _, ok := p.state[s]; !ok {
				t.Errorf("kind %v: state map missing state %v", kind, s)
			}
		}
	}
	// Only the light theme paints a surface background (real light mode);
	// dark and mono stay transparent.
	if darkPalette().surfaceBg != nil {
		t.Errorf("dark theme should not paint a surface background")
	}
	if monoPalette().surfaceBg != nil {
		t.Errorf("mono theme should not paint a surface background")
	}
	if lightPalette().surfaceBg == nil {
		t.Errorf("light theme should paint a surface background")
	}
}

// TestLightPaletteReadableForegrounds verifies the light theme's readability on
// a light surface: every visible style carries an explicit (non-faint) foreground
// tuned for #FAFAFA. The surface itself is NOT baked into the styles (Phase 37:
// baking a per-glyph #FAFAFA background left visible boxes whenever the
// terminal's own background was not #FAFAFA); the surface is provided by View()
// (View.BackgroundColor / fillBg) instead.
func TestLightPaletteReadableForegrounds(t *testing.T) {
	p := lightPalette()
	styles := []struct {
		name string
		st   lipgloss.Style
	}{
		{"title", p.title}, {"mode", p.mode}, {"header", p.header},
		{"selected", p.selected}, {"cursor", p.cursor}, {"dim", p.dim},
		{"body", p.body}, {"err", p.err}, {"warn", p.warn}, {"footer", p.footer},
		{"helpTitle", p.helpTitle}, {"helpPanel", p.helpPanel}, {"modal", p.modal},
		{"editorTitle", p.editorTitle}, {"editorLabel", p.editorLabel},
	}
	for _, s := range styles {
		if s.st.GetFaint() {
			t.Errorf("light style %q must not be faint (unreadable on light bg)", s.name)
		}
		if s.st.GetForeground() == nil {
			t.Errorf("light style %q should carry an explicit foreground tuned for the light surface", s.name)
		}
	}
	for state, st := range p.state {
		if st.GetForeground() == nil {
			t.Errorf("light state %v style should carry an explicit foreground", state)
		}
	}
	if p.dim.GetForeground() == nil {
		t.Errorf("light dim should have an explicit foreground, not faint")
	}
}

func TestResolveKind(t *testing.T) {
	clearEnv := func(t *testing.T) {
		t.Setenv("PORTATO_THEME", "")
		t.Setenv("NO_COLOR", "")
		t.Setenv("COLORFGBG", "")
	}
	cases := []struct {
		name       string
		set        func(t *testing.T)
		bgDark     bool
		hasRuntime bool
		want       themeKind
	}{
		{"runtime dark -> dark", clearEnv, true, true, themeDark},
		{"runtime light -> light", clearEnv, false, true, themeLight},
		{"no runtime, COLORFGBG dark -> dark", func(t *testing.T) {
			clearEnv(t)
			t.Setenv("COLORFGBG", "15;0")
		}, false, false, themeDark},
		{"no runtime, COLORFGBG light -> light", func(t *testing.T) {
			clearEnv(t)
			t.Setenv("COLORFGBG", "0;15")
		}, false, false, themeLight},
		{"no runtime, nothing -> default dark", clearEnv, false, false, themeDark},
		{"runtime answer wins over COLORFGBG", func(t *testing.T) {
			clearEnv(t)
			t.Setenv("COLORFGBG", "0;15") // light bg, but runtime says dark
		}, true, true, themeDark},
		{"PORTATO_THEME light forces regardless of runtime", func(t *testing.T) {
			clearEnv(t)
			t.Setenv("PORTATO_THEME", "light")
		}, true, true, themeLight},
		{"PORTATO_THEME dark forces regardless of runtime", func(t *testing.T) {
			clearEnv(t)
			t.Setenv("PORTATO_THEME", "dark")
		}, false, true, themeDark},
		{"PORTATO_THEME mono forces regardless of runtime", func(t *testing.T) {
			clearEnv(t)
			t.Setenv("PORTATO_THEME", "mono")
		}, false, true, themeMono},
		{"NO_COLOR -> mono regardless of runtime", func(t *testing.T) {
			clearEnv(t)
			t.Setenv("NO_COLOR", "1")
		}, false, true, themeMono},
		{"PORTATO_THEME wins over NO_COLOR", func(t *testing.T) {
			clearEnv(t)
			t.Setenv("NO_COLOR", "1")
			t.Setenv("PORTATO_THEME", "light")
		}, true, true, themeLight},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.set(t)
			if got := resolveKind(c.bgDark, c.hasRuntime); got != c.want {
				t.Errorf("resolveKind(%v,%v) = %v, want %v", c.bgDark, c.hasRuntime, got, c.want)
			}
		})
	}
}

// styleRGB extracts the 8-bit foreground RGB of a lipgloss style through the
// same color model lipgloss renders with (its ANSI/truecolor table). A style
// with no explicit foreground returns black.
func styleRGB(st lipgloss.Style) (r, g, b int) {
	c := st.GetForeground()
	if c == nil {
		return 0, 0, 0
	}
	cr, cg, cb, _ := c.RGBA()
	return int(cr >> 8), int(cg >> 8), int(cb >> 8)
}

// wcagLuminance computes the WCAG 2.x relative luminance of an 8-bit RGB triple.
func wcagLuminance(r, g, b int) float64 {
	ch := func(c int) float64 {
		v := float64(c) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(r) + 0.7152*ch(g) + 0.0722*ch(b)
}

// wcagContrast is the WCAG 2.x contrast ratio between two 8-bit RGB triples.
func wcagContrast(r1, g1, b1, r2, g2, b2 int) float64 {
	l1 := wcagLuminance(r1, g1, b1)
	l2 := wcagLuminance(r2, g2, b2)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// TestDarkPaletteContrastOnDarkHome locks the Phase 37 Task D color fix: every
// dark state color plus the title/cursor/editorTitle, err and warn roles clear
// WCAG AA (>= 4.5:1) on the dark home background #1E1E1E. The state colors are
// truecolor hex values (not ANSI-16 indices): the indices that preceded them
// (2/3) computed below 4.5:1 under lipgloss's own color model and rendered
// unpredictably across terminal palettes.
func TestDarkPaletteContrastOnDarkHome(t *testing.T) {
	const minContrast = 4.5
	const gR, gG, gB = 0x1E, 0x1E, 0x1E
	p := darkPalette()
	styles := []struct {
		name string
		st   lipgloss.Style
	}{
		{"title", p.title}, {"cursor", p.cursor}, {"editorTitle", p.editorTitle},
		{"err", p.err}, {"warn", p.warn},
		{"state[Off]", p.state[controller.Off]},
		{"state[Connecting]", p.state[controller.Connecting]},
		{"state[Connected]", p.state[controller.Connected]},
		{"state[Reconnecting]", p.state[controller.Reconnecting]},
		{"state[Error]", p.state[controller.Error]},
	}
	for _, s := range styles {
		r, g, b := styleRGB(s.st)
		if c := wcagContrast(r, g, b, gR, gG, gB); c < minContrast {
			t.Errorf("dark %s = #%02X%02X%02X contrast on #1E1E1E = %.2f:1, want >= %.1f:1",
				s.name, r, g, b, c, minContrast)
		}
	}
}

// TestLightPaletteContrastOnLightSurface locks the light palette against its
// #FAFAFA surface (painted by fillBg + View.BackgroundColor). The 166 -> #B45309
// swap is the one that crossed the 4.5:1 threshold for connecting/reconnecting/
// warn; the rest already passed.
func TestLightPaletteContrastOnLightSurface(t *testing.T) {
	const minContrast = 4.5
	const gR, gG, gB = 0xFA, 0xFA, 0xFA
	p := lightPalette()
	styles := []struct {
		name string
		st   lipgloss.Style
	}{
		{"title", p.title}, {"cursor", p.cursor}, {"editorTitle", p.editorTitle},
		{"err", p.err}, {"warn", p.warn},
		{"state[Off]", p.state[controller.Off]},
		{"state[Connecting]", p.state[controller.Connecting]},
		{"state[Connected]", p.state[controller.Connected]},
		{"state[Reconnecting]", p.state[controller.Reconnecting]},
		{"state[Error]", p.state[controller.Error]},
	}
	for _, s := range styles {
		r, g, b := styleRGB(s.st)
		if c := wcagContrast(r, g, b, gR, gG, gB); c < minContrast {
			t.Errorf("light %s = #%02X%02X%02X contrast on #FAFAFA = %.2f:1, want >= %.1f:1",
				s.name, r, g, b, c, minContrast)
		}
	}
}
