package controller

import (
	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/forward"
	routelog "github.com/kipkaev55/portato/internal/log"
)

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

	// Config returns a copy of the current configuration. The TUI editor uses
	// it to prefill the edit form and to check name uniqueness. Phase 10.
	Config() (*config.Config, error)
	// AddTunnel appends a new tunnel, persists it and applies the change.
	AddTunnel(t config.Tunnel) error
	// UpdateTunnel replaces the tunnel named name with t (rename allowed),
	// persists and applies it.
	UpdateTunnel(name string, t config.Tunnel) error
	// DeleteTunnel removes the tunnel named name, persists and applies it; an
	// active tunnel is stopped by the engine reload.
	DeleteTunnel(name string) error

	// Logs returns the recent in-memory log entries for the tunnel named name
	// (the Phase 11 ring buffer). An empty name returns every tunnel's logs.
	// The TUI logs screen (l) reads this; in standalone it is the local ring,
	// in attach it is fetched from the daemon. Phase 11.
	Logs(name string) ([]routelog.Entry, error)

	// AcceptHost appends the tunnel's pending unknown-host key (captured when
	// accept_new_hosts is false) to known_hosts and restarts the tunnel so it
	// connects. It errors when the tunnel has no pending key. Phase 11 (TOFU
	// prompt in the TUI).
	AcceptHost(name string) error
}
