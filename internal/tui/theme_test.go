package tui

import (
	"testing"

	"github.com/kipkaev55/portato/internal/controller"
)

func TestDetectKind(t *testing.T) {
	// Neutralise both vars by default; each case re-sets exactly what it needs.
	clearThemeEnv := func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		t.Setenv("COLORFGBG", "")
	}
	cases := []struct {
		name string
		set  func(t *testing.T)
		want themeKind
	}{
		{"default dark", clearThemeEnv, themeDark},
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
}
