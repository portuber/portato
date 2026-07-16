package tui

import (
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
