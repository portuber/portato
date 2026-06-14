package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/kipkaev55/portato/internal/controller"
)

type Model struct {
	ctrl   controller.Controller
	list   []controller.Status
	cursor int
	width  int
	height int
	mode   string
	help   bool
	quit   bool
}

func New(ctrl controller.Controller, mode string) Model {
	return Model{ctrl: ctrl, mode: mode, list: ctrl.List()}
}

type tickMsg struct{}

func waitForChange(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-ch
		return tickMsg{}
	}
}

func (m Model) Init() tea.Cmd {
	return waitForChange(m.ctrl.Changes())
}
