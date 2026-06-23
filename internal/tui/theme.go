package tui

import (
	"image/color"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/kipkaev55/portato/internal/controller"
)

// themeKind is the resolved colour scheme for the TUI. Phase 11.
type themeKind int

const (
	themeDark themeKind = iota
	themeLight
	themeMono
)

// detectKind picks a theme from the environment, in priority order:
//   - PORTATO_THEME (light|dark|mono) forces that theme; "auto"/empty falls
//     through to detection.
//   - NO_COLOR set (any value) → monochrome (no ANSI foregrounds).
//   - COLORFGBG "fg;bg" with bg ≤ 6 → dark; otherwise light.
//   - default → dark (the historical palette).
func detectKind() themeKind {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PORTATO_THEME"))) {
	case "light":
		return themeLight
	case "dark":
		return themeDark
	case "mono", "nocolor":
		return themeMono
	}
	if os.Getenv("NO_COLOR") != "" {
		return themeMono
	}
	if fg := os.Getenv("COLORFGBG"); fg != "" {
		parts := strings.Split(fg, ";")
		if len(parts) >= 2 {
			if bg, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
				if bg <= 6 {
					return themeDark
				}
				return themeLight
			}
		}
	}
	return themeDark
}

// palette holds every resolved lipgloss style the TUI uses. All package-level
// style variables in styles.go are aliases into one palette so the call sites
// (view.go / editor.go / logs.go) never change.
type palette struct {
	title       lipgloss.Style
	mode        lipgloss.Style
	header      lipgloss.Style
	selected    lipgloss.Style
	cursor      lipgloss.Style
	dim         lipgloss.Style
	body        lipgloss.Style
	err         lipgloss.Style
	warn        lipgloss.Style
	footer      lipgloss.Style
	helpTitle   lipgloss.Style
	helpPanel   lipgloss.Style
	modal       lipgloss.Style
	editorTitle lipgloss.Style
	editorLabel lipgloss.Style
	state       map[controller.State]lipgloss.Style
	// surfaceBg, when non-nil, is painted across the whole TUI surface (a
	// real "light mode" background). Nil = transparent (use the terminal's
	// own background). Only the light theme sets it.
	surfaceBg color.Color
}

func resolvePalette(kind themeKind) palette {
	switch kind {
	case themeLight:
		return lightPalette()
	case themeMono:
		return monoPalette()
	default:
		return darkPalette()
	}
}

// darkPalette is the historical palette (the pre-Phase-11 colours).
func darkPalette() palette {
	return palette{
		title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")),
		mode:        lipgloss.NewStyle().Faint(true),
		header:      lipgloss.NewStyle().Bold(true).Faint(true),
		selected:    lipgloss.NewStyle().Bold(true),
		cursor:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")),
		dim:         lipgloss.NewStyle().Faint(true),
		body:        lipgloss.NewStyle(),
		err:         lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		warn:        lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		footer:      lipgloss.NewStyle().Faint(true),
		helpTitle:   lipgloss.NewStyle().Bold(true).Underline(true),
		helpPanel:   lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(0, 2),
		modal:       lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(1, 3).Bold(true),
		editorTitle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")),
		editorLabel: lipgloss.NewStyle().Bold(true),
		state: map[controller.State]lipgloss.Style{
			controller.Off:          lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
			controller.Connecting:   lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
			controller.Connected:    lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
			controller.Reconnecting: lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
			controller.Error:        lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		},
	}
}

// lightPalette uses darker foregrounds that stay readable on a light surface
// (deeper green/red/orange, a blue title/cursor, a dark grey for dim text).
// Faint is avoided: on a light background it collapses to an unreadable pale
// grey. withBackground bakes the surface colour into every style so each
// styled glyph carries its own background regardless of any outer wrapper —
// the raw cells between styled runs are then covered by fillBg.
func lightPalette() palette {
	bg := lipgloss.Color("#FAFAFA")
	p := palette{
		title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("26")),
		mode:        lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		header:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("240")),
		selected:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("235")),
		cursor:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("26")),
		dim:         lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		body:        lipgloss.NewStyle().Foreground(lipgloss.Color("235")),
		err:         lipgloss.NewStyle().Foreground(lipgloss.Color("124")),
		warn:        lipgloss.NewStyle().Foreground(lipgloss.Color("166")),
		footer:      lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		helpTitle:   lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("235")),
		helpPanel:   lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(0, 2).Foreground(lipgloss.Color("235")),
		modal:       lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(1, 3).Bold(true).Foreground(lipgloss.Color("235")),
		editorTitle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("26")),
		editorLabel: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("235")),
		state: map[controller.State]lipgloss.Style{
			controller.Off:          lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
			controller.Connecting:   lipgloss.NewStyle().Foreground(lipgloss.Color("166")),
			controller.Connected:    lipgloss.NewStyle().Foreground(lipgloss.Color("28")),
			controller.Reconnecting: lipgloss.NewStyle().Foreground(lipgloss.Color("166")),
			controller.Error:        lipgloss.NewStyle().Foreground(lipgloss.Color("124")),
		},
		surfaceBg: bg,
	}
	p = p.withBackground(bg)
	return p
}

// withBackground returns a copy of the palette with bg set as the background of
// every style (including the per-state styles). Only the light theme uses it;
// dark/mono stay transparent and rely on the terminal's own background.
func (p palette) withBackground(bg color.Color) palette {
	if bg == nil {
		return p
	}
	p.title = p.title.Background(bg)
	p.mode = p.mode.Background(bg)
	p.header = p.header.Background(bg)
	p.selected = p.selected.Background(bg)
	p.cursor = p.cursor.Background(bg)
	p.dim = p.dim.Background(bg)
	p.body = p.body.Background(bg)
	p.err = p.err.Background(bg)
	p.warn = p.warn.Background(bg)
	p.footer = p.footer.Background(bg)
	p.helpTitle = p.helpTitle.Background(bg)
	p.helpPanel = p.helpPanel.Background(bg)
	p.modal = p.modal.Background(bg)
	p.editorTitle = p.editorTitle.Background(bg)
	p.editorLabel = p.editorLabel.Background(bg)
	states := make(map[controller.State]lipgloss.Style, len(p.state))
	for k, v := range p.state {
		states[k] = v.Background(bg)
	}
	p.state = states
	return p
}

// monoPalette keeps the layout but drops all foreground colours (NO_COLOR).
// State is still distinguishable via the indicator glyph (○/✗/●) and label
// text; the cursor is bold to stay visible without colour.
func monoPalette() palette {
	return palette{
		title:       lipgloss.NewStyle().Bold(true),
		mode:        lipgloss.NewStyle().Faint(true),
		header:      lipgloss.NewStyle().Bold(true).Faint(true),
		selected:    lipgloss.NewStyle().Bold(true),
		cursor:      lipgloss.NewStyle().Bold(true),
		dim:         lipgloss.NewStyle().Faint(true),
		body:        lipgloss.NewStyle(),
		err:         lipgloss.NewStyle().Bold(true),
		warn:        lipgloss.NewStyle().Bold(true),
		footer:      lipgloss.NewStyle().Faint(true),
		helpTitle:   lipgloss.NewStyle().Bold(true).Underline(true),
		helpPanel:   lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(0, 2),
		modal:       lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(1, 3).Bold(true),
		editorTitle: lipgloss.NewStyle().Bold(true),
		editorLabel: lipgloss.NewStyle().Bold(true),
		state: map[controller.State]lipgloss.Style{
			controller.Off:          lipgloss.NewStyle(),
			controller.Connecting:   lipgloss.NewStyle(),
			controller.Connected:    lipgloss.NewStyle().Bold(true),
			controller.Reconnecting: lipgloss.NewStyle(),
			controller.Error:        lipgloss.NewStyle().Bold(true),
		},
	}
}
