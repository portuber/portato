package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const commentedConfig = `# top-of-file comment
defaults:
  identity: ~/.ssh/id_ed25519
  known_hosts: ~/.ssh/known_hosts
# manage production access here
tunnels:
  - name: keep-me  # do not lose this comment
    type: local
    local: 5432
    remote: db:5432
    ssh: deploy@bastion:22
    enabled: false
  - name: edit-me
    type: remote
    remote: 9090
    ssh: deploy@bastion:22
    enabled: false
`

func writeTmpConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

func TestAddTunnelNode_AppendsAndPreservesComments(t *testing.T) {
	p := writeTmpConfig(t, commentedConfig)

	added := Tunnel{
		Name: "new-one", Type: "dynamic",
		Local: "1080", SSH: "deploy@bastion:22",
	}
	if err := AddTunnelNode(p, added); err != nil {
		t.Fatalf("AddTunnelNode: %v", err)
	}

	data, _ := os.ReadFile(p)
	out := string(data)
	for _, want := range []string{
		"# top-of-file comment",
		"# manage production access here",
		"# do not lose this comment",
		"name: new-one",
		"type: dynamic",
		"name: keep-me",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}

	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load after add: %v", err)
	}
	if len(c.Tunnels) != 3 {
		t.Fatalf("expected 3 tunnels, got %d", len(c.Tunnels))
	}
	last := c.Tunnels[2]
	if last.Name != "new-one" || last.Type != "dynamic" || last.ListenAddr() != "127.0.0.1:1080" {
		t.Errorf("added tunnel mismatch: %+v", last)
	}
}

func TestAddTunnelNode_CreatesTunnelsSequence(t *testing.T) {
	p := writeTmpConfig(t, "defaults:\n  identity: ~/.ssh/id\n")
	added := Tunnel{Name: "solo", Type: "local", Local: "1", Remote: "h:1", SSH: "u@h:22"}
	if err := AddTunnelNode(p, added); err != nil {
		t.Fatalf("AddTunnelNode: %v", err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Tunnels) != 1 || c.Tunnels[0].Name != "solo" {
		t.Errorf("expected solo tunnel, got %+v", c.Tunnels)
	}
}

func TestReplaceTunnelNode_RewritesAndPreservesOthers(t *testing.T) {
	p := writeTmpConfig(t, commentedConfig)

	repl := Tunnel{
		Name: "edit-me", Type: "dynamic",
		Local: "1080", SSH: "deploy@bastion:22",
	}
	if err := ReplaceTunnelNode(p, "edit-me", repl); err != nil {
		t.Fatalf("ReplaceTunnelNode: %v", err)
	}

	data, _ := os.ReadFile(p)
	out := string(data)
	for _, want := range []string{
		"# top-of-file comment",
		"# do not lose this comment", // untouched tunnel keeps its comment
		"type: dynamic",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}

	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load after replace: %v", err)
	}
	if len(c.Tunnels) != 2 {
		t.Fatalf("expected 2 tunnels, got %d", len(c.Tunnels))
	}
	if c.Tunnels[1].Type != "dynamic" || c.Tunnels[1].Name != "edit-me" {
		t.Errorf("replaced tunnel mismatch: %+v", c.Tunnels[1])
	}
}

func TestReplaceTunnelNode_Rename(t *testing.T) {
	p := writeTmpConfig(t, commentedConfig)
	repl := Tunnel{Name: "renamed", Type: "local", Local: "1", Remote: "h:1", SSH: "u@h:22"}
	if err := ReplaceTunnelNode(p, "edit-me", repl); err != nil {
		t.Fatalf("ReplaceTunnelNode: %v", err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	names := []string{c.Tunnels[0].Name, c.Tunnels[1].Name}
	if names[0] != "keep-me" || names[1] != "renamed" {
		t.Errorf("rename: got %v", names)
	}
}

func TestReplaceTunnelNode_NotFound(t *testing.T) {
	p := writeTmpConfig(t, commentedConfig)
	err := ReplaceTunnelNode(p, "nope", Tunnel{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestDeleteTunnelNode_RemovesAndPreservesOthers(t *testing.T) {
	p := writeTmpConfig(t, commentedConfig)

	if err := DeleteTunnelNode(p, "edit-me"); err != nil {
		t.Fatalf("DeleteTunnelNode: %v", err)
	}

	data, _ := os.ReadFile(p)
	out := string(data)
	for _, want := range []string{"# do not lose this comment", "name: keep-me"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "edit-me") {
		t.Errorf("edit-me should be gone\n%s", out)
	}

	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load after delete: %v", err)
	}
	if len(c.Tunnels) != 1 || c.Tunnels[0].Name != "keep-me" {
		t.Errorf("expected only keep-me, got %+v", c.Tunnels)
	}
}

func TestDeleteTunnelNode_NotFound(t *testing.T) {
	p := writeTmpConfig(t, commentedConfig)
	err := DeleteTunnelNode(p, "nope")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestWithTunnelAdded_DuplicateRejected(t *testing.T) {
	c, err := Load(writeTmpConfig(t, commentedConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	dup := Tunnel{Name: "keep-me", Type: "local", Local: "1", Remote: "h:1", SSH: "u@h:22"}
	if _, err := c.WithTunnelAdded(dup); err == nil {
		t.Error("expected duplicate-name validation error")
	}
	// The original config must be untouched (clone semantics).
	if len(c.Tunnels) != 2 {
		t.Errorf("original config mutated: %d tunnels", len(c.Tunnels))
	}
}

func TestWithTunnelReplaced_AllowsKeepingName(t *testing.T) {
	c, err := Load(writeTmpConfig(t, commentedConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repl := Tunnel{Name: "edit-me", Type: "dynamic", Local: "1080", SSH: "u@h:22"}
	out, err := c.WithTunnelReplaced("edit-me", repl)
	if err != nil {
		t.Fatalf("WithTunnelReplaced: %v", err)
	}
	if out.Tunnels[1].Type != "dynamic" {
		t.Errorf("replaced type = %q", out.Tunnels[1].Type)
	}
	if len(c.Tunnels) != 2 || c.Tunnels[1].Type != "remote" {
		t.Errorf("original config mutated: %+v", c.Tunnels[1])
	}
}

func TestWithTunnelRemoved(t *testing.T) {
	c, err := Load(writeTmpConfig(t, commentedConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out, err := c.WithTunnelRemoved("edit-me")
	if err != nil {
		t.Fatalf("WithTunnelRemoved: %v", err)
	}
	if len(out.Tunnels) != 1 || out.Tunnels[0].Name != "keep-me" {
		t.Errorf("got %+v", out.Tunnels)
	}
	if _, err := c.WithTunnelRemoved("nope"); err == nil {
		t.Error("expected not-found error")
	}
}
