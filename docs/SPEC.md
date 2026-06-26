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

For `portato` (smart): the daemon's presence is detected by reading the discovery marker (§6) for its socket path and PID, then probing the socket.

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
- **`localController`** (`controller/local.go`): wraps `forward.Engine`. `Changes()` forwards the Engine's event broker — every tunnel state transition pushes a signal through an owned, drop-old channel (Phase 9).
- **`remoteController`** (`controller/remote.go`): HTTP client to the daemon. `Changes()` reads the daemon's `GET /events` SSE stream and reconnects with exponential backoff on a stream break (Phase 9).

## 6. IPC (daemon <-> clients)

- **Transport:** a unix domain socket (a file). No TCP ports exposed to the network.
- **Socket discovery (Phase 12):** the daemon's socket lives in a semantically
  correct but *session-variable* runtime location, so the daemon advertises its
  actual path via a stable discovery marker that every client reads instead of
  guessing. Daemon and clients therefore always agree regardless of which
  shell/session launched them.
  - **Discovery marker** (the pointer, not the socket):
    `xdg.ConfigHome/portato/daemon.socket` — a small JSON document
    `{"socket":"<path>","pid":<int>}`, written atomically (tmp + rename),
    mode `0600`. Stable and env-independent.
  - **Socket** (the thing that is listened on), under a runtime/temp dir,
    uid-scoped to avoid collisions:
    - Linux: `$XDG_RUNTIME_DIR/portato-<uid>.sock` (`/run/user/<uid>`, a per-user
      tmpfs set by systemd/logind; falls back to `os.TempDir()` when unset).
    - macOS: `$TMPDIR/portato-<uid>.sock` (via `os.TempDir()`); macOS has no
      reliable per-user runtime dir (`XDG_RUNTIME_DIR` is not set by the OS and
      varies across terminal/tmux sessions), which is exactly why the marker is
      needed — the socket path differs per session but the marker always points
      at the live one.
  - **Liveness:** the source of truth is a `GET /healthz` probe, not the PID.
    A client reads the marker and probes the socket it advertises; if it
    answers, that path is used. A marker whose socket is silent is stale: when
    the owning PID is also gone (e.g. the daemon was `kill -9`'d) the marker
    and the leftover socket are removed, while a still-living PID (a wedged
    daemon) is left untouched. If the marker is absent or corrupt, the client
    falls back to probing the canonical runtime socket path directly — so a
    daemon that lost its marker (a misled client deleted it, schema drift, a
    crash) stays reachable instead of being reported "not running". Stale
    cleanup never deletes a socket that still answers, so a reused PID cannot
    evict a live daemon.
- **Override:** `--socket <path>` (or the `PORTATO_SOCKET` env var) bypasses
  discovery — the daemon binds the given path and clients dial it directly.
  Intended for tests and CI.
- **Protocol:** HTTP over the unix socket (`net.Listen("unix", path)` + `http.Serve`). JSON in request/response bodies.
- **Permissions:** the socket is created with mode `0600`, accessible only to the owner.
- **Endpoints:**

| Method   | Path                              | Action                            |
|----------|-----------------------------------|-----------------------------------|
| `GET`    | `/tunnels`                        | list of statuses                  |
| `POST`   | `/tunnels/{name}/enable`          | enable + persist `enabled=true`   |
| `POST`   | `/tunnels/{name}/disable`         | disable + persist `enabled=false` |
| `POST`   | `/tunnels/{name}/restart`         | down + up                         |
| `POST`   | `/reload`                         | re-read the config from disk      |
| `GET`    | `/events`                         | SSE stream of state-change signals (Phase 9) |
| `GET`    | `/config`                         | the current config (JSON) — for the TUI editor (Phase 10) |
| `POST`   | `/tunnels`                        | add a tunnel (validate, persist, reload) — Phase 10 |
| `PUT`    | `/tunnels/{name}`                 | replace a tunnel (rename allowed) — Phase 10 |
| `DELETE` | `/tunnels/{name}`                 | remove a tunnel (active one is stopped) — Phase 10 |
| `GET`    | `/logs?name=`                     | recent in-memory log entries for a tunnel (Phase 11 TUI logs screen) |
| `POST`   | `/tunnels/{name}/accept-host`     | append the tunnel's pending unknown-host key + restart (Phase 11 TOFU) |
| `GET`    | `/healthz`                        | liveness probe (smart-launcher)   |

`GET /events` (Phase 9) is a `text/event-stream`: the daemon subscribes a
client to the Engine's event broker and writes a signal-only `data: {}` frame
on every tunnel state change (plus one initial frame on connect and a 15s
heartbeat comment). The client reacts by re-fetching `GET /tunnels`. This
replaces the former 1s polling — an idle attached client issues no periodic
requests.

The Phase 10 config-editing endpoints (`GET /config`, `POST/PUT/DELETE
/tunnels`) make the daemon the single owner of config writes: an attached TUI
never touches the YAML directly, so a custom `--config` path on the daemon is
respected and concurrent clients cannot race. Persist is comment-preserving
(the file is edited as a `yaml.Node` tree, so comments on untouched tunnels
and on `defaults:` survive). Every mutation validates a prospective in-memory
config first, then patches the file, then reloads — on a validation error the
file is left untouched and a 4xx is returned.

**Key invariant:** every `enable/disable` writes `enabled` back to the YAML config. This is the foundation of the "leave in the background" hand-off: a fresh daemon reads the same config and brings up the same set of tunnels.

## 7. Config

Default path (via `xdg.ConfigHome`):

| OS     | Path                                              |
|--------|---------------------------------------------------|
| macOS  | `~/Library/Application Support/portato/config.yaml`  |
| Linux  | `~/.config/portato/config.yaml`                      |

Overridden by the global `--config` flag.

The IPC socket lives in a runtime/temp dir (see §6); its path is advertised via
a discovery marker under `xdg.ConfigHome`. Logs live in `xdg.StateHome`.

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
- **`remote` (`-R`)**: `remote` is listened **on the host**. A bare port or
  `:port` binds all interfaces via the `"*"` wildcard (`*:port`, the default —
  the common "expose my local service through the server" case); an explicit
  host is used as written (`127.0.0.1:port` for loopback-only, `0.0.0.0:port`,
  `[::]:port`, a public IP). Any non-loopback bind requires
  `GatewayPorts yes|clientspecified` in `sshd_config`; `local` is the address
  connections are forwarded to here.
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
the dial/backoff/keepalive scaffolding is shared with the local path. A bare
port or `:port` in `remote` is normalised to `*:port` (all interfaces); a
non-loopback bind needs `GatewayPorts yes|clientspecified` on the server.
The dynamic implementation (Phase 8): the local listener and accept-loop are
shared with the local path; each accepted connection is handed to a SOCKS5
server (`armon/go-socks5`) whose `Dial` is routed through the current
`ssh.Client`. No `remote` — the destination comes from the SOCKS request.

## 9. SSH client (native)

- `ssh.Dial` to the server with an `ssh.ClientConfig`:
  - **Auth:** try `ssh.PublicKeysCallback` from the agent, then `ssh.PublicKeys` from the `identity`.
  - **HostKeyCallback:** `knownhosts.New(hostsFile)`; with `accept_new_hosts: true` — a wrapper that appends a new key (TOFU).
  - **Timeout:** an explicit connect timeout (5s).
- Readable errors: `host key not in known_hosts` / `auth failed` / `connect refused` / `connect timeout`.

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

### Hotkeys

| Key            | Action                                                |
|----------------|-------------------------------------------------------|
| `↑`/`↓`, `j`/`k` | navigate the list                                   |
| `space`        | toggle the selected tunnel on/off                     |
| `r`            | restart the selected tunnel                           |
| `a` / `x`      | enable all / disable all                              |
| `e` / `n` / `d`| edit / create / delete the selected tunnel            |
| `C`            | duplicate the selected tunnel (under `<name>-copy`)   |
| `l`            | view the selected tunnel's logs                       |
| `/`            | substring filter over name/type/endpoint; `esc` clears |
| `R`            | reload the config from disk                           |
| `?` / `esc`    | toggle help (`esc` also clears an active filter and cancels a confirm modal) |
| `q` / `ctrl+c` | quit (with the "background?" modal in standalone when there are live tunnels) |

The header shows the mode: `standalone` or `attach @ <socket>`.

### Sub-screen keys

The `e`/`n`/`C` editor, the `l` logs screen, and the `/` filter each take over
key handling while open; `esc` returns to the list (the filter's `esc` also
clears the query).

| Screen         | Keys                         | Action                          |
|----------------|------------------------------|---------------------------------|
| Editor (`e`/`n`/`C`) | `tab` / `enter`, `shift+tab` | next / previous field     |
|                | `←` / `→` (on the Type field)| change the tunnel type          |
|                | `ctrl+s`                     | save                            |
|                | `esc`                        | cancel                          |
| Logs (`l`)     | `↑`/`↓`, `j`/`k`, `pgup`/`pgdn` | scroll                       |
|                | `g` / `G`                    | jump to top / bottom            |
|                | `L`                          | toggle the debug level          |
|                | `esc` / `l` / `q`            | close                           |
| Filter (`/`)   | type to filter live; `backspace` edits the query |               |
|                | `enter`                      | close the input, keep the filter |
|                | `esc`                        | clear the filter and close      |

## 12. The "leave in the background" hand-off

When quitting standalone mode (`q`):

1. If there are no live (Connecting/Connected/Reconnecting) tunnels -> exit immediately with `StopAll()`.
2. If there are live tunnels -> modal: `"N tunnels active. Leave them in the background? [y/N]"`.
3. **`y`**: standalone first runs `StopAll()` (releases the local ports), then spawns `portato daemon --config <cfg>` as a separate detached process (`exec.Command` + `cmd.Start()`, `Setsid`). The ports are released before the spawn on purpose: `Tunnel.Start` binds its listener synchronously and does not retry a failed bind, so by the time the daemon starts the ports must be free — otherwise the daemon's tunnel falls into `Error` with no recovery. The standalone process periodically (every 100ms, up to a 5s timeout) reads the discovery marker (§6) and probes the advertised socket with `GET /healthz`; once it gets a 200, it exits. The fresh daemon reads `enabled: true` from the persisted config (the section 6 invariant) and brings up the same set of tunnels.
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
- The slog handler also feeds an in-memory ring buffer (Phase 11) so the TUI
  logs screen (`l`) can show recent per-tunnel entries without reading the
  file; in attach mode they are fetched over `GET /logs`.
- Rotation (Phase 13): the file is a size-capped rotating writer
  (`internal/log` `RotatingWriter`, ~1 MiB, 3 archives) so a long-running
  daemon's log stays bounded — `portato.log`/`daemon.log` → `.log.1` → `.2`
  → `.3` (oldest dropped). `portato doctor` reports the path and the last
  rotation.

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
