package tui

import (
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

// detectKind picks a theme from the environment:
//   - NO_COLOR set (any value) → monochrome (no ANSI foregrounds).
//   - COLORFGBG "fg;bg" with bg ≤ 6 → dark; otherwise light.
//   - default → dark (the historical palette).
func detectKind() themeKind {
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
	dim         lipgloss.Style
	err         lipgloss.Style
	warn        lipgloss.Style
	footer      lipgloss.Style
	helpTitle   lipgloss.Style
	helpPanel   lipgloss.Style
	modal       lipgloss.Style
	editorTitle lipgloss.Style
	editorLabel lipgloss.Style
	state       map[controller.State]lipgloss.Style
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
		selected:    lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("63")).Foreground(lipgloss.Color("15")),
		dim:         lipgloss.NewStyle().Faint(true),
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

// lightPalette uses darker foregrounds that stay readable on a light terminal
// background (deeper green/red/orange, a blue title, a dark-on-light cursor).
func lightPalette() palette {
	return palette{
		title:       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("26")),
		mode:        lipgloss.NewStyle().Faint(true),
		header:      lipgloss.NewStyle().Bold(true).Faint(true),
		selected:    lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("26")).Foreground(lipgloss.Color("255")),
		dim:         lipgloss.NewStyle().Faint(true),
		err:         lipgloss.NewStyle().Foreground(lipgloss.Color("124")),
		warn:        lipgloss.NewStyle().Foreground(lipgloss.Color("130")),
		footer:      lipgloss.NewStyle().Faint(true),
		helpTitle:   lipgloss.NewStyle().Bold(true).Underline(true),
		helpPanel:   lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(0, 2),
		modal:       lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(1, 3).Bold(true),
		editorTitle: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("26")),
		editorLabel: lipgloss.NewStyle().Bold(true),
		state: map[controller.State]lipgloss.Style{
			controller.Off:          lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
			controller.Connecting:   lipgloss.NewStyle().Foreground(lipgloss.Color("130")),
			controller.Connected:    lipgloss.NewStyle().Foreground(lipgloss.Color("28")),
			controller.Reconnecting: lipgloss.NewStyle().Foreground(lipgloss.Color("130")),
			controller.Error:        lipgloss.NewStyle().Foreground(lipgloss.Color("124")),
		},
	}
}

// monoPalette keeps the layout but drops all foreground colours (NO_COLOR).
// State is still distinguishable via the indicator glyph (○/✗/●) and label
// text; the cursor uses reverse video so it stays visible.
func monoPalette() palette {
	return palette{
		title:       lipgloss.NewStyle().Bold(true),
		mode:        lipgloss.NewStyle().Faint(true),
		header:      lipgloss.NewStyle().Bold(true).Faint(true),
		selected:    lipgloss.NewStyle().Reverse(true),
		dim:         lipgloss.NewStyle().Faint(true),
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
