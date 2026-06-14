package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/kipkaev55/portato/internal/controller"
)

func Run(ctrl controller.Controller, mode string) error {
	p := tea.NewProgram(New(ctrl, mode))
	_, err := p.Run()
	return err
}
