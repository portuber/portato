package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/forward"
	"github.com/kipkaev55/portato/internal/ipctoken"
	routelog "github.com/kipkaev55/portato/internal/log"
)

// defaultTimeout caps a single HTTP round-trip to the daemon.
const defaultTimeout = 5 * time.Second

// Client talks to the portato daemon over a unix-domain socket. It is
// stateless and safe for concurrent use; the underlying http.Client is reused.
type Client struct {
	http       *http.Client
	stream     *http.Client
	socketPath string
}

// New builds a client that dials the daemon at socketPath. It best-effort reads
// the daemon's IPC bearer token from TokenPath(socketPath) (next to the socket)
// and attaches it to every request via a RoundTripper — so callers are
// authenticated automatically with no API change. A missing token file
// (old daemon, or --ipc-token off) yields no header and an open daemon answers
// 200, keeping the client backward compatible.
func New(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	token, _ := ipctoken.ReadToken(ipctoken.TokenPath(socketPath))
	authed := &tokenTransport{base: transport, token: token}
	return &Client{
		http:       &http.Client{Transport: authed, Timeout: defaultTimeout},
		stream:     &http.Client{Transport: authed}, // no overall timeout: SSE is long-lived
		socketPath: socketPath,
	}
}

// tokenTransport injects the daemon IPC bearer token into every request when
// the client was built with one. No-op when token is "" (old daemon, or escape
// hatch off) so the client stays backward compatible. The same header value is
// set on every call, so the mutation is idempotent across retries.
type tokenTransport struct {
	base  http.RoundTripper
	token string
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(req)
}

// Socket returns the socket path this client dials.
func (c *Client) Socket() string { return c.socketPath }

// Healthz reports whether the daemon is alive.
func (c *Client) Healthz() error {
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.get("/healthz", &resp)
}

// HealthzCtx is a context-aware liveness probe. The smart launcher uses it
// with a short deadline (200ms) so a missing daemon does not stall startup.
func (c *Client) HealthzCtx(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url("/healthz"), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return decodeError(resp)
	}
	return nil
}

// List returns the current status of every tunnel.
func (c *Client) List() ([]forward.Status, error) {
	var out []forward.Status
	if err := c.get("/tunnels", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Enable turns a tunnel on (the daemon persists enabled=true to the config).
func (c *Client) Enable(name string) error {
	_, err := c.post(fmt.Sprintf("/tunnels/%s/enable", name))
	return err
}

// Disable turns a tunnel off (persists enabled=false).
func (c *Client) Disable(name string) error {
	_, err := c.post(fmt.Sprintf("/tunnels/%s/disable", name))
	return err
}

// Restart stops and starts a tunnel without changing its persisted state.
func (c *Client) Restart(name string) error {
	_, err := c.post(fmt.Sprintf("/tunnels/%s/restart", name))
	return err
}

// AcceptHost appends the tunnel's pending unknown-host key to known_hosts on
// the daemon and restarts the tunnel (Phase 11 TOFU prompt).
func (c *Client) AcceptHost(name string) error {
	_, err := c.post(fmt.Sprintf("/tunnels/%s/accept-host", name))
	return err
}

// Reload makes the daemon re-read the config from disk.
func (c *Client) Reload() error {
	_, err := c.post("/reload")
	return err
}

// Config returns the daemon's current configuration (the daemon owns the real
// config path). Used by the TUI editor to prefill forms. Phase 10.
func (c *Client) Config() (*config.Config, error) {
	var cfg config.Config
	if err := c.get("/config", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// AddTunnel asks the daemon to create a tunnel (validates, persists, reloads).
func (c *Client) AddTunnel(t config.Tunnel) error {
	return c.sendBody(http.MethodPost, "/tunnels", t)
}

// UpdateTunnel asks the daemon to replace the tunnel named name with t.
func (c *Client) UpdateTunnel(name string, t config.Tunnel) error {
	return c.sendBody(http.MethodPut, fmt.Sprintf("/tunnels/%s", name), t)
}

// DeleteTunnel asks the daemon to remove the tunnel named name.
func (c *Client) DeleteTunnel(name string) error {
	return c.sendBody(http.MethodDelete, fmt.Sprintf("/tunnels/%s", name), nil)
}

// Logs fetches the recent in-memory log entries for a tunnel from the daemon's
// ring buffer (Phase 11). An empty name returns every tunnel's entries.
func (c *Client) Logs(name string) ([]routelog.Entry, error) {
	path := "/logs"
	if name != "" {
		path += "?name=" + neturl.QueryEscape(name)
	}
	var out []routelog.Entry
	if err := c.get(path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Events opens the daemon's SSE stream (GET /events) and returns the response
// body for line-by-line reading. The caller owns the ReadCloser and must close
// it to end the subscription. A context cancellation propagates to the stream.
// Uses a dedicated http.Client with no overall timeout: the stream is long-
// lived (Phase 9 push events).
func (c *Client) Events(ctx context.Context) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url("/events"), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, decodeError(resp)
	}
	return resp.Body, nil
}

func (c *Client) get(path string, out any) error {
	resp, err := c.http.Get(url(path))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return decodeError(resp)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) post(path string) (map[string]string, error) {
	resp, err := c.http.Post(url(path), "application/json", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, decodeError(resp)
	}
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body, nil
}

// sendBody issues a request with an optional JSON body and returns an error on
// a non-2xx response. Used by the tunnel mutation endpoints (Phase 10).
func (c *Client) sendBody(method, path string, body any) error {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url(path), r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return decodeError(resp)
	}
	return nil
}

func decodeError(resp *http.Response) error {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Error != "" {
		return fmt.Errorf("daemon: %s", body.Error)
	}
	return fmt.Errorf("daemon: unexpected status %s", resp.Status)
}

func url(path string) string {
	return "http://unix" + path
}
