package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/kipkaev55/portato/internal/forward"
)

// defaultTimeout caps a single HTTP round-trip to the daemon.
const defaultTimeout = 5 * time.Second

// Client talks to the portato daemon over a unix-domain socket. It is
// stateless and safe for concurrent use; the underlying http.Client is reused.
type Client struct {
	http       *http.Client
	socketPath string
}

// New builds a client that dials the daemon at socketPath.
func New(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		http:       &http.Client{Transport: transport, Timeout: defaultTimeout},
		socketPath: socketPath,
	}
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

// Reload makes the daemon re-read the config from disk.
func (c *Client) Reload() error {
	_, err := c.post("/reload")
	return err
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
