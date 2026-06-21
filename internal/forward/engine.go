package forward

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kipkaev55/portato/internal/config"
)

type tunneler interface {
	Start(ctx context.Context) error
	Stop() error
	Restart() error
	// Reconfigure updates a tunnel's config/defaults in place; it restarts the
	// tunnel only if it is currently running. Used by Engine.Reload.
	Reconfigure(cfg config.Tunnel, def config.Defaults) error
	Status() Status
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
func NewEngine(ctx context.Context, cfg *config.Config, log *slog.Logger) *Engine {
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
	}
	e.factory = func(t config.Tunnel, d config.Defaults, l *slog.Logger) tunneler {
		tn := NewTunnel(ctx, t, d, l)
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

// Reload applies a new config: tunnels no longer present are stopped and
// removed, new tunnels are added, and changed tunnels are restarted.
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
			e.tunnels[name] = e.factory(t, cfg.Defaults, e.log)
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
