package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// downloadCap bounds one asset download (the release archives are ~5-6 MiB;
// 100 MiB is a generous ceiling against a truncated or hostile response).
const downloadCap = 100 << 20

// ErrChecksumMismatch marks a downloaded asset whose SHA-256 does not match
// the checksums.txt line — apply aborts with nothing installed.
var ErrChecksumMismatch = errors.New("update: checksum mismatch")

// AssetName maps GOOS/GOARCH onto the goreleaser archive name
// (.goreleaser.yml name_template): portato_<ver>_<macOS|Windows|linux>_
// <x86_64|arm64> with .tar.gz everywhere except .zip on Windows.
func AssetName(version string, goos, goarch string) string {
	osPart := goos
	switch goos {
	case "darwin":
		osPart = "macOS"
	case "windows":
		osPart = "Windows"
	}
	archPart := goarch
	if goarch == "amd64" {
		archPart = "x86_64"
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("portato_%s_%s_%s%s", strings.TrimPrefix(version, "v"), osPart, archPart, ext)
}

// FindAsset returns the release asset matching GOOS/GOARCH (or the explicit
// override) and the checksums.txt asset, or a named error when either is
// missing — the caller never guesses at partial releases.
func FindAsset(rel Release, goos, goarch string) (archive Asset, checksums Asset, err error) {
	want := AssetName(rel.Version, goos, goarch)
	for _, a := range rel.Assets {
		switch a.Name {
		case want:
			archive = a
		case "checksums.txt":
			checksums = a
		}
	}
	if archive.Name == "" {
		return Asset{}, Asset{}, fmt.Errorf("update: release %s has no asset %q for %s/%s", rel.Version, want, goos, goarch)
	}
	if checksums.Name == "" {
		return Asset{}, Asset{}, fmt.Errorf("update: release %s has no checksums.txt", rel.Version)
	}
	return archive, checksums, nil
}

// Downloader fetches release assets. The zero downloader uses the same
// compile-time base discipline as the API client: asset URLs come from the
// GitHub API response (browser_download_url), so with no seam installed a
// download can only ever target github.com.
type Downloader struct {
	http *http.Client
}

// NewDownloader builds a Downloader (5-minute per-file timeout).
func NewDownloader() *Downloader {
	return &Downloader{http: &http.Client{Timeout: 5 * time.Minute}}
}

// Download streams url to path (created with mode), capping the body at
// downloadCap. The partial file is removed on any failure.
func (d *Downloader) Download(ctx context.Context, url, path string) (int64, error) {
	return d.downloadFile(ctx, url, path, 0o600)
}

func (d *Downloader) downloadFile(ctx context.Context, url, path string, mode os.FileMode) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("update: download: %w", err)
	}
	req.Header.Set("User-Agent", "portato/"+CurrentVersion)
	resp, err := d.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("update: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("update: download %s: unexpected status %s", url, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, fmt.Errorf("update: download: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return 0, fmt.Errorf("update: download: %w", err)
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, downloadCap))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return 0, fmt.Errorf("update: download: %w", err)
	}
	return n, nil
}

// VerifyChecksum parses the sha256sum-format checksums.txt and checks file
// against the line naming it. A missing line is a mismatch — a release
// archive must be covered.
func VerifyChecksum(checksumsPath, file string) error {
	return verifyChecksum(checksumsPath, file)
}

// verifyChecksum is the internal implementation.
func verifyChecksum(checksumsPath, file string) error {
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return fmt.Errorf("update: read checksums: %w", err)
	}
	base := filepath.Base(file)
	var want string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == base {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("%w: %s is not listed in checksums.txt", ErrChecksumMismatch, base)
	}
	h, err := sha256File(file)
	if err != nil {
		return err
	}
	if h != want {
		return fmt.Errorf("%w: %s is %s, want %s", ErrChecksumMismatch, base, h, want)
	}
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("update: hash: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("update: hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ExtractBinary pulls the single "portato" member out of the release archive
// (tar.gz or zip) into dst (mode taken from the current binary — archive
// bits are not trusted), without unpacking anything else.
func ExtractBinary(archivePath, dst string, mode os.FileMode) error {
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz"):
		return extractTarGz(archivePath, dst, mode)
	case strings.HasSuffix(archivePath, ".zip"):
		return extractZip(archivePath, dst, mode)
	default:
		return fmt.Errorf("update: unsupported archive %s", archivePath)
	}
}

func extractTarGz(archivePath, dst string, mode os.FileMode) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("update: open archive: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("update: open archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return errors.New("update: archive has no portato member")
		}
		if err != nil {
			return fmt.Errorf("update: read archive: %w", err)
		}
		if filepath.Base(hdr.Name) != "portato" || hdr.Typeflag != tar.TypeReg {
			continue
		}
		return writeMember(tr, dst, mode)
	}
}

func extractZip(archivePath, dst string, mode os.FileMode) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("update: open archive: %w", err)
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if filepath.Base(zf.Name) != "portato" || zf.FileInfo().IsDir() {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return fmt.Errorf("update: read archive: %w", err)
		}
		err = writeMember(rc, dst, mode)
		rc.Close()
		return err
	}
	return errors.New("update: archive has no portato member")
}

func writeMember(r io.Reader, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("update: extract: %w", err)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("update: extract: %w", err)
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("update: extract: %w", err)
	}
	return nil
}

// CurrentVersion is set by the cmd package (the embedded version) before
// apply runs; the downloader's User-Agent mirrors the API client's.
var CurrentVersion = "dev"

// SetCurrentVersion installs the embedded version for download User-Agents.
func SetCurrentVersion(v string) { CurrentVersion = v }

// runtimeOSArch is a seam for tests (AssetName/FindAsset callers pass
// explicit values; this default keeps the command layer honest).
func runtimeOSArch() (string, string) { return runtime.GOOS, runtime.GOARCH }
