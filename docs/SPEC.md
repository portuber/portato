# SPEC — technical specification of `portato`

> `portato` is an SSH port-forwarding manager with a TUI.
> The single source of truth for the stack, architecture, and contracts. Changes rarely.
> The workflow for phases is described in [`CONVENTIONS.md`](./CONVENTIONS.md).
> Phase status is tracked in [`ROADMAP.md`](./ROADMAP.md).

## 1. Goal and scope

- Manage a set of SSH port forwards from a single place (the TUI), like the MCP screen in opencode.
- Enable/disable tunnels interactively (spacebar).
- **Three operating modes** of a single binary:
  - **smart-launcher** (`portato` with no arguments): automatically chooses attach or standalone;
  - **daemon** (`portato daemon`): a background process hosting the tunnels + IPC server;
  - **attach/CLI** (`portato attach`, `portato list/enable/...`): clients of the daemon.
- On exit from standalone mode with live tunnels — a "leave in background?" modal with a flicker-free hand-off to the daemon.
- Cross-platform within the MVP scope: **macOS + Linux**. Windows — post-MVP.
- Autostart on system boot (launchd / systemd --user); tunnels are **disabled** by default.

## 2. Stack

| Purpose           | Library                                        |
|-------------------|------------------------------------------------|
| Language          | Go 1.22+                                        |
| CLI               | `github.com/spf13/cobra`                       |
| TUI               | `github.com/charmbracelet/bubbletea` + `bubbles` + `lipgloss` |
| SSH               | `golang.org/x/crypto/ssh` + `golang.org/x/crypto/ssh/knownhosts` (native, no system `ssh`) |
| Config            | `gopkg.in/yaml.v3`                             |
| Paths (XDG)       | `github.com/adrg/xdg`                          |
| Logging           | `log/slog` (standard library)                  |

No system dependency on `ssh` — everything goes through the Go SSH client.

## 3. Operating modes

```
portato                → smart launcher (root command):
                      ┌─ daemon running (socket alive)?
                      │   YES → attach mode:  remoteController + TUI
                      │   NO  → standalone mode: localController + TUI
                      │
                      └─ on exit (q) in standalone, if there are live tunnels:
                            modal "leave work in the background? [y/N]"
                              y → spawn detached `portato daemon`,
                                   wait for the socket to appear, exit
                              n → StopAll(), exit

portato daemon         → background process: Engine + HTTP-over-unix-socket
portato attach         → explicit TUI client of the daemon (errors if the daemon is not running)

portato list           → CLI: status table of all tunnels (stdout)
portato enable <name>  → CLI: enable a tunnel on the daemon
portato disable <name> → CLI: disable a tunnel on the daemon
portato restart <name> → CLI: restart a tunnel

portato install        → install system autostart (launchd / systemd --user)
portato uninstall      → remove autostart
portato --config <path> → custom config path (global flag)
portato --help
```

For `portato` (smart): the presence of the daemon is determined by attempting to connect to the socket + checking the liveness of the PID from the PID file.

## 4. Project layout

```
glm-complex/
├── go.mod
├── cmd/
│   └── portato/
│       └── main.go            # entry point, cobra root
├── internal/
│   ├── config/                # YAML load/save, defaults, validation, XDG paths
│   │   └── config.go
│   ├── forward/               # Tunnel + Engine: native ssh, reconnect, keepalive
│   │   ├── tunnel.go
│   │   ├── engine.go
│   │   └── ssh.go
│   ├── controller/            # Controller interface + local/remote impls
│   │   ├── controller.go
│   │   ├── local.go           # wrapper around Engine (for standalone)
│   │   └── remote.go          # HTTP client of the daemon (for attach/CLI)
│   ├── daemon/                # HTTP server over unix-socket
│   │   └── server.go
│   ├── client/                # HTTP client with a unix-socket dialer
│   │   └── client.go
│   ├── tui/                   # bubbletea: model/update/view/styles
│   │   ├── model.go
│   │   ├── list.go            # main screen — the list of tunnels
│   │   └── styles.go
│   ├── service/               # autostart, build-tagged per OS
│   │   ├── service.go         # common interface
│   │   ├── service_darwin.go  # launchd
│   │   └── service_linux.go   # systemd --user
│   ├── cmd/                   # cobra commands (extracted from main)
│   │   ├── root.go            # smart launcher
│   │   ├── daemon.go
│   │   ├── attach.go
│   │   ├── list.go
│   │   ├── enable.go
│   │   ├── disable.go
│   │   ├── restart.go
│   │   ├── install.go
│   │   └── uninstall.go
│   └── log/                   # slog setup, log paths in XDG state dir
├── config.example.yaml
├── Makefile                   # build / install-service / cross-compile
└── docs/                      # this GSD set
```

## 5. Controller — the bridge between the TUI and the modes

The TUI does not know whether it is talking to a local Engine or a remote daemon. This is provided by the abstraction:

```go
// internal/controller/controller.go
type Controller interface {
    List() []Status
    Enable(name string) error
    Disable(name string) error
    Restart(name string) error
    Reload() error              // re-read the config from disk
    Changes() <-chan struct{}   // signal "statuses have changed, redraw"
    Close() error
}

type Status struct {
    Name   string
    State  State               // Off | Connecting | Connected | Reconnecting | Error
    Error  string              // human-readable error if State == Error
    Type   string              // "local" (MVP); "remote","dynamic" — post-MVP
    Local  string              // local address
    Remote string              // remote address
    Uptime time.Duration       // since the transition to Connected
}
```

Implementations:
- **`localController`** (`controller/local.go`): wraps `forward.Engine`. `Changes()` — a `tea.Tick` channel at 1s in the MVP; replaced by a push from the Engine in post-MVP.
- **`remoteController`** (`controller/remote.go`): HTTP client of the daemon. `Changes()` — polling `GET /tunnels` once per 1s in the MVP; replaced by SSE/stream in post-MVP (Phase 9).

## 6. IPC (daemon ↔ clients)

- **Transport:** unix domain socket (a file). No TCP ports exposed to the network.
- **Socket path:**
  - Linux: `$XDG_RUNTIME_DIR/portato.sock` (fallback `/run/user/<uid>/portato.sock`).
  - macOS: `$(xdg.RuntimeDir)/portato.sock` (usually `/var/folders/.../T/portato.sock`); if `xdg.RuntimeDir` is empty — `$HOME/.config/portato/portato.sock`.
- **PID file:** next to the socket, `portato.pid`. On daemon startup, check process liveness by PID — protection against a double launch.
- **Protocol:** HTTP over the unix socket (`net.Listen("unix", path)` + `http.Serve`). JSON in request/response bodies.
- **Permissions:** the socket is created with mode `0600`, accessible only to the owner.
- **Endpoints:**

| Method  | Path                              | Action                            |
|---------|-----------------------------------|-----------------------------------|
| `GET`   | `/tunnels`                        | list of statuses                  |
| `POST`  | `/tunnels/{name}/enable`          | enable + persist `enabled=true`   |
| `POST`  | `/tunnels/{name}/disable`         | disable + persist `enabled=false` |
| `POST`  | `/tunnels/{name}/restart`         | down + up                         |
| `POST`  | `/reload`                         | re-read the config from disk      |
| `GET`   | `/healthz`                        | liveness probe (smart-launcher)   |

Post-MVP (Phase 9): add `GET /events` (SSE/chunked) for push updates.

**Key invariant:** every `enable/disable` writes `enabled` back into the YAML config. This is the foundation of the "leave in background" hand-off: the fresh daemon reads the same config and brings up the same set of tunnels.

## 7. Config

Default path (via `xdg.ConfigHome`):

| OS     | Path                                                |
|--------|-----------------------------------------------------|
| macOS  | `~/Library/Application Support/portato/config.yaml`  |
| Linux  | `~/.config/portato/config.yaml`                      |

Overridden by the global `--config` flag.

The IPC socket, PID file, and logs go into `xdg.RuntimeDir` / `xdg.StateHome` respectively.

### Schema

```yaml
defaults:
  identity: ~/.ssh/id_ed25519     # optional; empty → ssh-agent
  known_hosts: ~/.ssh/known_hosts
  accept_new_hosts: false         # TOFU: when true, new hosts are appended to known_hosts

tunnels:
  - name: db-stage                # unique, required
    type: local                   # MVP: only "local"; post-MVP: "remote" | "dynamic"
    local: 5432                   # port or host:port (defaults to 127.0.0.1)
    remote: 10.0.0.5:5432         # destination address relative to the ssh server
    ssh: user@bastion.example.com:22   # required; user and port are optional
    identity: ~/.ssh/id_ed25519   # optional; overrides defaults
    enabled: false                # disabled by default; the daemon persists the toggle here
```

### Authentication

- **Only**: SSH agent (if `SSH_AUTH_SOCK` is set) and/or `identity` files.
- Passwords and passphrases are **never stored in the config**.
- The passphrase for an identity is provided via the agent or an interactive prompt (post-MVP).

## 8. Tunnel types

| Type      | SSH flag | Semantics                                              | Phase     |
|-----------|----------|--------------------------------------------------------|-----------|
| `local`   | `-L`     | `local` (on this machine) → `remote` (on the host)     | **MVP**   |
| `remote`  | `-R`     | listen on the host, forward to `local` on this machine | Phase 7   |
| `dynamic` | `-D`     | SOCKS5 proxy on `local`, traffic through the `host`    | Phase 8   |

Implementation of local in the MVP: `net.Listen(local)` → `ssh.Client.Dial("tcp", remote)` → bidirectional `io.Copy`.

## 9. SSH client (native)

- `ssh.Dial` to the server with an `ssh.ClientConfig`:
  - **Auth:** try `ssh.PublicKeysCallback` from the agent, then `ssh.PublicKeys` from `identity`.
  - **HostKeyCallback:** `knownhosts.New(hostsFile)`; when `accept_new_hosts: true` — a wrapper that appends the new key (TOFU).
  - **Timeout:** an explicit connect timeout (5s).
- Readable errors: `unknown host` / `auth failed` / `connect refused`.

## 10. Reconnect and keepalive

- On a broken SSH session, the tunnel transitions to the `Reconnecting` state.
- Exponential backoff: **1s → 2s → 4s → ... → 30s max**.
- Reset the backoff after **~30s of stable `Connected`**.
- Keepalive: `ssh.Client.SendRequest("keepalive@openssh.com", true, nil)` every 30s; if no reply — transition to `Reconnecting`.
- Manual restart via `r` in the TUI or `portato restart <name>` (Down + Up without backoff).

## 11. TUI (main screen)

```
┌ Portato — Port Forwarding ────────────────────────────────┐
│  mode: standalone                                       │
│                                                         │
│   ●  db-stage    L  5432 → bastion:5432    ● connected   2m │
│   ○  admin       L  8080 → web:80          ○ off            │
│                                                         │
├─────────────────────────────────────────────────────────┤
│ ↑↓ move · space toggle · r restart · R reload · ? help │
└─────────────────────────────────────────────────────────┘
```

### Hotkeys (MVP, Phase 3)

| Key              | Action                                                |
|------------------|-------------------------------------------------------|
| `↑`/`↓`, `j`/`k` | navigate the list                                     |
| `space`          | enable/disable the selected tunnel                    |
| `r`              | restart the selected tunnel                           |
| `a`              | enable all                                            |
| `x`              | disable all                                           |
| `R`              | reload the config from disk                           |
| `?`              | help                                                  |
| `q`              | quit (with the "to background?" modal in standalone when there are live tunnels) |

The header shows the mode: `standalone` or `attach @ <socket>`.

Post-MVP hotkeys: `e/n/d` (Phase 10 — editor), `l` (Phase 11 — logs), `/` (filter/search).

## 12. "Leave in background" hand-off

On exit from standalone mode (`q`):

1. If there are no live (Connecting/Connected/Reconnecting) tunnels → exit immediately with `StopAll()`.
2. If there are live tunnels → modal: `"N tunnels are active. Leave them in the background? [y/N]"`.
3. **`y`**: spawn `portato daemon` as a separate detached process (`exec.Command` + `cmd.Start()`, without waiting). The standalone process periodically (every 100ms, up to a 5s timeout) tries `GET /healthz` on the socket; as soon as it gets a 200 — exit. The tunnels are not disturbed: they are already up in the fresh daemon (it has read `enabled:true` from the config that we just persisted on toggle).
4. **`n`** or `Esc`: `StopAll()` + exit.

MVP limitation: between the daemon's start and readiness there may be a slight flicker on client connections (the tunnel is re-initialized with a fresh SSH session). Post-MVP, tunnel FDs can be passed to the daemon via FD-passing, but this is not critical.

## 13. Autostart

| OS     | Method         | Where we put it                                                    |
|--------|----------------|--------------------------------------------------------------------|
| macOS  | launchd        | `~/Library/LaunchAgents/dev.portato.daemon.plist`, `RunAtLoad=true`, `KeepAlive=true` |
| Linux  | systemd --user | `~/.config/systemd/user/portato.service`, `Restart=on-failure`, lingering enabled |

`portato install` detects the OS and installs the appropriate mechanism; `portato uninstall` does the reverse.
Since tunnels are `enabled: false` by default, only the management daemon is started on system boot.

## 14. Logging

- `log/slog`, level `Info` (configurable via a `--log-level` flag post-MVP).
- Handler: text, writes to `xdg.StateHome/portato/portato.log` + stderr (in daemon mode, only the file + a separate `daemon.log`, in the `StandardOutPath`/`StandardErrorPath` of launchd/systemd).
- Each tunnel gets a sub-logger `log.With("tunnel", name)`.
- Rotation — simple (by size/time), added as needed (Phase 11).

## 15. Non-functional requirements

- **Cross-platform (MVP):** compiles and runs on darwin/amd64, darwin/arm64, linux/amd64, linux/arm64.
- **Single binary:** no external dependencies (the system `ssh` is not required).
- **First-run behavior:** on first launch, if there is no config — a sample is created and the path is shown to the user.
- **Tests:** key packages (`config`, `forward`, `controller`) are covered by unit tests (Phase 1, 2, 6).

## 16. Open questions (to resolve as we go)

- IPC authorization: filesystem permissions only (0600) or a token? → for now, 0600.
- Where to store the passphrase for an identity if the agent is unavailable? (for now: only the agent).
- Passing live SSH FDs to the new daemon during the hand-off (flicker-free transition) — post-MVP.
- Windows support — post-MVP (named pipe + the Run registry key).
