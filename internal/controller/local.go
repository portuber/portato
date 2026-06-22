package controller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/forward"
	routelog "github.com/kipkaev55/portato/internal/log"
)

type Local struct {
	engine  *forward.Engine
	cfg     *config.Config
	cfgPath string
	log     *slog.Logger
	ring    *routelog.Ring

	ctx    context.Context
	cancel context.CancelFunc

	initOnce  sync.Once
	closeOnce sync.Once
	changes   chan struct{}
	unsub     func()
	done      chan struct{}
}

func NewLocal(cfg *config.Config, cfgPath string, log *slog.Logger, ring *routelog.Ring) *Local {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Local{
		engine:  forward.NewEngine(ctx, cfg, log),
		cfg:     cfg,
		cfgPath: cfgPath,
		log:     log,
		ring:    ring,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (l *Local) List() []Status { return l.engine.List() }

// Enable starts a tunnel and persists enabled=true to the config file. This
// is the standalone-side mirror of the daemon's enable handler and the
// invariant the standalone->daemon hand-off relies on (SPEC §6): the config
// on disk is always the source of truth for which tunnels should be up.
func (l *Local) Enable(name string) error {
	if err := l.engine.Enable(name); err != nil {
		return err
	}
	l.setEnabled(name, true)
	return l.cfg.Save(l.cfgPath)
}

// Disable stops a tunnel and persists enabled=false to the config file.
func (l *Local) Disable(name string) error {
	if err := l.engine.Disable(name); err != nil {
		return err
	}
	l.setEnabled(name, false)
	return l.cfg.Save(l.cfgPath)
}

func (l *Local) Restart(name string) error { return l.engine.Restart(name) }

func (l *Local) Reload() error {
	cfg, err := config.Load(l.cfgPath)
	if err != nil {
		return err
	}
	l.engine.Reload(cfg)
	l.cfg = cfg
	return nil
}

// Config returns a deep copy of the current in-memory configuration. The TUI
// editor uses it to prefill forms and to enforce name uniqueness without
// touching the file.
func (l *Local) Config() (*config.Config, error) {
	return l.cfg.Clone(), nil
}

// AddTunnel validates the new tunnel against the current config, then applies
// a comment-preserving append to the YAML file and reloads. The file is not
// written unless validation passes.
func (l *Local) AddTunnel(t config.Tunnel) error {
	if _, err := l.cfg.WithTunnelAdded(t); err != nil {
		return err
	}
	if err := config.AddTunnelNode(l.cfgPath, t); err != nil {
		return err
	}
	return l.Reload()
}

// UpdateTunnel replaces the tunnel named name with t (rename allowed): validate
// the prospective config, patch the file, reload.
func (l *Local) UpdateTunnel(name string, t config.Tunnel) error {
	if _, err := l.cfg.WithTunnelReplaced(name, t); err != nil {
		return err
	}
	if err := config.ReplaceTunnelNode(l.cfgPath, name, t); err != nil {
		return err
	}
	return l.Reload()
}

// DeleteTunnel removes the tunnel named name: validate, patch, reload. If the
// tunnel is active, the engine reload stops and drops it.
func (l *Local) DeleteTunnel(name string) error {
	if _, err := l.cfg.WithTunnelRemoved(name); err != nil {
		return err
	}
	if err := config.DeleteTunnelNode(l.cfgPath, name); err != nil {
		return err
	}
	return l.Reload()
}

// Logs returns the recent in-memory log entries for name from the shared ring
// buffer (nil-safe: an unconfigured ring yields nil). Phase 11.
func (l *Local) Logs(name string) ([]routelog.Entry, error) {
	return l.ring.Lines(name), nil
}

func (l *Local) setEnabled(name string, enabled bool) {
	for i := range l.cfg.Tunnels {
		if l.cfg.Tunnels[i].Name == name {
			l.cfg.Tunnels[i].Enabled = enabled
			return
		}
	}
}

// Changes returns a push channel fed by the Engine's event broker (Phase 9).
// A forwarder goroutine copies Engine signals into an owned, drop-old channel
// so the channel can be closed cleanly on Close(). No polling ticker: every
// tunnel state transition reaches the TUI instantly.
func (l *Local) Changes() <-chan struct{} {
	l.initOnce.Do(func() {
		l.changes = make(chan struct{}, 1)
		l.done = make(chan struct{})
		sub, unsub := l.engine.Subscribe()
		l.unsub = unsub
		go l.forward(sub)
	})
	return l.changes
}

func (l *Local) forward(sub <-chan struct{}) {
	defer close(l.done)
	for {
		select {
		case <-l.ctx.Done():
			return
		case _, ok := <-sub:
			if !ok {
				return
			}
			select {
			case l.changes <- struct{}{}:
			default:
			}
		}
	}
}

func (l *Local) Close() error {
	l.closeOnce.Do(func() {
		l.cancel()
		if l.unsub != nil {
			l.unsub()
		}
		if l.done != nil {
			select {
			case <-l.done:
			case <-time.After(2 * time.Second):
			}
		}
		if l.changes != nil {
			close(l.changes)
		}
		l.engine.StopAll()
	})
	return nil
}
