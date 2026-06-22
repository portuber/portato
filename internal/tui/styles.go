package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/kipkaev55/portato/internal/controller"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	modeStyle     = lipgloss.NewStyle().Faint(true)
	headerStyle   = lipgloss.NewStyle().Bold(true).Faint(true)
	selectedStyle = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("63")).Foreground(lipgloss.Color("15"))
	dimStyle      = lipgloss.NewStyle().Faint(true)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	footerStyle   = lipgloss.NewStyle().Faint(true)
	helpTitle     = lipgloss.NewStyle().Bold(true).Underline(true)
	helpPanel     = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(0, 2)
	modalStyle    = lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(1, 3).Bold(true)

	editorTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	editorLabelStyle = lipgloss.NewStyle().Bold(true)
)

var stateStyle = map[controller.State]lipgloss.Style{
	controller.Off:          lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	controller.Connecting:   lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
	controller.Connected:    lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	controller.Reconnecting: lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
	controller.Error:        lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
}
