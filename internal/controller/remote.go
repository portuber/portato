package controller

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kipkaev55/portato/internal/client"
	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/forward"
	routelog "github.com/kipkaev55/portato/internal/log"
)

// daemonClient is the subset of *client.Client that Remote needs. Kept
// unexported so tests can inject a fake without standing up a real daemon.
type daemonClient interface {
	List() ([]forward.Status, error)
	Enable(name string) error
	Disable(name string) error
	Restart(name string) error
	Reload() error
	Events(ctx context.Context) (io.ReadCloser, error)
	Config() (*config.Config, error)
	AddTunnel(t config.Tunnel) error
	UpdateTunnel(name string, t config.Tunnel) error
	DeleteTunnel(name string) error
	Logs(name string) ([]routelog.Entry, error)
	AcceptHost(name string) error
	SetPassphrase(name, passphrase string) error
}

// Remote is a Controller backed by the daemon via an HTTP client over a unix
// socket. It is the attach/CLI counterpart of Local. Changes() drives the TUI
// from the daemon's SSE event stream (Phase 9): a state change on the daemon
// reaches attached clients instantly, with no 1s polling.
type Remote struct {
	client daemonClient

	initOnce  sync.Once
	closeOnce sync.Once
	changes   chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
}

// NewRemote wraps a daemon client as a Controller.
func NewRemote(c *client.Client) *Remote {
	ctx, cancel := context.WithCancel(context.Background())
	return &Remote{client: c, ctx: ctx, cancel: cancel}
}

func newRemote(c daemonClient) *Remote {
	ctx, cancel := context.WithCancel(context.Background())
	return &Remote{client: c, ctx: ctx, cancel: cancel}
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

// Config fetches the daemon's current configuration for the TUI editor. The
// daemon owns the config file (it knows the real --config path), so attached
// clients read it through the API rather than touching disk. Phase 10.
func (r *Remote) Config() (*config.Config, error) { return r.client.Config() }

func (r *Remote) AddTunnel(t config.Tunnel) error { return r.client.AddTunnel(t) }

func (r *Remote) UpdateTunnel(name string, t config.Tunnel) error {
	return r.client.UpdateTunnel(name, t)
}

func (r *Remote) DeleteTunnel(name string) error { return r.client.DeleteTunnel(name) }

// Logs fetches the daemon's recent in-memory log entries for the TUI logs
// screen. The daemon owns the ring buffer. Phase 11.
func (r *Remote) Logs(name string) ([]routelog.Entry, error) { return r.client.Logs(name) }

// AcceptHost asks the daemon to append the tunnel's pending unknown-host key
// and restart it (Phase 11 TOFU prompt).
func (r *Remote) AcceptHost(name string) error { return r.client.AcceptHost(name) }

// AcceptPassphrase sends the tunnel's identity passphrase to the daemon, which
// stores it and unblocks a dial waiting on it (Phase 19 passphrase prompt).
func (r *Remote) AcceptPassphrase(name, passphrase string) error {
	return r.client.SetPassphrase(name, passphrase)
}

// LiveListenerFiles is a no-op for the remote (attach) controller: it owns no
// tunnels and therefore no local listeners to hand off. The hand-off only runs
// in standalone mode, so this never participates; it exists to satisfy the
// Controller interface.
func (r *Remote) LiveListenerFiles() (map[string]*os.File, error) {
	return nil, nil
}

// Changes returns the push channel fed by the daemon's /events SSE stream.
// A goroutine reads frames, reconnects on break with exponential backoff,
// and coalesces bursts via a buffered drop-old channel. The stream closes
// with the channel when Close() is called.
func (r *Remote) Changes() <-chan struct{} {
	r.initOnce.Do(func() {
		r.changes = make(chan struct{}, 1)
		r.done = make(chan struct{})
		go r.streamLoop()
	})
	return r.changes
}

// streamLoop maintains a live SSE subscription to the daemon. On connect it
// reads data frames and forwards them as change signals; on any break it
// reconnects after exponential backoff. Returns when ctx is cancelled.
func (r *Remote) streamLoop() {
	defer close(r.done)
	backoff := streamReconnectBackoff
	for {
		if r.ctx.Err() != nil {
			return
		}
		rc, err := r.client.Events(r.ctx)
		if err != nil {
			if !r.sleep(backoff) {
				return
			}
			backoff = nextStreamBackoff(backoff)
			continue
		}
		backoff = streamReconnectBackoff
		live := r.scanStream(rc)
		_ = rc.Close()
		if !live {
			return // ctx cancelled
		}
		// Stream broke mid-flight: pause briefly, then re-establish.
		if !r.sleep(backoff) {
			return
		}
		backoff = nextStreamBackoff(backoff)
	}
}

// scanStream reads SSE frames until the stream ends or ctx is cancelled. Each
// data frame forwards a non-blocking signal into changes. Heartbeat comment
// lines and blank separators are ignored. Reports whether the loop ended due
// to a stream break (true) vs. ctx cancellation (false).
func (r *Remote) scanStream(rc io.ReadCloser) bool {
	scanner := bufio.NewScanner(rc)
	for scanner.Scan() {
		if r.ctx.Err() != nil {
			return false
		}
		if strings.HasPrefix(scanner.Text(), "data:") {
			select {
			case r.changes <- struct{}{}:
			default:
			}
		}
	}
	// Scanner stops on EOF/read error; a cancelled ctx means we intended to stop.
	return r.ctx.Err() == nil
}

func (r *Remote) sleep(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-r.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *Remote) Close() error {
	r.closeOnce.Do(func() {
		r.cancel()
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

const (
	streamReconnectBackoff = 100 * time.Millisecond
	maxStreamBackoff       = 5 * time.Second
)

func nextStreamBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > maxStreamBackoff {
		d = maxStreamBackoff
	}
	return d
}
