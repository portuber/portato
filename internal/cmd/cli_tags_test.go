package cmd

import (
	"strings"
	"testing"

	"github.com/portuber/portato/internal/forward"
)

// taggedStatuses is the stub roster for the --tag tests: two prod tubers (one
// also db), one untagged.
func taggedStatuses() []forward.Status {
	return []forward.Status{
		{Name: "db-prod", Type: "local", Local: "5432", Remote: "db:5432", State: forward.Off, Tags: []string{"prod", "db"}},
		{Name: "web-prod", Type: "local", Local: "8080", Remote: "web:80", State: forward.Off, Tags: []string{"prod"}},
		{Name: "cache", Type: "local", Local: "6379", Remote: "cache:6379", State: forward.Off},
	}
}

func TestResolveTagOrName(t *testing.T) {
	list := func() ([]forward.Status, error) { return taggedStatuses(), nil }

	t.Run("name passthrough", func(t *testing.T) {
		got, err := resolveTagOrName([]string{"db-prod"}, "", list)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != "db-prod" {
			t.Errorf("got %v, want [db-prod]", got)
		}
	})

	t.Run("tag matches case-insensitive", func(t *testing.T) {
		got, err := resolveTagOrName(nil, "Prod", list)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 || got[0] != "db-prod" || got[1] != "web-prod" {
			t.Errorf("got %v, want [db-prod web-prod]", got)
		}
	})

	t.Run("no match is error", func(t *testing.T) {
		if _, err := resolveTagOrName(nil, "nope", list); err == nil {
			t.Fatal("expected error for no match")
		}
	})

	t.Run("both is error", func(t *testing.T) {
		if _, err := resolveTagOrName([]string{"db-prod"}, "prod", list); err == nil {
			t.Fatal("expected error for both --tag and name")
		}
	})

	t.Run("neither is error", func(t *testing.T) {
		if _, err := resolveTagOrName(nil, "", list); err == nil {
			t.Fatal("expected error for neither --tag nor name")
		}
	})
}

// TestEnableDisableRestart_Tag covers the end-to-end --tag path: each command
// resolves the tagged set via the daemon's List, issues one RPC per tuber, and
// prints one line per tuber.
func TestEnableDisableRestart_Tag(t *testing.T) {
	t.Run("enable --tag prod", func(t *testing.T) {
		s := newStubServer(t, taggedStatuses())
		useStub(t, s)
		enableTag = "prod"
		t.Cleanup(func() { enableTag = "" })
		c, out, errOut := captureCmd()
		if err := enableRunE(c, nil); err != nil {
			t.Fatalf("enableRunE: %v", err)
		}
		if errOut.String() != "" {
			t.Errorf("unexpected stderr: %q", errOut.String())
		}
		for _, want := range []string{"enabled: db-prod", "enabled: web-prod"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("output missing %q\ngot:\n%s", want, out.String())
			}
		}
		if len(s.enabled) != 2 {
			t.Errorf("enable RPCs: got %v, want 2", s.enabled)
		}
	})

	t.Run("disable --tag prod", func(t *testing.T) {
		s := newStubServer(t, taggedStatuses())
		useStub(t, s)
		disableTag = "prod"
		t.Cleanup(func() { disableTag = "" })
		c, out, errOut := captureCmd()
		if err := disableRunE(c, nil); err != nil {
			t.Fatalf("disableRunE: %v", err)
		}
		if errOut.String() != "" {
			t.Errorf("unexpected stderr: %q", errOut.String())
		}
		if !strings.Contains(out.String(), "disabled: db-prod") || !strings.Contains(out.String(), "disabled: web-prod") {
			t.Errorf("output should name both prod tubers\ngot:\n%s", out.String())
		}
		if len(s.disabled) != 2 {
			t.Errorf("disable RPCs: got %v, want 2", s.disabled)
		}
	})

	t.Run("restart --tag db", func(t *testing.T) {
		s := newStubServer(t, taggedStatuses())
		useStub(t, s)
		restartTag = "db"
		t.Cleanup(func() { restartTag = "" })
		c, out, errOut := captureCmd()
		if err := restartRunE(c, nil); err != nil {
			t.Fatalf("restartRunE: %v", err)
		}
		if errOut.String() != "" {
			t.Errorf("unexpected stderr: %q", errOut.String())
		}
		if !strings.Contains(out.String(), "restarted: db-prod") {
			t.Errorf("output should name the db-tagged tuber\ngot:\n%s", out.String())
		}
		if strings.Contains(out.String(), "web-prod") {
			t.Errorf("web-prod is not db-tagged; should not be restarted\ngot:\n%s", out.String())
		}
		if len(s.restarts) != 1 || s.restarts[0] != "db-prod" {
			t.Errorf("restart RPCs: got %v, want [db-prod]", s.restarts)
		}
	})
}

// TestEnable_TagAndNameRejected confirms the RunE surfaces the mutual-exclusion
// error (both flags set).
func TestEnable_TagAndNameRejected(t *testing.T) {
	s := newStubServer(t, taggedStatuses())
	useStub(t, s)
	enableTag = "prod"
	t.Cleanup(func() { enableTag = "" })
	c, _, _ := captureCmd()
	if err := enableRunE(c, []string{"db-prod"}); err == nil {
		t.Fatal("expected error when both --tag and name given")
	}
}
