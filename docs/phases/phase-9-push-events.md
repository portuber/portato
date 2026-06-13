---
phase: 9
title: Push events instead of polling
status: todo
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

## Tasks (candidates)

- [ ] In `forward.Engine`: add `Subscribe() <-chan struct{}` (or `chan Event`); push on any `state` change of any tunnel.
- [ ] `localController.Changes()` — a wrapper over `engine.Subscribe()`, no ticks.
- [ ] On the daemon: `GET /events` — set `Content-Type: text/event-stream`, write SSE messages on subscription.
- [ ] `remoteController.Changes()` — HTTP client with streaming read of `/events`; reconnect on break.
- [ ] TUI: on receiving the `Changes()` signal — `ctrl.List()` + redraw. The Controller interface does not change.
- [ ] Optional: fallback to polling if the stream is down and does not recover for N seconds.

## Definition of Done

- [ ] On `space` (toggle a tunnel) in the TUI, the status updates instantly (< 100ms), with no visible delay.
- [ ] On auto-reconnect (sshd drop), the `Reconnecting → Connected` status appears immediately, without waiting for a tick.
- [ ] Load: when idle (no actions), the daemon does not respond to 1 request/sec from each client.
- [ ] Two concurrent TUIs (`portato attach` × 2) receive events simultaneously without desync.
- [ ] `go vet`, `gofmt` are clean.

## Technical details (preliminary)

- **SSE (Server-Sent Events):** standard `text/event-stream`, client is a `bufio.Scanner` over the response body. Simple and works through HTTP clients.
- **Chunked HTTP:** an alternative if SSE feels heavy; line-by-line writing of JSON events.
- **Backpressure:** if the client is slow, the event channel may overflow. Solution: buffer + drop-old (for the UI it is not critical to lose intermediate state).
- **Stream client reconnect:** on break — exponential backoff (milliseconds), with reconnection in the background. During the break — show `«connecting…»` in the TUI header.

## Open questions

- SSE or chunked JSON? SSE is the standard for browsers, but here the client is Go — both options are fine.
- Push full-status on event, or only a diff? MVP — full (simpler); diff — a post-MVP optimization.
