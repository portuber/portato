package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kipkaev55/portato/internal/client"
	"github.com/kipkaev55/portato/internal/forward"
)

// shortDir returns a short temp directory for the unix socket, avoiding the
// macOS SUN_LEN limit that long t.TempDir() paths can exceed.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// stubServer is a tiny daemon stand-in over a unix socket. It records the
// requests it receives so tests can assert which RPCs the CLI issued.
type stubServer struct {
	t        *testing.T
	mu       sync.Mutex
	server   *http.Server
	listener net.Listener
	socket   string

	enabled  []string
	disabled []string
	restarts []string
}

func newStubServer(t *testing.T, statuses []forward.Status) *stubServer {
	t.Helper()
	dir := shortDir(t)
	socket := filepath.Join(dir, "portato.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	mux := http.NewServeMux()
	s := &stubServer{t: t, server: &http.Server{Handler: mux}, listener: ln, socket: socket}

	known := map[string]struct{}{}
	for _, st := range statuses {
		known[st.Name] = struct{}{}
	}

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeStubJSON(t, w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /tunnels", func(w http.ResponseWriter, _ *http.Request) {
		writeStubJSON(t, w, statuses)
	})
	mux.HandleFunc("POST /tunnels/{name}/enable", func(w http.ResponseWriter, r *http.Request) {
		s.record(w, r, "enable", known)
	})
	mux.HandleFunc("POST /tunnels/{name}/disable", func(w http.ResponseWriter, r *http.Request) {
		s.record(w, r, "disable", known)
	})
	mux.HandleFunc("POST /tunnels/{name}/restart", func(w http.ResponseWriter, r *http.Request) {
		s.record(w, r, "restart", known)
	})

	go func() { _ = s.server.Serve(ln) }()
	t.Cleanup(func() { _ = s.server.Close() })
	return s
}

func (s *stubServer) record(w http.ResponseWriter, r *http.Request, op string, known map[string]struct{}) {
	name := r.PathValue("name")
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := known[name]; !ok {
		writeStubErr(s.t, w, http.StatusNotFound, "unknown tunnel %q", name)
		return
	}
	resp := map[string]string{"tunnel": name}
	switch op {
	case "enable":
		s.enabled = append(s.enabled, name)
		resp["status"] = "enabled"
	case "disable":
		s.disabled = append(s.disabled, name)
		resp["status"] = "disabled"
	case "restart":
		s.restarts = append(s.restarts, name)
		resp["status"] = "restarted"
	}
	writeStubJSON(s.t, w, resp)
}

func (s *stubServer) client() *client.Client { return client.New(s.socket) }

func writeStubJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

func writeStubErr(t *testing.T, w http.ResponseWriter, status int, format string, args ...any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf(format, args...)}); err != nil {
		t.Fatalf("encode err: %v", err)
	}
}

// useStub overrides the package-level dialDaemon to point at s for the test.
func useStub(t *testing.T, s *stubServer) {
	t.Helper()
	prev := dialDaemon
	dialDaemon = func() (*client.Client, error) { return s.client(), nil }
	t.Cleanup(func() { dialDaemon = prev })
}

// captureCmd returns a cobra command whose stdout/stderr are buffered.
func captureCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	c := &cobra.Command{}
	c.SetOut(out)
	c.SetErr(errOut)
	return c, out, errOut
}

func sampleStatuses() []forward.Status {
	return []forward.Status{
		{Name: "db-stage", Type: "local", Local: "5432", Remote: "bastion:5432", State: forward.Connected},
		{Name: "admin", Type: "local", Local: "8080", Remote: "web:80", State: forward.Off},
	}
}

func TestList_PrintsTable(t *testing.T) {
	s := newStubServer(t, sampleStatuses())
	useStub(t, s)

	c, out, errOut := captureCmd()
	if err := listRunE(c, nil); err != nil {
		t.Fatalf("listRunE: %v", err)
	}
	if errOut.String() != "" {
		t.Errorf("unexpected stderr: %q", errOut.String())
	}
	for _, want := range []string{"NAME", "db-stage", "5432 → bastion:5432", "connected", "admin", "off"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list output missing %q\ngot:\n%s", want, out.String())
		}
	}
}

func TestEnableDisableRestart_ConfirmAndRPC(t *testing.T) {
	s := newStubServer(t, sampleStatuses())
	useStub(t, s)

	cases := []struct {
		name string
		run  func(*cobra.Command, []string) error
		want string
	}{
		{"enable", enableRunE, "enabled: db-stage"},
		{"disable", disableRunE, "disabled: db-stage"},
		{"restart", restartRunE, "restarted: db-stage"},
	}
	for _, tc := range cases {
		c, out, errOut := captureCmd()
		if err := tc.run(c, []string{"db-stage"}); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if errOut.String() != "" {
			t.Errorf("%s stderr: %q", tc.name, errOut.String())
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("%s output = %q, want to contain %q", tc.name, out.String(), tc.want)
		}
	}

	if len(s.enabled) != 1 || s.enabled[0] != "db-stage" {
		t.Errorf("enable RPC: got %v", s.enabled)
	}
	if len(s.disabled) != 1 || s.disabled[0] != "db-stage" {
		t.Errorf("disable RPC: got %v", s.disabled)
	}
	if len(s.restarts) != 1 || s.restarts[0] != "db-stage" {
		t.Errorf("restart RPC: got %v", s.restarts)
	}
}

func TestEnable_UnknownTunnel(t *testing.T) {
	s := newStubServer(t, sampleStatuses())
	useStub(t, s)

	c, out, errOut := captureCmd()
	if err := enableRunE(c, []string{"nope"}); err == nil {
		t.Fatal("expected error for unknown tunnel")
	}
	if !strings.Contains(errOut.String(), "unknown tunnel") {
		t.Errorf("stderr should mention unknown tunnel; got %q", errOut.String())
	}
	if out.String() != "" {
		t.Errorf("stdout should be empty on error; got %q", out.String())
	}
}

func TestCLI_DaemonDownHint(t *testing.T) {
	prev := dialDaemon
	dialDaemon = func() (*client.Client, error) { return nil, errDaemonDown }
	t.Cleanup(func() { dialDaemon = prev })

	cases := []struct {
		name string
		run  func(*cobra.Command, []string) error
		args []string
	}{
		{"list", listRunE, nil},
		{"enable", enableRunE, []string{"x"}},
		{"disable", disableRunE, []string{"x"}},
		{"restart", restartRunE, []string{"x"}},
	}
	for _, tc := range cases {
		c, out, errOut := captureCmd()
		if err := tc.run(c, tc.args); err == nil {
			t.Errorf("%s: expected error when daemon down", tc.name)
		}
		if !strings.Contains(errOut.String(), "portato daemon is not running") {
			t.Errorf("%s stderr should contain daemon-down hint; got %q", tc.name, errOut.String())
		}
		if !strings.Contains(errOut.String(), "portato install") {
			t.Errorf("%s stderr should mention 'portato install'; got %q", tc.name, errOut.String())
		}
		if out.String() != "" {
			t.Errorf("%s stdout should be empty when daemon down; got %q", tc.name, out.String())
		}
	}
}

func TestProbeDaemon(t *testing.T) {
	s := newStubServer(t, sampleStatuses())

	if !probeDaemon(s.socket) {
		t.Error("probeDaemon should report true for a live unix-socket server")
	}

	dead := filepath.Join(shortDir(t), "missing.sock")
	if probeDaemon(dead) {
		t.Error("probeDaemon should report false when nothing is listening")
	}
}
