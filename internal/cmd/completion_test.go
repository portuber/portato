package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// setCfgFile points the package-level cfgFile (the --config backing store)
// at a temp config for the test and restores it on cleanup.
func setCfgFile(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := cfgFile
	cfgFile = path
	t.Cleanup(func() { cfgFile = prev })
}

func TestTuberNameCompletion_PrefixFilter(t *testing.T) {
	setCfgFile(t, "tubers:\n  - name: db-stage\n  - name: admin\n  - name: pull-db\n  - name: web-canary\n")
	cases := []struct {
		toComplete string
		want       []string
	}{
		{"", []string{"db-stage", "admin", "pull-db", "web-canary"}},
		{"db", []string{"db-stage"}},
		{"ad", []string{"admin"}},
		{"pull", []string{"pull-db"}},
		{"zzz", nil},
	}
	for _, tc := range cases {
		got, dir := tuberNameCompletion(nil, nil, tc.toComplete)
		if dir != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("toComplete=%q: directive=%v, want NoFileComp", tc.toComplete, dir)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("toComplete=%q: got %v, want %v", tc.toComplete, got, tc.want)
		}
	}
}

func TestTuberNameCompletion_NoConfig(t *testing.T) {
	prev := cfgFile
	cfgFile = filepath.Join(t.TempDir(), "does-not-exist.yaml")
	t.Cleanup(func() { cfgFile = prev })

	got, dir := tuberNameCompletion(nil, nil, "")
	if got != nil || dir != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("missing config: got (%v, %v), want (nil, NoFileComp)", got, dir)
	}
}

func TestTuberNameCompletion_BadYAML(t *testing.T) {
	setCfgFile(t, "tubers:\n  - name: \"unterminated\n")
	got, dir := tuberNameCompletion(nil, nil, "")
	if got != nil || dir != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("bad yaml: got (%v, %v), want (nil, NoFileComp)", got, dir)
	}
}

func TestTuberNameCompletion_ArgsGuard(t *testing.T) {
	setCfgFile(t, "tubers:\n  - name: db-stage\n")
	got, dir := tuberNameCompletion(nil, []string{"db-stage"}, "")
	if got != nil || dir != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("args guard: got (%v, %v), want (nil, NoFileComp)", got, dir)
	}
}

func TestTagValueCompletion_DistinctAndPrefix(t *testing.T) {
	setCfgFile(t, "tubers:\n  - name: db-prod\n    tags: [prod, db]\n  - name: web-prod\n    tags: [prod]\n  - name: cache\n")
	cases := []struct {
		toComplete string
		want       []string
	}{
		{"", []string{"prod", "db"}},
		{"p", []string{"prod"}},
		{"d", []string{"db"}},
		{"zzz", nil},
	}
	for _, tc := range cases {
		got, dir := tagValueCompletion(nil, nil, tc.toComplete)
		if dir != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("toComplete=%q: directive=%v, want NoFileComp", tc.toComplete, dir)
		}
		if !sameSet(got, tc.want) {
			t.Errorf("toComplete=%q: got %v, want %v", tc.toComplete, got, tc.want)
		}
	}
}

func TestTagValueCompletion_NoConfig(t *testing.T) {
	prev := cfgFile
	cfgFile = filepath.Join(t.TempDir(), "nope.yaml")
	t.Cleanup(func() { cfgFile = prev })
	got, dir := tagValueCompletion(nil, nil, "")
	if got != nil || dir != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("missing config: got (%v, %v), want (nil, NoFileComp)", got, dir)
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, s := range a {
		set[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := set[s]; !ok {
			return false
		}
	}
	return true
}

func TestCompletion_BashScript(t *testing.T) {
	if rootCmd.CompletionOptions.DisableDefaultCmd {
		t.Fatal("portato disables the default completion command; scripts can't be generated")
	}
	var buf bytes.Buffer
	if err := rootCmd.GenBashCompletionV2(&buf, true); err != nil {
		t.Fatalf("GenBashCompletionV2: %v", err)
	}
	if !strings.Contains(buf.String(), "portato") {
		t.Fatalf("bash completion script missing 'portato':\n%s", buf.String())
	}
}
