package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/config"
	"github.com/portuber/portato/internal/update"
)

// updateTestConfig writes a minimal config.yaml into a temp dir, points the
// cfgFile flag at it and redirects the check cache there.
func updateTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("defaults:\n  known_hosts: ~/.ssh/known_hosts\ntubers: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldCfg := cfgFile
	cfgFile = path
	t.Cleanup(func() { cfgFile = oldCfg })
	update.CachePathForTest(t, filepath.Join(dir, "update.json"))
	return path
}

// serveRelease spins a fixture releases/latest server and routes the client
// at it through the in-repo seam.
func serveRelease(t *testing.T, tag string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name": %q, "html_url": "https://example.com/%s"}`, tag, tag)
	}))
	t.Cleanup(srv.Close)
	update.SetBaseForTest(t, srv.URL)
}

func TestUpdateCheckAvailable(t *testing.T) {
	updateTestConfig(t)
	serveRelease(t, "v9.9.9")
	old := version
	version = "1.6.1"
	t.Cleanup(func() { version = old })

	out, err := runUpdateCmd(t, "check")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	for _, want := range []string{"current:  1.6.1", "latest:   v9.9.9", "update available: v1.6.1 -> v9.9.9", "https://example.com/v9.9.9"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// The explicit check feeds the cache for doctor/TUI.
	cache, err := update.LoadCache()
	if err != nil || cache.Latest != "v9.9.9" || cache.LastCheck.IsZero() {
		t.Errorf("cache = %+v, %v; want latest v9.9.9 with a timestamp", cache, err)
	}
}

func TestUpdateCheckUpToDate(t *testing.T) {
	updateTestConfig(t)
	serveRelease(t, "v1.6.1")
	old := version
	version = "1.6.1"
	t.Cleanup(func() { version = old })

	out, err := runUpdateCmd(t, "check")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(out, "up to date") {
		t.Errorf("output:\n%s", out)
	}
}

func TestUpdateCheckDevBuild(t *testing.T) {
	updateTestConfig(t)
	serveRelease(t, "v9.9.9")
	old := version
	version = "dev"
	t.Cleanup(func() { version = old })

	out, err := runUpdateCmd(t, "check")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	for _, want := range []string{"current:  dev", "not a release version (not comparable)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// A dev build's check does not poison the cache with a bogus verdict.
	cache, _ := update.LoadCache()
	if cache.Latest != "" {
		t.Errorf("cache.Latest = %q, want empty for a not-comparable current", cache.Latest)
	}
}

func TestUpdateCheckRateLimited(t *testing.T) {
	updateTestConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	update.SetBaseForTest(t, srv.URL)

	if _, err := runUpdateCmd(t, "check"); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("err = %v, want rate limit error", err)
	}
}

func TestUpdateConsentRoundTrip(t *testing.T) {
	path := updateTestConfig(t)

	if _, err := runUpdateCmd(t, "consent", "on"); err != nil {
		t.Fatalf("consent on: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.UpdateCheck == nil || !*cfg.Defaults.UpdateCheck {
		t.Errorf("after `on`: UpdateCheck = %v, want true", cfg.Defaults.UpdateCheck)
	}

	if _, err := runUpdateCmd(t, "consent", "off"); err != nil {
		t.Fatalf("consent off: %v", err)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.UpdateCheck == nil || *cfg.Defaults.UpdateCheck {
		t.Errorf("after `off`: UpdateCheck = %v, want false", cfg.Defaults.UpdateCheck)
	}

	if _, err := runUpdateCmd(t, "consent", "ask"); err != nil {
		t.Fatalf("consent ask: %v", err)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.UpdateCheck != nil {
		t.Errorf("after `ask`: UpdateCheck = %v, want nil (re-armed)", cfg.Defaults.UpdateCheck)
	}
}

func TestUpdateConsentBadArg(t *testing.T) {
	updateTestConfig(t)
	if err := updateConsentCmd.Args(updateConsentCmd, []string{"maybe"}); err == nil {
		t.Error("consent maybe accepted, want error")
	}
	if err := updateConsentCmd.Args(updateConsentCmd, nil); err == nil {
		t.Error("consent with no args accepted, want error")
	}
	if err := updateConsentCmd.Args(updateConsentCmd, []string{"on"}); err != nil {
		t.Errorf("consent on rejected: %v", err)
	}
}

func TestUpdateConsentMissingConfig(t *testing.T) {
	dir := t.TempDir()
	oldCfg := cfgFile
	cfgFile = filepath.Join(dir, "nope.yaml")
	t.Cleanup(func() { cfgFile = oldCfg })

	_, err := runUpdateCmd(t, "consent", "on")
	if err == nil || !strings.Contains(err.Error(), "nope.yaml") {
		t.Fatalf("err = %v, want missing-config error", err)
	}
}

// runUpdateCmd runs one of the update subcommand RunE functions in-process
// and captures its stdout, mirroring the captureCmd pattern of cli_test.go.
func runUpdateCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &strings.Builder{}
	c := &cobra.Command{}
	c.SetOut(out)
	c.SetErr(out)
	var err error
	switch args[0] {
	case "check":
		err = updateCheckRunE(c, args[1:])
	case "consent":
		err = updateConsentRunE(c, args[1:])
	default:
		t.Fatalf("unknown update subcommand %q", args[0])
	}
	return out.String(), err
}
