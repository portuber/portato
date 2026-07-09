package controller

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/forward"
	routelog "github.com/kipkaev55/portato/internal/log"
	"github.com/kipkaev55/portato/internal/secret"
)

type Local struct {
	engine  *forward.Engine
	cfg     *config.Config
	cfgPath string
	log     *slog.Logger
	ring    *routelog.Ring
	// secrets caches identity passphrases for the engine's dials (and persists
	// them to the OS keyring when cfg.Defaults.IdentityPassphraseStore is on).
	// nil only in tests that build a Local literal directly.
	secrets *secret.Store

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
	// The persist closure reads the flag live so a config reload that flips
	// identity_passphrase_store takes effect without rebuilding the store.
	store := secret.NewStore(secret.DefaultBackend(), func() bool {
		return cfg.Defaults.IdentityPassphraseStore
	})
	return &Local{
		engine:  forward.NewEngine(ctx, cfg, log, store),
		cfg:     cfg,
		cfgPath: cfgPath,
		log:     log,
		ring:    ring,
		secrets: store,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (l *Local) List() []Status { return l.engine.List() }

// StartEnabled starts every tunnel whose config has Enabled == true. The
// standalone launcher calls this right after building the controller so its
// initial state matches the daemon's boot-time StartEnabledWith (SPEC §6): an
// enabled:true tunnel is up in both modes, and a hand-off to the daemon brings
// up the same set instead of surprise new tunnels. Not on the Controller
// interface — attach mode never needs it (the daemon already owns its tunnels).
func (l *Local) StartEnabled() {
	l.engine.StartEnabled()
}

// LiveListenerFiles surfaces the engine's live local-listener fds for the
// standalone->daemon hand-off (Phase 16). The standalone sends these to the
// spawned daemon so the local ports never go down across the transition.
func (l *Local) LiveListenerFiles() (map[string]*os.File, error) {
	return l.engine.LiveListenerFiles()
}

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

// AcceptHost appends the tunnel's pending unknown-host key to known_hosts and
// restarts it (Phase 11 TOFU). The pending line was captured by the SSH
// host-key callback at rejection time and carried through Status.
func (l *Local) AcceptHost(name string) error {
	line := pendingHostLine(l.engine.List(), name)
	if line == "" {
		return fmt.Errorf("no pending host key for %q", name)
	}
	hosts := l.cfg.Defaults.ResolvedKnownHosts()
	if err := forward.AppendKnownHostLine(hosts, line); err != nil {
		return fmt.Errorf("append known_hosts: %w", err)
	}
	return l.engine.Restart(name)
}

// pendingHostLine finds the captured known_hosts line for name in a status
// snapshot. Returns "" when the tunnel has no pending key.
func pendingHostLine(statuses []forward.Status, name string) string {
	for _, st := range statuses {
		if st.Name == name {
			return st.PendingHostLine
		}
	}
	return ""
}

// AcceptPassphrase stores the passphrase for the tunnel's identity and unblocks
// a dial waiting on it (Phase 19). The identity path is the one the dial
// reported pending (Status.PendingPassphrase), falling back to the tunnel's
// resolved identity. No Restart: the blocked dial wakes on the store.
func (l *Local) AcceptPassphrase(name, passphrase string) error {
	if l.secrets == nil {
		return fmt.Errorf("passphrase storage unavailable")
	}
	path := identityPathFor(l.engine.List(), l.cfg, name)
	if path == "" {
		return fmt.Errorf("no identity to store a passphrase for %q", name)
	}
	if err := l.secrets.Set(path, passphrase); err != nil {
		return fmt.Errorf("store passphrase: %w", err)
	}
	return nil
}

// identityPathFor resolves the identity path a passphrase applies to: the path
// the dial reported pending (Status.PendingPassphrase), or the tunnel's
// resolved identity from config. "" when the tunnel has no identity.
func identityPathFor(statuses []forward.Status, cfg *config.Config, name string) string {
	for _, st := range statuses {
		if st.Name == name && st.PendingPassphrase != "" {
			return st.PendingPassphrase
		}
	}
	for _, t := range cfg.Tunnels {
		if t.Name == name {
			return t.ResolvedIdentity(cfg.Defaults)
		}
	}
	return ""
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
