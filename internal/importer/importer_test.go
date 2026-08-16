package importer

import (
	"os"
	"path/filepath"
	"testing"

	ssh_config "github.com/kevinburke/ssh_config"

	"github.com/portuber/portato/internal/config"
)

func decode(t *testing.T, s string) *ssh_config.Config {
	t.Helper()
	cfg, err := ssh_config.DecodeBytes([]byte(s))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return cfg
}

func TestScan_MultiForwardPerHost(t *testing.T) {
	cfg := decode(t, `
Host db
  HostName 10.0.0.5
  LocalForward 5432 10.0.0.6:5432
  DynamicForward 1080
  LocalForward 6379 redis.internal:6379
`)
	got := Scan(cfg)
	if len(got) != 3 {
		t.Fatalf("want 3 candidates, got %d: %+v", len(got), got)
	}
	want := []Candidate{
		{SSHHost: "db", Type: "local", Local: "5432", Remote: "10.0.0.6:5432"},
		{SSHHost: "db", Type: "dynamic", Local: "1080"},
		{SSHHost: "db", Type: "local", Local: "6379", Remote: "redis.internal:6379"},
	}
	for i, w := range want {
		if got[i].SSHHost != w.SSHHost || got[i].Type != w.Type || got[i].Local != w.Local || got[i].Remote != w.Remote {
			t.Errorf("candidate %d: got %+v, want %+v", i, got[i], w)
		}
	}
}

func TestScan_SkipsWildcardMatchAndPreamble(t *testing.T) {
	cfg := decode(t, `
LocalForward 9999 global:9999

Host *
  DynamicForward 1080

Match host ci
  LocalForward 8080 ci.internal:80

Host ci
  HostName ci.internal
  LocalForward 8080 ci.internal:8080
`)
	got := Scan(cfg)
	if len(got) != 1 {
		t.Fatalf("want 1 candidate (only the explicit Host ci block), got %d: %+v", len(got), got)
	}
	if got[0].SSHHost != "ci" || got[0].Type != "local" || got[0].Local != "8080" || got[0].Remote != "ci.internal:8080" {
		t.Errorf("got %+v", got[0])
	}
}

func TestScan_RemoteSemanticsSwap(t *testing.T) {
	cfg := decode(t, `
Host ci
  RemoteForward 8080 127.0.0.1:80
  RemoteForward 0.0.0.0:9090 localhost:90
  RemoteForward [::1]:7070 localhost:70
`)
	got := Scan(cfg)
	if len(got) != 3 {
		t.Fatalf("want 3 candidates, got %d: %+v", len(got), got)
	}
	// Bare port expands to loopback (OpenSSH default), explicit binds stay.
	want := []struct{ remote, local string }{
		{"127.0.0.1:8080", "127.0.0.1:80"},
		{"0.0.0.0:9090", "localhost:90"},
		{"[::1]:7070", "localhost:70"},
	}
	for i, w := range want {
		if got[i].Type != "remote" || got[i].Remote != w.remote || got[i].Local != w.local {
			t.Errorf("candidate %d: got %+v, want remote=%q local=%q", i, got[i], w.remote, w.local)
		}
	}
}

func TestScan_LocalBindFormsKeptVerbatim(t *testing.T) {
	cfg := decode(t, `
Host web
  LocalForward 127.0.0.1:8080 web.internal:80
  LocalForward [::1]:8443 web.internal:443
  LocalForward 3000 web.internal:3000
`)
	got := Scan(cfg)
	if len(got) != 3 {
		t.Fatalf("want 3 candidates, got %d: %+v", len(got), got)
	}
	for i, w := range []string{"127.0.0.1:8080", "[::1]:8443", "3000"} {
		if got[i].Local != w {
			t.Errorf("candidate %d local: got %q, want %q", i, got[i].Local, w)
		}
	}
}

func TestScan_UnsupportedFormsSkipped(t *testing.T) {
	cfg := decode(t, `
Host dock
  LocalForward /run/dock.sock /run/dock.sock
  RemoteForward 8080 /run/web.sock
  LocalForward
  DynamicForward 1080 notaport
  DynamicForward 1080
`)
	got := Scan(cfg)
	if len(got) != 1 {
		t.Fatalf("want 1 candidate (the plain DynamicForward), got %d: %+v", len(got), got)
	}
	if got[0].Type != "dynamic" || got[0].Local != "1080" {
		t.Errorf("got %+v", got[0])
	}
}

func TestScan_DuplicateDirectiveWithinImport(t *testing.T) {
	cfg := decode(t, `
Host db
  LocalForward 5432 10.0.0.6:5432
  LocalForward 5432 10.0.0.6:5432
`)
	got := Scan(cfg)
	if len(got) != 1 {
		t.Fatalf("want 1 candidate, got %d: %+v", len(got), got)
	}
}

func TestScan_PatternsAndNegation(t *testing.T) {
	cfg := decode(t, `
Host !legacy db staging
  LocalForward 5432 10.0.0.6:5432
`)
	got := Scan(cfg)
	if len(got) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(got))
	}
	if got[0].SSHHost != "db" {
		t.Errorf("sshHost: got %q, want db (first concrete pattern)", got[0].SSHHost)
	}
	if len(got[0].Patterns) != 2 || got[0].Patterns[0] != "db" || got[0].Patterns[1] != "staging" {
		t.Errorf("patterns: got %v, want [db staging]", got[0].Patterns)
	}
}

func TestScan_Include(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "work.conf")
	if err := os.WriteFile(sub, []byte(`
LocalForward 15432 10.9.9.9:5432

Host work
  HostName work.internal
  LocalForward 16022 work.internal:22
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := decode(t, "Host main\n  Include "+sub+"\n\nHost other\n  LocalForward 17070 x:70\n")
	got := Scan(cfg)
	if len(got) != 3 {
		t.Fatalf("want 3 candidates, got %d: %+v", len(got), got)
	}
	// The include's own Host block enumerates independently; its preamble
	// forward attributes to the including block (main).
	if got[0].SSHHost != "main" || got[0].Local != "15432" {
		t.Errorf("include preamble: got %+v, want sshHost=main local=15432", got[0])
	}
	if got[1].SSHHost != "work" || got[1].Local != "16022" {
		t.Errorf("include block: got %+v, want sshHost=work local=16022", got[1])
	}
	if got[2].SSHHost != "other" {
		t.Errorf("outer block: got %+v", got[2])
	}
}

func TestScan_IncludeWildcardSkipAppliesInside(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "glob.conf")
	if err := os.WriteFile(sub, []byte("Host *\n  LocalForward 19999 z:99\n\nHost real\n  LocalForward 15000 r:50\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := decode(t, "Include "+dir+"/*.conf\n")
	got := Scan(cfg)
	if len(got) != 1 {
		t.Fatalf("want 1 candidate (Host * skipped inside include), got %d: %+v", len(got), got)
	}
	if got[0].SSHHost != "real" {
		t.Errorf("got %+v", got[0])
	}
}

func TestScan_NilConfig(t *testing.T) {
	if got := Scan(nil); got != nil {
		t.Errorf("want nil, got %+v", got)
	}
}

func TestLoad_MissingFileIsNil(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent"))
	if err != nil || cfg != nil {
		t.Fatalf("want nil, nil; got %+v, %v", cfg, err)
	}
}

func TestLoad_UnreadableFileIsError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	if err := os.WriteFile(p, []byte("Host db\n\tLocalForward 5432 x:5432\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
	if _, err := Load(p); err == nil {
		t.Fatal("want error for unreadable file")
	}
}

func TestPlanImport_NamingAndDeconflict(t *testing.T) {
	sshCfg := decode(t, "Host db\n  HostName 10.0.0.5\n\nHost web.prod\n  HostName 10.0.0.9\n")
	cands := []Candidate{
		{SSHHost: "db", Patterns: []string{"db"}, Type: "local", Local: "5432", Remote: "10.0.0.6:5432"},
		{SSHHost: "db", Patterns: []string{"db"}, Type: "local", Local: "6379", Remote: "redis:6379"},
		{SSHHost: "web.prod", Patterns: []string{"web.prod"}, Type: "dynamic", Local: "1080"},
	}
	cfg := &config.Config{Tubers: []config.Tuber{{Name: "db-5432", Type: "local", Local: "9999", Remote: "other:9999", SSH: "elsewhere", Host: "elsewhere", Port: 22}}}
	plan, err := PlanImport(cfg, sshCfg, cands)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Add) != 3 || len(plan.Skipped) != 0 {
		t.Fatalf("want 3 add / 0 skipped, got %+v / %+v", plan.Add, plan.Skipped)
	}
	// Existing name "db-5432" is taken by an unrelated forward → -2 suffix.
	for i, w := range []string{"db-5432-2", "db-6379", "web-prod-1080"} {
		if plan.Add[i].Tuber.Name != w {
			t.Errorf("tuber %d name: got %q, want %q", i, plan.Add[i].Tuber.Name, w)
		}
		if !config.ValidName(plan.Add[i].Tuber.Name) {
			t.Errorf("tuber %d name %q is not a valid name", i, plan.Add[i].Tuber.Name)
		}
		if plan.Add[i].Tuber.Enabled {
			t.Errorf("tuber %d must import enabled=false", i)
		}
		if plan.Add[i].Tuber.SSH != plan.Add[i].Candidate.SSHHost {
			t.Errorf("tuber %d must keep the raw pattern in ssh:", i)
		}
	}
}

func TestPlanImport_DedupAgainstResolvedHost(t *testing.T) {
	sshCfg := decode(t, "Host db\n  HostName 10.0.0.5\n  Port 2222\n")
	cands := []Candidate{
		{SSHHost: "db", Patterns: []string{"db"}, Type: "local", Local: "5432", Remote: "10.0.0.6:5432"},
	}
	// The existing tuber spells the endpoint explicitly — same resolved
	// host:port as the alias, same forward → covered, skipped.
	cfg := &config.Config{Tubers: []config.Tuber{{
		Name: "existing", Type: "local", Local: "5432", Remote: "10.0.0.6:5432",
		SSH: "user@10.0.0.5:2222", Host: "10.0.0.5", Port: 2222,
	}}}
	plan, err := PlanImport(cfg, sshCfg, cands)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Skipped) != 1 || len(plan.Add) != 0 {
		t.Fatalf("want 0 add / 1 skipped, got %+v / %+v", plan.Add, plan.Skipped)
	}
}

func TestPlanImport_DedupWithinImportViaResolution(t *testing.T) {
	sshCfg := decode(t, "Host db db2\n  HostName 10.0.0.5\n")
	cands := []Candidate{
		{SSHHost: "db", Patterns: []string{"db", "db2"}, Type: "local", Local: "5432", Remote: "10.0.0.6:5432"},
		{SSHHost: "db2", Patterns: []string{"db2"}, Type: "local", Local: "5432", Remote: "10.0.0.6:5432"},
	}
	plan, err := PlanImport(&config.Config{}, sshCfg, cands)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Add) != 1 || len(plan.Skipped) != 1 {
		t.Fatalf("want 1 add / 1 skipped (same resolved endpoint), got %+v / %+v", plan.Add, plan.Skipped)
	}
}

func TestPlanImport_NoSSHConfigResolvesLiterally(t *testing.T) {
	// A nil ssh config (no ~/.ssh/config, e.g. --from a custom file) resolves
	// the pattern to itself, port 22 — Phase 44's literal fallback.
	cands := []Candidate{{SSHHost: "db", Type: "local", Local: "5432", Remote: "10.0.0.6:5432"}}
	cfg := &config.Config{Tubers: []config.Tuber{{
		Name: "existing", Type: "local", Local: "5432", Remote: "10.0.0.6:5432",
		SSH: "db", Host: "db", Port: 22,
	}}}
	plan, err := PlanImport(cfg, nil, cands)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Skipped) != 1 || len(plan.Add) != 0 {
		t.Fatalf("want 0 add / 1 skipped (literal resolution), got %+v / %+v", plan.Add, plan.Skipped)
	}
}

func TestFilter_CaseInsensitiveExactPattern(t *testing.T) {
	cands := []Candidate{
		{SSHHost: "db", Patterns: []string{"db", "staging"}, Type: "local"},
		{SSHHost: "web", Patterns: []string{"web"}, Type: "local"},
	}
	got := Filter(cands, []string{"DB"})
	if len(got) != 1 || got[0].SSHHost != "db" {
		t.Fatalf("got %+v, want the db block (any declared pattern matches)", got)
	}
	if got := Filter(cands, []string{"d*"}); len(got) != 0 {
		t.Fatalf("got %+v, want no fuzzy match — patterns address blocks exactly", got)
	}
}

func TestBlocks_DistinctInOrder(t *testing.T) {
	cands := []Candidate{
		{SSHHost: "db"}, {SSHHost: "db"}, {SSHHost: "web"}, {SSHHost: "db"},
	}
	got := Blocks(cands)
	if len(got) != 2 || got[0] != "db" || got[1] != "web" {
		t.Fatalf("got %v, want [db web]", got)
	}
}
