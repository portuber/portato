---
phase: 42
title: "portato logs"
status: in-progress
depends_on: [13]
---

## Goal

A `portato logs` CLI command to read the daemon's persisted log from the
shell — the missing piece called out in the docs-gap review. Today the
TUI's `l` view shows the in-memory ring buffer (live, short window,
attach-only), and `portato doctor` reports the log path + rotation but
not the content. Users debugging "what happened" have no CLI equivalent
of `docker logs` / `journalctl` and must locate and `tail` the file by
hand.

## Background

Phase 13 writes two size-rotated slog text files under
`xdg.StateHome/portato/`:

- `portato.log` — the standalone TUI.
- `daemon.log` — `portato daemon` (the typical background target).

`RotatingWriter` keeps the current file plus N archives
(`daemon.log`, `daemon.log.1`, `.2`, `.3`). Each record is one line:
`time=… level=INFO msg="…" tuber=db-stage …` — already human-readable and
grep-friendly.

The daemon also serves its in-memory ring buffer over `GET /logs`
(`internal/daemon/server.go`), which the attach-mode TUI polls for the
`l` screen. That ring is a short recent window and only exists while the
daemon runs, so it is not suited to a `logs` CLI. Phase 42 therefore
reads the **persisted file** — works without a running daemon, gives the
full history + archives, and supports `--follow` / `--since`.

## Tasks

- [x] Phase file + ROADMAP row/summary/current-work updated; status `[~]`.
- [x] `internal/cmd/logs.go`: `logsCmd` (`portato logs`) + `runLogs`.
      Resolves the file via `log.DaemonPath()`, falling back to
      `log.Path()` when the daemon log is absent. Prints filtered slog
      text lines to stdout.
- [x] Flags: `-f, --follow` (tail live, re-open on rotation);
      `-n, --lines N` (last N records; default: the whole current file);
      `--since <dur|time>` (records newer than; parse the `time=` attr);
      `--tuber <name>` (records whose `tuber=` attr matches, with a
      substring fallback); `--all` (merge archives `.1`/`.2`/`.3`
      oldest-first, then the current file).
- [x] Register `logsCmd` on the root command (`internal/cmd/root.go`),
      parallel to `list`/`stop`/`reload`.
- [x] `internal/cmd/logs_test.go`: a temp log file + archives with sample
      records (various times / levels / tubers); cover the default dump,
      `--since`, `-n`, `--tuber`, `--all`. (`--follow` is timing-based —
      manual / a light test.)
- [x] `docs/SPEC.md` + `README.md`: a command-table row for
      `portato logs` + a short logging note (path, rotation, the
      command); cross-link from Troubleshooting.

## Definition of Done

- [ ] `portato logs` prints the daemon log (fallback standalone) from the
      persisted file, with no running daemon required.
- [ ] `-f/--follow` tails new lines and survives a rotation (re-opens
      when the file shrinks or the inode changes).
- [ ] `-n/--lines`, `--since`, `--tuber`, `--all` all behave as specified.
- [ ] A nonexistent log file prints a clear message (path + "run portato
      / portato daemon to create it") and exits 0 (not an error).
- [ ] `go build ./...`, `gofmt -l .`, `go vet ./...`, `golangci-lint run
      ./...` are clean; new functions sit under gocyclo 15; the phase's
      tests are green.
- [ ] README command-table + SPEC note updated.

## Verification

```sh
make fmt && make vet && make test && make lint

# manual:
#   portato daemon &        # produce some log lines
#   portato logs            # dump
#   portato logs -f         # follow live
#   portato logs -n 50      # last 50
#   portato logs --since 10m
#   portato logs --tuber db-stage
#   portato logs --all      # include rotated archives
```

## Technical details (sketch)

- `internal/log` already exposes `DaemonPath()` / `Path()` — reuse, do
  not duplicate. Archives share the base name with `.1` / `.2` / `.3`
  suffixes (the `RotatingWriter` convention).
- Record parsing: each line is one slog text record; split off the
  leading `time=<RFC3339>` token for `--since`, and match `tuber=<name>`
  (or fall back to a substring) for `--tuber`. A line that fails to
  parse is passed through unchanged — never drop log content.
- Follow: after the initial dump, seek to end and poll (a ~250ms
  ticker); if the file size drops below the last offset or the inode
  changes (rotated), re-open from the start of the new file. Honour
  SIGINT to exit cleanly.
- `-n` / `--all` interaction: with `--all`, "last N" is taken across the
  merged oldest→newest stream.
