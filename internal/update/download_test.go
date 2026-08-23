package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssetName(t *testing.T) {
	cases := []struct {
		version, goos, goarch, want string
	}{
		{"v1.7.0", "darwin", "arm64", "portato_1.7.0_macOS_arm64.tar.gz"},
		{"v1.7.0", "darwin", "amd64", "portato_1.7.0_macOS_x86_64.tar.gz"},
		{"1.7.0", "linux", "amd64", "portato_1.7.0_linux_x86_64.tar.gz"},
		{"v1.8.0", "linux", "arm64", "portato_1.8.0_linux_arm64.tar.gz"},
		{"v1.8.0", "windows", "amd64", "portato_1.8.0_Windows_x86_64.zip"},
		{"v1.8.0", "windows", "arm64", "portato_1.8.0_Windows_arm64.zip"},
	}
	for _, tc := range cases {
		if got := AssetName(tc.version, tc.goos, tc.goarch); got != tc.want {
			t.Errorf("AssetName(%s,%s,%s) = %q, want %q", tc.version, tc.goos, tc.goarch, got, tc.want)
		}
	}
}

func fixtureReleaseAssets() []Asset {
	return []Asset{
		{Name: "portato_1.8.0_macOS_arm64.tar.gz", URL: "http://x/mac.tar.gz"},
		{Name: "portato_1.8.0_Windows_x86_64.zip", URL: "http://x/win.zip"},
		{Name: "checksums.txt", URL: "http://x/checksums.txt"},
		{Name: "portato_1.8.0_linux_amd64.deb", URL: "http://x/l.deb"},
	}
}

func TestFindAsset(t *testing.T) {
	rel := Release{Version: "v1.8.0", Assets: fixtureReleaseAssets()}
	if _, _, err := FindAsset(rel, "darwin", "arm64"); err != nil {
		t.Errorf("darwin/arm64: %v", err)
	}
	if _, _, err := FindAsset(rel, "windows", "amd64"); err != nil {
		t.Errorf("windows/amd64: %v", err)
	}
	if _, _, err := FindAsset(rel, "linux", "riscv64"); err == nil || !strings.Contains(err.Error(), "no asset") {
		t.Errorf("linux/riscv64: err = %v, want no-asset error", err)
	}
	noSum := Release{Version: "v1.8.0", Assets: []Asset{{Name: "portato_1.8.0_linux_arm64.tar.gz"}}}
	if _, _, err := FindAsset(noSum, "linux", "arm64"); err == nil || !strings.Contains(err.Error(), "checksums.txt") {
		t.Errorf("no checksums: err = %v, want missing-checksums error", err)
	}
}

// buildTarGz builds an in-memory tar.gz with the given members.
func buildTarGz(t *testing.T, members map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range members {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildZip(t *testing.T, members map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range members {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractBinaryTarGz(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "a.tar.gz")
	payload := "#!/bin/sh\necho new-binary\n"
	if err := os.WriteFile(arch, buildTarGz(t, map[string]string{
		"portato/LICENSE":   "mit",
		"portato/portato":   payload,
		"portato/README.md": "readme",
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "portato")
	if err := ExtractBinary(arch, dst, 0o755); err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != payload {
		t.Errorf("extracted %q, want the portato member only", got)
	}
	if info, err := os.Stat(dst); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("dst mode = %v, %v; want 0755 (from the current binary, not the archive)", info, err)
	}
}

func TestExtractBinaryZip(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "a.zip")
	payload := "new-windows-binary"
	if err := os.WriteFile(arch, buildZip(t, map[string]string{
		"LICENSE":   "mit",
		"portato":   payload,
		"README.md": "readme",
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "portato")
	if err := ExtractBinary(arch, dst, 0o755); err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != payload {
		t.Errorf("extracted %q", got)
	}
}

func TestExtractBinaryMissingMember(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "a.tar.gz")
	if err := os.WriteFile(arch, buildTarGz(t, map[string]string{"LICENSE": "mit"}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ExtractBinary(arch, filepath.Join(dir, "out"), 0o755); err == nil || !strings.Contains(err.Error(), "no portato member") {
		t.Errorf("err = %v, want no-member error", err)
	}
}

// serveFixtureRelease spins a server answering every path with the
// corresponding fixture body (asset bytes + checksums.txt) and returns its
// URL. Used with the downloader directly.
func serveFixtureRelease(t *testing.T, files map[string][]byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestDownloadAndVerify(t *testing.T) {
	dir := t.TempDir()
	binary := []byte("fake-release-binary-contents")
	other := []byte("checksums target for another file")
	sums := fmt.Sprintf("%s  portato_1.8.0_macOS_arm64.tar.gz\n%s  portato_1.8.0_Windows_x86_64.zip\n",
		sha256Hex(binary), sha256Hex(other))
	base := serveFixtureRelease(t, map[string][]byte{
		"portato_1.8.0_macOS_arm64.tar.gz": binary,
		"checksums.txt":                    []byte(sums),
	})

	d := newDownloader()
	arch := filepath.Join(dir, "portato_1.8.0_macOS_arm64.tar.gz")
	n, err := d.downloadFile(context.Background(), base+"/portato_1.8.0_macOS_arm64.tar.gz", arch, 0o600)
	if err != nil || n != int64(len(binary)) {
		t.Fatalf("download: %v, %d bytes", err, n)
	}
	if _, err := d.downloadFile(context.Background(), base+"/checksums.txt", filepath.Join(dir, "sums.txt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksum(filepath.Join(dir, "sums.txt"), arch); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// Corrupt one byte → mismatch sentinel.
	if err := os.WriteFile(arch, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksum(filepath.Join(dir, "sums.txt"), arch); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
}

func TestDownloadUnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)
	d := newDownloader()
	dst := filepath.Join(t.TempDir(), "f")
	if _, err := d.downloadFile(context.Background(), srv.URL+"/gone", dst, 0o600); err == nil || !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("err = %v, want unexpected-status", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("failed download left a partial file")
	}
}

func TestDownloadCap(t *testing.T) {
	if downloadCap != 100<<20 {
		t.Errorf("downloadCap = %d, want 100 MiB", downloadCap)
	}
	_ = time.Now
}

func TestVerifyChecksumMissingLine(t *testing.T) {
	dir := t.TempDir()
	sums := filepath.Join(dir, "sums.txt")
	if err := os.WriteFile(sums, []byte(sha256Hex([]byte("x"))+"  other.tar.gz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "portato_1.8.0_macOS_arm64.tar.gz")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksum(sums, f); err == nil || !strings.Contains(err.Error(), "not listed") {
		t.Fatalf("err = %v, want not-listed", err)
	}
}
