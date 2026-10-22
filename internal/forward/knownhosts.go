package forward

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// maxKnownHostsLineLen is the longest line knownhosts.New can read before its
// bufio.Scanner gives up with "token too long" — an error without a line
// number, which the recovery loop below cannot attribute to a line. Lines
// past it are skipped up front; real known_hosts entries never come close.
const maxKnownHostsLineLen = bufio.MaxScanTokenSize - 2

// warnedKnownHostsLines deduplicates malformed-line warnings per process: the
// loader runs on every dial, so a restarting tuber would otherwise repeat the
// same complaint on each attempt.
var warnedKnownHostsLines sync.Map

// loadKnownHosts builds the host-key callback from the known_hosts file the
// same way knownhosts.New does, except that malformed lines are skipped with
// a warning instead of failing the whole load. x/crypto's New is
// all-or-nothing: one bad line (a typo'd key type such as "ssh-ed25518", a
// truncated entry) breaks every tunnel with "load known_hosts", while OpenSSH
// itself ignores such lines and keeps going. Skipping works by re-reading the
// file into a temp copy with the offending lines commented out — line numbers
// stay identical to the original — until New accepts it, so all matching
// semantics (patterns, hashed hosts, markers) remain x/crypto's. Unreadable
// files and unexpected failures stay fatal, as before.
func loadKnownHosts(hosts string, log *slog.Logger) (ssh.HostKeyCallback, error) {
	base, err := knownhosts.New(hosts)
	if err == nil {
		return base, nil
	}
	fatal := fmt.Errorf("load known_hosts %s: %w", hosts, err)

	data, err := os.ReadFile(hosts)
	if err != nil {
		return nil, fatal
	}
	lines := strings.Split(string(data), "\n")

	tmp, err := os.CreateTemp("", "portato-knownhosts-*")
	if err != nil {
		return nil, fatal
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)

	skip := make([]bool, len(lines))
	for i, line := range lines {
		if len(line) > maxKnownHostsLineLen {
			skip[i] = true
			warnMalformedKnownHostsLine(log, hosts, i+1, errors.New("line too long"))
		}
	}

	// Each round comments out one more malformed line, so at most
	// len(lines)+1 rounds are needed; running out means something is off
	// and the original error is returned unchanged.
	for range len(lines) + 1 {
		if err := writeSanitizedKnownHosts(path, lines, skip); err != nil {
			return nil, fatal
		}
		base, err := knownhosts.New(path)
		if err == nil {
			return base, nil
		}
		num, reason := parseKnownHostsLineError(err, path)
		if num < 1 || num > len(lines) || skip[num-1] {
			return nil, fatal
		}
		skip[num-1] = true
		warnMalformedKnownHostsLine(log, hosts, num, reason)
	}
	return nil, fatal
}

// writeSanitizedKnownHosts writes lines to path with the flagged ones emptied
// out: knownhosts.New skips blank lines, while the line count — and therefore
// the numbering reported back in errors — stays identical to the original.
// (A skipped line cannot be "# "-commented instead: a line past the scanner
// limit trips bufio.Scanner before the comment is even seen.)
func writeSanitizedKnownHosts(path string, lines []string, skip []bool) error {
	var buf bytes.Buffer
	for i, line := range lines {
		if !skip[i] {
			buf.WriteString(line)
		}
		buf.WriteByte('\n')
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

// parseKnownHostsLineError extracts the line number and the inner error from
// a per-line knownhosts failure, whose shape is
// "knownhosts: <file>:<line>: <err>". Any other error shape yields 0,
// telling the caller the failure is not a single-line problem it can comment
// out.
func parseKnownHostsLineError(err error, path string) (int, error) {
	prefix := "knownhosts: " + path + ":"
	msg := err.Error()
	if !strings.HasPrefix(msg, prefix) {
		return 0, err
	}
	rest := msg[len(prefix):]
	sep := strings.IndexByte(rest, ':')
	if sep < 0 {
		return 0, err
	}
	num, convErr := strconv.Atoi(rest[:sep])
	if convErr != nil || num < 1 {
		return 0, err
	}
	return num, errors.New(strings.TrimSpace(rest[sep+1:]))
}

// warnMalformedKnownHostsLine logs one warning per file line per process.
func warnMalformedKnownHostsLine(log *slog.Logger, hosts string, line int, reason error) {
	key := hosts + ":" + strconv.Itoa(line)
	if _, dup := warnedKnownHostsLines.LoadOrStore(key, struct{}{}); dup {
		return
	}
	log.Warn("known_hosts: skipping malformed line", "file", hosts, "line", line, "err", reason)
}
