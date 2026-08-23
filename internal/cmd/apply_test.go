package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/portuber/portato/internal/update"
)

// buildFixtureArchive builds the release tar.gz with a single portato
// member of newBinaryContents, returning the archive bytes and a matching
// sha256sum line for the platform's asset name.
func buildFixtureArchive(t *testing.T, newBinaryContents string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "portato", Mode: 0o755, Size: int64(len(newBinaryContents))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(newBinaryContents)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(buf.Bytes())
	sums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(h[:]), update.AssetName("v9.9.9", GOOS, GOARCH))
	return buf.Bytes(), sums
}

func cobraCommand() cobra.Command { return cobra.Command{} }

// applyFixture spins a fixture "GitHub": a releases/latest JSON whose asset
// URLs point at the same server's file endpoints (a real tar.gz + matching
// checksums.txt), all routed through the in-repo seam.
func applyFixture(t *testing.T, newBinaryContents string) {
	t.Helper()
	archiveBytes, sums := buildFixtureArchive(t, newBinaryContents)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/repos/portuber/portato/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","html_url":"https://example.com/v9.9.9","assets":[
			{"name":%q,"browser_download_url":%q},
			{"name":"checksums.txt","browser_download_url":%q}]}`,
			update.AssetName("v9.9.9", GOOS, GOARCH), srv.URL+"/dl/archive.tar.gz", srv.URL+"/dl/checksums.txt")
	})
	mux.HandleFunc("/dl/archive.tar.gz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveBytes)
	})
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sums))
	})
	update.SetBaseForTest(t, srv.URL)
}

// applySeams installs the standard apply seams: a temp "installed" binary at
// path, TTY true/false, confirm yes/no. Returns nothing; call resetApplySeams
// implicitly via t.Cleanup.
func installApplySeams(t *testing.T, path string, tty bool, confirm bool) {
	t.Helper()
	prevExe, prevTTY, prevConfirm := applyExecutable, applyTTY, applyConfirm
	applyExecutable = func() (string, error) { return path, nil }
	applyTTY = func() bool { return tty }
	applyConfirm = func(_ io.Writer, _ string) bool { return confirm }
	t.Cleanup(func() { applyExecutable, applyTTY, applyConfirm = prevExe, prevTTY, prevConfirm })

	prevYes, prevForce, prevDry := applyYes, applyForce, applyDry
	applyYes, applyForce, applyDry = false, false, false
	t.Cleanup(func() { applyYes, applyForce, applyDry = prevYes, prevForce, prevDry })

	if err := os.WriteFile(path, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runApply(t *testing.T, args ...string) (string, error) {
	t.Helper()
	_ = args
	out := &strings.Builder{}
	c := &cobra.Command{}
	c.SetOut(out)
	c.SetErr(out)
	err := updateApplyRunE(c, nil)
	return out.String(), err
}

func TestApplyManagedChannelRefused(t *testing.T) {
	dir := t.TempDir()
	// A go-install layout (…/go/bin/portato) — the suffix heuristic works
	// anywhere, including a temp dir, unlike the /opt/homebrew prefix.
	exe := filepath.Join(dir, "go", "bin", "portato")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	installApplySeams(t, exe, true, true)
	applyFixture(t, "new-binary")
	old := version
	version = "1.7.0"
	t.Cleanup(func() { version = old })

	out, err := runApply(t)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, want := range []string{"channel: goinstall", "refusing the in-place swap", "go install github.com/portuber/portato/cmd/portato@latest"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// The binary is untouched.
	got, _ := os.ReadFile(exe)
	if string(got) != "old-binary" {
		t.Error("managed refusal modified the installed binary")
	}
}

func TestApplyManagedForceOverrides(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "go", "bin", "portato")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	installApplySeams(t, exe, true, true)
	applyFixture(t, "new-binary")
	applyForce = true
	applyYes = true
	old := version
	version = "1.7.0"
	t.Cleanup(func() { version = old })

	out, err := runApply(t)
	if err != nil {
		t.Fatalf("apply --force: %v", err)
	}
	if !strings.Contains(out, "updated v1.7.0 -> v9.9.9") {
		t.Errorf("output:\n%s", out)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "new-binary" {
		t.Errorf("binary = %q, want the fixture payload", got)
	}
	if _, err := os.Stat(exe + ".old"); err != nil {
		t.Error("no portato.old backup after a swap")
	}
}

func TestApplyDirectFullCycle(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "portato")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	installApplySeams(t, exe, true, true)
	applyFixture(t, "fresh-release-binary")
	old := version
	version = "1.7.0"
	t.Cleanup(func() { version = old })

	out, err := runApply(t)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, want := range []string{"update available: v1.7.0 -> v9.9.9", "channel: direct", "updated v1.7.0 -> v9.9.9", "rollback: mv"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "fresh-release-binary" {
		t.Errorf("binary = %q", got)
	}
	// Second run: up to date (the cache version var is unchanged, but the
	// fixture still serves v9.9.9 > 1.7.0 — so this asserts idempotence of
	// the swap mechanics only when version is bumped; skip here).
}

func TestApplyDryRunTouchesNothing(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "portato")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	installApplySeams(t, exe, true, true)
	applyFixture(t, "new-binary")
	applyDry = true
	old := version
	version = "1.7.0"
	t.Cleanup(func() { version = old })

	out, err := runApply(t)
	if err != nil {
		t.Fatalf("apply --dry-run: %v", err)
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("output:\n%s", out)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "old-binary" {
		t.Error("dry run modified the binary")
	}
}

func TestApplyNonTTYWithoutYes(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "portato")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	installApplySeams(t, exe, false, true)
	applyFixture(t, "new-binary")
	old := version
	version = "1.7.0"
	t.Cleanup(func() { version = old })

	_, err := runApply(t)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v, want non-TTY --yes requirement", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "old-binary" {
		t.Error("refused non-TTY apply modified the binary")
	}
}

func TestApplyUpToDate(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "portato")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	installApplySeams(t, exe, true, true)
	applyFixture(t, "new-binary")
	old := version
	version = "9.9.9"
	t.Cleanup(func() { version = old })

	out, err := runApply(t)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !strings.Contains(out, "up to date") {
		t.Errorf("output:\n%s", out)
	}
}

func TestApplyDevBuildRefused(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "portato")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	installApplySeams(t, exe, true, true)
	applyFixture(t, "new-binary")
	old := version
	version = "dev"
	t.Cleanup(func() { version = old })

	_, err := runApply(t)
	if err == nil || !strings.Contains(err.Error(), "not a release") {
		t.Fatalf("err = %v, want dev-build refusal", err)
	}
}

func TestApplyChecksumMismatchAborts(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "portato")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	installApplySeams(t, exe, true, true)
	// A fixture whose checksums.txt lies about the archive.
	archiveBytes, _ := buildFixtureArchive(t, "new-binary")
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/repos/portuber/portato/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","assets":[
			{"name":%q,"browser_download_url":%q},
			{"name":"checksums.txt","browser_download_url":%q}]}`,
			update.AssetName("v9.9.9", GOOS, GOARCH), srv.URL+"/dl/archive.tar.gz", srv.URL+"/dl/checksums.txt")
	})
	mux.HandleFunc("/dl/archive.tar.gz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archiveBytes) })
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000  " + update.AssetName("v9.9.9", GOOS, GOARCH) + "\n"))
	})
	update.SetBaseForTest(t, srv.URL)

	old := version
	version = "1.7.0"
	t.Cleanup(func() { version = old })

	_, err := runApply(t)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "old-binary" {
		t.Error("failed apply modified the installed binary")
	}
}
