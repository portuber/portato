package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setLogsFlags sets the package-level logs flags for a test and restores them
// afterwards (the flags are bound to package vars by init()).
func setLogsFlags(t *testing.T, follow bool, lines int, since, tuber string, all bool) {
	t.Helper()
	pf, pl, ps, pt, pa := logsFollow, logsLines, logsSince, logsTuber, logsAll
	logsFollow, logsLines, logsSince, logsTuber, logsAll = follow, lines, since, tuber, all
	t.Cleanup(func() { logsFollow, logsLines, logsSince, logsTuber, logsAll = pf, pl, ps, pt, pa })
}

// pointLogsAt redirects the logs command at the given candidate paths (daemon
// first) for the duration of the test.
func pointLogsAt(t *testing.T, paths ...string) {
	t.Helper()
	prev := logsPaths
	logsPaths = func() []string { return paths }
	t.Cleanup(func() { logsPaths = prev })
}

// logLine builds a slog-text-shaped record at t with the given fields. msg is
// always quoted (slog quotes when it contains spaces; the parser does not care).
func logLine(t time.Time, level, msg, tuber string) string {
	ts := t.UTC().Format(time.RFC3339Nano)
	if tuber == "" {
		return fmt.Sprintf("time=%s level=%s msg=%q", ts, level, msg)
	}
	return fmt.Sprintf("time=%s level=%s msg=%q tuber=%s", ts, level, msg, tuber)
}

func writeLog(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestLogs_DefaultDump prints every record when no filter is set.
func TestLogs_DefaultDump(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "daemon.log")
	now := time.Now().UTC()
	body := strings.Join([]string{
		logLine(now.Add(-3*time.Minute), "INFO", "daemon started", ""),
		logLine(now.Add(-2*time.Minute), "DEBUG", "connecting", "db-stage"),
		logLine(now.Add(-1*time.Minute), "INFO", "connection forwarded", "db-stage"),
		"", // trailing newline
	}, "\n")
	writeLog(t, daemon, body)
	pointLogsAt(t, daemon, filepath.Join(dir, "portato.log"))

	setLogsFlags(t, false, 0, "", "", false)
	c, out, errOut := captureCmd()
	if err := logsRunE(c, nil); err != nil {
		t.Fatalf("logsRunE: %v", err)
	}
	if errOut.String() != "" {
		t.Errorf("unexpected stderr: %q", errOut.String())
	}
	got := out.String()
	for _, want := range []string{"daemon started", "connecting", "connection forwarded"} {
		if !strings.Contains(got, want) {
			t.Errorf("default dump missing %q\ngot:\n%s", want, got)
		}
	}
}

// TestLogs_NoLogFile prints a friendly message and exits 0 when no log exists.
func TestLogs_NoLogFile(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "daemon.log")
	pointLogsAt(t, daemon, filepath.Join(dir, "portato.log"))

	setLogsFlags(t, false, 0, "", "", false)
	c, out, _ := captureCmd()
	if err := logsRunE(c, nil); err != nil {
		t.Fatalf("logsRunE: %v", err)
	}
	if !strings.Contains(out.String(), "no log file yet") {
		t.Errorf("expected 'no log file yet' message, got: %q", out.String())
	}
	if !strings.Contains(out.String(), daemon) {
		t.Errorf("expected the message to name the path %q, got: %q", daemon, out.String())
	}
}

// TestLogs_FallsBackToStandalone reads portato.log when daemon.log is absent.
func TestLogs_FallsBackToStandalone(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "daemon.log")
	standalone := filepath.Join(dir, "portato.log")
	writeLog(t, standalone, logLine(time.Now().UTC(), "INFO", "standalone line", "")+"\n")
	pointLogsAt(t, daemon, standalone)

	setLogsFlags(t, false, 0, "", "", false)
	c, out, _ := captureCmd()
	if err := logsRunE(c, nil); err != nil {
		t.Fatalf("logsRunE: %v", err)
	}
	if !strings.Contains(out.String(), "standalone line") {
		t.Errorf("standalone fallback missing the record, got: %q", out.String())
	}
}

// TestLogs_TuberFilter keeps only the matching tuber's records.
func TestLogs_TuberFilter(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "daemon.log")
	now := time.Now().UTC()
	body := strings.Join([]string{
		logLine(now.Add(-2*time.Minute), "INFO", "forwarded", "db-stage"),
		logLine(now.Add(-1*time.Minute), "INFO", "forwarded", "admin-ui"),
		"",
	}, "\n")
	writeLog(t, daemon, body)
	pointLogsAt(t, daemon)

	setLogsFlags(t, false, 0, "", "db-stage", false)
	c, out, _ := captureCmd()
	if err := logsRunE(c, nil); err != nil {
		t.Fatalf("logsRunE: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "tuber=db-stage") {
		t.Errorf("expected db-stage record, got: %q", got)
	}
	if strings.Contains(got, "admin-ui") {
		t.Errorf("--tuber must drop admin-ui records, got: %q", got)
	}
}

// TestLogs_SinceFilter drops records older than the window.
func TestLogs_SinceFilter(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "daemon.log")
	now := time.Now().UTC()
	body := strings.Join([]string{
		logLine(now.Add(-30*time.Minute), "INFO", "old record", "db-stage"),   // dropped
		logLine(now.Add(-1*time.Minute), "INFO", "recent record", "db-stage"), // kept
		"",
	}, "\n")
	writeLog(t, daemon, body)
	pointLogsAt(t, daemon)

	setLogsFlags(t, false, 0, "10m", "", false)
	c, out, _ := captureCmd()
	if err := logsRunE(c, nil); err != nil {
		t.Fatalf("logsRunE: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "recent record") {
		t.Errorf("--since 10m must keep the recent record, got: %q", got)
	}
	if strings.Contains(got, "old record") {
		t.Errorf("--since 10m must drop the 30m-old record, got: %q", got)
	}
}

// TestLogs_LinesLimit keeps only the last N records.
func TestLogs_LinesLimit(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "daemon.log")
	now := time.Now().UTC()
	body := strings.Join([]string{
		logLine(now.Add(-4*time.Minute), "INFO", "first", ""),
		logLine(now.Add(-3*time.Minute), "INFO", "second", ""),
		logLine(now.Add(-2*time.Minute), "INFO", "third", ""),
		logLine(now.Add(-1*time.Minute), "INFO", "fourth", ""),
		"",
	}, "\n")
	writeLog(t, daemon, body)
	pointLogsAt(t, daemon)

	setLogsFlags(t, false, 2, "", "", false)
	c, out, _ := captureCmd()
	if err := logsRunE(c, nil); err != nil {
		t.Fatalf("logsRunE: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "third") || !strings.Contains(got, "fourth") {
		t.Errorf("-n 2 must keep the last two records, got: %q", got)
	}
	if strings.Contains(got, "first") || strings.Contains(got, "second") {
		t.Errorf("-n 2 must drop the older records, got: %q", got)
	}
}

// TestLogs_AllMergesArchives spans the rotated archive (.1) then the current
// file, oldest-first.
func TestLogs_AllMergesArchives(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "daemon.log")
	archive := daemon + ".1"
	now := time.Now().UTC()
	writeLog(t, archive, logLine(now.Add(-1*time.Hour), "INFO", "archived record", "")+"\n")
	writeLog(t, daemon, logLine(now.Add(-1*time.Minute), "INFO", "current record", "")+"\n")
	pointLogsAt(t, daemon)

	setLogsFlags(t, false, 0, "", "", true)
	c, out, _ := captureCmd()
	if err := logsRunE(c, nil); err != nil {
		t.Fatalf("logsRunE: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "archived record") || !strings.Contains(got, "current record") {
		t.Errorf("--all must include archive + current, got: %q", got)
	}
	// Order: archive (older) before current (newer).
	if idxArch, idxCur := strings.Index(got, "archived record"), strings.Index(got, "current record"); idxArch >= idxCur {
		t.Errorf("--all must emit archive before current, got idx %d >= %d", idxArch, idxCur)
	}
}

// TestLogs_AllOmitsArchivesByDefault confirms --all is opt-in.
func TestLogs_AllOmitsArchivesByDefault(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "daemon.log")
	archive := daemon + ".1"
	writeLog(t, archive, logLine(time.Now().Add(-time.Hour).UTC(), "INFO", "archived record", "")+"\n")
	writeLog(t, daemon, logLine(time.Now().UTC(), "INFO", "current record", "")+"\n")
	pointLogsAt(t, daemon)

	setLogsFlags(t, false, 0, "", "", false)
	c, out, _ := captureCmd()
	if err := logsRunE(c, nil); err != nil {
		t.Fatalf("logsRunE: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "archived record") {
		t.Errorf("default must not read archives, got: %q", got)
	}
}

// TestParseSince covers duration, RFC3339, date-only, and invalid input.
func TestParseSince(t *testing.T) {
	now := time.Now()
	cases := []struct {
		in   string
		ok   bool
		zero bool // expect zero time (no filter)
	}{
		{"", true, true},
		{"10m", true, false},
		{"1h30m", true, false},
		{now.Add(-5 * time.Minute).Format(time.RFC3339), true, false},
		{"2026-07-26", true, false},
		{"not-a-time", false, false},
	}
	for _, tc := range cases {
		got, err := parseSince(tc.in)
		switch {
		case err != nil && tc.ok:
			t.Errorf("parseSince(%q): unexpected error %v", tc.in, err)
		case err == nil && !tc.ok:
			t.Errorf("parseSince(%q): expected error, got %v", tc.in, got)
		case tc.ok && tc.zero && !got.IsZero():
			t.Errorf("parseSince(%q): expected zero time, got %v", tc.in, got)
		}
	}
}

// TestParseSinceDurationBounds verifies the duration form bounds the window.
func TestParseSinceDurationBounds(t *testing.T) {
	cutoff, err := parseSince("10m")
	if err != nil {
		t.Fatalf("parseSince 10m: %v", err)
	}
	if d := time.Since(cutoff); d < 9*time.Minute || d > 11*time.Minute {
		t.Errorf("parseSince 10m cutoff off by >1m: %v ago", d)
	}
}

// TestParseLogTime covers a well-formed record, a line without a timestamp,
// and a malformed timestamp.
func TestParseLogTime(t *testing.T) {
	ts := "2026-07-26T21:01:00.123456789Z"
	t1, ok := parseLogTime("time=" + ts + ` level=INFO msg="x"`)
	if !ok || !t1.Equal(timeMustParse(ts)) {
		t.Errorf("parseLogTime valid: ok=%v t=%v", ok, t1)
	}
	if _, ok := parseLogTime("no timestamp here"); ok {
		t.Error("parseLogTime must fail on a line without time=")
	}
	if _, ok := parseLogTime("time=not-a-time level=INFO"); ok {
		t.Error("parseLogTime must fail on a malformed timestamp")
	}
}

func timeMustParse(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

// TestLineMatchesTuber covers the attribute match, a non-match, a missing
// attribute, a quoted value, and the empty-name pass-through.
func TestLineMatchesTuber(t *testing.T) {
	line := `time=2026-07-26T21:00:00Z level=INFO msg="connection forwarded" tuber=db-stage`
	if !lineMatchesTuber(line, "db-stage") {
		t.Error("expected match for tuber=db-stage")
	}
	if lineMatchesTuber(line, "admin-ui") {
		t.Error("must not match a different tuber")
	}
	noAttr := `time=2026-07-26T21:00:00Z level=INFO msg="daemon started"`
	if lineMatchesTuber(noAttr, "db-stage") {
		t.Error("must not match when the attr is absent")
	}
	if !lineMatchesTuber(noAttr, "") {
		t.Error("empty name must match everything (no filter)")
	}
	quoted := `time=2026-07-26T21:00:00Z level=INFO msg="x" tuber="db stage"`
	if !lineMatchesTuber(quoted, "db stage") {
		t.Error("expected match for a quoted tuber value")
	}
}

// TestLineKeptSincePassesUntimedLines pins the "never drop log content" rule:
// a line without a parseable timestamp is kept under --since.
func TestLineKeptSincePassesUntimedLines(t *testing.T) {
	since := time.Now().Add(-5 * time.Minute)
	if !lineKept("garbage line without timestamp", since, "") {
		t.Error("an untimestamped line must be kept under --since")
	}
	old := time.Now().Add(-1 * time.Hour).UTC()
	oldLine := logLine(old, "INFO", "old", "")
	if lineKept(oldLine, since, "") {
		t.Error("an old timestamped line must be dropped under --since")
	}
}

// TestResolveLogPath covers daemon-exists, standalone-fallback, and neither.
func TestResolveLogPath(t *testing.T) {
	dir := t.TempDir()
	daemon := filepath.Join(dir, "daemon.log")
	standalone := filepath.Join(dir, "portato.log")

	// Neither exists.
	pointLogsAt(t, daemon, standalone)
	if p, ok := resolveLogPath(); ok || p != daemon {
		t.Errorf("neither exists: got (%q,%v), want (%q,false)", p, ok, daemon)
	}

	// Only standalone.
	writeLog(t, standalone, "x\n")
	pointLogsAt(t, daemon, standalone)
	if p, ok := resolveLogPath(); !ok || p != standalone {
		t.Errorf("standalone fallback: got (%q,%v), want (%q,true)", p, ok, standalone)
	}

	// Daemon wins when both exist.
	writeLog(t, daemon, "x\n")
	pointLogsAt(t, daemon, standalone)
	if p, ok := resolveLogPath(); !ok || p != daemon {
		t.Errorf("daemon priority: got (%q,%v), want (%q,true)", p, ok, daemon)
	}
}

// TestLogFilesOrdering pins the archive ordering (oldest-first) under --all and
// the bare current-file path without it.
func TestLogFilesOrdering(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "daemon.log")
	writeLog(t, base+".3", "oldest\n")
	writeLog(t, base+".1", "newer archive\n")
	writeLog(t, base, "current\n")

	got := logFiles(base, true)
	want := []string{base + ".3", base + ".1", base}
	if len(got) != len(want) {
		t.Fatalf("logFiles --all: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("logFiles --all[%d]: got %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}

	if got := logFiles(base, false); len(got) != 1 || got[0] != base {
		t.Errorf("logFiles default: got %v, want [%q]", got, base)
	}
}
