package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fixtureRelease is a realistic releases/latest payload (the goreleaser
// archive names from .goreleaser.yml + checksums.txt).
const fixtureRelease = `{
  "tag_name": "v1.7.0",
  "html_url": "https://github.com/portuber/portato/releases/tag/v1.7.0",
  "published_at": "2026-08-21T10:00:00Z",
  "assets": [
    {"name": "portato_1.7.0_macOS_arm64.tar.gz",
     "browser_download_url": "https://example.com/portato_1.7.0_macOS_arm64.tar.gz",
     "size": 100},
    {"name": "portato_1.7.0_Windows_x86_64.zip",
     "browser_download_url": "https://example.com/portato_1.7.0_Windows_x86_64.zip",
     "size": 200},
    {"name": "checksums.txt",
     "browser_download_url": "https://example.com/checksums.txt",
     "size": 3}
  ]
}`

// newFixtureClient spins an httptest server answering with the given handler
// and routes a Client at it through the SetBaseForTest seam — proving the
// production base (DefaultBase) is what NewClient dials otherwise.
func newFixtureClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	SetBaseForTest(t, srv.URL)
	return NewClient("portato/test")
}

func TestLatestOK(t *testing.T) {
	var gotAccept, gotUA, gotPath string
	c := newFixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fixtureRelease))
	})
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if gotPath != "/repos/portuber/portato/releases/latest" {
		t.Errorf("request path = %q", gotPath)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotUA != "portato/test" {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if rel.Version != "v1.7.0" {
		t.Errorf("Version = %q", rel.Version)
	}
	if rel.URL != "https://github.com/portuber/portato/releases/tag/v1.7.0" {
		t.Errorf("URL = %q", rel.URL)
	}
	if want := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC); !rel.PublishedAt.Equal(want) {
		t.Errorf("PublishedAt = %v", rel.PublishedAt)
	}
	if len(rel.Assets) != 3 {
		t.Fatalf("Assets = %d, want 3", len(rel.Assets))
	}
	if rel.Assets[0].Name != "portato_1.7.0_macOS_arm64.tar.gz" || rel.Assets[0].Size != 100 {
		t.Errorf("asset[0] = %+v", rel.Assets[0])
	}
}

func TestLatestRateLimited(t *testing.T) {
	c := newFixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	})
	if _, err := c.Latest(context.Background()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestLatestForbiddenNotRateLimit(t *testing.T) {
	// A 403 without the exhausted header is a plain error, not the sentinel.
	c := newFixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	_, err := c.Latest(context.Background())
	if err == nil || errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want non-sentinel error", err)
	}
}

func TestLatestUnexpectedStatus(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		c := newFixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		})
		_, err := c.Latest(context.Background())
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("status %d: err = %v, want unexpected-status error", code, err)
		}
	}
}

func TestLatestMalformed(t *testing.T) {
	c := newFixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json at all"))
	})
	if _, err := c.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("err = %v, want malformed error", err)
	}
}

func TestLatestMissingTag(t *testing.T) {
	c := newFixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"html_url": "https://example.com"}`))
	})
	if _, err := c.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "no tag_name") {
		t.Fatalf("err = %v, want no-tag_name error", err)
	}
}

func TestLatestNetworkRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // the port is now closed; the dial must fail
	SetBaseForTest(t, url)
	c := NewClient("portato/test")
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("err = nil, want network error")
	}
}

func TestNewClientDialsDefaultBase(t *testing.T) {
	// With no seam installed, the client must target the real GitHub API —
	// the runtime-redirect escape hatch does not exist.
	c := NewClient("portato/test")
	if c.base != DefaultBase {
		t.Errorf("base = %q, want %q", c.base, DefaultBase)
	}
}
