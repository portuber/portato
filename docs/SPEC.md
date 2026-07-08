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
                          `--json`: one JSON document (machine-readable, Phase 20)
portato enable <name>  -> CLI: enable a tunnel on the daemon
portato disable <name> -> CLI: disable a tunnel on the daemon
portato restart <name> -> CLI: restart a tunnel

portato install        -> install system autostart (launchd / systemd --user)
portato uninstall      -> remove autostart
portato add-identity <path>     -> store an SSH identity passphrase in the OS keyring (Phase 19)
portato forget-identity <path>  -> remove a stored identity passphrase (Phase 19)
portato --config <path> -> custom config path (global flag)
portato --log-level <l> -> debug|info|warn|error (global, Phase 20; default info)
portato --socket <path> -> override the daemon IPC socket (global)
portato --help
portato --version       -> print the logo banner + version/commit/date and exit (pipe-safe)
```

For `portato` (smart): the daemon's presence is detected by reading the discovery marker (§6) for its socket path and PID, then probing the socket.

> **Easter egg (Phase 25):** `portato --help` and `portato help` end with the
> line `And please, pórtate bien` — the Spanish imperative *¡pórtate bien!*
> ("behave yourself!"), a near-homophone of the brand *portato*. The potato emoji 🥔
> is appended only when the terminal is emoji-capable (the §11 logo gate:
> `GOOS=darwin` default, `PORTATO_LOGO_EMOJI=on|off` override, off under
> `PORTATO_LOGO=off`). Subcommand `--help` output is unchanged.

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
- **Socket activation (Phase 22):** under systemd the service manager can own the
  listening socket and hand it to the daemon. `portato install` writes a
  `portato.socket` unit (`ListenStream=/run/user/<uid>/portato-<uid>.sock`,
  `SocketMode=0600`) and a `portato.service` that `Requires`+`After`s it; when
  started, systemd passes the bound socket via `LISTEN_FDS` and the daemon serves
  on it instead of binding (it still runs at boot to manage enabled tunnels).
  Off activation (or non-Linux) the daemon self-binds as before. launchd socket
  activation would need a libc call (`launch_activate_socket_fd`) that requires
  cgo, incompatible with the pure-Go single binary, so macOS stays bind-on-start.
- **Authorization (Phase 18):** layered on top of the `0600` socket, the daemon
  authenticates every IPC request with a bearer token. At startup it generates
  a 32-byte (`crypto/rand`) token, writes it hex-encoded to
  `<socketDir>/portato.token` (mode `0600`, atomically next to the unix socket
  it binds), and rejects any request whose `Authorization: Bearer <token>`
  header does not match with `401` (constant-time compare, `healthz` included).
  Clients read the token best-effort from that path and attach the header
  automatically (one `RoundTripper`); the discovery `healthz` probe does too, so
  liveness checks still reach an authenticated daemon. A missing token file
  (an older daemon, or the escape hatch) means no header and an open daemon
  answers `200` — backward compatible on both ends. `--ipc-token off`
  (or `PORTATO_NO_IPC_TOKEN=1`) is the break-glass hatch: no token is
  generated and the daemon serves openly over the `0600` socket.
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
  socks5_user: alice              # optional (Phase 20): default SOCKS5 user/pass
  socks5_password: $secret        # for type=dynamic tunnels; empty -> NoAuth
  identity_passphrase_store: false # opt-in (Phase 19): persist identity passphrases in the OS keyring
  log:                            # optional (Phase 22): persistent log-rotation knobs
    max_size_mb: 1                # rotate the log file at this size; 0 -> default (1 MiB)
    max_age_days: 0               # purge rotated archives older than N days; 0 -> disabled
    retain: 3                     # rotated archives to keep (.1 .. .retain); 0 -> default (3)

tunnels:
  - name: db-stage                # unique, required
    type: local                   # "local" (-L), "remote" (-R), or "dynamic" (-D)
    local: 5432                   # port or host:port (defaults to 127.0.0.1)
    remote: 10.0.0.5:5432         # see below — meaning depends on type
    ssh: user@bastion.example.com:22   # required; user and port are optional
    identity: ~/.ssh/id_ed25519   # optional; overrides defaults
    enabled: false                # off by default; the daemon persists toggles here
    # socks5_user / socks5_password (Phase 20): per-tunnel override of defaults,
    # honoured only by type=dynamic. Both empty (after fallback) -> NoAuth.
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
  request and dialed on the host via `ssh.Client.Dial`. Optional SOCKS5
  user/pass authentication (Phase 20): `socks5_user`/`socks5_password` (tunnel
  or defaults) make the proxy require `UserPass`; when both resolve empty the
  proxy is open (NoAuth, loopback bind only).

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
Phase 20 adds optional SOCKS5 user/pass auth: a resolved
(`tunnels[].socks5_*` over `defaults.socks5_*`) non-empty user+pass pair is
installed as a `StaticCredentials` store, switching the proxy to `UserPass`;
otherwise NoAuth (the pre-Phase-20 behaviour).

## 9. SSH client (native)

- `ssh.Dial` to the server with an `ssh.ClientConfig`:
  - **Auth:** try `ssh.PublicKeysCallback` from the agent, then `ssh.PublicKeys` from the `identity`.
  - **Passphrase-protected identity (Phase 19):** if `ssh.ParsePrivateKey` reports a missing passphrase, the dial obtains one from the passphrase store (`internal/secret` — an in-memory cache backed by the OS keyring) and retries with `ssh.ParsePrivateKeyWithPassphrase`. With none available it surfaces `Status.PendingPassphrase` and **blocks** (the store's `Wait`) until the TUI/CLI provides one, instead of spinning the reconnect backoff. A wrong passphrase is invalidated and re-prompted.
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
| `p`            | enter the passphrase for a passphrase-blocked selected tunnel (manual affordance; the modal also auto-opens on block); `space` always toggles (Phase 30) |
| `r`            | restart the selected tunnel                           |
| `a` / `x`      | enable all / disable all                              |
| `e` / `n` / `d`| edit / create / delete the selected tunnel            |
| `C`            | duplicate the selected tunnel (under `<name>-copy`)   |
| `l`            | view the selected tunnel's logs                       |
| `/`            | fuzzy (subsequence) filter over name/type/endpoint; `esc` clears (Phase 20; substring fallback) |
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

### Branding / logo

The potato logo appears in three places — never on the working screen:

- **empty-list splash** — when the tunnel list is empty and the terminal is
  tall enough (≥ ~18 rows), the centered logo sits above the "no tunnels"
  hint; a short terminal shows the hint only.
- **help (`?`) overlay** — the compact logo is prepended above the hotkey list
  (same height gate).
- **`portato --version`** — the logo banner followed by a
  `portato <version> (<commit>, <date>)` line. Pipe-safe: when stdout is not a
  terminal the inline image and all ANSI are suppressed and the braille
  variant is used, so `portato --version | head` stays clean.

A small potato emoji 🥔 marks the header before the title, on `GOOS=darwin`
only (where it renders cleanly at 2 cells); `PORTATO_LOGO_EMOJI=on|off`
overrides it, and `PORTATO_LOGO=off` hides it too.

Rendering picks the best the terminal supports (override with
`PORTATO_LOGO=auto|image|braille|block|off`):

| Mode    | When                                                               |
|---------|--------------------------------------------------------------------|
| image   | iTerm2 / WezTerm (`TERM_PROGRAM`) — inline PNG via OSC 1337.       |
| braille | default on macOS (Terminal.app) and Linux — outline-braille ASCII. |
| block   | `GOOS=windows` — solid block (robust on legacy conhost).           |
| off     | no big logo anywhere, no header emoji.                             |

All variants are 28×12 cells; the ASCII glyphs are tinted with the theme's
title accent, except under the mono theme / `NO_COLOR` (plain glyphs). The
assets are `go:embed`ded in `internal/logo/`, so the binary needs nothing on
disk.

## 12. The "leave in the background" hand-off

When quitting standalone mode (`q`):

1. If there are no live (Connecting/Connected/Reconnecting) tunnels -> exit immediately with `StopAll()`.
2. If there are live tunnels -> modal: `"N tunnels active. Leave them in the background? [y/N]"`.
3. **`y`**: the standalone moves its live tunnels to a detached daemon via the **seamless FD hand-off** (Phase 16) when possible, falling back to the close+rebind path otherwise:
   - **Seamless (default):** for each live `local`/`dynamic` tunnel the standalone dups its already-bound local listener (`(*net.TCPListener).File`), opens a one-shot SOCK_STREAM unix transfer socket, spawns `portato daemon --config <cfg> --listen-fds <sockpath>` (detached, `Setsid`) and sends the dup'd fds (SCM_RIGHTS) over the transfer socket. The daemon reconstructs each listener (`net.FileListener`) and adopts it — skipping its own `net.Listen` — so the kernel listening socket never closes: the standalone's and the daemon's dup'd fds reference the same socket, and the standalone closes its copy only after the daemon's `GET /healthz` answers. The local port therefore stays continuously available across the transition. The established SSH session is **not** moved — `golang.org/x/crypto/ssh` keeps the transport's crypto state in process memory and cannot resume it in another process — so the daemon re-dials; `type=remote` tunnels have no local listener and simply re-dial.
   - **Fallback** (no live local listeners, or any pre-spawn step fails): the standalone runs `StopAll()` (releasing the local ports), spawns the daemon without `--listen-fds`, and waits for `healthz`. The brief gap between release and the daemon's rebind is the legacy MVP blip.

   In both paths the standalone probes the advertised socket (§6) with `GET /healthz` every 100ms up to a 5s timeout, then exits; the fresh daemon reads `enabled: true` from the persisted config (the §6 invariant) and brings up the same set of tunnels.
4. **`n`** or `enter`: `StopAll()` + exit; **`Esc`**: cancel — close the modal and return to the list (without stopping the tunnels and without exiting).

Limitation: the seamless hand-off preserves continuous **local-port** availability (a `nc -z` to the local port never fails across the transition), but the underlying SSH session is re-established by the daemon — so a forwarded connection in flight at the moment of the hand-off is dropped (its SSH channel dies with the standalone's `*ssh.Client`); only new connections are seamless. This is a fundamental limit of FD-passing with `golang.org/x/crypto/ssh` (no cross-process session resume), not a defect.

## 13. Autostart

| OS     | Method          | Where we put it                                                     |
|--------|-----------------|---------------------------------------------------------------------|
| macOS  | launchd         | `~/Library/LaunchAgents/dev.portato.daemon.plist`, `RunAtLoad=true`, `KeepAlive=true` |
| Linux  | systemd --user  | `~/.config/systemd/user/portato.service` (+ `portato.socket`), `Restart=on-failure`, lingering enabled |

`portato install` detects the OS and installs the right mechanism; `portato uninstall` reverses it.
Since tunnels are `enabled: false` by default, at system boot **only** the control daemon is brought up.

On Linux `install` also writes and enables `portato.socket` (Phase 22 socket
activation); the service `Requires`+`After`s it so systemd hands the daemon the
pre-bound IPC socket via `LISTEN_FDS`. macOS launchd does **not** get a `Sockets`
dict: claiming the handed fd needs a libc call that would require cgo, breaking
the pure-Go single binary — socket activation there is deferred.

## 14. Logging

- `log/slog`, level `Info` by default. The root persistent flag `--log-level
  debug|info|warn|error` (Phase 20) sets the file handler's threshold, so
  `--log-level debug` surfaces debug lines in the log file and `error` silences
  info. The in-memory ring (see below) keeps capturing at Debug independently,
  so the TUI logs screen's debug toggle works regardless of the flag.
- Handler: text, writes to `xdg.StateHome/portato/portato.log` + stderr (in daemon mode, only the file + a separate `daemon.log`, in the `StandardOutPath`/`StandardErrorPath` of launchd/systemd).
- Each tunnel gets a sub-logger `log.With("tunnel", name)`.
- The slog handler also feeds an in-memory ring buffer (Phase 11) so the TUI
  logs screen (`l`) can show recent per-tunnel entries without reading the
  file; in attach mode they are fetched over `GET /logs`.
- Rotation (Phase 13, config-driven in Phase 22): the file is a size-capped
  rotating writer (`internal/log` `RotatingWriter`) so a long-running daemon's
  log stays bounded — `portato.log`/`daemon.log` → `.log.1` → `.2` → `.3`
  (oldest dropped). The knobs are operator-tunable via the `defaults.log.*`
  block: `max_size_mb` (rotate at this size; default 1 MiB), `retain` (archives
  to keep; default 3), and `max_age_days` (purge archives older than N days at
  rotation; 0 disables). Age never triggers a rotation — the trigger stays
  size-driven. `portato doctor` reports the path and the last rotation.

## 15. Non-functional requirements

- **Cross-platform (MVP):** compiles and runs on darwin/amd64, darwin/arm64, linux/amd64, linux/arm64.
- **Single binary:** no external dependencies (the system `ssh` is not required).
- **Startup behavior:** on the first run, if there is no config — an example is created and the path is shown to the user.
- **Tests:** the key packages (`config`, `forward`, `controller`) are covered by unit tests (Phases 1, 2, 6).

## 16. Open questions (to resolve as we go)

- IPC authorization: only filesystem permissions (0600) or a token? -> **resolved (Phase 18)**: a 32-byte bearer token in `<socketDir>/portato.token`, layered on the `0600` socket; `--ipc-token off` disables it. See §6.
- Where to store a passphrase for an identity when the agent is unavailable? -> **resolved (Phase 19)**: an in-memory cache (per process, so reconnects don't re-prompt) plus the OS keyring (macOS Keychain / Linux Secret Service / Windows Credential Manager via `zalando/go-keyring`) for cross-restart persistence. Opt-in keyring persistence via `defaults.identity_passphrase_store` (off by default); explicit `portato add-identity`/`forget-identity` always write/clear the keyring. Nothing is ever written to disk in plaintext. See §9.
- Passing live listener FDs to the new daemon during hand-off (a seamless transition) -> **resolved (Phase 16)**: the standalone dups its local listeners and sends them (SCM_RIGHTS) over a one-shot transfer socket; the daemon adopts them via `net.FileListener`, so the local ports never go down. The SSH session itself is re-dialed (no cross-process resume in `golang.org/x/crypto/ssh`); only local-port availability is seamless. See §12.
- Windows support — post-MVP (named pipe + the registry Run key).
