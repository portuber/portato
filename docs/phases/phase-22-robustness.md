---
phase: 22
title: Robustness (socket activation, concurrent-start flock, log-rotation knobs)
status: in-progress
depends_on: [6, 12, 13]
---

## Goal

Three production-hardening pieces: launchd/systemd **socket activation** (the
service manager owns the listening socket and hands it to the daemon),
**concurrent-start hardening** (two daemons started at once do not fight over
the IPC socket), and **config-driven log-rotation knobs** (currently
hardcoded).

## Tasks

- [ ] Socket activation:
  - systemd: `coreos/go-systemd/activation` — `Listeners()` reads
    `LISTEN_FDS`; if a listener is handed in, serve on it instead of binding.
  - launchd: a `Sockets` dict in the plist → the fd is handed via `launchd`
    activation; serve on it.
  - The daemon serves on the activated listener(s) when present, else binds its
    own (current behavior).
- [ ] `internal/service`: generate the plist with a `Sockets` entry and the
      unit with a `portato.socket` (`ListenStream`) so `install` enables the
      socket alongside the service.
- [ ] Concurrent-start: `flock` (`golang.org/x/sys/unix.Flock`) on the IPC
      discovery marker. The second daemon started at once detects the lock and
      exits `0` with "already running" — augmenting/replacing the current
      PID-file-based check in `daemon.New`/`ensureNotRunning`.
- [ ] Log-rotation config: `defaults.log.max_size_mb`, `max_age_days`,
      `retain` — passed into `internal/log`'s rotating writer (currently
      hardcoded in `internal/log/file.go`).
- [ ] Tests: extend `e2e/systemd-docker` for socket activation; a concurrent
      -start test (two daemons → one wins); a forced-rotation unit test.

## Definition of Done

- [ ] Under systemd socket activation, `systemctl --user start portato.socket`
      → `portato list` works without the service having bound the socket.
- [ ] Two simultaneous `portato daemon` invocations → one wins and serves, the
      other exits `0` with a clear "already running" message.
- [ ] Log rotation honors `max_size_mb` / `retain` (a forced-rotation test
      produces the expected number of rotated files).
- [ ] `go vet ./...`, `gofmt -l .`, `go test ./...` clean.

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
