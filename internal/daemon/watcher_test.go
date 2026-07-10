package daemon

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/portuber/portato/internal/config"
)

// restoreWatchSeams saves the package-level watcher timing seams and restores
// them on cleanup so a test can shrink them without leaking to other tests.
func restoreWatchSeams(t *testing.T) {
	t.Helper()
	poll, settle := watchPollInterval, watchSettle
	t.Cleanup(func() {
		watchPollInterval, watchSettle = poll, settle
	})
}

// newWatchTestServer builds a server over a temp config (one tuber "db") with
// a fake engine and a file watcher wired to reloadFromWatch, ready to react to
// edits of cfgPath. Returns the server, its engine and the captured log buffer.
func newWatchTestServer(t *testing.T) (*Server, *fakeEngine, string, *bytes.Buffer) {
	t.Helper()
	dir := shortDir(t)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig()
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	fe := newFakeEngine(cfg)
	var logbuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logbuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	sock := filepath.Join(dir, "portato.sock")
	marker := filepath.Join(dir, "daemon.socket")
	s := newServer(fe, cfg, cfgPath, sock, marker, log, nil)
	s.watcher = newWatcher(cfgPath, s.reloadFromWatch, log)
	s.watcher.start()
	t.Cleanup(s.watcher.stop)
	return s, fe, cfgPath, &logbuf
}

func feNames(fe *fakeEngine) []string {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	out := make([]string, 0, len(fe.cfg.Tubers))
	for _, t := range fe.cfg.Tubers {
		out = append(out, t.Name)
	}
	return out
}

func serverTuberCount(s *Server) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cfg.Tubers)
}

func TestWatcher_AppliesEdit(t *testing.T) {
	restoreWatchSeams(t)
	watchPollInterval = 10 * time.Millisecond
	watchSettle = 25 * time.Millisecond

	s, fe, cfgPath, _ := newWatchTestServer(t)

	// Add a second tuber and save.
	cfg2 := testConfig()
	cfg2.Tubers = append(cfg2.Tubers, config.Tuber{
		Name: "web", Type: "local", Local: "8080", Remote: "web:80", SSH: "u@h:22",
	})
	if err := cfg2.Save(cfgPath); err != nil {
		t.Fatalf("save edited config: %v", err)
	}

	waitFor(func() bool { return len(feNames(fe)) == 2 }, time.Second)
	if got := serverTuberCount(s); got != 2 {
		t.Errorf("server config after edit: %d tubers, want 2", got)
	}
}

func TestWatcher_BadEditKeepsLastGood(t *testing.T) {
	restoreWatchSeams(t)
	watchPollInterval = 10 * time.Millisecond
	watchSettle = 25 * time.Millisecond

	s, fe, cfgPath, logbuf := newWatchTestServer(t)

	// First make a good edit to a known 2-tuber state.
	cfg2 := testConfig()
	cfg2.Tubers = append(cfg2.Tubers, config.Tuber{
		Name: "web", Type: "local", Local: "8080", Remote: "web:80", SSH: "u@h:22",
	})
	if err := cfg2.Save(cfgPath); err != nil {
		t.Fatalf("save edited config: %v", err)
	}
	waitFor(func() bool { return len(feNames(fe)) == 2 }, time.Second)

	// Now write a syntactically broken config.
	if err := os.WriteFile(cfgPath, []byte("tubers:\n  - name: db\n    : : : bad yaml\n"), 0o600); err != nil {
		t.Fatalf("write bad config: %v", err)
	}
	// Let the watcher detect, settle, attempt and skip.
	time.Sleep(200 * time.Millisecond)

	if got := serverTuberCount(s); got != 2 {
		t.Errorf("server config after bad edit: %d tubers, want 2 (last-good)", got)
	}
	if n := len(feNames(fe)); n != 2 {
		t.Errorf("engine after bad edit: %d tubers, want 2 (last-good)", n)
	}
	if !bytes.Contains(logbuf.Bytes(), []byte("config reload skipped")) {
		t.Errorf("expected a reload-skipped log line; got:\n%s", logbuf.String())
	}
}

func TestWatcher_VanishSkipsReload(t *testing.T) {
	restoreWatchSeams(t)
	watchPollInterval = 10 * time.Millisecond
	watchSettle = 25 * time.Millisecond

	s, _, cfgPath, logbuf := newWatchTestServer(t)

	if err := os.Remove(cfgPath); err != nil {
		t.Fatalf("remove config: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	// No crash, last-good config (1 tuber) survives.
	if got := serverTuberCount(s); got != 1 {
		t.Errorf("server config after vanish: %d tubers, want 1 (last-good)", got)
	}
	if !bytes.Contains(logbuf.Bytes(), []byte("config vanished")) {
		t.Errorf("expected a vanished log line; got:\n%s", logbuf.String())
	}
}

// TestWatcher_CoalescesBurst asserts a rapid save burst collapses into a
// single reload (not one-per-write).
func TestWatcher_CoalescesBurst(t *testing.T) {
	restoreWatchSeams(t)
	watchPollInterval = 10 * time.Millisecond
	watchSettle = 40 * time.Millisecond

	s, fe, cfgPath, _ := newWatchTestServer(t)

	var reloadsBefore int64
	mu := &sync.Mutex{}
	wrappedReload := func() error {
		mu.Lock()
		reloadsBefore++
		mu.Unlock()
		return s.reloadFromWatch()
	}
	// Rebuild the watcher with the counting reload.
	s.watcher.stop()
	s.watcher = newWatcher(cfgPath, wrappedReload, s.log)
	s.watcher.start()

	// Fire several distinct writes back-to-back.
	for i := 0; i < 4; i++ {
		c := testConfig()
		c.Tubers = []config.Tuber{{
			Name: "db", Type: "local", Local: "5500", Remote: "db:5432", SSH: "u@h:22",
		}}
		c.Tubers[0].Local = "550" + string(rune('0'+i)) // vary content -> size/mtime differ
		_ = c.Save(cfgPath)
		time.Sleep(3 * time.Millisecond)
	}

	if !waitFor(func() bool {
		mu.Lock()
		defer mu.Unlock()
		return reloadsBefore >= 1
	}, time.Second) {
		t.Fatalf("watcher did not apply the burst; reloads=%d", reloadsBefore)
	}
	time.Sleep(150 * time.Millisecond) // ensure no further reloads

	mu.Lock()
	defer mu.Unlock()
	if reloadsBefore != 1 {
		t.Errorf("burst should coalesce to 1 reload, got %d", reloadsBefore)
	}
	if n := len(feNames(fe)); n != 1 {
		t.Errorf("engine should have the final 1-tuber config, got %d", n)
	}
}
