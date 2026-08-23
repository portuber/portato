package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/portuber/portato/internal/update"
)

// sha256Line renders the sha256sum line for the fixture archive.
func sha256Line(t *testing.T, b []byte, name string) string {
	t.Helper()
	h := sha256.Sum256(b)
	return fmt.Sprintf("%s  %s\n", hex.EncodeToString(h[:]), name)
}

// serveVersionedRelease spins a fixture GitHub serving one release with the
// given tag whose archive carries payload. Returns nothing; routes through
// the in-repo base seam.
func serveVersionedRelease(t *testing.T, tag, payload string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "portato", Mode: 0o755, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	assetName := update.AssetName(tag, GOOS, GOARCH)
	checksums := sha256Line(t, buf.Bytes(), assetName)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/repos/portuber/portato/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://example.com/%s","assets":[
			{"name":%q,"browser_download_url":%q},
			{"name":"checksums.txt","browser_download_url":%q}]}`,
			tag, tag, assetName, srv.URL+"/dl/archive", srv.URL+"/dl/checksums")
	})
	mux.HandleFunc("/dl/archive", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(buf.Bytes()) })
	mux.HandleFunc("/dl/checksums", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(checksums)) })
	update.SetBaseForTest(t, srv.URL)
}

func TestApplyE2EIdempotentSecondRun(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "portato")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	installApplySeams(t, exe, true, true)
	_ = os.Remove(exe)
	if err := os.WriteFile(exe, []byte("v1-binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	serveVersionedRelease(t, "v2.0.0", "v2-binary")
	old := version
	version = "1.0.0"
	t.Cleanup(func() { version = old })

	// First apply: swap happens, mode preserved from the old binary.
	out, err := runApply(t)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if !strings.Contains(out, "updated v1.0.0 -> v2.0.0") {
		t.Fatalf("first apply output:\n%s", out)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "v2-binary" {
		t.Fatalf("binary = %q", got)
	}
	if info, err := os.Stat(exe); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, %v; want 0700 preserved from the old binary", info, err)
	}
	oldBytes, err := os.ReadFile(exe + ".old")
	if err != nil || string(oldBytes) != "v1-binary" {
		t.Errorf("rollback copy = %q, %v", oldBytes, err)
	}

	// The binary now reports 2.0.0: the second apply sees "up to date".
	version = "2.0.0"
	out, err = runApply(t)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if !strings.Contains(out, "up to date") {
		t.Fatalf("second apply output:\n%s", out)
	}
	// And nothing changed on disk.
	got, _ = os.ReadFile(exe)
	if string(got) != "v2-binary" {
		t.Errorf("binary changed after an up-to-date apply: %q", got)
	}
}

func TestApplyE2EDowngradeRefused(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "portato")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	installApplySeams(t, exe, true, true)
	if err := os.WriteFile(exe, []byte("newer-build"), 0o755); err != nil {
		t.Fatal(err)
	}
	serveVersionedRelease(t, "v0.1.0", "ancient-binary")
	old := version
	version = "9.9.9"
	t.Cleanup(func() { version = old })

	out, err := runApply(t)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !strings.Contains(out, "up to date (v9.9.9)") {
		t.Fatalf("downgrade offer output:\n%s", out)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "newer-build" {
		t.Error("downgrade attempt modified the binary")
	}
}

func TestApplyE2EScmRefusalNotOverridable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "portato")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	installApplySeams(t, exe, true, true)
	if err := os.WriteFile(exe, []byte("scm-held"), 0o755); err != nil {
		t.Fatal(err)
	}
	serveVersionedRelease(t, "v2.0.0", "new-binary")
	prevSCM := scmServiceInstalled
	scmServiceInstalled = func() bool { return true }
	t.Cleanup(func() { scmServiceInstalled = prevSCM })
	applyForce = true
	old := version
	version = "1.0.0"
	t.Cleanup(func() { version = old })

	_, err := runApply(t)
	if err == nil || !strings.Contains(err.Error(), "SCM-held") {
		t.Fatalf("err = %v, want SCM refusal even under --force", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "scm-held" {
		t.Error("SCM refusal modified the binary")
	}
}

func TestApplyE2ESecondSwapReplacesBackup(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "portato")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	installApplySeams(t, exe, true, true)
	if err := os.WriteFile(exe, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := version
	t.Cleanup(func() { version = old })

	serveVersionedRelease(t, "v2.0.0", "v2")
	version = "1.0.0"
	if _, err := runApply(t); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	serveVersionedRelease(t, "v3.0.0", "v3")
	version = "2.0.0"
	if _, err := runApply(t); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "v3" {
		t.Fatalf("binary = %q after two applies", got)
	}
	// One level of rollback only: .old holds v2, not v1.
	backup, err := os.ReadFile(exe + ".old")
	if err != nil || string(backup) != "v2" {
		t.Errorf("backup = %q, %v; want the previous version only (v2)", backup, err)
	}
}
