---
phase: 26
title: Fix — a renamed tunnel must keep running under its new name
status: in-progress
depends_on: [10]
---

> Bugfix phase. Editing only the `Name` of a **running** tunnel in the TUI
> editor stops it and silently leaves it `Off` under the new name, instead of
> restarting it — contradicting the Phase 10 spec. This phase makes `Reload`
> honor `enabled: true` for tunnels that just appeared (rename or add), matching
> what already happens at daemon boot.

## Goal

After renaming a live tunnel (`e` → edit `Name` → `Ctrl+S`), the forward must
come back up under the new name, not die. The user should not have to press
`space` again. The same rule closes the analogous gap for any newly-added
tunnel whose config carries `enabled: true` (consistent with `StartEnabled` at
boot).

## Background — why it happens today

The TUI editor preserves the source tunnel's `Enabled` flag
(`internal/tui/editor.go:73`, `:241`) and commits a rename via
`Controller.UpdateTunnel(originalName, t)` → `PUT /tunnels/{name}`
(`internal/client/client.go:189-191`). The daemon handler always reloads
unconditionally (`internal/daemon/server.go:694-720`, reload at `:715`) via
`applyReload` → `Engine.Reload` (`server.go:643-651`).

`Engine.Reload` keys everything by tunnel **Name**
(`internal/forward/engine.go:202-242`):

- Loop 1 (`:216-221`): the old name is absent from the new set → `tn.Stop()`
  (`:218`) and `delete`. The running tunnel is torn down.
- Loop 2 (`:223-237`): the new name is absent from the old configs → the
  `!existed` branch builds a fresh tunnel via the factory (`:228`) and
  `continue`s. The factory only constructs (`engine.go:105-109`); it never
  starts. So the renamed tunnel is created in the **Off** state.

`Reload` never calls `StartEnabled`/`Enable`; that only happens at daemon boot
(`server.go:235`). So a previously-running, `enabled: true` tunnel ends up Off
after a rename. The `tunnelChanged`/`Reconfigure` path (`engine.go:231-236`,
`:263-277`) — which restarts a running tunnel only when connection-affecting
fields change — is dead code for the rename case: a rename never reaches it
(the old and new name never share a map key).

`docs/phases/phase-10-tui-editor.md:173-176` states the intended behavior
explicitly:

> **Rename = restart.** Renaming a live tunnel stops it and starts it under
> the new name (`engine.Reload` sees the old name gone, a new one present).

The current code honors the "stops it" half and drops the "starts it under the
new name" half. This phase closes that gap.

Note on `Enabled` being a reliable signal: toggling a tunnel on via `space`
calls `handleEnable`, which does `setEnabled(name, true)` **and**
`s.cfg.Save(cfgPath)` (`server.go:430-450`). So a running tunnel always has
`enabled: true` persisted, and the editor reads it back through `GET /config`.
The flag is therefore a correct "should be up" hint at reload time.

## Design decision (locked at phase start)

**Start newly-appeared tunnels whose config has `Enabled == true`, inside
`Engine.Reload`.** This is the single, minimal change; it mirrors daemon boot's
`StartEnabled` and fixes both the rename path and the analogous add path
uniformly. Existing (same-name) tunnels keep the current
`tunnelChanged`/`Reconfigure` semantics — `Enabled` stays excluded from
` tunnelChanged` (toggling it must not restart an existing tunnel).

| Aspect | Decision |
|---|---|
| Where | `internal/forward/engine.go`, `Reload`, Loop 2 `!existed` branch. |
| Rule | After `e.factory(...)`, if `t.Enabled` → `tn.Start(e.ctx)`. |
| Locking | Inline, same as the existing `Stop()` (Loop 1) and `Reconfigure()` calls already made under `e.mu` in `Reload`. `Tunnel.Start` is non-blocking for the SSH dial (it spawns a goroutine; only a local `net.Listen` happens inline — `tunnel.go:102-148`), so it does not lengthen the critical section meaningfully. |
| Off tunnels | `Enabled == false` → not started (unchanged; rename of an off tunnel stays off). |
| Newly added tunnels | An added tunnel with `enabled: true` now starts on reload — consistent with boot. Previously it stayed Off (latent gap). No existing test asserts otherwise. |
| Scope of change | One engine method. No controller/daemon/client/TUI/config changes. |

Rejected alternative — detect renames explicitly (map old name → new name) to
preserve the *exact* run state: rejected because `Reload` cannot distinguish a
rename from an unrelated delete+add (Name is the only identity key), so any
heuristic would be brittle. Keying off `Enabled` is the intended, documented
contract.

## Tasks

- [x] `internal/forward/engine.go` — in `Reload`, change the `!existed` branch
      from "build and continue" to "build; if `t.Enabled`, `tn.Start(e.ctx)`;
      continue". Update the `Reload` doc comment to note that newly-appeared
      `enabled` tunnels are started.
- [x] `internal/forward/engine_test.go` —
      `TestEngineReload_RenameRunningRestartsUnderNewName`: start `a`, reload
      with `a` renamed to `c` (same connection fields, `Enabled: true`); assert
      `a.stops == 1`, a new fake exists for `c`, **`c.starts == 1`**, and the
      resulting set is `{c}` (no `a`). Mirrors `TestEngineReload` (`:160-192`).
- [x] `internal/forward/engine_test.go` —
      `TestEngineReload_NewEnabledTunnelStarts`: reload that adds a fresh
      `Enabled: true` tunnel; assert its fake `starts == 1`. Also assert a
      fresh `Enabled: false` tunnel has `starts == 0` (guards the off case).
- [x] `internal/forward/engine_test.go` —
      `TestEngineReload_RenameOffStaysOff`: rename an **off** (`Enabled: false`)
      tunnel; assert the new-name fake has `starts == 0` and `stops == 0`.
- [x] Confirm `internal/daemon/server_tunnels_test.go`
      `TestServer_UpdateTunnel_Rename` still passes (it asserts persistence
      only; the daemon-level restart is not asserted there because `fakeEngine`
      does not model start/stop — see Technical details).
- [x] `make fmt && make vet && go build ./... && go test ./...` clean.
- [x] `docs/ROADMAP.md` + this file: phase row + status flip on
      start/complete.

## Definition of Done

- [ ] A running (`enabled: true`) tunnel renamed via the TUI editor reappears
      under the new name in `Connecting`/`Connected`, **not** `Off` — verified
      manually in both standalone (`portato`) and attach (`portato attach`).
      (Mechanism proven by `TestEngineReload_RenameRunningRestartsUnderNewName`;
      the manual smoke is the one item pending the human's environment.)
- [x] An off (`enabled: false`) tunnel renamed stays off (no spurious start).
- [x] A tunnel renamed via the daemon/client path (`UpdateTunnel`) restarts
      under the new name (covered by the engine-level test, which is the
      authoritative layer for this behavior).
- [x] A newly-added tunnel with `enabled: true` starts on reload; an
      `enabled: false` one does not.
- [x] `tunnelChanged` still excludes `Enabled` for *same-name* edits (toggling
      `enabled` alone does not restart an existing tunnel) — unchanged
      (`TestEngineReload_OffChangedUpdatesConfigNotStarted`,
      `TestTunnelReconfigureUpdatesStatus` still green).
- [x] `go vet ./...`, `gofmt -l .`, `go test ./...` clean.

## Verification

```sh
# Unit tests (authoritative for the restart-on-rename behavior)
go test ./internal/forward/... -run 'TestEngineReload' -v
make fmt && make vet && go build ./... && go test ./...
```

Manual smoke — the exact reported scenario:

1. `portato` (or `portato attach`) → move to a tunnel, press `space` → it
   reaches `Connected` (`nc -z 127.0.0.1 <local>` succeeds).
2. Press `e` → change **only** `Name` → `Ctrl+S`.
3. The list now shows the tunnel under the new name and it returns to
   `Connecting` → `Connected` on its own; `nc -z` to the local port keeps
   working without pressing `space`.
4. Repeat with an off tunnel (`space` to disable, then rename) → it stays Off
   under the new name.

## Technical details

- The change is a ~3-line edit in one method (`Engine.Reload`). No new files,
  no API/IPC/config surface change, no dependency change.
- `Tunnel.Start` (`tunnel.go:102-148`) returns quickly: for `remote` it only
  spawns a goroutine; for `local`/`dynamic` it does a local `net.Listen`
  (fast) then spawns the run goroutine that performs the SSH dial. The SSH dial
  itself never blocks the engine mutex. `Reload` already calls `tn.Stop()`
  (Loop 1) and `Reconfigure()` → `Restart()` (Loop 2) under `e.mu`, so an
  inline `Start` is consistent with the existing critical-section usage.
- Why the authoritative test is at the engine layer, not the daemon layer:
  `internal/daemon/server_test.go`'s `fakeEngine.Reload` (`:78-82`) merely
  swaps the in-memory config — it does not model per-tunnel start/stop, so it
  cannot assert restart-on-rename. `internal/forward/engine_test.go`'s
  `newTestEngine`/fake tunneler records `starts`/`stops`/`restarts`/`cfg`, so
  the assertion belongs there (next to `TestEngineReload`). The daemon's
  `fakeEngine` would need a start/stop model to assert this end-to-end; that is
  out of scope for a one-method engine fix.
- Both standalone (`localController`) and attach (`remoteController`) flows go
  through `Engine.Reload`, so the single fix covers both modes.

## Out of scope

- Reconstructing a full rename map (old → new) to transfer non-`Enabled`
  runtime state. `Enabled` is the documented contract; that is enough.
- Modeling start/stop in the daemon's `fakeEngine` for an end-to-end daemon
  rename test (see Technical details).
- Changing `tunnelChanged`'s `Enabled` exclusion for same-name edits.

## Commit plan

1. `docs(phase-26): plan` — create this file + ROADMAP row (`[ ]`).
2. `fix(forward): restart renamed tunnel under its new name` — the `Reload`
   change + engine tests.
3. `docs(phase-26): complete` — DoD checklist + `[ ] -> [x]`.

## Start guard

This phase is `status: todo`. It starts only on an explicit "start phase 26"
command (per docs/CONVENTIONS.md). The first action then is commit 1
(planning) — creating this file + the ROADMAP row.
