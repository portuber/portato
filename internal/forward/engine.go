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

type tuberer interface {
	Start(ctx context.Context) error
	// StartWith starts the tuber reusing a pre-bound listener (passed in during
	// a hand-off) instead of binding its own. Local/dynamic tubers only.
	StartWith(ctx context.Context, ln net.Listener) error
	Stop() error
	Restart() error
	// Reconfigure updates a tuber's config/defaults in place; it restarts the
	// tuber only if it is currently running. Used by Engine.Reload.
	Reconfigure(cfg config.Tuber, def config.Defaults) error
	Status() Status
	// ListenerFile returns a dup'd fd for the tuber's local listener, or
	// ErrNoListener when there is nothing to pass (stopped / type=remote).
	// Drives the standalone->daemon hand-off FD transfer (Phase 16).
	ListenerFile() (*os.File, error)
}

// Engine is a thread-safe manager of a set of tubers derived from a Config.
// It is the seam that localController (Phase 3) and daemon (Phase 4) build on.
type Engine struct {
	mu       sync.RWMutex
	ctx      context.Context
	cfg      *config.Config
	log      *slog.Logger
	defaults config.Defaults
	tubers   map[string]tuberer
	configs  map[string]config.Tuber
	factory  func(config.Tuber, config.Defaults, *slog.Logger) tuberer
	// provider (Phase 19) supplies identity passphrases to every tuber; nil
	// disables passphrase support. Set once at construction.
	provider PassphraseProvider
	// passwordProvider (Phase 35) supplies SSH account passwords to every tuber
	// that opts into password_auth; nil disables password support. Set once at
	// construction.
	passwordProvider PasswordProvider

	// Event broker (Phase 9): every tuber state change fans out to
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

// Subscribe returns a channel that receives struct{}{} on every tuber state
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

// notify fans a change signal to every subscriber. Called by tubers (via the
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

// NewEngine builds an Engine with all tubers constructed but not started.
// provider (optional, Phase 19) supplies identity passphrases to the tubers;
// nil disables passphrase support (passphrase-protected keys need an agent).
// passwordProvider (optional, Phase 35) supplies SSH account passwords to the
// tubers that opt into password_auth; nil disables password support.
func NewEngine(ctx context.Context, cfg *config.Config, log *slog.Logger, provider PassphraseProvider, passwordProvider PasswordProvider) *Engine {
	if log == nil {
		log = slog.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	e := &Engine{
		ctx:              ctx,
		cfg:              cfg,
		log:              log,
		defaults:         cfg.Defaults,
		tubers:           make(map[string]tuberer),
		configs:          make(map[string]config.Tuber),
		provider:         provider,
		passwordProvider: passwordProvider,
	}
	e.factory = func(t config.Tuber, d config.Defaults, l *slog.Logger) tuberer {
		tn := NewTuber(ctx, t, d, l, e.provider, e.passwordProvider)
		tn.onChange = e.notify
		return tn
	}
	e.buildAll()
	return e
}

func (e *Engine) buildAll() {
	for _, t := range e.cfg.Tubers {
		if _, ok := e.tubers[t.Name]; ok {
			continue
		}
		e.tubers[t.Name] = e.factory(t, e.cfg.Defaults, e.log)
		e.configs[t.Name] = t
	}
}

func (e *Engine) Enable(name string) error {
	tn, ok := e.lookup(name)
	if !ok {
		return fmt.Errorf("unknown tuber %q", name)
	}
	return tn.Start(e.ctx)
}

func (e *Engine) Disable(name string) error {
	tn, ok := e.lookup(name)
	if !ok {
		return fmt.Errorf("unknown tuber %q", name)
	}
	return tn.Stop()
}

func (e *Engine) Restart(name string) error {
	tn, ok := e.lookup(name)
	if !ok {
		return fmt.Errorf("unknown tuber %q", name)
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

// StartEnabled starts every tuber whose config has Enabled == true.
func (e *Engine) StartEnabled() {
	e.mu.RLock()
	jobs := make([]tuberer, 0)
	for _, t := range e.cfg.Tubers {
		if t.Enabled {
			if tn, ok := e.tubers[t.Name]; ok {
				jobs = append(jobs, tn)
			}
		}
	}
	e.mu.RUnlock()
	for _, tn := range jobs {
		_ = tn.Start(e.ctx)
	}
}

// StartEnabledWith starts every enabled tuber, reusing a pre-bound listener
// from adopted (keyed by tuber name) where present -- the standalone->daemon
// hand-off adoption path (Phase 16) -- and binding normally for the rest. Any
// adopted listener whose tuber is unknown or disabled is closed so its port
// does not leak; StartWith errors (e.g. an adopted listener for a type=remote
// tuber, which has no local listener) are logged, not fatal.
func (e *Engine) StartEnabledWith(adopted map[string]net.Listener) {
	e.mu.RLock()
	type job struct {
		tn   tuberer
		ln   net.Listener // nil -> bind normally
		name string
	}
	jobs := make([]job, 0)
	used := make(map[string]bool)
	for _, t := range e.cfg.Tubers {
		if !t.Enabled {
			continue
		}
		tn, ok := e.tubers[t.Name]
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
				e.log.Warn("adopted listener rejected; falling back to bind", "tuber", j.name, "err", err)
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
			e.log.Warn("closing unused adopted listener", "tuber", name)
			_ = ln.Close()
		}
	}
}

// List returns a snapshot of all tuber statuses, in config order.
func (e *Engine) List() []Status {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Status, 0, len(e.tubers))
	seen := make(map[string]bool, len(e.tubers))
	for _, t := range e.cfg.Tubers {
		if tn, ok := e.tubers[t.Name]; ok {
			out = append(out, tn.Status())
			seen[t.Name] = true
		}
	}
	for name, tn := range e.tubers {
		if !seen[name] {
			out = append(out, tn.Status())
		}
	}
	return out
}

// LiveListenerFiles returns one dup'd listener fd per running local/dynamic
// tuber, keyed by tuber name, for the standalone->daemon hand-off (Phase 16).
// Stopped tubers and type=remote tubers (no local listener) are skipped via
// ErrNoListener; a running local/dynamic tuber that fails to produce its fd is
// a hard error, since omitting it would leave the daemon without that listener
// and reintroduce the port-availability gap the hand-off is meant to remove.
// The caller owns each returned *os.File and must close it (typically after the
// daemon has acked adoption).
func (e *Engine) LiveListenerFiles() (map[string]*os.File, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]*os.File)
	for _, t := range e.cfg.Tubers {
		tn, ok := e.tubers[t.Name]
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

// Reload applies a new config: tubers no longer present are stopped and
// removed, new tubers are added (and started if enabled), and changed
// tubers are restarted.
func (e *Engine) Reload(cfg *config.Config) {
	e.mu.Lock()
	defer e.mu.Unlock()

	oldDefaults := e.defaults
	oldConfigs := e.configs
	e.cfg = cfg
	e.defaults = cfg.Defaults

	newSet := make(map[string]config.Tuber, len(cfg.Tubers))
	for _, t := range cfg.Tubers {
		newSet[t.Name] = t
	}

	for name, tn := range e.tubers {
		if _, ok := newSet[name]; !ok {
			_ = tn.Stop()
			delete(e.tubers, name)
		}
	}

	newConfigs := make(map[string]config.Tuber, len(cfg.Tubers))
	for name, t := range newSet {
		newConfigs[name] = t
		old, existed := oldConfigs[name]
		if !existed {
			tn := e.factory(t, cfg.Defaults, e.log)
			e.tubers[name] = tn
			if t.Enabled {
				_ = tn.Start(e.ctx)
			}
			continue
		}
		if tuberChanged(old, t) || oldDefaults != cfg.Defaults {
			// Reconfigure (not bare Restart): update the tuber's cfg so Status()
			// reflects the new Local/Remote, and restart only if it was running —
			// editing an off tuber must not start it.
			_ = e.tubers[name].Reconfigure(t, cfg.Defaults)
		}
	}
	e.configs = newConfigs
	// Reload reshapes the tuber set; ensure subscribers refresh even when
	// the change is only an added/removed Off tuber (no per-tuber notify).
	e.notify()
}

func (e *Engine) lookup(name string) (tuberer, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	tn, ok := e.tubers[name]
	return tn, ok
}

func (e *Engine) snapshot() []tuberer {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]tuberer, 0, len(e.tubers))
	for _, tn := range e.tubers {
		out = append(out, tn)
	}
	return out
}

// tuberChanged reports whether connection-relevant fields differ.
// Enabled is intentionally excluded: toggling it must not restart a tuber.
func tuberChanged(a, b config.Tuber) bool {
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
	if a.PasswordAuth != b.PasswordAuth {
		return true
	}
	return false
}
