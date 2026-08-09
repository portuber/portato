package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

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
// prints one line per tuber. Table-driven to keep per-function complexity low
// (CodeFactor runs gocyclo on test files too; the local .golangci.yml exempts
// them). Mirrors the TestEnableDisableRestart_ConfirmAndRPC shape.
func TestEnableDisableRestart_Tag(t *testing.T) {
	cases := []struct {
		name    string
		tag     string
		setTag  func(string)
		run     func(*cobra.Command, []string) error
		getRPCs func(*stubServer) []string
		verb    string
		want    []string
		notWant []string
	}{
		{
			name: "enable --tag prod", tag: "prod",
			setTag: func(v string) { enableTag = v },
			run:    enableRunE, getRPCs: func(s *stubServer) []string { return s.enabled },
			verb: "enabled", want: []string{"db-prod", "web-prod"},
		},
		{
			name: "disable --tag prod", tag: "prod",
			setTag: func(v string) { disableTag = v },
			run:    disableRunE, getRPCs: func(s *stubServer) []string { return s.disabled },
			verb: "disabled", want: []string{"db-prod", "web-prod"},
		},
		{
			name: "restart --tag db", tag: "db",
			setTag: func(v string) { restartTag = v },
			run:    restartRunE, getRPCs: func(s *stubServer) []string { return s.restarts },
			verb: "restarted", want: []string{"db-prod"}, notWant: []string{"web-prod"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStubServer(t, taggedStatuses())
			useStub(t, s)
			tc.setTag(tc.tag)
			t.Cleanup(func() { tc.setTag("") })
			c, out, errOut := captureCmd()
			if err := tc.run(c, nil); err != nil {
				t.Fatalf("run: %v", err)
			}
			if errOut.String() != "" {
				t.Errorf("unexpected stderr: %q", errOut.String())
			}
			for _, name := range tc.want {
				if !strings.Contains(out.String(), tc.verb+": "+name) {
					t.Errorf("output missing %q\ngot:\n%s", tc.verb+": "+name, out.String())
				}
			}
			for _, name := range tc.notWant {
				if strings.Contains(out.String(), name) {
					t.Errorf("%s should not appear in output\ngot:\n%s", name, out.String())
				}
			}
			if got := tc.getRPCs(s); !sameSet(got, tc.want) {
				t.Errorf("RPCs: got %v, want %v", got, tc.want)
			}
		})
	}
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
