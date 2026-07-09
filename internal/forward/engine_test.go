package forward

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/portuber/portato/internal/config"
)

type fakeTunnel struct {
	cfg        config.Tunnel
	mu         sync.Mutex
	state      State
	starts     atomic.Int64
	stops      atomic.Int64
	restarts   atomic.Int64
	withStarts atomic.Int64
	startErr   error

	// listenerFile/listenerErr drive ListenerFile for hand-off tests. A nil
	// file and nil err yield ErrNoListener (the "nothing to pass" case).
	listenerFile *os.File
	listenerErr  error
	// adopted records the listener passed to StartWith (nil for a plain Start).
	adopted net.Listener
}

func (f *fakeTunnel) Start(ctx context.Context) error {
	f.starts.Add(1)
	f.mu.Lock()
	f.state = Connected
	f.mu.Unlock()
	return f.startErr
}

func (f *fakeTunnel) StartWith(_ context.Context, ln net.Listener) error {
	f.starts.Add(1)
	f.withStarts.Add(1)
	f.mu.Lock()
	f.adopted = ln
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

func (f *fakeTunnel) Reconfigure(cfg config.Tunnel, _ config.Defaults) error {
	f.mu.Lock()
	running := f.state != Off
	f.cfg = cfg
	f.mu.Unlock()
	if running {
		return f.Restart()
	}
	return nil
}

func (f *fakeTunnel) Status() Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Status{Name: f.cfg.Name, Type: f.cfg.Type, State: f.state}
}

func (f *fakeTunnel) ListenerFile() (*os.File, error) {
	if f.listenerErr != nil {
		return nil, f.listenerErr
	}
	if f.listenerFile != nil {
		return f.listenerFile, nil
	}
	return nil, ErrNoListener
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
	if err := e.Enable("a"); err != nil { // a is running -> a changed tunnel must restart
		t.Fatalf("Enable a: %v", err)
	}

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
		t.Errorf("Reload: a.restarts = %d, want 1 (running + remote changed)", fakes["a"].restarts.Load())
	}
	if fakes["a"].cfg.Remote != "changed:2" {
		t.Errorf("Reload: a.cfg.Remote = %q, want changed:2 (reconfigured)", fakes["a"].cfg.Remote)
	}
	if fakes["b"].stops.Load() != 1 {
		t.Errorf("Reload: b.stops = %d, want 1 (removed)", fakes["b"].stops.Load())
	}
	if _, ok := fakes["c"]; !ok {
		t.Error("Reload: new tunnel c was not built")
	}
}

// TestEngineReload_OffChangedUpdatesConfigNotStarted guards the fix: a tunnel
// that is off must have its config updated on Reload but must NOT be started.
func TestEngineReload_OffChangedUpdatesConfigNotStarted(t *testing.T) {
	cfg := &config.Config{Tunnels: []config.Tunnel{tunnelCfg("a")}}
	e, fakes := newTestEngine(cfg)
	// a is off (never enabled).

	changed := tunnelCfg("a")
	changed.Remote = "new:9"
	e.Reload(&config.Config{Tunnels: []config.Tunnel{changed}})

	if fakes["a"].cfg.Remote != "new:9" {
		t.Errorf("off tunnel cfg not updated: Remote = %q", fakes["a"].cfg.Remote)
	}
	if fakes["a"].starts.Load() != 0 {
		t.Errorf("off tunnel was started: starts = %d, want 0", fakes["a"].starts.Load())
	}
	if fakes["a"].restarts.Load() != 0 {
		t.Errorf("off tunnel was restarted: restarts = %d, want 0", fakes["a"].restarts.Load())
	}
}

func TestEngineReloadDefaultsChangedRestarts(t *testing.T) {
	cfg := &config.Config{Defaults: config.Defaults{Identity: "/tmp/a"}, Tunnels: []config.Tunnel{tunnelCfg("a")}}
	e, fakes := newTestEngine(cfg)
	if err := e.Enable("a"); err != nil { // defaults-change restarts only running tunnels now
		t.Fatalf("Enable a: %v", err)
	}

	newCfg := &config.Config{
		Defaults: config.Defaults{Identity: "/tmp/b"},
		Tunnels:  []config.Tunnel{tunnelCfg("a")},
	}
	e.Reload(newCfg)
	if fakes["a"].restarts.Load() != 1 {
		t.Errorf("Reload(defaults changed): a.restarts = %d, want 1", fakes["a"].restarts.Load())
	}
}

// TestTunnelReconfigureUpdatesStatus is the direct regression for the reported
// bug: after editing a tunnel, Status() must show the new Local/Remote (the cfg
// is swapped in place), and an off tunnel must not be started.
func TestTunnelReconfigureUpdatesStatus(t *testing.T) {
	tn := NewTunnel(context.Background(), tunnelCfg("a"), config.Defaults{}, slog.Default(), nil)

	newCfg := tunnelCfg("a")
	newCfg.Remote = "changed:9"
	newCfg.Local = "127.0.0.1:20000"
	if err := tn.Reconfigure(newCfg, config.Defaults{}); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	st := tn.Status()
	if st.Remote != "changed:9" {
		t.Errorf("Status.Remote = %q, want changed:9", st.Remote)
	}
	if st.Local != "127.0.0.1:20000" {
		t.Errorf("Status.Local = %q, want 127.0.0.1:20000", st.Local)
	}
	if st.State != Off {
		t.Errorf("State = %v, want Off (reconfigure must not start an off tunnel)", st.State)
	}
}

// TestEngineReload_RenameRunningRestartsUnderNewName guards the Phase 26 fix:
// a running, enabled tunnel that is renamed must be started under its new name
// instead of being left Off. See docs/phases/phase-26-rename-restart-fix.md.
func TestEngineReload_RenameRunningRestartsUnderNewName(t *testing.T) {
	src := tunnelCfg("a")
	src.Enabled = true
	e, fakes := newTestEngine(&config.Config{Tunnels: []config.Tunnel{src}})
	if err := e.Enable("a"); err != nil { // make it running
		t.Fatalf("Enable a: %v", err)
	}

	renamed := tunnelCfg("c")
	renamed.Enabled = true
	e.Reload(&config.Config{Tunnels: []config.Tunnel{renamed}})

	if fakes["a"].stops.Load() != 1 {
		t.Errorf("old name: a.stops = %d, want 1 (removed)", fakes["a"].stops.Load())
	}
	c, ok := fakes["c"]
	if !ok {
		t.Fatal("Reload: renamed tunnel c was not built")
	}
	if c.starts.Load() != 1 {
		t.Errorf("renamed tunnel: c.starts = %d, want 1 (restart under new name)", c.starts.Load())
	}
	names := make(map[string]bool)
	for _, s := range e.List() {
		names[s.Name] = true
	}
	if !names["c"] || names["a"] {
		t.Errorf("Reload: resulting set = %v, want {c}", names)
	}
}

// TestEngineReload_NewEnabledTunnelStarts: a newly-added tunnel whose config has
// Enabled == true is started on Reload (mirrors StartEnabled at boot); an added
// disabled tunnel is not started.
func TestEngineReload_NewEnabledTunnelStarts(t *testing.T) {
	e, fakes := newTestEngine(&config.Config{})

	on := tunnelCfg("on")
	on.Enabled = true
	off := tunnelCfg("off")
	off.Enabled = false
	e.Reload(&config.Config{Tunnels: []config.Tunnel{on, off}})

	if fakes["on"].starts.Load() != 1 {
		t.Errorf("enabled new tunnel: on.starts = %d, want 1", fakes["on"].starts.Load())
	}
	if fakes["off"].starts.Load() != 0 {
		t.Errorf("disabled new tunnel: off.starts = %d, want 0", fakes["off"].starts.Load())
	}
}

// TestEngineReload_RenameOffStaysOff: renaming a tunnel that is off (Enabled
// false) must not start it under the new name.
func TestEngineReload_RenameOffStaysOff(t *testing.T) {
	src := tunnelCfg("a")
	src.Enabled = false
	e, fakes := newTestEngine(&config.Config{Tunnels: []config.Tunnel{src}})

	renamed := tunnelCfg("c")
	renamed.Enabled = false
	e.Reload(&config.Config{Tunnels: []config.Tunnel{renamed}})

	c, ok := fakes["c"]
	if !ok {
		t.Fatal("Reload: renamed tunnel c was not built")
	}
	if c.starts.Load() != 0 {
		t.Errorf("off renamed tunnel: c.starts = %d, want 0", c.starts.Load())
	}
	if c.stops.Load() != 0 {
		t.Errorf("off renamed tunnel: c.stops = %d, want 0", c.stops.Load())
	}
}

func TestEngineLiveListenerFiles(t *testing.T) {
	cfg := &config.Config{Tunnels: []config.Tunnel{tunnelCfg("a"), tunnelCfg("b"), tunnelCfg("c")}}
	e, fakes := newTestEngine(cfg)

	fa, err := os.CreateTemp("", "portato-a-*")
	if err != nil {
		t.Fatalf("createtemp a: %v", err)
	}
	t.Cleanup(func() { _ = fa.Close() })
	t.Cleanup(func() { _ = os.Remove(fa.Name()) })
	fc, err := os.CreateTemp("", "portato-c-*")
	if err != nil {
		t.Fatalf("createtemp c: %v", err)
	}
	t.Cleanup(func() { _ = fc.Close() })
	t.Cleanup(func() { _ = os.Remove(fc.Name()) })
	// "a" and "c" have a live listener; "b" stays without (ErrNoListener).
	fakes["a"].listenerFile = fa
	fakes["c"].listenerFile = fc

	got, err := e.LiveListenerFiles()
	if err != nil {
		t.Fatalf("LiveListenerFiles: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 files, got %d", len(got))
	}
	if got["a"] != fa || got["c"] != fc {
		t.Errorf("unexpected files: a=%p(want %p) c=%p(want %p)", got["a"], fa, got["c"], fc)
	}
}

func TestEngineLiveListenerFiles_HardError(t *testing.T) {
	cfg := &config.Config{Tunnels: []config.Tunnel{tunnelCfg("a")}}
	e, fakes := newTestEngine(cfg)
	fakes["a"].listenerErr = errors.New("boom")

	if _, err := e.LiveListenerFiles(); err == nil {
		t.Fatal("want error for a tunnel that fails to produce its fd, got nil")
	}
}

func TestTunnelListenerFile(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	addr := ln.Addr().String()

	tn := &Tunnel{
		baseCtx:  context.Background(),
		cfg:      tunnelCfg("a"),
		listener: ln,
		running:  true,
	}

	f, err := tn.ListenerFile()
	if err != nil {
		t.Fatalf("ListenerFile: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	// File dups the fd; the original listener must keep accepting.
	connCh := make(chan net.Conn, 1)
	go func() {
		c, e := ln.Accept()
		if e == nil {
			connCh <- c
		}
	}()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	select {
	case got := <-connCh:
		_ = got.Close()
	case <-time.After(time.Second):
		t.Fatal("original listener did not accept after File()")
	}

	// Stopped tunnel -> ErrNoListener.
	tn.running = false
	if _, err := tn.ListenerFile(); !errors.Is(err, ErrNoListener) {
		t.Errorf("stopped: want ErrNoListener, got %v", err)
	}

	// Remote type has no local listener -> ErrNoListener.
	tn.running = true
	tn.cfg.Type = "remote"
	if _, err := tn.ListenerFile(); !errors.Is(err, ErrNoListener) {
		t.Errorf("remote: want ErrNoListener, got %v", err)
	}
}

// TestEngineStartEnabledWith_Adopts: an enabled tunnel with an adopted listener
// is started via StartWith (no bind); a disabled tunnel's adopted listener is
// closed so it does not leak.
func TestEngineStartEnabledWith_Adopts(t *testing.T) {
	on := tunnelCfg("a")
	on.Enabled = true
	off := tunnelCfg("b")
	off.Enabled = false
	e, fakes := newTestEngine(&config.Config{Tunnels: []config.Tunnel{on, off}})

	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ln1: %v", err)
	}
	defer ln1.Close()
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ln2: %v", err)
	}
	adopted := map[string]net.Listener{"a": ln1, "b": ln2}

	e.StartEnabledWith(adopted)

	if fakes["a"].withStarts.Load() != 1 {
		t.Errorf("a: want withStarts=1, got %d", fakes["a"].withStarts.Load())
	}
	if fakes["a"].adopted != ln1 {
		t.Error("a: StartWith did not receive the adopted listener")
	}
	if fakes["b"].starts.Load() != 0 {
		t.Errorf("b: disabled tunnel should not start, got starts=%d", fakes["b"].starts.Load())
	}

	// ln2 belonged to a disabled tunnel: it must have been closed. A closed
	// listener's Accept returns immediately; a live one blocks.
	errCh := make(chan error, 1)
	go func() {
		_, e := ln2.Accept()
		errCh <- e
	}()
	select {
	case e := <-errCh:
		if e == nil {
			t.Error("ln2 Accept returned nil; listener not closed")
		}
	case <-time.After(300 * time.Millisecond):
		t.Error("ln2 Accept still blocking; listener was not closed")
	}
}
