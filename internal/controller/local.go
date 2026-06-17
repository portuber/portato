package controller

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/forward"
)

type Local struct {
	engine   *forward.Engine
	cfg      *config.Config
	cfgPath  string
	log      *slog.Logger
	interval time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	initOnce sync.Once
	changes  chan struct{}
	ticker   *time.Ticker
	done     chan struct{}

	closeOnce sync.Once
}

func NewLocal(cfg *config.Config, cfgPath string, log *slog.Logger) *Local {
	return newLocal(cfg, cfgPath, log, time.Second)
}

func newLocal(cfg *config.Config, cfgPath string, log *slog.Logger, interval time.Duration) *Local {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Local{
		engine:   forward.NewEngine(ctx, cfg, log),
		cfg:      cfg,
		cfgPath:  cfgPath,
		log:      log,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
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

func (l *Local) setEnabled(name string, enabled bool) {
	for i := range l.cfg.Tunnels {
		if l.cfg.Tunnels[i].Name == name {
			l.cfg.Tunnels[i].Enabled = enabled
			return
		}
	}
}

func (l *Local) Changes() <-chan struct{} {
	l.initOnce.Do(func() {
		l.changes = make(chan struct{}, 1)
		l.ticker = time.NewTicker(l.interval)
		l.done = make(chan struct{})
		go l.tickLoop()
	})
	return l.changes
}

func (l *Local) tickLoop() {
	defer close(l.done)
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-l.ticker.C:
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
		if l.ticker != nil {
			l.ticker.Stop()
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
