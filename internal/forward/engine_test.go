package forward

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kipkaev55/portato/internal/config"
)

type fakeTunnel struct {
	cfg      config.Tunnel
	mu       sync.Mutex
	state    State
	starts   atomic.Int64
	stops    atomic.Int64
	restarts atomic.Int64
	startErr error
}

func (f *fakeTunnel) Start(ctx context.Context) error {
	f.starts.Add(1)
	f.mu.Lock()
	f.state = Connected
	f.mu.Unlock()
	return f.startErr
}

func (f *fakeTunnel) Stop() error {
	f.stops.Add(1)
	f.mu.Lock()
	f.state = Off
	f.mu.Unlock()
	return nil
}

func (f *fakeTunnel) Restart() error {
	f.restarts.Add(1)
	f.mu.Lock()
	f.state = Connected
	f.mu.Unlock()
	return nil
}

func (f *fakeTunnel) Status() Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Status{Name: f.cfg.Name, Type: f.cfg.Type, State: f.state}
}

func newTestEngine(cfg *config.Config) (*Engine, map[string]*fakeTunnel) {
	fakes := make(map[string]*fakeTunnel)
	e := &Engine{
		ctx:      context.Background(),
		cfg:      cfg,
		log:      slog.Default(),
		defaults: cfg.Defaults,
		tunnels:  make(map[string]tunneler),
		configs:  make(map[string]config.Tunnel),
		factory: func(t config.Tunnel, d config.Defaults, l *slog.Logger) tunneler {
			ft := &fakeTunnel{cfg: t}
			fakes[t.Name] = ft
			return ft
		},
	}
	e.buildAll()
	return e, fakes
}

func tunnelCfg(name string) config.Tunnel {
	return config.Tunnel{
		Name: name, Type: "local", Local: "10000",
		Remote: "x:1", SSH: "u@h:22", User: "u", Host: "h", Port: 22,
	}
}

func TestEngineEnableDisableRestart(t *testing.T) {
	cfg := &config.Config{Tunnels: []config.Tunnel{tunnelCfg("a")}}
	e, fakes := newTestEngine(cfg)

	if err := e.Enable("a"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if fakes["a"].starts.Load() != 1 {
		t.Errorf("after Enable: starts = %d, want 1", fakes["a"].starts.Load())
	}
	if err := e.Restart("a"); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if fakes["a"].restarts.Load() != 1 {
		t.Errorf("after Restart: restarts = %d, want 1", fakes["a"].restarts.Load())
	}
	if err := e.Disable("a"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if fakes["a"].stops.Load() != 1 {
		t.Errorf("after Disable: stops = %d, want 1", fakes["a"].stops.Load())
	}
	if err := e.Enable("missing"); err == nil {
		t.Error("Enable(unknown): want error, got nil")
	}
}

func TestEngineListOrder(t *testing.T) {
	cfg := &config.Config{Tunnels: []config.Tunnel{tunnelCfg("a"), tunnelCfg("b"), tunnelCfg("c")}}
	e, _ := newTestEngine(cfg)
	list := e.List()
	if len(list) != 3 {
		t.Fatalf("List len = %d, want 3", len(list))
	}
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if list[i].Name != w {
			t.Errorf("List[%d].Name = %q, want %q", i, list[i].Name, w)
		}
	}
}

func TestEngineUpAllDownAllStartEnabled(t *testing.T) {
	a := tunnelCfg("a")
	a.Enabled = true
	b := tunnelCfg("b")
	b.Enabled = false
	cfg := &config.Config{Tunnels: []config.Tunnel{a, b}}
	e, fakes := newTestEngine(cfg)

	e.StartEnabled()
	if fakes["a"].starts.Load() != 1 {
		t.Errorf("StartEnabled: a.starts = %d, want 1", fakes["a"].starts.Load())
	}
	if fakes["b"].starts.Load() != 0 {
		t.Errorf("StartEnabled: b.starts = %d, want 0", fakes["b"].starts.Load())
	}

	e.UpAll()
	e.DownAll()
	for name, f := range fakes {
		if name == "b" && f.starts.Load() < 1 {
			t.Errorf("UpAll: b.starts = %d, want >= 1", f.starts.Load())
		}
		if f.stops.Load() < 1 {
			t.Errorf("DownAll: %s.stops = %d, want >= 1", name, f.stops.Load())
		}
	}
}

func TestEngineReload(t *testing.T) {
	cfg := &config.Config{Tunnels: []config.Tunnel{tunnelCfg("a"), tunnelCfg("b")}}
	e, fakes := newTestEngine(cfg)

	changed := tunnelCfg("a")
	changed.Remote = "changed:2"
	newCfg := &config.Config{Tunnels: []config.Tunnel{changed, tunnelCfg("c")}}

	e.Reload(newCfg)

	names := make(map[string]bool)
	for _, s := range e.List() {
		names[s.Name] = true
	}
	if !names["a"] || !names["c"] || names["b"] {
		t.Errorf("Reload: resulting set = %v, want {a,c}", names)
	}
	if fakes["a"].restarts.Load() != 1 {
		t.Errorf("Reload: a.restarts = %d, want 1 (remote changed)", fakes["a"].restarts.Load())
	}
	if fakes["b"].stops.Load() != 1 {
		t.Errorf("Reload: b.stops = %d, want 1 (removed)", fakes["b"].stops.Load())
	}
	if _, ok := fakes["c"]; !ok {
		t.Error("Reload: new tunnel c was not built")
	}
}

func TestEngineReloadDefaultsChangedRestarts(t *testing.T) {
	cfg := &config.Config{Defaults: config.Defaults{Identity: "/tmp/a"}, Tunnels: []config.Tunnel{tunnelCfg("a")}}
	e, fakes := newTestEngine(cfg)

	newCfg := &config.Config{
		Defaults: config.Defaults{Identity: "/tmp/b"},
		Tunnels:  []config.Tunnel{tunnelCfg("a")},
	}
	e.Reload(newCfg)
	if fakes["a"].restarts.Load() != 1 {
		t.Errorf("Reload(defaults changed): a.restarts = %d, want 1", fakes["a"].restarts.Load())
	}
}
