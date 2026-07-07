---
phase: 29
title: standalone and daemon enabled-auto-start consistency
status: todo
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

## Decision needed (open)

Pick one before implementing:

- **(A)** the standalone also auto-starts `enabled: true` tunnels on launch —
  recommended, consistent with the daemon and least surprising.
- **(B)** on hand-off the daemon starts only the tunnels the standalone had
  running (plus any already up), so no surprise new tunnels appear.
- **(C)** leave the behaviour as-is and document the asymmetry explicitly.

## Tasks

- [ ] After the decision: implement the chosen behaviour in `runStandalone`
      and/or the hand-off spawn.
- [ ] Update SPEC §6 / §12 to match the chosen rule.
- [ ] Tests for the chosen start set (standalone launch + state after hand-off).

## Definition of Done

- [ ] A documented, consistent rule for which tunnels are running after a
      standalone launch and after a hand-off.
- [ ] No "surprise tunnels" after hand-off (or, under (A), they are expected
      because they also run in the standalone).
- [ ] `go vet ./...`, `gofmt -l .`, `go test ./...` clean.

## Verification

Per the chosen option — see "Decision needed".

## Technical details

- Touches `runStandalone` (`internal/cmd/root.go`) and possibly the hand-off;
  the daemon's `StartEnabled`/`StartEnabledWith` is unchanged.
