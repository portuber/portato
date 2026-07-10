package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/portuber/portato/internal/client"
	"github.com/portuber/portato/internal/config"
)

const testTubersYAML = `# top comment
defaults:
  identity: ~/.ssh/id
# the db tuber
tubers:
  - name: db  # keep this comment
    type: local
    local: 5432
    remote: db:5432
    ssh: u@h:22
    enabled: false
`

// newTuberServer stands up a daemon backed by a config file written from
// yamlContent (so comments survive into the test) and a fake engine.
func newTuberServer(t *testing.T, yamlContent string) (*Server, *fakeEngine, string, *client.Client) {
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
	_, _, _, tc := newTuberServer(t, testTubersYAML)
	cfg, err := tc.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if len(cfg.Tubers) != 1 || cfg.Tubers[0].Name != "db" {
		t.Errorf("config = %+v", cfg.Tubers)
	}
	if cfg.Defaults.Identity != "~/.ssh/id" {
		t.Errorf("defaults identity = %q", cfg.Defaults.Identity)
	}
}

func TestServer_AddTuber(t *testing.T) {
	_, fe, cfgPath, tc := newTuberServer(t, testTubersYAML)

	added := config.Tuber{Name: "web", Type: "local", Local: "8080", Remote: "web:80", SSH: "u@h:22"}
	if err := tc.AddTuber(added); err != nil {
		t.Fatalf("AddTuber: %v", err)
	}

	// Persisted to YAML.
	persisted, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after add: %v", err)
	}
	if len(persisted.Tubers) != 2 || persisted.Tubers[1].Name != "web" {
		t.Errorf("persisted tubers = %+v", persisted.Tubers)
	}

	// Engine list reflects the reload.
	names := tuberNames(fe)
	if !contains(names, "web") || !contains(names, "db") {
		t.Errorf("engine names after add = %v", names)
	}
}

func TestServer_AddTuber_PreservesComments(t *testing.T) {
	_, _, cfgPath, tc := newTuberServer(t, testTubersYAML)

	added := config.Tuber{Name: "web", Type: "dynamic", Local: "1080", SSH: "u@h:22"}
	if err := tc.AddTuber(added); err != nil {
		t.Fatalf("AddTuber: %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	out := string(data)
	for _, want := range []string{"# top comment", "# the db tuber", "# keep this comment"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestServer_AddTuber_DuplicateName(t *testing.T) {
	_, _, _, tc := newTuberServer(t, testTubersYAML)
	dup := config.Tuber{Name: "db", Type: "local", Local: "1", Remote: "h:1", SSH: "u@h:22"}
	if err := tc.AddTuber(dup); err == nil {
		t.Error("expected conflict/error for duplicate name")
	}
}

func TestServer_AddTuber_InvalidType(t *testing.T) {
	_, _, _, tc := newTuberServer(t, testTubersYAML)
	bad := config.Tuber{Name: "x", Type: "bogus", Local: "1", Remote: "h:1", SSH: "u@h:22"}
	if err := tc.AddTuber(bad); err == nil {
		t.Error("expected validation error for bad type")
	}
}

func TestServer_UpdateTuber(t *testing.T) {
	_, fe, cfgPath, tc := newTuberServer(t, testTubersYAML)

	repl := config.Tuber{Name: "db", Type: "dynamic", Local: "1080", SSH: "u@h:22"}
	if err := tc.UpdateTuber("db", repl); err != nil {
		t.Fatalf("UpdateTuber: %v", err)
	}
	persisted, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after update: %v", err)
	}
	if len(persisted.Tubers) != 1 || persisted.Tubers[0].Type != "dynamic" {
		t.Errorf("persisted = %+v", persisted.Tubers)
	}
	if fe.cfg.Tubers[0].Type != "dynamic" {
		t.Errorf("engine cfg not reloaded: %+v", fe.cfg.Tubers[0])
	}
}

func TestServer_UpdateTuber_Rename(t *testing.T) {
	_, _, cfgPath, tc := newTuberServer(t, testTubersYAML)
	repl := config.Tuber{Name: "renamed", Type: "local", Local: "5432", Remote: "db:5432", SSH: "u@h:22"}
	if err := tc.UpdateTuber("db", repl); err != nil {
		t.Fatalf("UpdateTuber rename: %v", err)
	}
	persisted, _ := config.Load(cfgPath)
	if len(persisted.Tubers) != 1 || persisted.Tubers[0].Name != "renamed" {
		t.Errorf("rename persisted = %+v", persisted.Tubers)
	}
}

func TestServer_UpdateTuber_Unknown(t *testing.T) {
	_, _, _, tc := newTuberServer(t, testTubersYAML)
	err := tc.UpdateTuber("nope", config.Tuber{Name: "x", Type: "local", Local: "1", Remote: "h:1", SSH: "u@h:22"})
	if err == nil {
		t.Error("expected not-found error")
	}
}

func TestServer_DeleteTuber(t *testing.T) {
	_, fe, cfgPath, tc := newTuberServer(t, testTubersYAML)

	if err := tc.DeleteTuber("db"); err != nil {
		t.Fatalf("DeleteTuber: %v", err)
	}
	persisted, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load after delete: %v", err)
	}
	if len(persisted.Tubers) != 0 {
		t.Errorf("expected empty tubers, got %+v", persisted.Tubers)
	}
	if len(fe.cfg.Tubers) != 0 {
		t.Errorf("engine cfg not reloaded: %+v", fe.cfg.Tubers)
	}
}

func TestServer_DeleteTuber_PreservesComments(t *testing.T) {
	_, _, cfgPath, tc := newTuberServer(t, testTubersYAML)
	if err := tc.DeleteTuber("db"); err != nil {
		t.Fatalf("DeleteTuber: %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	out := string(data)
	// The top and defaults comments survive; the tuber block is gone.
	for _, want := range []string{"# top comment"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "name: db") {
		t.Errorf("db should be removed\n%s", out)
	}
}

func TestServer_DeleteTuber_Unknown(t *testing.T) {
	_, _, _, tc := newTuberServer(t, testTubersYAML)
	if err := tc.DeleteTuber("nope"); err == nil {
		t.Error("expected not-found error")
	}
}

func tuberNames(fe *fakeEngine) []string {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	out := make([]string, 0, len(fe.cfg.Tubers))
	for _, t := range fe.cfg.Tubers {
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
