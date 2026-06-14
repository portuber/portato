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
		cfgPath:  cfgPath,
		log:      log,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (l *Local) List() []Status { return l.engine.List() }

func (l *Local) Enable(name string) error  { return l.engine.Enable(name) }
func (l *Local) Disable(name string) error { return l.engine.Disable(name) }
func (l *Local) Restart(name string) error { return l.engine.Restart(name) }

func (l *Local) Reload() error {
	cfg, err := config.Load(l.cfgPath)
	if err != nil {
		return err
	}
	l.engine.Reload(cfg)
	return nil
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
