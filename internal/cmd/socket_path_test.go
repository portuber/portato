package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/portuber/portato/internal/ipctoken"
)

// TestSocketPath_JSON pins the `portato socket-path --json` contract: one
// JSON object whose socket honours the PORTATO_SOCKET override, whose
// token_path is the token file next to that socket, and whose version is the
// CLI binary's build version ("dev" in tests). No daemon is running.
func TestSocketPath_JSON(t *testing.T) {
	t.Setenv("PORTATO_SOCKET", "/tmp/pt-cmd-override.sock")

	prev := socketPathJSON
	socketPathJSON = true
	t.Cleanup(func() { socketPathJSON = prev })

	c, out, errOut := captureCmd()
	if err := socketPathCmd.RunE(c, nil); err != nil {
		t.Fatalf("socketPathCmd: %v", err)
	}
	if errOut.String() != "" {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
	var p struct {
		Socket    string `json:"socket"`
		TokenPath string `json:"token_path"`
		Version   string `json:"version"`
	}
	if err := json.Unmarshal(out.Bytes(), &p); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if p.Socket != "/tmp/pt-cmd-override.sock" {
		t.Errorf("socket = %q, want the PORTATO_SOCKET override", p.Socket)
	}
	if want := ipctoken.TokenPath("/tmp/pt-cmd-override.sock"); p.TokenPath != want {
		t.Errorf("token_path = %q, want %q", p.TokenPath, want)
	}
	if p.Version != "dev" {
		t.Errorf("version = %q, want %q (test build default)", p.Version, "dev")
	}
}

// TestSocketPath_Plain covers the default human-readable output.
func TestSocketPath_Plain(t *testing.T) {
	t.Setenv("PORTATO_SOCKET", "/tmp/pt-cmd-plain.sock")

	prev := socketPathJSON
	socketPathJSON = false
	t.Cleanup(func() { socketPathJSON = prev })

	c, out, errOut := captureCmd()
	if err := socketPathCmd.RunE(c, nil); err != nil {
		t.Fatalf("socketPathCmd: %v", err)
	}
	if errOut.String() != "" {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
	for _, want := range []string{
		"socket:     /tmp/pt-cmd-plain.sock",
		"token_path: " + ipctoken.TokenPath("/tmp/pt-cmd-plain.sock"),
		"version:    dev",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plain output missing %q:\n%s", want, out.String())
		}
	}
}
