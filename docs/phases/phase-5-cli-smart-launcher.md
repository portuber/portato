---
phase: 5
title: CLI commands, smart launcher, and hand-off
status: in-progress
depends_on: [4]
---

## Goal

Bring it all together. Three things:

1. **CLI commands** (`portato list/enable/disable/restart`) as daemon clients — for scripts
   and cases when a TUI is not needed.
2. **Smart launcher** in the root command `portato` (no arguments): automatically detects
   whether the daemon is running (socket is alive) and chooses a mode — `attach` or `standalone`.
3. **The "leave in background?" modal** when exiting standalone with live tunnels: flicker-free
   hand-off — spawn a separate `portato daemon` process, wait for readiness, exit.

After this phase the utility fully matches the "three modes of operation" concept.

## Phase scope (what we do)

- Real implementations of cobra commands `list`, `enable`, `disable`, `restart` as daemon clients.
- Clear errors when the daemon is not running.
- Smart-launcher in root: probe the socket → `attach` or `standalone`.
- A modal in the TUI when exiting standalone with live tunnels.
- Hand-off: spawn `portato daemon` (detached), wait-for-socket, exit.

## Phase scope (what we do NOT do)

- Push events — Phase 9 (polling for now).
- TUI editor — Phase 10.
- Autostart (launchd/systemd) — Phase 6.

## Tasks

### CLI commands

- [x] `glm-complex/internal/cmd/list.go` (replace the stub):
  - [x] Create `client.New(socketPath)`.
  - [x] `Healthz()` — on error, a clear message: `«portato daemon is not running. Start it with 'portato daemon' or set up autostart with 'portato install'.»` + exit code 1.
  - [x] `List()` → print a table to stdout (name, type, local→remote, status, uptime). Format — simple aligned text; post-MVP a `--json` flag can be added.
- [x] `glm-complex/internal/cmd/enable.go`, `disable.go`, `restart.go` (replace stubs):
  - [x] Common pattern: `client.New` → `Healthz` → call the corresponding method → on success, a short confirmation (`enabled: <name>`).
  - [x] Argument `<name>` via cobra `Args: ExactArgs(1)`.
  - [x] If no tunnel with that name exists — the daemon error is surfaced readably.

### Smart-launcher

- [x] `glm-complex/internal/cmd/root.go` (extend Phase 3):
  - [x] Before loading the config: probe the socket via `client.New(socketPath).Healthz()` with a short timeout (200ms).
  - [x] If the daemon is alive → **attach** mode:
    - create `controller.Remote(client, ...)`, call `tui.Run(ctrl)`.
    - this is equivalent to `portato attach`, but implicit.
  - [x] If the daemon does not respond → **standalone** mode (as in Phase 3):
    - `config.Load/EnsureExample` → `controller.NewLocal(...)` → `tui.Run(ctrl)`.
  - [x] The TUI header shows the selected mode: `standalone` or `attach @ <socket>`.
  - [x] Optional flag `--force-standalone` (to skip auto-detection).

### Hand-off "leave in background?"

- [x] `glm-complex/internal/tui/model.go` (extend Phase 3):
  - [x] New field `mode string` (`"standalone"` | `"attach"`). If `attach` — normal exit without a modal.
  - [x] Field `confirmQuit bool` — flag to show the modal.
  - [x] In `Update` for `q` / `ctrl+c`:
    - If `mode == "attach"` → immediately `Close()` + `tea.Quit`.
    - If `mode == "standalone"`:
      - Count live tunnels (`List()` filtered by `State ∈ {Connecting, Connected, Reconnecting}`).
      - If 0 → `Close()` + `tea.Quit`.
      - If > 0 → `confirmQuit = true` (show the modal).
- [x] Modal in `view.go`:
  - [x] Centered window: `«N tunnels are active. Leave work in the background? [y/N]»`.
  - [x] Keys: `y` — yes, `n` / `Esc` / `enter` — no (default).
- [x] `glm-complex/internal/tui/handoff.go`:
  - [x] `func handoff(ctx) error`:
    - Before spawning, ensure that `cfg` on disk matches the current Engine state (if in standalone the user toggled, `localController` should already have persisted via `Enable/Disable` → `config.Save`). Check and save if necessary.
    - `exec.Command(os.Executable(), "daemon", "--config", cfgPath)` + `cmd.Stdin/Stdout/Stderr = nil` + `cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}` (detached). `cmd.Start()`.
    - Poll `client.New(socketPath).Healthz()` every 100ms up to a 5s timeout.
    - On success — `tea.Quit`. On timeout — fallback: `Close()` + exit with a warning in the log/message.
- [x] In `Update`:
  - `y` → run `handoff` asynchronously, show `«Starting daemon...»`, on success — `tea.Quit`.
  - `n`/`Esc`/`enter` → `confirmQuit = false` + `Close()` + `tea.Quit`.

### Related minor items

- [x] `glm-complex/internal/cmd/paths.go` (or in `daemon/paths.go`): a shared function `SocketPath() string` and `PIDPath() string`, used by all commands. Avoid duplication.
- [x] In `daemon.New` accept a flag `spawned bool` (or simply ignore it) — it does not matter whether the smart-launcher or the user manually spawns it.

## Definition of Done

- [ ] `portato list` prints a table of tunnels with their statuses (when the daemon is running).
- [ ] `portato enable <name>`, `portato disable <name>`, `portato restart <name>` work and are confirmed by the next `portato list`.
- [x] Without the daemon, all CLI commands produce a clear error with a hint (no panic/stack trace).
- [ ] `portato` with no arguments:
  - With the daemon running, opens the TUI in attach mode; the header shows `attach @ <socket>`.
  - Without the daemon, opens the TUI in standalone mode; the header shows `standalone`.
- [ ] In standalone mode, pressing `q` with live tunnels present shows the modal "Leave work in the background?".
- [ ] Answering `y` in the modal:
  - spawns a `portato daemon` process,
  - the TUI waits for the socket to come up (≤ 5s),
  - exits; the tunnels keep running in the daemon.
- [ ] Answering `n` / `Esc` / `enter` → all tunnels are stopped correctly, the application exits.
- [ ] After hand-off: `portato list` confirms the tunnels are `Connected` in the daemon.
- [ ] In attach mode, `q` simply closes the TUI (without a modal); the daemon's tunnels keep running.
- [x] `go vet ./...` and `gofmt -l .` are clean.

## Verification

```sh
cd glm-complex
make build

# 1. CLI commands (with daemon):
./bin/portato daemon &
./bin/portato list
./bin/portato enable <name>
./bin/portato list
./bin/portato disable <name>
kill -TERM $(cat "$HOME/.config/portato/portato.pid")

# 2. CLI commands without daemon:
./bin/portato list    # expected message: "daemon is not running..."

# 3. Smart-launcher → attach:
./bin/portato daemon &
./bin/portato         # opens the TUI in attach mode, header "attach @ <socket>"
# q — exit, the daemon keeps running
kill -TERM $(cat "$HOME/.config/portato/portato.pid")

# 4. Smart-launcher → standalone + hand-off:
./bin/portato         # daemon not running → standalone, header "standalone"
# space — enable a tunnel
# q → modal
#   y → daemon is spawned, the tunnel keeps running
# exit
./bin/portato list    # the tunnel is Connected in the fresh daemon
kill -TERM $(cat "$HOME/.config/portato/portato.pid")

# 5. Standalone with no live tunnels — exit without a modal:
./bin/portato         # standalone, no toggles
# q → exit immediately (without a modal)
```

## Technical details

- **Healthz probe timeout:** 200ms. If the daemon is there, it responds quickly; if not, connect-refused is also instant. Will not delay startup.
- **Hand-off determinism:** since `localController.Enable/Disable` always persists `enabled` to YAML (invariant from Phase 4), the config on disk is the source of truth. The fresh daemon reads the same file and brings up tunnels with `Enabled=true`. Hand-off = `spawn` + `wait-for-socket`.
- **Setsid for a detached daemon:** `syscall.SysProcAttr{Setsid: true}` creates a new session; the process does not die with its parent. Works the same on macOS and Linux.
- **Hand-off race:** between the standalone exit and the daemon bringing up the tunnels there is a window (~hundreds of ms) during which the local port may be unavailable (the listener is already closed in standalone, but not yet opened in the daemon). This is an MVP trade-off. If critical — post-MVP FD-passing (pass the `*net.Listener` via an `os.File` FD to the subprocess).
- **Spawn mechanism:** `exec.Command(os.Executable(), "daemon", "--config", cfgPath)` — we take the same binary as standalone. Cheap and symmetric.
- **Default in the modal = N:** a safe default — a stray enter from the user will drop the tunnels (explicit expectation), but will not leave a daemon running in the background by accident.
- **Hand-off logging:** if spawn failed (executable deleted / no permissions), show a readable error in the TUI and fall back to `Close()` + exit.

## Implementation notes (deviations from the plan)

- **Persisting `enabled` in standalone.** The plan assumed that `localController.Enable/Disable` already persists `enabled` to YAML. In fact, before Phase 5 only the daemon side (`server.go`) persisted it; the standalone toggle did not write to disk. This is fixed within the phase (`feat(controller): persist enabled flag on standalone toggle`) — without it, hand-off would have brought up a stale set of tunnels.
- **Hand-off sequence = `Close` → `spawn` → `wait`.** The plan/SPEC described `spawn` → `wait`. The implementation first calls `ctrl.Close()` (`StopAll`, releasing local ports), then spawns, then waits for the socket. Reason: `Tunnel.Start` binds the listener synchronously and does not retry a failed bind — the ports must be free by the time the daemon starts, otherwise the daemon's tunnel falls into `Error` with no recovery. SPEC §12 has been updated.
- **The `spawned` flag on `daemon.New` is omitted** (the plan allowed "or simply ignore it"): a spawn from hand-off and a manual `portato daemon` are indistinguishable, so the flag is unnecessary.
- **TUI API:** `tui.Run`/`tui.New` take an `Options{Mode, CfgPath, SocketPath}` instead of a mode string — to thread the paths through for hand-off.

## Phase deliverable

- A fully functional utility in three modes: smart (`portato`), daemon (`portato daemon`), attach/CLI.
- Hand-off works: you can work in standalone, exit "to the background" — and the tunnels keep living in the daemon.
- The MVP is ready for the final phase: autostart + E2E (Phase 6).
