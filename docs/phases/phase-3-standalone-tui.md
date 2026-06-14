---
phase: 3
title: Standalone TUI
status: in-progress
depends_on: [2]
---

## Goal

Launch `portato` with no arguments in **standalone mode**: a TUI with a live list of
tunnels, toggled with the space bar. The tunnels live in this same process (in-process).
This is the first "visible" product — the user immediately sees the result of the work and
can toggle tunnels.

In this phase we do NOT build a daemon and IPC — that's Phase 4. We also do NOT build the
smart launcher (daemon auto-detection) and the "to background?" modal — that's Phase 5. Here
the root command `portato` simply brings up `localController` + TUI.

## Phase scope (what we do)

- `Controller` interface (`internal/controller/controller.go`).
- `localController` — a wrapper over `Engine` (for standalone mode).
- `Changes() <-chan struct{}` — a tick every 1s, a signal for the TUI to redraw.
- Bubble Tea TUI: list of tunnels, navigation, toggle, hotkeys.
- Lipgloss styles, colored statuses.
- Wiring into `cmd/root.go` (replacing the stub with a real launch).
- `time.Ticker` 1s for live statuses (inside `Local`; the TUI subscribes to the channel via a channel-listening `tea.Cmd`).

## Phase scope (what we do NOT do)

- Daemon/IPC/remoteController — Phase 4.
- Smart-launcher (daemon auto-detection) — Phase 5.
- "leave in background?" modal — Phase 5 (for now `q` = `StopAll()` + exit).
- Tunnel editor (e/n/d) — Phase 10.

## Tasks

- [ ] `glm-complex/internal/controller/controller.go`:
  - [ ] `type State int` (re-export from forward, or define here and convert — choose).
  - [ ] `type Status struct { Name string; State State; Error string; Type string; Local, Remote string; Uptime time.Duration }`.
  - [ ] `type Controller interface { List() []Status; Enable(name) error; Disable(name) error; Restart(name) error; Reload() error; Changes() <-chan struct{}; Close() error }`.
- [ ] `glm-complex/internal/controller/local.go`:
  - [ ] `type Local struct { engine *forward.Engine; cfg *config.Config; cfgPath string; changes chan struct{}; }`.
  - [ ] `func NewLocal(cfg, cfgPath, log) *Local` — creates the Engine, does not start it.
  - [ ] `Enable/Disable/Restart` → `engine.*` + a non-blocking signal to `changes`.
  - [ ] `Reload()` → `config.Load` + `engine.Reload`.
  - [ ] `Changes()` → return the channel; launch a goroutine with a `time.Ticker` 1s that non-blockingly sends to the channel.
  - [ ] `Close()` → `engine.StopAll()` + close the channel.
- [ ] `glm-complex/internal/tui/styles.go`:
  - [ ] Lipgloss styles: header, table rows, footer with hotkeys, colored status indicators (green connected, gray off, yellow connecting/reconnecting, red error).
- [ ] `glm-complex/internal/tui/model.go`:
  - [ ] `type Model struct { ctrl controller.Controller; list []controller.Status; cursor int; width, height int; mode string; help bool; quit bool; pendingQuit bool }`.
  - [ ] `mode` = `"standalone"` (later it will be `"attach @ <socket>"`).
- [ ] `glm-complex/internal/tui/update.go`:
  - [ ] `Init()` — initial `List()` + subscription to `Changes()`.
  - [ ] `Update(msg)`:
    - `tea.KeyPressMsg` (v2):
      - `↑`/`k` — cursor up; `↓`/`j` — cursor down.
      - `space` — if Off is selected → `Enable`; if Connected/Connecting → `Disable`.
      - `r` — `Restart` the selected one.
      - `a` — `Enable` all; `x` — `Disable` all.
      - `R` — `Reload()` the config.
      - `?` — toggle the help window (an inset at the bottom or a modal).
      - `q` / `ctrl+c` — `Close()` + `tea.Quit`.
    - `tea.WindowSizeMsg` — update `width/height`.
    - signal from `Changes()` → `ctrl.List()` + redraw.
- [ ] `glm-complex/internal/tui/view.go`:
  - [ ] Header: `portato — Port Forwarding` + `mode: standalone`.
  - [ ] Table: `●/○` | `name` | `type` | `local → remote` | `status` | `uptime`.
  - [ ] Footer: hotkeys `↑↓ move · space toggle · r restart · a/x all · R reload · ? help · q quit`.
  - [ ] Highlight the selected row (like the blue bar in MCP).
  - [ ] When `help=true` — an additional panel describing all hotkeys.
- [ ] `glm-complex/internal/tui/run.go`:
  - [ ] `func Run(ctrl controller.Controller, mode string) error` — `tea.NewProgram(model)`, `Run()`, handle the error. AltScreen and `tea.KeyPressMsg`/`tea.NewView` — per the v2 API (`view.AltScreen = true` in `View()`, not the `tea.WithAltScreen()` option).
- [ ] `glm-complex/internal/cmd/root.go` (replacing the stub):
  - [ ] `rootCmd.RunE`: load the config (`config.Load` or `EnsureExample`), create a logger (stderr + file), create `controller.NewLocal(...)`, call `tui.Run(ctrl)`.
  - [ ] Graceful shutdown: on TUI exit — `ctrl.Close()` (which does `StopAll()`).
  - [ ] The `--config` flag is honored.

## Definition of Done

- [ ] `portato` (no arguments, no daemon) opens a TUI with the list of tunnels from the config.
- [ ] `↑↓/jk` move the cursor, the selected row is highlighted.
- [ ] `space` on an `Off` tunnel enables it; `Connecting → Connected` is reflected in the UI within ~1s.
- [ ] `space` on `Connected/Connecting` disables it (`→ Off`).
- [ ] `r` restarts the selected tunnel.
- [ ] `a` enables all, `x` disables all.
- [ ] `R` reloads the config from disk (new/deleted tunnels appear/disappear).
- [ ] `?` shows the help window, another `?` (or `Esc`) hides it.
- [ ] `q` exits, releasing all local ports (a subsequent launch does not fail with `address already in use`).
- [ ] Real traffic flows: after `space` on a local tunnel, `curl http://localhost:<local>` reaches the remote service over SSH.
- [ ] SSH errors (no key / unknown host / auth failed) are shown in the UI as `Error` with the reason; the application does not crash.
- [ ] `go vet ./...` and `gofmt -l .` are clean.

## Verification

```sh
cd glm-complex
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

- **Changes channel pattern:** the local `Controller` launches a goroutine with `time.NewTicker(time.Second)` that non-blockingly sends `struct{}{}` to the channel. The controller does **not** depend on bubbletea (this matters for `remoteController` in Phase 4). The TUI subscribes: from `Init()`/`Update()` it returns a `tea.Cmd` that blocks on `<-changes` and emits a local `tickMsg`, after which it re-subscribes. On every message — `ctrl.List()` + redraw. In Phase 9, `Changes()` will switch to push from the Engine without changing the interface.
- **Bubble Tea v2:** the project uses `charm.land/bubbletea/v2`. Hence `View() tea.View` (via `tea.NewView`), `tea.KeyPressMsg` (not `tea.KeyMsg`), space is `case "space":`, and AltScreen is the field `view.AltScreen = true` in `View()`, not the `tea.WithAltScreen()` option on `NewProgram`.
- **Non-blocking operations:** `Enable/Disable/Restart` launch the SSH connection in a goroutine (inside the Engine) and return immediately; the TUI must not hang on SSH operations. The status is updated via `Changes()`.
- **MouseOption:** do NOT enable (many terminals glitch); keyboard only.
- **Lipgloss colors:** test on dark and light themes; use standard ANSI colors, don't hardcode RGB.
- **Reload semantics:** after `R` — `config.Load` + `engine.Reload`. Running tunnels that haven't changed in the config are NOT restarted (diff by the serialized view of the tunnel).

## Phase output artifact

- `portato` in standalone mode: the user sees the TUI, toggles local tunnels, traffic flows.
- The architectural foundation for Phase 4: the `Controller` interface + `localController` are ready; it remains to add `remoteController` and the daemon.
