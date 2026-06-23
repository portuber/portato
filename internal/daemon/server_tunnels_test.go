package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kipkaev55/portato/internal/client"
	"github.com/kipkaev55/portato/internal/config"
)

const testTunnelsYAML = `# top comment
defaults:
  identity: ~/.ssh/id
# the db tunnel
tunnels:
  - name: db  # keep this comment
    type: local
    local: 5432
    remote: db:5432
    ssh: u@h:22
    enabled: false
`

// newTunnelServer stands up a daemon backed by a config file written from
// yamlContent (so comments survive into the test) and a fake engine.
func newTunnelServer(t *testing.T, yamlContent string) (*Server, *fakeEngine, string, *client.Client) {
	t.Helper()
	dir := shortDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	sock := filepath.Join(dir, "portato.sock")
	fe := newFakeEngine(cfg)
	s := newServer(fe, cfg, cfgPath, sock, filepath.Join(dir, "daemon.socket"), slog.Default(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	go s.Start(ctx)
	if err := waitForFile(sock, 2*time.Second); err != nil {
		cancel()
		t.Fatalf("socket not created: %v", err)
	}
	t.Cleanup(cancel)
	return s, fe, cfgPath, client.New(sock)
}

func TestServer_GetConfig(t *testing.T) {
	_, _, _, tc := newTunnelServer(t, testTunnelsYAML)
	cfg, err := tc.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if len(cfg.Tunnels) != 1 || cfg.Tunnels[0].Name != "db" {
		t.Errorf("config = %+v", cfg.Tunnels)
	}
	if cfg.Defaults.Identity != "~/.ssh/id" {
		t.Errorf("defaults identity = %q", cfg.Defaults.Identity)
	}
}

func TestServer_AddTunnel(t *testing.T) {
	_, fe, cfgPath, tc := newTunnelServer(t, testTunnelsYAML)

	added := config.Tunnel{Name: "web", Type: "local", Local: "8080", Remote: "web:80", SSH: "u@h:22"}
	if err := tc.AddTunnel(added); err != nil {
		t.Fatalf("AddTunnel: %v", err)
	}

	// Persisted to YAML.
	persisted, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after add: %v", err)
	}
	if len(persisted.Tunnels) != 2 || persisted.Tunnels[1].Name != "web" {
		t.Errorf("persisted tunnels = %+v", persisted.Tunnels)
	}

	// Engine list reflects the reload.
	names := tunnelNames(fe)
	if !contains(names, "web") || !contains(names, "db") {
		t.Errorf("engine names after add = %v", names)
	}
}

func TestServer_AddTunnel_PreservesComments(t *testing.T) {
	_, _, cfgPath, tc := newTunnelServer(t, testTunnelsYAML)

	added := config.Tunnel{Name: "web", Type: "dynamic", Local: "1080", SSH: "u@h:22"}
	if err := tc.AddTunnel(added); err != nil {
		t.Fatalf("AddTunnel: %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	out := string(data)
	for _, want := range []string{"# top comment", "# the db tunnel", "# keep this comment"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestServer_AddTunnel_DuplicateName(t *testing.T) {
	_, _, _, tc := newTunnelServer(t, testTunnelsYAML)
	dup := config.Tunnel{Name: "db", Type: "local", Local: "1", Remote: "h:1", SSH: "u@h:22"}
	if err := tc.AddTunnel(dup); err == nil {
		t.Error("expected conflict/error for duplicate name")
	}
}

func TestServer_AddTunnel_InvalidType(t *testing.T) {
	_, _, _, tc := newTunnelServer(t, testTunnelsYAML)
	bad := config.Tunnel{Name: "x", Type: "bogus", Local: "1", Remote: "h:1", SSH: "u@h:22"}
	if err := tc.AddTunnel(bad); err == nil {
		t.Error("expected validation error for bad type")
	}
}

func TestServer_UpdateTunnel(t *testing.T) {
	_, fe, cfgPath, tc := newTunnelServer(t, testTunnelsYAML)

	repl := config.Tunnel{Name: "db", Type: "dynamic", Local: "1080", SSH: "u@h:22"}
	if err := tc.UpdateTunnel("db", repl); err != nil {
		t.Fatalf("UpdateTunnel: %v", err)
	}
	persisted, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after update: %v", err)
	}
	if len(persisted.Tunnels) != 1 || persisted.Tunnels[0].Type != "dynamic" {
		t.Errorf("persisted = %+v", persisted.Tunnels)
	}
	if fe.cfg.Tunnels[0].Type != "dynamic" {
		t.Errorf("engine cfg not reloaded: %+v", fe.cfg.Tunnels[0])
	}
}

func TestServer_UpdateTunnel_Rename(t *testing.T) {
	_, _, cfgPath, tc := newTunnelServer(t, testTunnelsYAML)
	repl := config.Tunnel{Name: "renamed", Type: "local", Local: "5432", Remote: "db:5432", SSH: "u@h:22"}
	if err := tc.UpdateTunnel("db", repl); err != nil {
		t.Fatalf("UpdateTunnel rename: %v", err)
	}
	persisted, _ := config.Load(cfgPath)
	if len(persisted.Tunnels) != 1 || persisted.Tunnels[0].Name != "renamed" {
		t.Errorf("rename persisted = %+v", persisted.Tunnels)
	}
}

func TestServer_UpdateTunnel_Unknown(t *testing.T) {
	_, _, _, tc := newTunnelServer(t, testTunnelsYAML)
	err := tc.UpdateTunnel("nope", config.Tunnel{Name: "x", Type: "local", Local: "1", Remote: "h:1", SSH: "u@h:22"})
	if err == nil {
		t.Error("expected not-found error")
	}
}

func TestServer_DeleteTunnel(t *testing.T) {
	_, fe, cfgPath, tc := newTunnelServer(t, testTunnelsYAML)

	if err := tc.DeleteTunnel("db"); err != nil {
		t.Fatalf("DeleteTunnel: %v", err)
	}
	persisted, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after delete: %v", err)
	}
	if len(persisted.Tunnels) != 0 {
		t.Errorf("expected empty tunnels, got %+v", persisted.Tunnels)
	}
	if len(fe.cfg.Tunnels) != 0 {
		t.Errorf("engine cfg not reloaded: %+v", fe.cfg.Tunnels)
	}
}

func TestServer_DeleteTunnel_PreservesComments(t *testing.T) {
	_, _, cfgPath, tc := newTunnelServer(t, testTunnelsYAML)
	if err := tc.DeleteTunnel("db"); err != nil {
		t.Fatalf("DeleteTunnel: %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	out := string(data)
	// The top and defaults comments survive; the tunnel block is gone.
	for _, want := range []string{"# top comment"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "name: db") {
		t.Errorf("db should be removed\n%s", out)
	}
}

func TestServer_DeleteTunnel_Unknown(t *testing.T) {
	_, _, _, tc := newTunnelServer(t, testTunnelsYAML)
	if err := tc.DeleteTunnel("nope"); err == nil {
		t.Error("expected not-found error")
	}
}

func tunnelNames(fe *fakeEngine) []string {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	out := make([]string, 0, len(fe.cfg.Tunnels))
	for _, t := range fe.cfg.Tunnels {
		out = append(out, t.Name)
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
