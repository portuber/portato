package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrRateLimited marks an anonymous GitHub API rate-limit hit (HTTP 403 with
// X-RateLimit-Remaining: 0) — a temporary condition: the caller keeps the
// cached latest and retries after the TTL, never storms.
var ErrRateLimited = errors.New("update: github: rate limited (retry later)")

const (
	// DefaultBase is the anonymous GitHub REST API root. The update source
	// is deliberately not configurable at runtime (no env, no flag): the
	// checker and the future self-update must talk to GitHub and nowhere
	// else. Tests redirect via SetBaseForTest — an in-repo seam that
	// production code cannot even call (it takes a testing-style hook).
	DefaultBase = "https://api.github.com"
	// repoPath targets the portato repo; releases/latest resolves to the
	// newest non-draft non-prerelease release, which under the
	// strict-vX.Y.Z VERSIONING policy is exactly "the latest version".
	repoPath        = "repos/portuber/portato/releases/latest"
	requestTimeout  = 10 * time.Second
	responseBodyCap = 1 << 20
)

// apiBase is the API root NewClient dials. It equals DefaultBase in every
// production build; only SetBaseForTest (in-repo tests) reassigns it.
var apiBase = DefaultBase

// testHook is the slice of testing.TB SetBaseForTest needs — an interface so
// this file does not import testing (which would register test flags into
// production binaries).
type testHook interface {
	Cleanup(func())
}

// SetBaseForTest points the client at a fixture API root for the duration of
// one test (auto-restored via the hook's Cleanup). Exported because package
// tests in internal/cmd exercise the checker end-to-end the same way.
func SetBaseForTest(t testHook, base string) {
	prev := apiBase
	apiBase = base
	t.Cleanup(func() { apiBase = prev })
}

// Asset is one downloadable release file (the goreleaser archives plus
// checksums.txt).
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is the latest GitHub release as far as the checker cares.
type Release struct {
	Version     string // tag_name verbatim, e.g. "v1.7.0"
	URL         string // human-facing release page
	PublishedAt time.Time
	Assets      []Asset
}

// Client queries the GitHub Releases API for the portato repo. Zero-value
// unsafe; use NewClient.
type Client struct {
	base      string
	userAgent string
	http      *http.Client
}

// NewClient builds a Client. The userAgent must identify the binary
// ("portato/<version>" — GitHub requires a UA and rejects the Go default
// less politely than a named one). The base URL is the compile-time
// DefaultBase (see the apiBase note on runtime redirectability).
func NewClient(userAgent string) *Client {
	return &Client{
		base:      apiBase,
		userAgent: userAgent,
		http:      &http.Client{Timeout: requestTimeout},
	}
}

// Latest fetches the newest (non-draft, non-prerelease) release. Error
// taxonomy: network failure, ErrRateLimited, unexpected HTTP status, or a
// malformed body — all plain errors; nothing here panics or blocks beyond
// the caller's context.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/"+repoPath, nil)
	if err != nil {
		return Release{}, fmt.Errorf("update: github: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("update: github: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return Release{}, ErrRateLimited
	default:
		return Release{}, fmt.Errorf("update: github: unexpected status %s", resp.Status)
	}

	var payload struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Assets      []Asset   `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, responseBodyCap)).Decode(&payload); err != nil {
		return Release{}, fmt.Errorf("update: github: malformed response: %w", err)
	}
	if payload.TagName == "" {
		return Release{}, errors.New("update: github: malformed response: no tag_name")
	}
	return Release{
		Version:     payload.TagName,
		URL:         payload.HTMLURL,
		PublishedAt: payload.PublishedAt,
		Assets:      payload.Assets,
	}, nil
}
