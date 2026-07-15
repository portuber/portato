---
phase: 3
title: Standalone TUI
status: done
depends_on: [2]
---

## Goal

Run `portato` with no arguments in **standalone mode**: a TUI with a live list of tunnels, toggled with the spacebar. The tunnels live in the same process (in-process). This is the first "visible" product — the user immediately sees the result of the work and can toggle tunnels.

In this phase we do NOT build the daemon and IPC — that is Phase 4. We also do NOT build the smart launcher (daemon autodetection) and the "to background?" modal — that is Phase 5. Here the root command `portato` simply brings up `localController` + TUI.

## Phase scope (what we do)

- `Controller` interface (`internal/controller/controller.go`).
- `localController` — a wrapper over `Engine` (for standalone mode).
- `Changes() <-chan struct{}` — a tick every 1s, a signal to the TUI to redraw.
- Bubble Tea TUI: tunnel list, navigation, toggle, hotkeys.
- Lipgloss styles, colored statuses.
- Wiring into `cmd/root.go` (replacing the stub with a real launch).
- `time.Ticker` 1s for live statuses (inside `Local`; the TUI subscribes to the channel via a channel-listening `tea.Cmd`).

## Phase scope (what we do NOT do)

- Daemon/IPC/remoteController — Phase 4.
- Smart-launcher (daemon autodetection) — Phase 5.
- The "keep in background?" modal — Phase 5 (for now `q` = `StopAll()` + exit).
- Tunnel editor (e/n/d) — Phase 10.

## Tasks

- [x] `portato/internal/controller/controller.go`:
  - [x] `type State int` (re-export from forward, or define here and convert — choose). **Decided:** re-export (`type State = forward.State`, `type Status = forward.Status`) + state constants.
  - [x] `type Status struct { ... }` — re-exported from forward; the interface uses `controller.Status`.
  - [x] `type Controller interface { List() []Status; Enable(name) error; Disable(name) error; Restart(name) error; Reload() error; Changes() <-chan struct{}; Close() error }`.
- [x] `portato/internal/controller/local.go`:
  - [x] `type Local struct { engine *forward.Engine; cfgPath string; ... changes chan struct{} }` (+ ctx/cancel, interval, Once-guards).
  - [x] `func NewLocal(cfg, cfgPath, log) *Local` — creates the Engine, does not start it.
  - [x] `Enable/Disable/Restart` → `engine.*` (the signal to `changes` comes from a separate ticker, see below).
  - [x] `Reload()` → `config.Load` + `engine.Reload`.
  - [x] `Changes()` → return the channel; lazily starts a goroutine with `time.NewTicker(1s)` that non-blockingly sends to the channel. The controller package does NOT depend on bubbletea.
  - [x] `Close()` → idempotent (sync.Once): cancel ctx + stop ticker + wait for the goroutine + close the channel + `engine.StopAll()`.
- [x] `portato/internal/tui/styles.go`:
  - [x] Lipgloss styles: header, table rows, footer with hotkeys, colored status indicators (green connected, gray off, yellow connecting/reconnecting, red error).
- [x] `portato/internal/tui/model.go`:
  - [x] `type Model struct { ctrl controller.Controller; list []controller.Status; cursor int; width, height int; mode string; help bool; quit bool }` (`pendingQuit` is not needed — in Phase 3 `q` exits immediately).
  - [x] `mode` = `"standalone"` (later it will be `"attach @ <socket>"`).
- [x] `portato/internal/tui/update.go`:
  - [x] `Init()` — initial `List()` + subscription to `Changes()` (via channel-listening `tea.Cmd`).
  - [x] `Update(msg)`:
    - `tea.KeyPressMsg` (v2):
      - `↑`/`k` — cursor up; `↓`/`j` — cursor down.
      - `space` — if the selected is Off → `Enable`; otherwise → `Disable`.
      - `r` — `Restart` of the selected.
      - `a` — `Enable` all; `x` — `Disable` all.
      - `R` — `Reload()` the config.
      - `?` — toggle the help window (a callout at the bottom or a modal).
      - `q` / `ctrl+c` — `tea.Quit` (`ctrl.Close()` is called via defer in root.go).
    - `tea.WindowSizeMsg` — update `width/height`.
    - signal from `Changes()` → `ctrl.List()` + redraw.
- [x] `portato/internal/tui/view.go`:
  - [x] Header: `portato — Port Forwarding` + `mode: standalone`.
  - [x] Table: `●/○` | `name` | `type` | `local → remote` | `status` | `uptime`.
  - [x] Footer: hotkeys `↑↓ move · space toggle · r restart · a/x all · R reload · ? help · q quit`.
  - [x] Highlighting of the selected row (a blue bar).
  - [x] When `help=true` — an additional panel with a description of all hotkeys.
- [x] `portato/internal/tui/run.go`:
  - [x] `func Run(ctrl controller.Controller, mode string) error` — `tea.NewProgram(model)`, `Run()`, handle the error. AltScreen and `tea.KeyPressMsg`/`tea.NewView` — per the v2 API (`view.AltScreen = true` in `View()`, not the `tea.WithAltScreen()` option).
- [x] `portato/internal/cmd/root.go` (replacing the stub):
  - [x] `rootCmd.RunE`: load the config (`config.Load`, if absent a sample is created), create a logger (a file under the XDG state home — stderr is removed because it breaks the alt-screen), create `controller.NewLocal(...)`, call `tui.Run(ctrl, "standalone")`.
  - [x] Graceful shutdown: on TUI exit — `ctrl.Close()` (which does `StopAll()`).
  - [x] The `--config` flag is honored.

## Definition of Done

- [x] `portato` (no arguments, no daemon) opens a TUI with the list of tunnels from the config.
- [x] `↑↓/jk` move the cursor, the selected row is highlighted.
- [x] `space` on an `Off` tunnel enables it; `Connecting → Connected` is reflected in the UI within ~1s.
- [x] `space` on a `Connected/Connecting` tunnel disables it (`→ Off`).
- [x] `r` restarts the selected tunnel.
- [x] `a` enables all, `x` disables all.
- [x] `R` reloads the config from disk (new/deleted tunnels appear/disappear).
- [x] `?` shows the help window, `?` again (or `Esc`) — hides it.
- [x] `q` exits, releases all local ports (a subsequent run does not fail with `address already in use`).
- [x] Real traffic flows: after `space` on a local tunnel, a request reaches the remote service over SSH (verified by connecting `mysql -h 127.0.0.1 -P <local>` and `show databases` → real databases).
- [x] SSH errors (no key / unknown host / auth failed) are shown in the UI as `Error` with the reason; the application does not crash.
- [x] `go vet ./...` and `gofmt -l .` are clean.

## Verification

```sh
cd portato
make build

# Prepare a test sshd and add a tunnel to the config:
$EDITOR "$(go run ./cmd/portato config-path 2>/dev/null || echo ~/.config/portato/config.yaml)"
# or on macOS: ~/Library/Application Support/portato/config.yaml

./bin/portato
# In the TUI:
#   ↑↓ navigation
#   space enables/disables
#   r — restart
#   a / x — all on/off
#   R — reload
#   ? — help
#   q — quit

# In another terminal while the TUI is running:
nc -z 127.0.0.1 <local_port>      # after space — success; after another space — failure
curl http://localhost:<local_port>/...   # traffic reaches the remote
```

## Technical details

- **Changes channel pattern:** the local `Controller` starts a goroutine with `time.NewTicker(time.Second)` that non-blockingly sends `struct{}{}` to the channel. The controller does **not** depend on bubbletea (this matters for `remoteController` in Phase 4). The TUI subscribes: from `Init()`/`Update()` it returns a `tea.Cmd` that blocks on `<-changes` and emits a local `tickMsg`, after which it re-subscribes. On each message — `ctrl.List()` + redraw. In Phase 9 `Changes()` will switch to a push from the Engine without changing the interface.
- **Bubble Tea v2:** the project uses `charm.land/bubbletea/v2`. Therefore `View() tea.View` (via `tea.NewView`), `tea.KeyPressMsg` (not `tea.KeyMsg`), space — `case "space":`, and AltScreen is the field `view.AltScreen = true` in `View()`, not the `tea.WithAltScreen()` option of `NewProgram`.
- **Non-blocking operations:** `Enable/Disable/Restart` start the SSH connection in a goroutine (inside the Engine) and return control immediately; the TUI must not hang on SSH operations. The status is updated via `Changes()`.
- **MouseOption:** do NOT enable (many terminals glitch); keyboard only.
- **Lipgloss colors:** check on dark and light themes; use standard ANSI colors, do not hardcode RGB.
- **Reload semantics:** after `R` — `config.Load` + `engine.Reload`. Running tunnels that did not change in the config are NOT restarted (diff by the serialized view of the tunnel).

## Phase deliverable

- `portato` in standalone mode: the user sees the TUI, toggles local tunnels, traffic flows.
- The architectural foundation for Phase 4: the `Controller` interface + `localController` are ready, it remains to add `remoteController` and the daemon.
