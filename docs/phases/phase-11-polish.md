---
phase: 11
title: Polish (logs, themes, doctor, CI)
status: todo
depends_on: [6]
---

> Outline phase. Detailed when approached. The candidates below are a menu; the
> user sets priorities before starting.

## Goal

Bring the product to a mature state: convenient log viewing in the TUI, themes,
diagnostics, tests, CI. After this phase `portato` is ready for everyday use
without caveats.

## Task candidates

### Logs in TUI

- [ ] `internal/tui/logs.go` — screen for viewing the selected tunnel's logs (`l`).
- [ ] Filtering by tunnel (slog attribute `tunnel=name`).
- [ ] Scrolling (`bubbles/viewport`), auto-scroll to the bottom, scroll pause.
- [ ] On-the-fly level switching (info/debug).

### Themes / appearance

- [ ] Light/dark theme support (via `NO_COLOR`, `COLORFGBG`, or config).
- [ ] Unify spacing/state colors (connected/error/reconnecting).
- [ ] Adaptive table width to the terminal.
- [ ] `/` — filter/search by name in the tunnel list.

### TOFU prompt in TUI

- [ ] On an unknown host — interactive prompt in the TUI: accept the key? (fingerprint shown), save to `~/.ssh/known_hosts`.
- [ ] CLI mode (`portato enable` without TUI) — read confirmation from stdin or fail with a clear error (if non-interactive).

### Log rotation

- [ ] Size- or time-based rotation of `portato.log` / `daemon.log` (via `lumberjack` or a simple custom mechanism).

### Tests

- [ ] Unit tests: `config` (validation, save/load round-trip), `forward` (backoff, states, remote/dynamic directions), `controller` (local+remote loopback), `daemon`+`client` (HTTP API over loopback).
- [ ] Integration test: a local sshd in the test, a full cycle `Enable → traffic → Disable` for local/remote/dynamic.
- [ ] `go test ./...` coverage without failures on all target OSes (via `GOOS` in CI).

### CI

- [ ] GitHub Actions (or equivalent): `go vet`, `gofmt -l`, `go test`, cross-compilation for darwin/linux × amd64/arm64.
- [ ] `goreleaser` (optional) for releases with binaries.

### Documentation

- [ ] `README.md` — full guide: installation, config, TUI, daemon, per-OS autostart, troubleshooting, remote/dynamic/SOCKS scenarios.
- [ ] `docs/USAGE.md` with screenshots and examples.
- [ ] `--help` texts proofread and consistent.

### Security / UX

- [ ] `portato doctor` — diagnostics: availability of config, keys, agent, daemon socket, lingering (Linux).
- [ ] A helpful message when the local port is busy (suggest `lsof -i :<port>`).
- [ ] Verify the IPC socket is isolated (`0600` after installation).

## Definition of Done

Refined during detailing. Baseline minimum:

- [ ] `l` in the TUI opens a live tunnel log with scrolling and filtering.
- [ ] Light/dark theme is detected automatically and does not break readability.
- [ ] An unknown SSH host does not crash the app — it allows accepting the key in the TUI.
- [ ] `portato doctor` passes on a healthy system and gives useful hints on problems.
- [ ] `go test ./...` passes; coverage of key packages ≥ a baseline threshold (to be defined in the phase).
- [ ] CI is green on main: vet, fmt, test, cross-compilation.
- [ ] README covers all scenarios: MVP launch, daemon, per-OS autostart, remote/dynamic/SOCKS, troubleshooting.
- [ ] `make build-all` builds binaries for all target OSes/architectures.
- [ ] `go vet ./...` and `gofmt -l .` are clean.

## Technical details (preliminary)

- The phase is deliberately an outline phase — the user sets priorities before starting.
- Each category (logs/themes/CI/...) can become a mini-subphase; during detailing, break them into checklists as in Phases 0–6.
- Do not attempt to do everything from the candidates at once — single out an MVP polish (logs + TOFU + tests) and defer themes/CI based on time.

## Open questions

- Priorities within the phase: what comes first (logs, themes, CI, doctor)?
- Is `portato doctor` needed as a separate subcommand, or should diagnostics be embedded into command errors?
