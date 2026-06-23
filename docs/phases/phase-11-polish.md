---
phase: 11
title: Polish (logs, themes, doctor, CI)
status: done
depends_on: [6]
---

## Goal

Bring the product to a mature state: convenient log viewing in the TUI,
automatic light/dark theming, an interactive TOFU (unknown-host) prompt,
a `portato doctor` diagnostics command, CI, and refreshed docs. After this
phase `portato` is ready for everyday use without caveats.

## Design decisions (locked at phase start)

1. **Scope = the baseline DoD** (all 9 items): logs in TUI, automatic theme
   detection, the TOFU prompt, `portato doctor`, a coverage baseline, CI,
   README, `make build-all`, clean vet/fmt. **Deferred** (candidates, not
   DoD): log rotation, the `/` list filter, `goreleaser`. *— delivered in
   [Phase 13](./phase-13-polish-2.md): a size-rotating log writer, the `/`
   list filter, and the goreleaser release config.*
2. **Logs via an in-memory ring buffer.** A custom `slog.Handler` writes to
   the file handler AND records each record into a process-owned `Ring`
   (keyed by the `tunnel` slog attribute). This works identically in
   standalone (TUI reads the shared ring) and attach (the daemon exposes the
   ring via `GET /logs?name=`).
3. **TOFU = TUI modal on the error state.** The unknown host key is captured
   at rejection time inside the SSH host-key callback, stored on the `Tunnel`
   (as a pre-built known_hosts line + host + fingerprint), and surfaced via
   `forward.Status` (JSON-serialisable strings → IPC-safe). On `a` the TUI
   calls `Controller.AcceptHost(name)` which appends the line to
   `known_hosts` and restarts the tunnel. No handshake-callback surgery.
4. **Themes** are a `theme` struct of resolved `lipgloss.Style`s, chosen by
   `detectTheme()` from `NO_COLOR` (→ monochrome) / `COLORFGBG`
   (`fg;bg`, bg ≤ 6 → dark) / default dark. The package-level style `var`s
   become fields on `Model`, set once in `New`.
5. **`portato doctor`** is a plain cobra subcommand; each check prints
   `✓`/`✗` + a hint, exit 1 on any failure.

## Scope (what lands)

- `internal/log/ring.go` — the ring buffer + a `slog.Handler` that feeds it.
- `Controller.Logs(name)` + `Controller.AcceptHost(name)` (interface),
  `Local`/`Remote` impls, `fakeCtrl`/`fakeEngine` updated.
- `GET /logs?name=` and `POST /tunnels/{name}/accept-host` daemon endpoints;
  matching `client` methods.
- `internal/tui/logs.go` — the `l` logs screen (viewport, scrolling,
  auto-scroll, level toggle).
- `internal/tui/theme.go` + refactor of `styles.go`/`view.go`/`editor.go`
  onto a palette.
- TOFU modal in the TUI.
- `internal/cmd/doctor.go` — `portato doctor`.
- `.github/workflows/ci.yml` — vet / fmt / test / cross-compile.
- `Makefile` `build-all` + `cover` targets.
- `README.md` rewrite; `SPEC.md` touch-ups (§6 endpoints, §11 hotkeys, §14).

## Architecture

### Logs

- `log.Entry{Time, Level, Tunnel, Msg}`; `Ring` holds the last N (≈2000)
  entries total; `Ring.Lines(tunnel string) []Entry` filters by the `tunnel`
  attr (empty `tunnel` = all). Thread-safe.
- `log.Setup` returns `(*slog.Logger, *Ring, io.Closer, error)`. The handler
  is a fan-out: the standard text handler to the file, plus a record into the
  ring (extracting the `tunnel` attr from `record.Attrs`).
- `controller.Local` and `daemon.Server` receive the `*Ring`. `Local.Logs`
  reads it directly; `Server` serves it at `GET /logs?name=` (JSON array).
- `client.Client.Logs(name)` + `controller.Remote.Logs`.
- The TUI logs screen re-fetches on the existing 1s `redrawTick` while open
  (acceptable: it is a transient modal, not the idle tunnel-status path that
  Phase 9 made push-driven).

### TOFU

- `forward.wrapHostKey`: on an unknown-host rejection, build the
  `knownhosts.Line` + capture host/fingerprint, store via a new sink param
  (nil-safe). Export `forward.AppendKnownHostLine(file, line)`.
- `forward.Tunnel` gains `pendingHost/Fingerprint/HostLine`;
  `forward.Status` gains the same fields `omitempty` (strings → IPC-safe).
- `controller.AcceptHost(name)`: Local reads the pending line from
  `engine.List()`, appends to `cfg.Defaults.ResolvedKnownHosts()`, restarts.
  Remote → `POST /tunnels/{name}/accept-host`.
- TUI: when the selected tunnel has `PendingHost != ""`, show the accept
  modal; `a` accepts.

### Themes

- `detectTheme() themeKind` → `theme{...}` with the resolved styles. The view
  helpers read from `m.theme.*`. Editor styles move there too.

## Tasks

- [x] `docs(phase-11): start` — detail this file, flip status.
- [x] `feat(log):` ring buffer + handler; thread `*Ring` through `Setup`,
  `Local`, `daemon.New`.
- [x] `feat(controller,daemon,client):` `Logs` + `GET /logs` + client method.
- [x] `feat(tui):` logs screen (`internal/tui/logs.go`) + `l` key + footer.
- [x] `feat(forward,controller,daemon,client,tui):` TOFU capture + accept.
- [x] `feat(tui):` light/dark/monochrome themes.
- [x] `feat(cmd):` `portato doctor`.
- [x] `ci(build):` GitHub Actions + `make build-all` + `make cover`.
- [x] `docs:` README rewrite + SPEC touch-ups.
- [x] `make fmt && make vet && make build && make build-all && go test ./...` clean.
- [x] `docs(phase-11): complete`.

## Definition of Done

- [x] `l` in the TUI opens a live per-tunnel log with scrolling and level filtering.
- [x] Light/dark theme is detected automatically and does not break readability.
- [x] An unknown SSH host does not crash the app — it can be accepted from the TUI.
- [x] `portato doctor` passes on a healthy system and gives useful hints on problems.
- [x] `go test ./...` passes; total coverage ≈ 69% (`make cover`), key packages
      (`config`, `forward`, `controller`, `daemon`, `client`, `tui`) covered.
- [x] CI is green on main: vet, fmt, test (`-race`), cross-compilation.
- [x] README covers all scenarios: launch, daemon, per-OS autostart,
      remote/dynamic/SOCKS, logs, TOFU, doctor, troubleshooting.
- [x] `make build-all` builds binaries for darwin/linux × amd64/arm64.
- [x] `go vet ./...` and `gofmt -l .` are clean.

## Verification

```
make fmt && make vet && make build && make build-all && go test ./...
make cover
```

Manual smoke:
- `l` on a tunnel → live log view; `↑↓`/`pgup`/`pgdn` scroll; level toggle
  filters; `esc` returns to the list. Works standalone and in attach.
- `NO_COLOR=1 portato` → monochrome; `COLORFGBG=0;15` → dark; `0;0` → light.
- Point a tunnel at a host not in `known_hosts` (with `accept_new_hosts:
  false`) → TUI shows the accept-key modal → `a` → the key is appended and
  the tunnel connects.
- `portato doctor` → all `✓` on a healthy machine; flip one precondition →
  the matching `✗` + hint, exit 1.
- CI run is green; `bin/portato-darwin-arm64` etc. exist after `make build-all`.

## Technical notes / risks

- **Theme refactor** touches every style reference in `view.go`/`editor.go`.
  Mechanical but wide; covered by existing + new view tests.
- **Logs in attach** re-introduces a periodic request, but only while the
  logs screen is open — distinct from the Phase-9 idle tunnel-status path.
- **TOFU capture** changes `dialSSH`/`hostKeyCallback` signatures; `ssh_test.go`
  is updated. Sinks are nil-safe.
