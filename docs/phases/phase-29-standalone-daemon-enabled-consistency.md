---
phase: 29
title: standalone and daemon enabled-auto-start consistency
status: done
depends_on: []
---

## Goal

Resolve the standalone↔daemon asymmetry that surprised the user during the
Phase 16 E2E: the daemon auto-starts every `enabled: true` tunnel (SPEC §6),
but the standalone TUI starts none of them (only on `space`). Quitting the
standalone with a hand-off therefore brings up tunnels the user never toggled
in that session ("where did these come from?").

## Background

`runStandalone` (`internal/cmd/root.go`) builds the engine but never calls
`StartEnabled`; the daemon calls `StartEnabled` / `StartEnabledWith` (Phase 16).
So an `enabled: true` tunnel sits idle in the standalone but starts in the
daemon the moment the hand-off spawns it.

## Decision (resolved)

Options considered (decision recorded below):

- **(A)** the standalone also auto-starts `enabled: true` tunnels on launch —
  recommended, consistent with the daemon and least surprising.
- **(B)** on hand-off the daemon starts only the tunnels the standalone had
  running (plus any already up), so no surprise new tunnels appear.
- **(C)** leave the behaviour as-is and document the asymmetry explicitly.

> **Decision (2026-07-09): (A).** `runStandalone` calls `engine.StartEnabled`
> after building the local controller, so the standalone launches the same set
> of tunnels the daemon would (`StartEnabledWith`). This keeps the SPEC §6
> invariant ("the config on disk is the source of truth for which tunnels are
> up") true in both modes and removes the hand-off "surprise tunnels": the
> daemon now adopts/starts exactly what the standalone already had running.
> Enabled-but-unconnectable tunnels (no network, bad host) surface as
> Reconnecting/Error — the Engine's existing behaviour — so no extra TUI work.

## Tasks

- [x] After the decision: implement the chosen behaviour in `runStandalone`
      and/or the hand-off spawn.
- [x] Update SPEC §6 / §12 to match the chosen rule.
- [x] Tests for the chosen start set (standalone launch + state after hand-off).

## Definition of Done

- [x] A documented, consistent rule for which tunnels are running after a
      standalone launch and after a hand-off.
- [x] No "surprise tunnels" after hand-off (or, under (A), they are expected
      because they also run in the standalone).
- [x] `go vet ./...`, `gofmt -l .`, `go test ./...` clean.

## Verification

Per the chosen option — see "Decision needed".

## Technical details

- Touches `runStandalone` (`internal/cmd/root.go`) and possibly the hand-off;
  the daemon's `StartEnabled`/`StartEnabledWith` is unchanged.
