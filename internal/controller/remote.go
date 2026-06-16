package controller

import (
	"sync"
	"time"

	"github.com/kipkaev55/portato/internal/client"
	"github.com/kipkaev55/portato/internal/forward"
)

// daemonClient is the subset of *client.Client that Remote needs. Kept
// unexported so tests can inject a fake without standing up a real daemon.
type daemonClient interface {
	List() ([]forward.Status, error)
	Enable(name string) error
	Disable(name string) error
	Restart(name string) error
	Reload() error
}

// Remote is a Controller backed by the daemon via an HTTP client over a unix
// socket. It is the attach/CLI counterpart of Local. Changes() drives the TUI
// via 1s polling (Phase 9 will replace this with push events).
type Remote struct {
	client   daemonClient
	interval time.Duration

	initOnce  sync.Once
	closeOnce sync.Once
	changes   chan struct{}
	stop      chan struct{}
	done      chan struct{}
}

// NewRemote wraps a daemon client as a Controller.
func NewRemote(c *client.Client) *Remote {
	return &Remote{client: c, interval: time.Second}
}

func newRemote(c daemonClient, interval time.Duration) *Remote {
	return &Remote{client: c, interval: interval}
}

func (r *Remote) List() []Status {
	out, err := r.client.List()
	if err != nil {
		return []Status{}
	}
	if out == nil {
		return []Status{}
	}
	return out
}

func (r *Remote) Enable(name string) error  { return r.client.Enable(name) }
func (r *Remote) Disable(name string) error { return r.client.Disable(name) }
func (r *Remote) Restart(name string) error { return r.client.Restart(name) }
func (r *Remote) Reload() error             { return r.client.Reload() }

func (r *Remote) Changes() <-chan struct{} {
	r.initOnce.Do(func() {
		r.changes = make(chan struct{}, 1)
		r.stop = make(chan struct{})
		r.done = make(chan struct{})
		go r.tickLoop()
	})
	return r.changes
}

func (r *Remote) tickLoop() {
	defer close(r.done)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			select {
			case r.changes <- struct{}{}:
			default:
			}
		}
	}
}

func (r *Remote) Close() error {
	r.closeOnce.Do(func() {
		if r.stop != nil {
			close(r.stop)
		}
		if r.done != nil {
			select {
			case <-r.done:
			case <-time.After(2 * time.Second):
			}
		}
		if r.changes != nil {
			close(r.changes)
		}
	})
	return nil
}
