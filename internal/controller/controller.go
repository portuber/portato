package controller

import (
	"os"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/forward"
	routelog "github.com/portuber/portato/internal/log"
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
	// AddTuber appends a new tuber, persists it and applies the change.
	AddTuber(t config.Tuber) error
	// UpdateTuber replaces the tuber named name with t (rename allowed),
	// persists and applies it.
	UpdateTuber(name string, t config.Tuber) error
	// DeleteTuber removes the tuber named name, persists and applies it; an
	// active tuber is stopped by the engine reload.
	DeleteTuber(name string) error

	// Logs returns the recent in-memory log entries for the tuber named name
	// (the Phase 11 ring buffer). An empty name returns every tuber's logs.
	// The TUI logs screen (l) reads this; in standalone it is the local ring,
	// in attach it is fetched from the daemon. Phase 11.
	Logs(name string) ([]routelog.Entry, error)

	// AcceptHost appends the tuber's pending unknown-host key (captured when
	// accept_new_hosts is false) to known_hosts and restarts the tuber so it
	// connects. It errors when the tuber has no pending key. Phase 11 (TOFU
	// prompt in the TUI).
	AcceptHost(name string) error

	// AcceptPassphrase provides the passphrase for the tuber's identity
	// (Status.PendingPassphrase) and unblocks a dial waiting on it. The
	// passphrase is stored in the process cache (and the OS keyring when
	// identity_passphrase_store is on); no Restart is needed — the blocked
	// dial wakes on the store. Phase 19 (passphrase prompt in the TUI/CLI).
	AcceptPassphrase(name, passphrase string) error

	// AcceptPassword provides the SSH password for the tuber's account
	// (Status.PendingPassword) and unblocks a dial waiting on it. The password
	// is stored in the process cache (and the OS keyring when
	// ssh_password_store is on); no Restart is needed — the blocked dial wakes
	// on the store. Phase 35 (password prompt in the TUI/CLI). A wrong password
	// is rejected by the server and re-prompted by the dial loop.
	AcceptPassword(name, password string) error

	// LiveListenerFiles returns a dup'd fd for each running local/dynamic
	// tuber's local listener, keyed by tuber name, for the standalone->daemon
	// hand-off (Phase 16). An empty map means there is nothing to pass (no live
	// local listeners) and the hand-off falls back to the Phase 5 close+rebind
	// path. The remote (attach) controller has no live listeners and always
	// returns an empty map.
	LiveListenerFiles() (map[string]*os.File, error)
}
