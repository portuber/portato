---
phase: 9
title: Push events instead of polling
status: done
depends_on: [6]
---

> Outline phase. To be detailed when approached.

## Goal

Replace 1s polling (in `Controller.Changes()`) with push events: the daemon pushes tunnel state
changes to connected clients in real time. The TUI updates instantly, without delay and without
extra load on the daemon.

## Scope (preliminary)

- An endpoint `GET /events` on the daemon — chunked HTTP or SSE (`text/event-stream`).
- `forward.Engine` gets an event subscription: every time `Tunnel.state` changes — push to the channel of all subscribers.
- `remoteController.Changes()` — reads the stream from the daemon, forwards it into its own channel.
- `localController.Changes()` — subscribes directly to the `Engine` (no HTTP — instant).
- The old 1s polling is removed (or kept as a fallback on stream break).

## Tasks

- [x] In `forward.Engine`: `Subscribe() (<-chan struct{}, func())` broker; every tunnel state change fans out via an `onChange` callback wired by the factory (drop-old).
- [x] `localController.Changes()` — forwards `engine.Subscribe()` through an owned, drop-old channel; no ticks.
- [x] On the daemon: `GET /events` — `text/event-stream`, signal-only `data: {}` frames, initial frame on connect, 15s heartbeat.
- [x] `remoteController.Changes()` — streaming read of `/events` via `bufio.Scanner`; exponential backoff reconnect on break.
- [x] TUI: unchanged — on the `Changes()` signal it does `ctrl.List()` + redraw. The Controller interface does not change.
- [~] Optional: fallback to polling if the stream is down — **skipped by decision**; reconnect-with-backoff covers breaks and keeps the "no idle load" DoD clean.

## Definition of Done

- [x] On `space` (toggle a tunnel) in the TUI, the status updates instantly (< 100ms), with no visible delay. — state transitions fire synchronously through the broker; no tick delay.
- [x] On auto-reconnect (sshd drop), the `Reconnecting → Connected` status appears immediately. — `setState` in the reconnect loop emits on every transition.
- [x] Load: when idle (no actions), the daemon does not respond to 1 request/sec from each client. — polling removed; one long-lived SSE connection, server-side heartbeat only.
- [x] Two concurrent TUIs (`portato attach` × 2) receive events simultaneously without desync. — covered by `TestServer_EventsStreamMultipleClients`.
- [x] `go vet`, `gofmt` are clean. — verified (`gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green).

## Technical details

- **SSE (Server-Sent Events):** standard `text/event-stream`, client is a `bufio.Scanner` over the response body. **Chosen over chunked JSON.**
- **Backpressure:** if the client is slow, the event channel may overflow. Solution: buffer (16 in the broker, 1 in the controllers) + non-blocking drop-old send.
- **Stream client reconnect:** on break — exponential backoff (100ms → 5s max), reconnection in the background. During a break the TUI simply does not redraw until the stream resumes.
- **Wire format:** signal-only — `data: {}` per event; the client re-fetches `GET /tunnels` to redraw. Keeps the `Controller` interface (`Changes() <-chan struct{}`) unchanged.

## Open questions (resolved)

- SSE or chunked JSON? → **SSE** (`text/event-stream`).
- Push full-status on event, or only a diff? → **signal-only** (no payload); the client re-fetches via `List()`. Diff/full-status is a future optimization.

## Note

The `fix(daemon)` patch landed during this phase (a fixed macOS
`~/Library/Application Support/portato/` socket path, replacing the
session-variable `XDG_RUNTIME_DIR` lookup) was a short-term workaround.
**Phase 12 (Robust IPC socket discovery) supersedes it**: the socket now lives
in a runtime/temp dir and is advertised via a stable discovery marker, so the
fixed-path workaround and the build-tagged `paths_darwin.go` / `paths_unix.go`
files have been removed.
