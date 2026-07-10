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
tubers:
  - name: keep-me  # do not lose this comment
    type: local
    local: 5432
    remote: db:5432
    ssh: deploy@bastion:22
    enabled: false
  - name: edit-me
    type: remote
    local: 127.0.0.1:9090
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

func TestAddTuberNode_AppendsAndPreservesComments(t *testing.T) {
	p := writeTmpConfig(t, commentedConfig)

	added := Tuber{
		Name: "new-one", Type: "dynamic",
		Local: "1080", SSH: "deploy@bastion:22",
	}
	if err := AddTuberNode(p, added); err != nil {
		t.Fatalf("AddTuberNode: %v", err)
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
	if len(c.Tubers) != 3 {
		t.Fatalf("expected 3 tubers, got %d", len(c.Tubers))
	}
	last := c.Tubers[2]
	if last.Name != "new-one" || last.Type != "dynamic" || last.ListenAddr() != "127.0.0.1:1080" {
		t.Errorf("added tuber mismatch: %+v", last)
	}
}

func TestAddTuberNode_CreatesTubersSequence(t *testing.T) {
	p := writeTmpConfig(t, "defaults:\n  identity: ~/.ssh/id\n")
	added := Tuber{Name: "solo", Type: "local", Local: "1", Remote: "h:1", SSH: "u@h:22"}
	if err := AddTuberNode(p, added); err != nil {
		t.Fatalf("AddTuberNode: %v", err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Tubers) != 1 || c.Tubers[0].Name != "solo" {
		t.Errorf("expected solo tuber, got %+v", c.Tubers)
	}
}

func TestReplaceTuberNode_RewritesAndPreservesOthers(t *testing.T) {
	p := writeTmpConfig(t, commentedConfig)

	repl := Tuber{
		Name: "edit-me", Type: "dynamic",
		Local: "1080", SSH: "deploy@bastion:22",
	}
	if err := ReplaceTuberNode(p, "edit-me", repl); err != nil {
		t.Fatalf("ReplaceTuberNode: %v", err)
	}

	data, _ := os.ReadFile(p)
	out := string(data)
	for _, want := range []string{
		"# top-of-file comment",
		"# do not lose this comment", // untouched tuber keeps its comment
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
	if len(c.Tubers) != 2 {
		t.Fatalf("expected 2 tubers, got %d", len(c.Tubers))
	}
	if c.Tubers[1].Type != "dynamic" || c.Tubers[1].Name != "edit-me" {
		t.Errorf("replaced tuber mismatch: %+v", c.Tubers[1])
	}
}

func TestReplaceTuberNode_Rename(t *testing.T) {
	p := writeTmpConfig(t, commentedConfig)
	repl := Tuber{Name: "renamed", Type: "local", Local: "1", Remote: "h:1", SSH: "u@h:22"}
	if err := ReplaceTuberNode(p, "edit-me", repl); err != nil {
		t.Fatalf("ReplaceTuberNode: %v", err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	names := []string{c.Tubers[0].Name, c.Tubers[1].Name}
	if names[0] != "keep-me" || names[1] != "renamed" {
		t.Errorf("rename: got %v", names)
	}
}

func TestReplaceTuberNode_NotFound(t *testing.T) {
	p := writeTmpConfig(t, commentedConfig)
	err := ReplaceTuberNode(p, "nope", Tuber{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestDeleteTuberNode_RemovesAndPreservesOthers(t *testing.T) {
	p := writeTmpConfig(t, commentedConfig)

	if err := DeleteTuberNode(p, "edit-me"); err != nil {
		t.Fatalf("DeleteTuberNode: %v", err)
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
	if len(c.Tubers) != 1 || c.Tubers[0].Name != "keep-me" {
		t.Errorf("expected only keep-me, got %+v", c.Tubers)
	}
}

func TestDeleteTuberNode_NotFound(t *testing.T) {
	p := writeTmpConfig(t, commentedConfig)
	err := DeleteTuberNode(p, "nope")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestWithTuberAdded_DuplicateRejected(t *testing.T) {
	c, err := Load(writeTmpConfig(t, commentedConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	dup := Tuber{Name: "keep-me", Type: "local", Local: "1", Remote: "h:1", SSH: "u@h:22"}
	if _, err := c.WithTuberAdded(dup); err == nil {
		t.Error("expected duplicate-name validation error")
	}
	// The original config must be untouched (clone semantics).
	if len(c.Tubers) != 2 {
		t.Errorf("original config mutated: %d tubers", len(c.Tubers))
	}
}

func TestWithTuberReplaced_AllowsKeepingName(t *testing.T) {
	c, err := Load(writeTmpConfig(t, commentedConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repl := Tuber{Name: "edit-me", Type: "dynamic", Local: "1080", SSH: "u@h:22"}
	out, err := c.WithTuberReplaced("edit-me", repl)
	if err != nil {
		t.Fatalf("WithTuberReplaced: %v", err)
	}
	if out.Tubers[1].Type != "dynamic" {
		t.Errorf("replaced type = %q", out.Tubers[1].Type)
	}
	if len(c.Tubers) != 2 || c.Tubers[1].Type != "remote" {
		t.Errorf("original config mutated: %+v", c.Tubers[1])
	}
}

func TestWithTuberRemoved(t *testing.T) {
	c, err := Load(writeTmpConfig(t, commentedConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out, err := c.WithTuberRemoved("edit-me")
	if err != nil {
		t.Fatalf("WithTuberRemoved: %v", err)
	}
	if len(out.Tubers) != 1 || out.Tubers[0].Name != "keep-me" {
		t.Errorf("got %+v", out.Tubers)
	}
	if _, err := c.WithTuberRemoved("nope"); err == nil {
		t.Error("expected not-found error")
	}
}
