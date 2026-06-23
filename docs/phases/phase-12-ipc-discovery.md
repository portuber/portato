---
phase: 12
title: Robust IPC socket discovery (discovery-file)
status: done
depends_on: [4]
---

> Post-MVP polish. Replaces the phase-9 `fix(daemon)` short-term patch (fixed
> Application Support socket path) with the proper design.

## Goal

The daemon advertises its actual socket path via a stable, env-independent
discovery file; clients read it instead of guessing. The socket itself moves to
a semantically correct runtime location (`$TMPDIR` on macOS, `$XDG_RUNTIME_DIR`
on Linux). Daemon and every client always agree regardless of which
shell/session launched them.

## Background

On macOS `adrg/xdg` derives `xdg.RuntimeDir` from `XDG_RUNTIME_DIR`, which the
OS does not set and which varies across terminal/tmux sessions, so the daemon
and a client launched from another shell resolved different socket paths.
Phase 9's `fix(daemon)` patched it with a fixed
`~/Library/Application Support/portato/` path — correct-enough but
unconventional (a socket in a "data" directory). Phase 12 replaces that patch
with the proper architecture: a runtime socket + a stable pointer to it.

## Tasks

- [x] `internal/daemon/discovery.go`: `DiscoveryPath()` (stable marker under
      `xdg.ConfigHome/portato/daemon.socket`), atomic `Write(socketPath, pid)`
      (tmp + rename), `Read() (socket, pid, err)`, `Remove()`.
- [x] Socket location → runtime dir: mac `$TMPDIR`, linux `$XDG_RUNTIME_DIR`,
      fallback `os.TempDir()/portato-<uid>.sock`. Replaces the build-tagged
      `paths_darwin.go` / `paths_unix.go` fixed path introduced in phase 9's
      `fix(daemon)`.
- [x] Daemon `Start`: bind socket → `discovery.Write`; `Shutdown` / cleanup →
      `discovery.Remove` + remove the socket file.
- [x] Clients (`dialDaemon`, `attach`, the smart-launcher`): `discovery.Read` →
      connect to the advertised path; keep the PID-liveness check for stale
      markers. Remove direct `daemon.SocketPath()` calls from clients.
- [x] `ensureNotRunning` reads the PID from the discovery marker.
- [x] Optional: `--socket` / `PORTATO_SOCKET` override (for tests / CI).
- [x] SPEC §6: rewrite the socket-path section to the discovery model.
- [x] Note in the phase-9 file that phase 12 supersedes its `fix(daemon)` patch.

## Definition of Done

- [x] From any shell/session (different `XDG_RUNTIME_DIR`, different `TMPDIR`),
      `portato list` / `attach` find a running daemon with no `unset`.
- [x] `lsof` / the marker content confirms the socket lives under `$TMPDIR`
      (macOS) / `$XDG_RUNTIME_DIR` (Linux).
- [x] A stale marker (daemon `kill -9`) is detected on the first client request
      → friendly "not running", no hang.
- [x] Graceful daemon shutdown removes both the socket and the marker.
- [x] `go build ./...`, `gofmt -l .`, `go vet ./...`, `go test ./...` are clean.
- [x] SPEC §6 is updated to the discovery model.

## Verification

1. Start the daemon in shell A (`XDG_RUNTIME_DIR=X`); run `portato list` in
   shell B (`XDG_RUNTIME_DIR=Y`) → succeeds; the marker shows the `$TMPDIR`
   socket path.
2. `kill -9 <pid>` → the next `portato list` reports "not running".
3. `Ctrl+C` the daemon → both the marker and the socket are removed.

## Technical details

- Marker format: small JSON `{"socket":"...","pid":1234}`, written atomically
  (tmp + rename).
- Marker location: `xdg.ConfigHome/portato/daemon.socket` (stable; this is the
  pointer, not the socket itself). Mode `0600`.
- Socket: under `os.TempDir()`-ish, mode `0600` (already); named to avoid
  collisions.
- Concurrent start: PID-liveness via the marker (+ optional flock on the marker
  file).
- Out of scope: launchd/systemd socket activation (a possible future phase-6
  enhancement); multiple daemons / contexts (the marker could become a
  directory later).

## Open questions

- Marker format: JSON vs plain two lines? (lean JSON for extensibility.)
- Include the `--socket` / `PORTATO_SOCKET` override now or later? (lean: now,
  trivial.)
