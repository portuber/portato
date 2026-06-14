package controller

import "github.com/kipkaev55/portato/internal/forward"

type (
	State  = forward.State
	Status = forward.Status
)

const (
	Off          = forward.Off
	Connecting   = forward.Connecting
	Connected    = forward.Connected
	Reconnecting = forward.Reconnecting
	Error        = forward.Error
)

type Controller interface {
	List() []Status
	Enable(name string) error
	Disable(name string) error
	Restart(name string) error
	Reload() error
	Changes() <-chan struct{}
	Close() error
}
