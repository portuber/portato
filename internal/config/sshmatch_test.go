package config

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	ssh_config "github.com/kevinburke/ssh_config"
)

// lenientForTest runs LenientSSHConfig and returns its output plus every
// warning it produced.
func lenientForTest(t *testing.T, text, path string) (string, []string) {
	t.Helper()
	var warns []string
	out := LenientSSHConfig([]byte(text), path, func(msg string) { warns = append(warns, msg) })
	return string(out), warns
}

// decodeFiltered asserts the filtered text still parses and resolves the
// given alias/key, proving the surviving blocks keep their meaning.
func decodeFiltered(t *testing.T, filtered, alias, key, want string) {
	t.Helper()
	cfg, err := ssh_config.DecodeBytes([]byte(filtered))
	if err != nil {
		t.Fatalf("decode filtered config: %v\n---\n%s", err, filtered)
	}
	if got, _ := cfg.Get(alias, key); got != want {
		t.Errorf("Get(%q, %q) = %q, want %q", alias, key, got, want)
	}
}

func TestLenientMatchExecSkipped(t *testing.T) {
	in := "Host ok\n  HostName 1.2.3.4\n\nMatch exec \"wm.exe\"\n  User dead\n  HostName dead.example\n\nHost after\n  User live\n"
	out, warns := lenientForTest(t, in, "/home/u/.ssh/config")

	if strings.Contains(out, "Match exec") || strings.Contains(out, "dead") {
		t.Errorf("Match block leaked into the filtered config:\n%s", out)
	}
	for _, want := range []string{"Host ok", "HostName 1.2.3.4", "Host after", "User live"} {
		if !strings.Contains(out, want) {
			t.Errorf("filtered config lost %q:\n%s", want, out)
		}
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "Match exec") || !strings.Contains(warns[0], "/home/u/.ssh/config") {
		t.Errorf("warnings = %v, want one naming the skipped block and its file", warns)
	}
	decodeFiltered(t, out, "ok", "HostName", "1.2.3.4")
	decodeFiltered(t, out, "after", "User", "live")
}

func TestLenientMatchAllAndHostKept(t *testing.T) {
	in := "Match all\n  Compression yes\n\nMatch host a b\n  User x\n"
	out, warns := lenientForTest(t, in, "/home/u/.ssh/config")

	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none for supported criteria", warns)
	}
	if !strings.Contains(out, "Match all") || !strings.Contains(out, "Match host a b") {
		t.Errorf("supported Match blocks must survive:\n%s", out)
	}
	decodeFiltered(t, out, "a", "User", "x")
	decodeFiltered(t, out, "anyhost", "Compression", "yes")
}

func TestLenientMatchMultiCriterionSkipped(t *testing.T) {
	// "host a exec x" would decode as patterns a/exec/x in the library —
	// wrong semantics; the filter must drop it instead.
	in := "Match host a exec x\n  User dead\n\nHost ok\n  User live\n"
	out, warns := lenientForTest(t, in, "/home/u/.ssh/config")

	if strings.Contains(out, "Match host") || strings.Contains(out, "dead") {
		t.Errorf("multi-criterion Match block leaked:\n%s", out)
	}
	if len(warns) != 1 {
		t.Errorf("warnings = %v, want one", warns)
	}
	decodeFiltered(t, out, "ok", "User", "live")
}

func TestLenientMatchEqualsForm(t *testing.T) {
	in := "Match=exec foo\n  User dead\nHost ok\n  User live\n"
	out, warns := lenientForTest(t, in, "/home/u/.ssh/config")

	if strings.Contains(out, "exec") {
		t.Errorf("Match= form leaked:\n%s", out)
	}
	if len(warns) != 1 {
		t.Errorf("warnings = %v, want one", warns)
	}
}

func TestLenientMatchQuotedCommentNotACriterion(t *testing.T) {
	// A '#' inside quotes is not a comment; a criterion-looking word inside
	// a quoted command is still just an argument to exec — but the block is
	// headed by exec anyway. The real assertion: "Match all" with a trailing
	// comment mentioning "host" stays supported.
	in := "Match all # host note\n  Compression yes\n"
	out, warns := lenientForTest(t, in, "/home/u/.ssh/config")

	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none: a comment must not look like a second criterion", warns)
	}
	if !strings.Contains(out, "Match all") {
		t.Errorf("Match all lost:\n%s", out)
	}
}

func TestLenientIncludeExpandedAndFiltered(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub.conf")
	if err := os.WriteFile(sub, []byte("Match exec x\n  User dead\n\nHost inc\n  HostName inc.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	main := "Include " + sub + "\n\nHost top\n  User t\n"
	out, warns := lenientForTest(t, main, filepath.Join(dir, "config"))

	if strings.Contains(out, "Include") || strings.Contains(out, "Match exec") || strings.Contains(out, "dead") {
		t.Errorf("include not spliced or Match block leaked:\n%s", out)
	}
	for _, want := range []string{"Host inc", "HostName inc.example", "Host top"} {
		if !strings.Contains(out, want) {
			t.Errorf("filtered config lost %q:\n%s", want, out)
		}
	}
	if len(warns) != 1 || !strings.Contains(warns[0], sub) {
		t.Errorf("warnings = %v, want one naming the included file", warns)
	}
	decodeFiltered(t, out, "inc", "HostName", "inc.example")
	decodeFiltered(t, out, "top", "User", "t")
}

func TestLenientIncludeRelativeUsesSSHDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-based resolution differs on Windows")
	}
	dir := t.TempDir()
	sshDir := filepath.Join(dir, ".ssh", "config.d")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "x.conf"), []byte("Host rel\n  HostName rel.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := os.Getenv("HOME")
	t.Setenv("HOME", dir)
	defer os.Setenv("HOME", prev)

	out, warns := lenientForTest(t, "Include config.d/x.conf\n", filepath.Join(dir, ".ssh", "config"))
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}
	if !strings.Contains(out, "Host rel") {
		t.Errorf("relative include not resolved against ~/.ssh:\n%s", out)
	}
}

func TestLenientIncludeCycleTerminates(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.conf")
	b := filepath.Join(dir, "b.conf")
	if err := os.WriteFile(a, []byte("Include "+b+"\nHost ha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("Include "+a+"\nHost hb\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, warns := lenientForTest(t, "Include "+a+"\nHost top\n", filepath.Join(dir, "config"))
	if !strings.Contains(out, "Host ha") || !strings.Contains(out, "Host hb") || !strings.Contains(out, "Host top") {
		t.Errorf("cycle dropped usable blocks:\n%s", out)
	}
	cycled := false
	for _, w := range warns {
		if strings.Contains(w, "cycle") {
			cycled = true
		}
	}
	if !cycled {
		t.Errorf("warnings = %v, want a cycle warning", warns)
	}
}

func TestLenientIncludeUnreadableKept(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file perms are not enforced the same way on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses file perms")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub.conf")
	if err := os.WriteFile(sub, []byte("Host x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o600) })

	line := "Include " + sub
	out, _ := lenientForTest(t, line+"\n", filepath.Join(dir, "config"))
	if !strings.Contains(out, line) {
		t.Errorf("an unreadable include must keep the directive for the decoder to report:\n%s", out)
	}
}

func TestLoadUserSSHConfigMatchExecTolerated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-based resolution differs on Windows")
	}
	dir := t.TempDir()
	sshDir := filepath.Join(dir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	conf := "Match exec \"run_only_on_laptop\"\n  User dead\n\nHost mine\n  HostName 10.0.0.9\n  User u\n  Port 2222\n  IdentityFile ~/.ssh/k\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := os.Getenv("HOME")
	t.Setenv("HOME", dir)
	defer os.Setenv("HOME", prev)

	prevLog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(prevLog)

	cfg, err := loadUserSSHConfig()
	if err != nil {
		t.Fatalf("loadUserSSHConfig: %v (the issue #4 regression)", err)
	}
	if cfg == nil {
		t.Fatal("cfg = nil, want a parsed config")
	}
	if got, _ := cfg.Get("mine", "HostName"); got != "10.0.0.9" {
		t.Errorf("Get(mine, HostName) = %q, want 10.0.0.9", got)
	}
}
