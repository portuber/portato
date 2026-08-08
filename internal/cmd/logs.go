package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	routelog "github.com/portuber/portato/internal/log"
)

var (
	logsFollow bool
	logsLines  int
	logsSince  string
	logsTuber  string
	logsAll    bool
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Print the daemon log (tail/follow, like `docker logs`)",
	Long: `Print the persisted portato daemon log (daemon.log), falling back to the
standalone portato.log when no daemon log exists. Reads the file directly, so
no running daemon is required — useful for debugging "what happened".

  portato logs              # dump the whole current log
  portato logs -f           # follow live (survives rotation)
  portato logs -n 50        # last 50 records
  portato logs --since 10m  # records from the last 10 minutes
  portato logs --tuber db-stage
  portato logs --all        # include rotated archives (.1/.2/.3)

Filters compose: --since and --tuber narrow the output; -n takes the last N
of what remains; --all spans the archives oldest-first before the current
file. Output is the raw slog text (grep-friendly).`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          logsRunE,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "tail new log lines live (survives rotation)")
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 0, "print only the last N records (0 = all)")
	logsCmd.Flags().StringVar(&logsSince, "since", "", "only records newer than this (e.g. 10m, 1h30m, or an RFC3339 timestamp)")
	logsCmd.Flags().StringVar(&logsTuber, "tuber", "", "only records for this tuber (matches the tuber=<name> attribute)")
	logsCmd.Flags().BoolVar(&logsAll, "all", false, "include rotated archives (.1/.2/.3), oldest-first")
	_ = logsCmd.RegisterFlagCompletionFunc("tuber", tuberNameCompletion)
}

// logArchiveSuffixes is the RotatingWriter's archive chain, oldest-first: .3
// is the oldest archive, .1 the newest, and the bare path is the current file.
var logArchiveSuffixes = []string{".3", ".2", ".1"}

// logsRunE resolves the log file, prints the filtered selection, and (when
// --follow is set) tails new lines until interrupted.
func logsRunE(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	since, err := parseSince(logsSince)
	if err != nil {
		return err
	}

	base, exists := resolveLogPath()
	if !exists {
		fmt.Fprintf(out, "no log file yet (%s); run `portato` or `portato daemon` to create it.\n", base)
		return nil
	}

	files := logFiles(base, logsAll)
	lines, err := readFilteredFiles(files, since, logsTuber)
	if err != nil {
		return err
	}
	if logsLines > 0 && len(lines) > logsLines {
		lines = lines[len(lines)-logsLines:]
	}
	for _, l := range lines {
		fmt.Fprintln(out, l)
	}

	if !logsFollow {
		return nil
	}
	return followLog(out, base, since, logsTuber)
}

// logsPaths returns the candidate log paths in priority order (daemon first,
// then standalone). It is a variable so tests can redirect it to a temp dir
// without depending on the real XDG state home (which the xdg lib caches at
// package init).
var logsPaths = func() []string {
	return []string{routelog.DaemonPath(), routelog.DefaultPath()}
}

// resolveLogPath picks the file to read: the first existing candidate (daemon
// log, then standalone). When none exists it returns the primary path (for the
// "no log file yet" message) with exists=false.
func resolveLogPath() (string, bool) {
	paths := logsPaths()
	for _, p := range paths {
		if fileExists(p) {
			return p, true
		}
	}
	if len(paths) > 0 {
		return paths[0], false
	}
	return routelog.DaemonPath(), false
}

// logFiles returns the files to read in chronological order (oldest-first):
// with includeArchives, the existing archives (.3/.2/.1) then the current file;
// without it, just the current file.
func logFiles(base string, includeArchives bool) []string {
	if !includeArchives {
		return []string{base}
	}
	out := make([]string, 0, len(logArchiveSuffixes)+1)
	for _, suf := range logArchiveSuffixes {
		if fileExists(base + suf) {
			out = append(out, base+suf)
		}
	}
	out = append(out, base)
	return out
}

// readFilteredFiles reads each file in order, returning the records that pass
// the --since / --tuber filters.
func readFilteredFiles(files []string, since time.Time, tuber string) ([]string, error) {
	var out []string
	for _, p := range files {
		f, err := os.Open(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("open %s: %w", p, err)
		}
		s := bufio.NewScanner(f)
		// Allow long slog records (a verbose debug line with many attrs).
		s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for s.Scan() {
			line := s.Text()
			if lineKept(line, since, tuber) {
				out = append(out, line)
			}
		}
		_ = f.Close()
		if err := s.Err(); err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
	}
	return out, nil
}

// lineKept reports whether a record survives the --since / --tuber filters. A
// record without a parseable timestamp is kept under --since (never drop log
// content); --tuber is a selector, so unattributed records are excluded.
func lineKept(line string, since time.Time, tuber string) bool {
	if !lineMatchesTuber(line, tuber) {
		return false
	}
	if !since.IsZero() {
		if t, ok := parseLogTime(line); ok && t.Before(since) {
			return false
		}
	}
	return true
}

// parseLogTime extracts the leading time=<RFC3339Nano> value from a slog text
// record. ok=false when the line lacks a parseable timestamp; callers keep
// such lines rather than dropping them.
func parseLogTime(line string) (time.Time, bool) {
	const prefix = "time="
	if !strings.HasPrefix(line, prefix) {
		return time.Time{}, false
	}
	rest := line[len(prefix):]
	end := strings.IndexByte(rest, ' ')
	if end < 0 {
		end = len(rest)
	}
	t, err := time.Parse(time.RFC3339Nano, rest[:end])
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// parseSince accepts a Go duration ("10m", "1h30m") or an RFC3339(Nano)
// timestamp, returning the earliest time a record may have to be shown. An
// empty string disables the filter.
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("--since: expected a duration (e.g. 10m) or timestamp (e.g. 2026-07-26T13:00:00Z), got %q", s)
}

// lineMatchesTuber reports whether the record carries tuber=<name>. It locates
// the tuber= attribute at a token boundary (so "tuber=" inside a quoted msg
// value does not false-match) and parses its value, honouring slog's
// double-quote escaping (a value with spaces is wrapped in quotes).
func lineMatchesTuber(line, name string) bool {
	if name == "" {
		return true
	}
	const needle = "tuber="
	for start := 0; start < len(line); {
		idx := strings.Index(line[start:], needle)
		if idx < 0 {
			return false
		}
		idx += start
		start = idx + len(needle)
		// Boundary: the attribute must be preceded by start-of-line or a space,
		// not buried inside a quoted msg= value.
		if idx != 0 && line[idx-1] != ' ' {
			continue
		}
		return matchTuberValue(line[idx+len(needle):], name)
	}
	return false
}

// matchTuberValue reports whether the value at the start of rest equals name,
// handling slog's unquoted ("db-stage") and quoted ("db stage") forms.
func matchTuberValue(rest, name string) bool {
	if strings.HasPrefix(rest, `"`) {
		end := strings.IndexByte(rest[1:], '"')
		if end < 0 {
			return false // malformed: no closing quote
		}
		return rest[1:1+end] == name
	}
	if sp := strings.IndexByte(rest, ' '); sp >= 0 {
		rest = rest[:sp] // unquoted value ends at the next space
	}
	return rest == name
}

// followLog tails base for new lines, printing those that pass the filters,
// until interrupted (SIGINT). It survives a size-rotated log: when the path's
// size drops below the last seen offset (the RotatingWriter renamed the file
// aside and started fresh), it re-opens from the start of the new file.
func followLog(w io.Writer, base string, since time.Time, tuber string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	f, err := os.Open(base)
	if err != nil {
		return fmt.Errorf("open %s: %w", base, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	br := bufio.NewReader(f)
	lastSize := fileSize(f)

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			cur, ok := pathSize(base)
			if !ok {
				continue // vanished between rotations; retry next tick
			}
			if cur < lastSize {
				// Rotated: reopen from the start of the fresh file.
				if nf, err := os.Open(base); err == nil {
					_ = f.Close()
					f = nf
					br = bufio.NewReader(f)
					lastSize = 0
				} else {
					continue
				}
			} else {
				lastSize = cur
			}
			if err := drainNewLines(br, w, since, tuber); err != nil {
				return err
			}
		}
	}
}

// drainNewLines reads whatever is currently available on br and prints the
// records that pass the filters. It returns at EOF (nothing more right now).
func drainNewLines(br *bufio.Reader, w io.Writer, since time.Time, tuber string) error {
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			clean := strings.TrimRight(line, "\n")
			if lineKept(clean, since, tuber) {
				fmt.Fprintln(w, clean)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func fileSize(f *os.File) int64 {
	if st, err := f.Stat(); err == nil {
		return st.Size()
	}
	return 0
}

// pathSize returns the current size of the path; ok=false if it is gone.
func pathSize(p string) (int64, bool) {
	if st, err := os.Stat(p); err == nil {
		return st.Size(), true
	}
	return 0, false
}
