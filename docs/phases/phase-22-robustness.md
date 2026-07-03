---
phase: 22
title: Robustness (socket activation, concurrent-start flock, log-rotation knobs)
status: done
depends_on: [6, 12, 13]
---

## Goal

Three production-hardening pieces: launchd/systemd **socket activation** (the
service manager owns the listening socket and hands it to the daemon),
**concurrent-start hardening** (two daemons started at once do not fight over
the IPC socket), and **config-driven log-rotation knobs** (currently
hardcoded).

## Tasks

- [x] Socket activation:
  - systemd: hand-rolled `LISTEN_FDS`/`LISTEN_PID` parsing (no `coreos/go-systemd`
    dependency); if a listener is handed in, serve on it instead of binding.
  - launchd: a `Sockets` dict in the plist → **deferred** — claiming the fd needs
    `launch_activate_socket_fd` (libc, cgo), incompatible with the pure-Go single
    binary; documented in SPEC §6/§13. macOS stays bind-on-start.
  - The daemon serves on the activated listener(s) when present, else binds its
    own (current behavior).
- [x] `internal/service`: generate the unit with a `portato.socket`
  (`ListenStream=/run/user/<uid>/portato-<uid>.sock`, `SocketMode=0600`) so
  `install` enables the socket alongside the service (which `Requires`+`After`s
  it). The launchd plist `Sockets` entry is deferred (see above).
- [x] Concurrent-start: `flock` (`golang.org/x/sys/unix.Flock`) on a dedicated
      `<confighome>/portato/daemon.lock` (not the marker, to keep the marker's
      atomic rewrite clean). The second daemon started at once detects the lock
      and exits `0` with "already running", augmenting the PID-file-based check in
      `daemon.New`/`ensureNotRunning`. Crash-safe (kernel releases the lock on
      exit). Unix-only; `!unix` stub no-ops until Phase 17's `LockFileEx`.
- [x] Log-rotation config: `defaults.log.max_size_mb`, `max_age_days`, `retain`
      — passed into `internal/log`'s rotating writer. `max_age_days` is a
      retention purge (archives older than N days dropped at rotation), not a
      rotation trigger.
- [x] Tests: `e2e/systemd-docker` extended for socket activation (green); a
      concurrent-start unit test (two `New`s → one wins via `ErrAlreadyRunning`);
      forced-rotation + age-purge unit tests.

## Definition of Done

- [x] Under systemd socket activation, `systemctl --user start portato.socket`
      → `portato list` works without the service having bound the socket.
      (Verified: fresh `docker build` + `e2e/systemd-docker check` → exit 0,
      including the socket-activation block.)
- [x] Two simultaneous `portato daemon` invocations → one wins and serves, the
      other exits `0` with a clear "already running" message. (Verified
      empirically with two real processes, both in the Linux container and on
      macOS: the loser prints "daemon already running" and exits 0; plus the
      `ErrAlreadyRunning` unit test.)
- [x] Log rotation honors `max_size_mb` / `retain` (a forced-rotation test
      produces the expected number of rotated files). (`TestSetup_HonorsLogOptions`
      + age-purge tests.)
- [x] `go vet ./...`, `gofmt -l .`, `go test ./...` clean. (darwin + `GOOS=linux`
      build green.)

## Verification

```sh
# systemd socket activation (Linux container):
systemctl --user start portato.socket
./bin/portato list                       # works, service bound the socket via LISTEN_FDS

# concurrent start:
./bin/portato daemon & ./bin/portato daemon    # second exits 0, "already running"

# rotation:
# set log.max_size_mb: 1 in config, generate >1 MB of logs, observe rotation.
```

## Technical details

- Socket activation is per-OS and the fiddliest part: launchd's plist `Sockets`
  semantics differ from systemd's `ListenStream` + `LISTEN_FDS=1`. Gate the
  activation listener detection behind a build-tagged helper.
- `flock` is unix-only; Windows (Phase 17) will need `LockFileEx` (deferred).
- Reuse the `e2e/systemd-docker` harness to verify the systemd side; the
  launchd side can reuse the macOS kill/`kickstart` checks from Phase 6.
- Out of scope: time-based rotation triggers (size is the MVP knob); syslog
  forwarding.
