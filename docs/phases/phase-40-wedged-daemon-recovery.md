---
phase: 40
title: "Recover from / prevent the wedged daemon"
status: in-progress
depends_on: []
---

## Goal

A portato daemon on macOS can get *wedged*: alive (holding the single-instance
flock and all local listener ports) but unreachable over its IPC socket, so
`portato daemon` says "already running", `portato stop` says "no daemon
running", and the standalone TUI falls back and reports "address already in
use" on every local port the wedged daemon still holds. This phase prevents
the common cause and adds a recovery path for the rest.

## Background

The IPC socket lives under a runtime/temp dir so the path can differ per
shell/session, with a stable discovery marker pointing at the live one
(SPEC §6). On macOS that dir is `$TMPDIR` (`internal/daemon/paths_unix.go`),
which macOS reaps periodically and rotates across sessions. When the socket
file is unlinked under a running daemon, the kernel keeps the listener fd open
on the orphaned inode: the daemon still owns its local ports and the flock,
but no client can `connect()` to it. SPEC §6 describes this exact state as a
"wedged daemon" but never gave it a recovery path — and `portato stop` made it
worse by deleting the discovery marker (the only record of the live PID) the
moment its healthz probe went silent (`internal/cmd/stop.go`).

Linux is safe as long as `$XDG_RUNTIME_DIR` is set (the per-user tmpfs logind
manages is not reaped); it degrades to the same `$TMPDIR` class only when that
var is unset (bare SSH without `pam_systemd`, containers, WSL, some CI).
Windows uses a named pipe (no socket file to reap) so it is unaffected.

## Tasks

- [x] Phase file + ROADMAP/summary/current-work updated; status `[~]`.
- [x] Prevention: on darwin, place the IPC socket in a stable, owner-only dir
      (`xdg.StateHome/portato/`, co-located with the daemon log) instead of the
      reaped `$TMPDIR`. `internal/daemon/paths_unix.go::RuntimeSocketPath` +
      a testable seam (xdg.StateHome is cached at package init, so the seam
      pattern mirrors `discoveryPathFn`/`lockPathFn`). `transport_unix.Listen`
      already `MkdirAll`s the parent 0700 and chmods the socket 0600.
- [x] Recovery (`stop`): when no socket answers but the marker points at a live
      portato PID, SIGTERM that PID and poll `pidAlive` (not healthz) until it
      exits, instead of deleting the marker and printing "no daemon running".
      Guard against PID reuse by confirming the process is portato. New seams
      for tests.
- [x] Diagnostics (`doctor`): when the socket is unreachable but the marker has
      a live portato PID, fail with a "wedged" line that names the PID and
      suggests `portato stop`, instead of the benign "not running" info line.
- [x] Export `daemon.PidAlive` (thin wrapper) so `cmd` reuses the per-OS
      pidAlive logic instead of re-implementing it.
- [x] Tests: stop wedged-recovery (alive PID, silent socket → SIGTERM by PID,
      poll pidAlive) + wedged-but-wrong-process (no signal); doctor wedged
      diagnostic (+ idle-when-PID-dead); darwin `RuntimeSocketPath` sits under
      the stable dir (via the seam); `withIsolatedDiscovery` updated to redirect
      the new darwin seam.
- [x] SPEC §6: rewrite the macOS socket bullet to the stable location + the
      reaping rationale.

## Definition of Done

- [x] On darwin, `RuntimeSocketPath()` returns a path under
      `xdg.StateHome/portato/` (not `os.TempDir()`); the parent is created
      0700 and the socket file is 0600 (unchanged). Linux/Windows paths are
      unchanged.
- [x] A wedged daemon (live PID in the marker, silent socket) is stopped by
      `portato stop` (SIGTERM by PID, waits for the process to exit) and is
      reported by `portato doctor` with its PID; `stop` does **not** delete the
      marker before signalling, and does **not** signal a PID whose process is
      not portato.
- [x] `go build ./...`, `gofmt -l .`, `go vet ./...`, `golangci-lint run ./...`
      are clean; the phase's tests are green.
- [x] SPEC §6 describes the macOS socket location accurately; phase-12 doc
      points here without rewriting history.

## Verification

```sh
make fmt && make vet && make test && make lint

# manual wedged-recovery repro (darwin):
./bin/portato daemon &
sleep 1
pid=$(pgrep -f 'portato daemon')
mark=$(cat "${XDG_CONFIG_HOME:-$HOME/.config}/portato/daemon.socket")
# unlink the socket file under the running daemon, simulating $TMPDIR reaping:
rm -f "$(dirname "$(pgrep -fl 'portato daemon' | head -1)")"/portato-*.sock
# better: just kill -STOP is not it; instead simulate by removing the socket path:
# (the marker records the socket path)
socket=$(python3 -c "import json,sys;print(json.load(open('$mark'))['socket'])")
rm -f "$socket"
./bin/portato doctor    # -> "✗ daemon  wedged: pid <pid> alive ..."
./bin/portato stop      # -> recovers by PID, prints "daemon stopped (pid N; socket was unreachable)"
./bin/portato list      # -> "no daemon running"
```

## Technical details

- `internal/daemon/paths_unix.go`: darwin branch of `RuntimeSocketPath` returns
  `filepath.Join(runtimeSocketDir(), portato-<uid>.sock)` where
  `runtimeSocketDir` is a `var func() string` defaulting to
  `filepath.Join(xdg.StateHome, "portato")`. Non-darwin unchanged.
- `internal/daemon/pidalive.go` (new, no build tag): `func PidAlive(pid int)
  bool { return pidAlive(pid) }` — thin export; per-OS `pidAlive` stays.
- `internal/cmd/stop.go`: split `stopRunE` into the live-socket path (unchanged
  behaviour) and a wedged-recovery path keyed on `PidAlive(m.PID)` +
  `processIsPortato(m.PID)`. New seams: `stopPidAlive`, `stopProcessIsPortato`.
- `processIsPortato(pid)`: `ps -p <pid> -o comm=` contains "portato" (best
  effort; mirrors the existing `exec.Command` use in doctor).
- `internal/cmd/doctor.go::checkDaemon`: after the unreachable-socket branch,
  read the marker; live portato PID → `d.fail("daemon", "wedged: pid %d alive,
  socket unreachable — run `portato stop`", pid)`.
