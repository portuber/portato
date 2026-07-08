---
phase: 30
title: TUI toggle vs passphrase-prompt on a pending tunnel
status: in-progress
depends_on: []
---

## Goal

Fix the TUI trap where `space` on a passphrase-pending tunnel forces the
passphrase modal instead of toggling, so you cannot disable a
connecting / passphrase-blocked tunnel. Surfaced during the Phase 16 E2E
(`test-myserver-biz` could not be turned off without giving it a passphrase).

## Background

`internal/tui/update.go:169-175`: `case "space": if PendingPassphrase != "" {
open modal }`. A tunnel stuck dialing on a missing passphrase can then only be
"given a passphrase," never disabled, via `space`.

## Tasks

- [ ] Decide the passphrase-entry affordance once `space` no longer opens the
      modal: a dedicated key (e.g. `p`) OR auto-open via `autoOpenIfPending`
      when an Off tunnel is enabled and then blocks.
- [ ] `space` toggles purely on State (active → Disable; Off → Enable),
      ignoring `PendingPassphrase`.
- [ ] Ensure a passphrase can still be entered for a tunnel that needs one
      (via the chosen affordance).
- [ ] Tests: `space` disables a pending tunnel; a passphrase is still enterable.

## Definition of Done

- [ ] `space` on a connecting / passphrase-pending tunnel DISABLES it.
- [ ] A passphrase can still be entered for a tunnel that needs one (via the
      chosen affordance).
- [ ] `go vet ./...`, `gofmt -l .`, `go test ./...` clean.

## Verification

In the TUI, enable a tunnel whose identity needs a passphrase → it goes
`connecting` / pending → `space` disables it (no modal); the chosen affordance
still lets you enter the passphrase to connect.

## Technical details

- Touches `internal/tui/update.go` (the `space` handler) and the passphrase
  modal trigger (`autoOpenIfPending`, `handlePassphraseKey`).
