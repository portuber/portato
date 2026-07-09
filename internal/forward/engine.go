package forward

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/portuber/portato/internal/config"
)

type tunneler interface {
	Start(ctx context.Context) error
	// StartWith starts the tunnel reusing a pre-bound listener (passed in during
	// a hand-off) instead of binding its own. Local/dynamic tunnels only.
	StartWith(ctx context.Context, ln net.Listener) error
	Stop() error
	Restart() error
	// Reconfigure updates a tunnel's config/defaults in place; it restarts the
	// tunnel only if it is currently running. Used by Engine.Reload.
	Reconfigure(cfg config.Tunnel, def config.Defaults) error
	Status() Status
	// ListenerFile returns a dup'd fd for the tunnel's local listener, or
	// ErrNoListener when there is nothing to pass (stopped / type=remote).
	// Drives the standalone->daemon hand-off FD transfer (Phase 16).
	ListenerFile() (*os.File, error)
}

// Engine is a thread-safe manager of a set of tunnels derived from a Config.
// It is the seam that localController (Phase 3) and daemon (Phase 4) build on.
type Engine struct {
	mu       sync.RWMutex
	ctx      context.Context
	cfg      *config.Config
	log      *slog.Logger
	defaults config.Defaults
	tunnels  map[string]tunneler
	configs  map[string]config.Tunnel
	factory  func(config.Tunnel, config.Defaults, *slog.Logger) tunneler
	// provider (Phase 19) supplies identity passphrases to every tunnel; nil
	// disables passphrase support. Set once at construction.
	provider PassphraseProvider

	// Event broker (Phase 9): every tunnel state change fans out to
	// subscribers as a non-blocking "something changed" signal. The local
	// controller subscribes directly; the daemon's /events stream forwards
	// the same signal over SSE.
	subMu sync.RWMutex
	subs  map[chan struct{}]struct{}
}

// subscriberBuffer caps each subscriber's signal queue. Sends are non-blocking
// (drop-old): a slow consumer loses intermediate signals but always sees the
// latest state on the next List() — the right trade-off for a UI redraw tick.
const subscriberBuffer = 16

// Subscribe returns a channel that receives struct{}{} on every tunnel state
// change, plus an unsubscribe func that must be called to stop delivery. Safe
// for concurrent use; notify never blocks (drop-old on a full buffer).
func (e *Engine) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, subscriberBuffer)
	e.subMu.Lock()
	if e.subs == nil {
		e.subs = make(map[chan struct{}]struct{})
	}
	e.subs[ch] = struct{}{}
	e.subMu.Unlock()
	var once sync.Once
	unsub := func() {
		once.Do(func() {
			e.subMu.Lock()
			delete(e.subs, ch)
			e.subMu.Unlock()
		})
	}
	return ch, unsub
}

// notify fans a change signal to every subscriber. Called by tunnels (via the
// onChange callback wired in the factory) and internally after engine-level
// mutations. Non-blocking: a full subscriber buffer drops the oldest signal.
func (e *Engine) notify() {
	e.subMu.RLock()
	for ch := range e.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	e.subMu.RUnlock()
}

// NewEngine builds an Engine with all tunnels constructed but not started.
// provider (optional, Phase 19) supplies identity passphrases to the tunnels;
// nil disables passphrase support (passphrase-protected keys need an agent).
func NewEngine(ctx context.Context, cfg *config.Config, log *slog.Logger, provider PassphraseProvider) *Engine {
	if log == nil {
		log = slog.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	e := &Engine{
		ctx:      ctx,
		cfg:      cfg,
		log:      log,
		defaults: cfg.Defaults,
		tunnels:  make(map[string]tunneler),
		configs:  make(map[string]config.Tunnel),
		provider: provider,
	}
	e.factory = func(t config.Tunnel, d config.Defaults, l *slog.Logger) tunneler {
		tn := NewTunnel(ctx, t, d, l, e.provider)
		tn.onChange = e.notify
		return tn
	}
	e.buildAll()
	return e
}

func (e *Engine) buildAll() {
	for _, t := range e.cfg.Tunnels {
		if _, ok := e.tunnels[t.Name]; ok {
			continue
		}
		e.tunnels[t.Name] = e.factory(t, e.cfg.Defaults, e.log)
		e.configs[t.Name] = t
	}
}

func (e *Engine) Enable(name string) error {
	tn, ok := e.lookup(name)
	if !ok {
		return fmt.Errorf("unknown tunnel %q", name)
	}
	return tn.Start(e.ctx)
}

func (e *Engine) Disable(name string) error {
	tn, ok := e.lookup(name)
	if !ok {
		return fmt.Errorf("unknown tunnel %q", name)
	}
	return tn.Stop()
}

func (e *Engine) Restart(name string) error {
	tn, ok := e.lookup(name)
	if !ok {
		return fmt.Errorf("unknown tunnel %q", name)
	}
	return tn.Restart()
}

func (e *Engine) UpAll() {
	for _, tn := range e.snapshot() {
		_ = tn.Start(e.ctx)
	}
}

func (e *Engine) DownAll() {
	for _, tn := range e.snapshot() {
		_ = tn.Stop()
	}
}

// StopAll is an alias for DownAll used at engine shutdown.
func (e *Engine) StopAll() { e.DownAll() }

// StartEnabled starts every tunnel whose config has Enabled == true.
func (e *Engine) StartEnabled() {
	e.mu.RLock()
	jobs := make([]tunneler, 0)
	for _, t := range e.cfg.Tunnels {
		if t.Enabled {
			if tn, ok := e.tunnels[t.Name]; ok {
				jobs = append(jobs, tn)
			}
		}
	}
	e.mu.RUnlock()
	for _, tn := range jobs {
		_ = tn.Start(e.ctx)
	}
}

// StartEnabledWith starts every enabled tunnel, reusing a pre-bound listener
// from adopted (keyed by tunnel name) where present -- the standalone->daemon
// hand-off adoption path (Phase 16) -- and binding normally for the rest. Any
// adopted listener whose tunnel is unknown or disabled is closed so its port
// does not leak; StartWith errors (e.g. an adopted listener for a type=remote
// tunnel, which has no local listener) are logged, not fatal.
func (e *Engine) StartEnabledWith(adopted map[string]net.Listener) {
	e.mu.RLock()
	type job struct {
		tn   tunneler
		ln   net.Listener // nil -> bind normally
		name string
	}
	jobs := make([]job, 0)
	used := make(map[string]bool)
	for _, t := range e.cfg.Tunnels {
		if !t.Enabled {
			continue
		}
		tn, ok := e.tunnels[t.Name]
		if !ok {
			continue
		}
		ln := adopted[t.Name]
		if ln != nil {
			used[t.Name] = true
		}
		jobs = append(jobs, job{tn: tn, ln: ln, name: t.Name})
	}
	e.mu.RUnlock()
	for _, j := range jobs {
		if j.ln != nil {
			if err := j.tn.StartWith(e.ctx, j.ln); err != nil {
				e.log.Warn("adopted listener rejected; falling back to bind", "tunnel", j.name, "err", err)
				_ = j.tn.Start(e.ctx)
			}
		} else {
			_ = j.tn.Start(e.ctx)
		}
	}
	// Close adopted listeners the daemon did not consume (unknown name or
	// disabled): leaving them open would leak the port on the daemon side.
	for name, ln := range adopted {
		if !used[name] {
			e.log.Warn("closing unused adopted listener", "tunnel", name)
			_ = ln.Close()
		}
	}
}

// List returns a snapshot of all tunnel statuses, in config order.
func (e *Engine) List() []Status {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Status, 0, len(e.tunnels))
	seen := make(map[string]bool, len(e.tunnels))
	for _, t := range e.cfg.Tunnels {
		if tn, ok := e.tunnels[t.Name]; ok {
			out = append(out, tn.Status())
			seen[t.Name] = true
		}
	}
	for name, tn := range e.tunnels {
		if !seen[name] {
			out = append(out, tn.Status())
		}
	}
	return out
}

// LiveListenerFiles returns one dup'd listener fd per running local/dynamic
// tunnel, keyed by tunnel name, for the standalone->daemon hand-off (Phase 16).
// Stopped tunnels and type=remote tunnels (no local listener) are skipped via
// ErrNoListener; a running local/dynamic tunnel that fails to produce its fd is
// a hard error, since omitting it would leave the daemon without that listener
// and reintroduce the port-availability gap the hand-off is meant to remove.
// The caller owns each returned *os.File and must close it (typically after the
// daemon has acked adoption).
func (e *Engine) LiveListenerFiles() (map[string]*os.File, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]*os.File)
	for _, t := range e.cfg.Tunnels {
		tn, ok := e.tunnels[t.Name]
		if !ok {
			continue
		}
		f, err := tn.ListenerFile()
		if err != nil {
			if errors.Is(err, ErrNoListener) {
				continue
			}
			return nil, fmt.Errorf("listener %s: %w", t.Name, err)
		}
		out[t.Name] = f
	}
	return out, nil
}

// Reload applies a new config: tunnels no longer present are stopped and
// removed, new tunnels are added (and started if enabled), and changed
// tunnels are restarted.
func (e *Engine) Reload(cfg *config.Config) {
	e.mu.Lock()
	defer e.mu.Unlock()

	oldDefaults := e.defaults
	oldConfigs := e.configs
	e.cfg = cfg
	e.defaults = cfg.Defaults

	newSet := make(map[string]config.Tunnel, len(cfg.Tunnels))
	for _, t := range cfg.Tunnels {
		newSet[t.Name] = t
	}

	for name, tn := range e.tunnels {
		if _, ok := newSet[name]; !ok {
			_ = tn.Stop()
			delete(e.tunnels, name)
		}
	}

	newConfigs := make(map[string]config.Tunnel, len(cfg.Tunnels))
	for name, t := range newSet {
		newConfigs[name] = t
		old, existed := oldConfigs[name]
		if !existed {
			tn := e.factory(t, cfg.Defaults, e.log)
			e.tunnels[name] = tn
			if t.Enabled {
				_ = tn.Start(e.ctx)
			}
			continue
		}
		if tunnelChanged(old, t) || oldDefaults != cfg.Defaults {
			// Reconfigure (not bare Restart): update the tunnel's cfg so Status()
			// reflects the new Local/Remote, and restart only if it was running —
			// editing an off tunnel must not start it.
			_ = e.tunnels[name].Reconfigure(t, cfg.Defaults)
		}
	}
	e.configs = newConfigs
	// Reload reshapes the tunnel set; ensure subscribers refresh even when
	// the change is only an added/removed Off tunnel (no per-tunnel notify).
	e.notify()
}

func (e *Engine) lookup(name string) (tunneler, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	tn, ok := e.tunnels[name]
	return tn, ok
}

func (e *Engine) snapshot() []tunneler {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]tunneler, 0, len(e.tunnels))
	for _, tn := range e.tunnels {
		out = append(out, tn)
	}
	return out
}

// tunnelChanged reports whether connection-relevant fields differ.
// Enabled is intentionally excluded: toggling it must not restart a tunnel.
func tunnelChanged(a, b config.Tunnel) bool {
	if a.Name != b.Name || a.Type != b.Type {
		return true
	}
	if a.Local != b.Local || a.Remote != b.Remote {
		return true
	}
	if a.SSH != b.SSH || a.Identity != b.Identity {
		return true
	}
	if a.User != b.User || a.Host != b.Host || a.Port != b.Port {
		return true
	}
	return false
}
