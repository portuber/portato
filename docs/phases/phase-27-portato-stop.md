---
phase: 27
title: "portato stop — gracefully terminate the daemon"
status: done
depends_on: []
---

## Goal

A first-class `portato stop` CLI command (and `make stop`) that gracefully
terminates the running daemon. Today the daemon can only be stopped by an
external `kill`/`pkill`, which is a real UX gap surfaced as friction during the
Phase 16 E2E work.

## Background

The daemon (Phase 4) runs until SIGTERM/SIGINT (`internal/cmd/daemon.go`).
There is no `portato stop`; users must `kill <pid>` or
`pkill -f 'portato daemon'`. A `POST /reload` endpoint exists but there is no
`portato reload` either — that is tracked separately in Phase 28.

## Tasks

- [x] `internal/cmd/stop.go`: a cobra command that resolves the daemon socket
      (`daemon.ResolveSocket`, honoring `--socket`), reads the discovery marker
      for the PID, sends SIGTERM, and polls `healthz` until the socket goes
      silent (~5s). No marker / dead PID / silent socket -> "no daemon running"
      (exit 0) + stale-marker cleanup. Overridable seams for tests.
- [x] Register `stopCmd` in `Execute()`; add `portato stop` to the root help
      `Modes` list.
- [x] `Makefile`: a `stop` target (`./bin/portato stop`) + `.PHONY`.
- [x] `internal/cmd/stop_test.go`: no-daemon, stale-marker, live→stopped,
      timeout.

## Definition of Done

- [x] `portato stop` stops a running daemon (SIGTERM, waits for the socket to
      go silent); prints `no daemon running` when none is running (exit 0) and
      cleans stale markers.
- [x] `make stop` works from the repo.
- [x] `go vet ./...`, `gofmt -l .`, `go test ./...` clean; cross-compilation
      darwin/linux × amd64/arm64 green.

## Verification

```sh
make build
./bin/portato daemon --config ./config.yaml &
./bin/portato stop            # daemon stops
./bin/portato list            # "no daemon running"
```

## Technical details

- Reuse `daemon.ReadMarker` / `daemon.DiscoveryPath` / `daemon.ResolveSocket`;
  `client.New(socket).HealthzCtx` for the down-probe.
- The PID comes from the discovery marker. `--socket` targets a specific daemon.
  A socket-activated daemon (systemd) is owned by the service manager: warn and
  defer to `systemctl --user stop portato`.
- Tests mirror the `handoff.go` seam pattern (overridable `stopKill`,
  `stopProbe`, and a marker-path seam) so no real process is signalled.
