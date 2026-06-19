# SPEC — `portato` technical specification

> `portato` is an SSH port-forwarding manager with a TUI.
> The single source of truth for the stack, architecture, and contracts. Changes rarely.
> The phase workflow is described in [`CONVENTIONS.md`](./CONVENTIONS.md).
> The phase status lives in [`ROADMAP.md`](./ROADMAP.md).

## 1. Goal and scope

- Manage a set of SSH port forwards from a single place (the TUI), like the MCP screen in opencode.
- Turn tunnels on/off interactively (space).
- **Three modes** for a single binary:
  - **smart-launcher** (`portato` with no args): automatically picks attach or standalone;
  - **daemon** (`portato daemon`): a background process holding tunnels + an IPC server;
  - **attach/CLI** (`portato attach`, `portato list/enable/...`): clients to the daemon.
- When quitting standalone mode with live tunnels — a "leave running in the background?" modal with a seamless hand-off to the daemon.
- Cross-platform within the MVP: **macOS + Linux**. Windows is post-MVP.
- Autostart at system boot (launchd / systemd --user); tunnels are **off** by default.

## 2. Stack

| Purpose          | Library                                        |
|------------------|------------------------------------------------|
| Language         | Go 1.25+                                        |
| CLI              | `github.com/spf13/cobra`                       |
| TUI              | `charm.land/bubbletea/v2` + `charm.land/bubbles/v2` + `charm.land/lipgloss/v2` |
| SSH              | `golang.org/x/crypto/ssh` + `golang.org/x/crypto/ssh/knownhosts` (native, no system `ssh`) |
| Config           | `gopkg.in/yaml.v3`                             |
| Paths (XDG)      | `github.com/adrg/xdg`                          |
| Logging          | `log/slog` (standard library)                  |

No system dependency on `ssh` — everything goes through the Go SSH client.

## 3. Operating modes

```
portato                -> smart launcher (root command):
                       ┌─ is the daemon running (socket alive)?
                       │   YES -> attach mode:   remoteController + TUI
                       │   NO  -> standalone mode: localController + TUI
                       │
                       └─ on quit (q) in standalone, if there are live tunnels:
                             "leave running in the background? [y/N]" modal
                               y -> spawn a detached `portato daemon`,
                                    wait for the socket to appear, exit
                               n -> StopAll(), exit

portato daemon         -> background process: Engine + HTTP-over-unix-socket
portato attach         -> explicit TUI client to the daemon (error if the daemon is not running)

portato list           -> CLI: a table of every tunnel's status (stdout)
portato enable <name>  -> CLI: enable a tunnel on the daemon
portato disable <name> -> CLI: disable a tunnel on the daemon
portato restart <name> -> CLI: restart a tunnel

portato install        -> install system autostart (launchd / systemd --user)
portato uninstall      -> remove autostart
portato --config <path> -> custom config path (global flag)
portato --help
```

For `portato` (smart): the daemon's presence is detected by trying to connect to the socket and checking the liveness of the PID from the PID file.

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
│   │   ├── local.go           # wraps Engine (for standalone)
│   │   └── remote.go          # HTTP client to the daemon (for attach/CLI)
│   ├── daemon/                # HTTP server over a unix socket
│   │   └── server.go
│   ├── client/                # HTTP client with a unix-socket dialer
│   │   └── client.go
│   ├── tui/                   # bubbletea: model/update/view/styles
│   │   ├── model.go
│   │   ├── list.go            # main screen — the tunnel list
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
│   └── log/                   # slog setup, log paths in the XDG state dir
├── config.example.yaml
├── Makefile                   # build / install-service / cross-compile
└── docs/                      # this GSD documentation set
```

## 5. Controller — the bridge between the TUI and the modes

The TUI does not know whether it talks to a local Engine or a remote daemon. This is what the abstraction provides:

```go
// internal/controller/controller.go
type Controller interface {
    List() []Status
    Enable(name string) error
    Disable(name string) error
    Restart(name string) error
    Reload() error              // re-read the config from disk
    Changes() <-chan struct{}   // "statuses changed, redraw" signal
    Close() error
}

type Status struct {
    Name   string
    State  State               // Off | Connecting | Connected | Reconnecting | Error
    Error  string              // human-readable error when State == Error
    Type   string              // "local" | "remote" | "dynamic"
    Local  string              // local address
    Remote string              // remote address
    Uptime time.Duration       // since entering Connected
}
```

Implementations:
- **`localController`** (`controller/local.go`): wraps `forward.Engine`. `Changes()` is a channel fed by a `time.Ticker` at 1s in the MVP (the controller does not depend on bubbletea); replaced by a push from the Engine in post-MVP.
- **`remoteController`** (`controller/remote.go`): HTTP client to the daemon. `Changes()` is a `GET /tunnels` poll once per second in the MVP; replaced by SSE/stream in post-MVP (Phase 9).

## 6. IPC (daemon <-> clients)

- **Transport:** a unix domain socket (a file). No TCP ports exposed to the network.
- **Socket path:**
  - Linux: `$XDG_RUNTIME_DIR/portato.sock` (fallback `/run/user/<uid>/portato.sock`).
  - macOS: `$(xdg.RuntimeDir)/portato.sock` (usually `/var/folders/.../T/portato.sock`); if `xdg.RuntimeDir` is empty — `$HOME/.config/portato/portato.sock`.
- **PID file:** next to the socket, `portato.pid`. On daemon startup, verify the process is alive via the PID — protection against double launches.
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

**Key invariant:** every `enable/disable` writes `enabled` back to the YAML config. This is the foundation of the "leave in the background" hand-off: a fresh daemon reads the same config and brings up the same set of tunnels.

## 7. Config

Default path (via `xdg.ConfigHome`):

| OS     | Path                                              |
|--------|---------------------------------------------------|
| macOS  | `~/Library/Application Support/portato/config.yaml`  |
| Linux  | `~/.config/portato/config.yaml`                      |

Overridden by the global `--config` flag.

The IPC socket, PID file, and logs live in `xdg.RuntimeDir` / `xdg.StateHome` respectively.

### Schema

```yaml
defaults:
  identity: ~/.ssh/id_ed25519     # optional; empty -> ssh-agent
  known_hosts: ~/.ssh/known_hosts
  accept_new_hosts: false         # TOFU: when true, new hosts are appended to known_hosts

tunnels:
  - name: db-stage                # unique, required
    type: local                   # "local" (-L), "remote" (-R), or "dynamic" (-D)
    local: 5432                   # port or host:port (defaults to 127.0.0.1)
    remote: 10.0.0.5:5432         # see below — meaning depends on type
    ssh: user@bastion.example.com:22   # required; user and port are optional
    identity: ~/.ssh/id_ed25519   # optional; overrides defaults
    enabled: false                # off by default; the daemon persists toggles here
```

The meaning of `local`/`remote` depends on `type`:

- **`local` (`-L`)**: `local` is listened on this machine; `remote` is the
  destination dialed **on the host**.
- **`remote` (`-R`)**: `remote` is listened **on the host** (a bare port binds
  loopback, the OpenSSH default; a non-loopback host needs `GatewayPorts yes` in
  `sshd_config`); `local` is the address connections are forwarded to here.
- **`dynamic` (`-D`)**: `local` is a SOCKS5 proxy listen address; `remote` is
  unused (ignored). Each connection's destination is taken from the SOCKS
  request and dialed on the host via `ssh.Client.Dial`. No SOCKS auth (loopback
  bind only).

### Authentication

- **Only**: SSH agent (when `SSH_AUTH_SOCK` is set) and/or `identity` files.
- Passwords and passphrases are **never stored in the config**.
- A passphrase for an identity goes through the agent or an interactive prompt (post-MVP).

## 8. Tunnel types

| Type      | SSH flag | Semantics                                            | Phase      |
|-----------|----------|------------------------------------------------------|------------|
| `local`   | `-L`     | `local` (on this machine) -> `remote` (on the host)  | **MVP**    |
| `remote`  | `-R`     | listen on the host, forward to `local` on this machine | **Phase 7** |
| `dynamic` | `-D`     | a SOCKS5 proxy on `local`, traffic via the `host`    | **Phase 8** |

The local implementation in the MVP: `net.Listen(local)` -> `ssh.Client.Dial("tcp", remote)` -> bidirectional `io.Copy`.
The remote implementation (Phase 7): `ssh.Client.Listen("tcp", remote)` -> accept
-> `net.Dial("tcp", local)` -> bidirectional `io.Copy`. The remote listener is
tied to the SSH client's lifetime, so it is re-established on every reconnect;
the dial/backoff/keepalive scaffolding is shared with the local path.
The dynamic implementation (Phase 8): the local listener and accept-loop are
shared with the local path; each accepted connection is handed to a SOCKS5
server (`armon/go-socks5`) whose `Dial` is routed through the current
`ssh.Client`. No `remote` — the destination comes from the SOCKS request.

## 9. SSH client (native)

- `ssh.Dial` to the server with an `ssh.ClientConfig`:
  - **Auth:** try `ssh.PublicKeysCallback` from the agent, then `ssh.PublicKeys` from the `identity`.
  - **HostKeyCallback:** `knownhosts.New(hostsFile)`; with `accept_new_hosts: true` — a wrapper that appends a new key (TOFU).
  - **Timeout:** an explicit connect timeout (5s).
- Readable errors: `unknown host` / `auth failed` / `connect refused`.

## 10. Reconnect and keepalive

- When the SSH session drops, the tunnel enters the `Reconnecting` state.
- Exponential backoff: **1s -> 2s -> 4s -> ... -> 30s max**.
- Backoff resets after **~30s of stable `Connected`**.
- Keepalive: `ssh.Client.SendRequest("keepalive@openssh.com", true, nil)` every 30s; if no answer — transition to `Reconnecting`.
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

| Key            | Action                                                |
|----------------|-------------------------------------------------------|
| `↑`/`↓`, `j`/`k` | navigate the list                                   |
| `space`        | toggle the selected tunnel on/off                     |
| `r`            | restart the selected tunnel                           |
| `a`            | enable all                                            |
| `x`            | disable all                                           |
| `R`            | reload the config from disk                           |
| `?`            | help                                                  |
| `q`            | quit (with the "background?" modal in standalone when there are live tunnels) |

The header shows the mode: `standalone` or `attach @ <socket>`.

Post-MVP hotkeys: `e/n/d` (Phase 10 — editor), `l` (Phase 11 — logs), `/` (filter/search).

## 12. The "leave in the background" hand-off

When quitting standalone mode (`q`):

1. If there are no live (Connecting/Connected/Reconnecting) tunnels -> exit immediately with `StopAll()`.
2. If there are live tunnels -> modal: `"N tunnels active. Leave them in the background? [y/N]"`.
3. **`y`**: standalone first runs `StopAll()` (releases the local ports), then spawns `portato daemon --config <cfg>` as a separate detached process (`exec.Command` + `cmd.Start()`, `Setsid`). The ports are released before the spawn on purpose: `Tunnel.Start` binds its listener synchronously and does not retry a failed bind, so by the time the daemon starts the ports must be free — otherwise the daemon's tunnel falls into `Error` with no recovery. The standalone process periodically (every 100ms, up to a 5s timeout) tries `GET /healthz` on the socket; once it gets a 200, it exits. The fresh daemon reads `enabled: true` from the persisted config (the section 6 invariant) and brings up the same set of tunnels.
4. **`n`** or `enter`: `StopAll()` + exit; **`Esc`**: cancel — close the modal and return to the list (without stopping the tunnels and without exiting).

MVP limitation: between the standalone `StopAll()` and the daemon rebinding/SSH-handshaking the tunnels, there is a window (~hundreds of ms — seconds) during which the local port is unavailable. This is an MVP compromise. Post-MVP — passing the tunnels' FDs to the daemon via FD-passing (a seamless transition).

## 13. Autostart

| OS     | Method          | Where we put it                                                     |
|--------|-----------------|---------------------------------------------------------------------|
| macOS  | launchd         | `~/Library/LaunchAgents/dev.portato.daemon.plist`, `RunAtLoad=true`, `KeepAlive=true` |
| Linux  | systemd --user  | `~/.config/systemd/user/portato.service`, `Restart=on-failure`, lingering enabled |

`portato install` detects the OS and installs the right mechanism; `portato uninstall` reverses it.
Since tunnels are `enabled: false` by default, at system boot **only** the control daemon is brought up.

## 14. Logging

- `log/slog`, level `Info` (configurable via a `--log-level` flag post-MVP).
- Handler: text, writes to `xdg.StateHome/portato/portato.log` + stderr (in daemon mode, only the file + a separate `daemon.log`, in the `StandardOutPath`/`StandardErrorPath` of launchd/systemd).
- Each tunnel gets a sub-logger `log.With("tunnel", name)`.
- Rotation is simple (size/time), added as needed (Phase 11).

## 15. Non-functional requirements

- **Cross-platform (MVP):** compiles and runs on darwin/amd64, darwin/arm64, linux/amd64, linux/arm64.
- **Single binary:** no external dependencies (the system `ssh` is not required).
- **Startup behavior:** on the first run, if there is no config — an example is created and the path is shown to the user.
- **Tests:** the key packages (`config`, `forward`, `controller`) are covered by unit tests (Phases 1, 2, 6).

## 16. Open questions (to resolve as we go)

- IPC authorization: only filesystem permissions (0600) or a token? -> 0600 for now.
- Where to store a passphrase for an identity when the agent is unavailable? (for now: agent only).
- Passing live SSH FDs to the new daemon during hand-off (a seamless transition) — post-MVP.
- Windows support — post-MVP (named pipe + the registry Run key).
