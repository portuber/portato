package controller

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/forward"
)

const oneTunnel = "  - name: t1\n    type: local\n    local: \"19999\"\n    remote: 127.0.0.1:5432\n    ssh: user@127.0.0.1:2222\n"

func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

func TestLocal_ListOrderAndState(t *testing.T) {
	body := "defaults:\n  identity: ~/.ssh/id_ed25519\ntunnels:\n" +
		"  - name: alpha\n    type: local\n    local: \"19991\"\n    remote: 127.0.0.1:5432\n    ssh: user@127.0.0.1:2222\n" +
		"  - name: beta\n    type: local\n    local: \"19992\"\n    remote: 127.0.0.1:5432\n    ssh: user@127.0.0.1:2222\n"
	p := writeConfigFile(t, body)
	cfg := mustLoad(t, p)

	l := NewLocal(cfg, p, nil, nil)
	got := l.List()
	if len(got) != 2 {
		t.Fatalf("List len = %d, want 2", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "beta" {
		t.Errorf("order = %q, %q; want alpha, beta", got[0].Name, got[1].Name)
	}
	for _, s := range got {
		if s.State != Off {
			t.Errorf("%s state = %v, want Off", s.Name, s.State)
		}
	}
	_ = l.Close()
}

func TestLocal_UnknownTunnelErrors(t *testing.T) {
	p := writeConfigFile(t, "defaults:\n  identity: ~/.ssh/id_ed25519\ntunnels:\n"+oneTunnel)
	cfg := mustLoad(t, p)
	l := NewLocal(cfg, p, nil, nil)

	for _, fn := range []func(string) error{l.Enable, l.Disable, l.Restart} {
		if err := fn("nope"); err == nil {
			t.Errorf("expected error for unknown tunnel")
		}
	}
	_ = l.Close()
}

func TestLocal_ReloadAddsTunnel(t *testing.T) {
	p := writeConfigFile(t, "defaults:\n  identity: ~/.ssh/id_ed25519\ntunnels:\n"+oneTunnel)
	cfg := mustLoad(t, p)
	l := NewLocal(cfg, p, nil, nil)

	if got := len(l.List()); got != 1 {
		t.Fatalf("initial List len = %d, want 1", got)
	}

	second := "defaults:\n  identity: ~/.ssh/id_ed25519\ntunnels:\n" + oneTunnel +
		"  - name: t2\n    type: local\n    local: \"19998\"\n    remote: 127.0.0.1:5432\n    ssh: user@127.0.0.1:2222\n"
	if err := os.WriteFile(p, []byte(second), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	if err := l.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := len(l.List()); got != 2 {
		t.Errorf("after reload List len = %d, want 2", got)
	}
	_ = l.Close()
}

func TestLocal_ReloadBadConfig(t *testing.T) {
	p := writeConfigFile(t, "defaults:\n  identity: ~/.ssh/id_ed25519\ntunnels:\n"+oneTunnel)
	cfg := mustLoad(t, p)
	l := NewLocal(cfg, p, nil, nil)

	if err := os.WriteFile(p, []byte("tunnels:\n  - name: bad name\n"), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	if err := l.Reload(); err == nil {
		t.Errorf("Reload on invalid config: want error, got nil")
	}
	if got := len(l.List()); got != 1 {
		t.Errorf("after failed reload List len = %d, want unchanged 1", got)
	}
	_ = l.Close()
}

func TestLocal_EnableDisablePersists(t *testing.T) {
	p := writeConfigFile(t, "defaults:\n  identity: ~/.ssh/id_ed25519\ntunnels:\n"+oneTunnel)
	cfg := mustLoad(t, p)
	l := NewLocal(cfg, p, nil, nil)
	defer l.Close()

	if err := l.Enable("t1"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if reloaded := mustLoad(t, p); !reloaded.Tunnels[0].Enabled {
		t.Errorf("after Enable, config on disk has enabled=false; want true (hand-off invariant)")
	}

	if err := l.Disable("t1"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if reloaded := mustLoad(t, p); reloaded.Tunnels[0].Enabled {
		t.Errorf("after Disable, config on disk has enabled=true; want false")
	}
}

func TestLocal_EnableUnknownDoesNotPersist(t *testing.T) {
	p := writeConfigFile(t, "defaults:\n  identity: ~/.ssh/id_ed25519\ntunnels:\n"+oneTunnel)
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg := mustLoad(t, p)
	l := NewLocal(cfg, p, nil, nil)
	defer l.Close()

	if err := l.Enable("nope"); err == nil {
		t.Fatal("Enable(unknown): want error, got nil")
	}
	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("Enable(unknown) should not have rewritten the config file")
	}
}

func TestLocal_ChangesPushesAndCloses(t *testing.T) {
	p := writeConfigFile(t, "defaults:\n  identity: ~/.ssh/id_ed25519\ntunnels:\n"+oneTunnel)
	cfg := mustLoad(t, p)
	l := NewLocal(cfg, p, nil, nil)
	defer l.Close()
	ch := l.Changes()

	// Reload drives engine.Reload, which fans a notify out to subscribers.
	if err := l.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no push signal after Reload")
	}
}

// TestLocal_StartEnabledStartsOnlyEnabled verifies the standalone launch path:
// StartEnabled starts the tunnel whose config has Enabled == true and leaves the
// disabled one Off. Tunnel.Start binds the local listener and sets Connecting
// synchronously (Error only if the bind fails), so the assertion needs no
// polling — the enabled tunnel is never Off right after the call.
func TestLocal_StartEnabledStartsOnlyEnabled(t *testing.T) {
	body := "defaults:\n  identity: ~/.ssh/id_ed25519\ntunnels:\n" +
		"  - name: on\n    type: local\n    local: \"19994\"\n    remote: 127.0.0.1:5432\n    ssh: user@127.0.0.1:2222\n    enabled: true\n" +
		"  - name: off\n    type: local\n    local: \"19995\"\n    remote: 127.0.0.1:5432\n    ssh: user@127.0.0.1:2222\n"
	p := writeConfigFile(t, body)
	cfg := mustLoad(t, p)
	l := NewLocal(cfg, p, nil, nil)
	defer l.Close()

	for _, s := range l.List() {
		if s.State != Off {
			t.Fatalf("%s state = %v before StartEnabled, want Off", s.Name, s.State)
		}
	}

	l.StartEnabled()

	states := make(map[string]State, 2)
	for _, s := range l.List() {
		states[s.Name] = s.State
	}
	if states["on"] == Off {
		t.Errorf(`enabled "on" state = Off after StartEnabled, want not Off`)
	}
	if states["off"] != Off {
		t.Errorf(`disabled "off" state = %v after StartEnabled, want Off`, states["off"])
	}
}

func mustLoad(t *testing.T, p string) *config.Config {
	t.Helper()
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

// TestPendingHostLineLookup exercises the helper behind AcceptHost (Phase 11
// TOFU): it finds the captured known_hosts line for a tunnel, and returns ""
// for an unknown tunnel or one without a pending key.
func TestPendingHostLineLookup(t *testing.T) {
	statuses := []forward.Status{
		{Name: "a", PendingHostLine: "a ssh-ed25519 AAAA"},
		{Name: "b"},
	}
	if got := pendingHostLine(statuses, "a"); got != "a ssh-ed25519 AAAA" {
		t.Errorf("pendingHostLine(a) = %q", got)
	}
	if got := pendingHostLine(statuses, "b"); got != "" {
		t.Errorf("pendingHostLine(b) = %q, want empty", got)
	}
	if got := pendingHostLine(statuses, "missing"); got != "" {
		t.Errorf("pendingHostLine(missing) = %q, want empty", got)
	}
}

// TestLocal_AcceptHostNoPending proves AcceptHost fails cleanly when the
// tunnel has no captured key (e.g. it was never rejected).
func TestLocal_AcceptHostNoPending(t *testing.T) {
	p := writeConfigFile(t, "tunnels:\n  - name: t1\n    type: local\n    local: \"19993\"\n    remote: 127.0.0.1:5432\n    ssh: user@127.0.0.1:2222\n")
	cfg := mustLoad(t, p)
	l := NewLocal(cfg, p, nil, nil)
	if err := l.AcceptHost("t1"); err == nil {
		t.Fatal("AcceptHost without a pending key should error")
	}
}
