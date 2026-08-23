# SPEC — `portato` technical specification

> `portato` is an SSH port-forwarding manager with a TUI.
> The single source of truth for the stack, architecture, and contracts. Changes rarely.
> The phase workflow is described in [`CONVENTIONS.md`](./CONVENTIONS.md).
> The phase status lives in [`ROADMAP.md`](./ROADMAP.md).

## 1. Goal and scope

- Manage a set of SSH port forwards from a single place (the TUI), like the MCP screen in opencode.
- Reach hosts behind a bastion via `jump:` (ProxyJump / OpenSSH `-J`).
- Reuse a `~/.ssh/config` Host alias as `ssh:` (HostName/User/Port/IdentityFile/ProxyJump resolved).
- Organise tubers with `tags:` — `enable|disable|restart --tag X`, a `#tag` filter in the TUI, and `a` / `x` over a filtered view.
- Turn tunnels on/off interactively (space).
- **Three modes** for a single binary:
  - **smart-launcher** (`portato` with no args): automatically picks attach or standalone;
  - **daemon** (`portato daemon`): a background process holding tunnels + an IPC server;
  - **attach/CLI** (`portato attach`, `portato list/enable/...`): clients to the daemon.
- When quitting standalone mode with live tunnels — a "leave running in the background?" modal with a seamless hand-off to the daemon.
- Cross-platform: **macOS + Linux** (MVP); **Windows** (Phase 17 + 47 — named-pipe IPC + SCM service autostart).
- Autostart at system boot (launchd / systemd --user); tunnels are **off** by default.
- Shell TAB completion of subcommands and tuber names (`bash`/`zsh`/`fish`/`powershell`).

## 2. Stack

| Purpose          | Library                                        |
|------------------|------------------------------------------------|
| Language         | Go 1.25+                                        |
| CLI              | `github.com/spf13/cobra`                       |
| TUI              | `charm.land/bubbletea/v2` + `charm.land/bubbles/v2` + `charm.land/lipgloss/v2` |
| SSH              | `golang.org/x/crypto/ssh` + `golang.org/x/crypto/ssh/knownhosts` (native, no system `ssh`) |
| Config           | `gopkg.in/yaml.v3`                             |
| Paths (XDG)      | `github.com/adrg/xdg`                          |
| IPC (Windows)    | `github.com/Microsoft/go-winio` (named pipe)   |
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
portato reload         -> CLI: force the running daemon to re-read config.yaml (Phase 28)
portato stop           -> CLI: gracefully stop the running daemon — SIGTERM via the marker PID (Phase 27)

portato install        -> install system autostart (launchd / systemd --user)
portato uninstall      -> remove autostart
portato add-identity <path>     -> store an SSH identity passphrase in the OS keyring (Phase 19)
portato forget-identity <path>  -> remove a stored identity passphrase (Phase 19)
portato --config <path> -> custom config path (global flag)
portato --log-level <l> -> debug|info|warn|error (global, Phase 20; default info)
portato --socket <path> -> override the daemon IPC socket (global)
portato --help
portato --version       -> print the logo banner + version/commit/date and exit (pipe-safe)
portato license         -> print license info: MIT + source URL + pointer to bundled THIRD_PARTY_LICENSES.txt
portato license --full  -> also print the full MIT License text (embedded in the binary)
portato --license       -> print the license summary and exit (pipe-safe; parallel to --version)
portato completion <shell> -> emit a TAB-completion script (bash|zsh|fish|powershell); source it so
                           enable/disable/restart/forward <name> and logs --tuber complete tuber
                           names from config.yaml (no daemon needed)
portato update check    -> CLI: fetch the latest GitHub release and compare with the running
                           binary (explicit, ignores consent and cache age; exit 0 on "available"
                           and "up to date", non-zero only on error) (Phase 49)
portato update consent <on|off|ask>
                       -> set defaults.update_check: on = daily background checks, off = never,
                           ask = forget the answer, re-ask on the next interactive launch (Phase 49)
portato update apply   -> CLI: download + verify (SHA-256 vs checksums.txt) + swap the binary in
                           place, keeping a one-level portato.old rollback; package-managed
                           installs get their own upgrade command instead (Phase 50).
                           Flags: --dry-run, --yes (non-TTY), --force (never overrides the
                           checksum; never overrides a Windows SCM-held binary)
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
portato/
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
- **`localController`** (`controller/local.go`): wraps `forward.Engine`. `Changes()` forwards the Engine's event broker — every tunnel state transition pushes a signal through an owned, drop-old channel (Phase 9). The standalone launcher calls its `StartEnabled` right after construction so every `enabled: true` tunnel is up on launch, matching the daemon's boot-time `StartEnabledWith` (§6).
- **`remoteController`** (`controller/remote.go`): HTTP client to the daemon. `Changes()` reads the daemon's `GET /events` SSE stream and reconnects with exponential backoff on a stream break (Phase 9).

## 6. IPC (daemon <-> clients)

- **Transport:** a unix domain socket (a file) on darwin/linux; a **named pipe** (`\\.\pipe\portato`) on Windows. No TCP ports exposed to the network.
- **Socket discovery (Phase 12):** the daemon's socket lives in a semantically
  correct but *session-variable* runtime location, so the daemon advertises its
  actual path via a stable discovery marker that every client reads instead of
  guessing. Daemon and clients therefore always agree regardless of which
  shell/session launched them.
  - **Discovery marker** (the pointer, not the socket):
    `xdg.ConfigHome/portato/daemon.socket` — a small JSON document
    `{"socket":"<path>","pid":<int>}`, written atomically (tmp + rename),
    mode `0600`. Stable and env-independent.
  - **Socket** (the thing that is listened on), uid-scoped to avoid collisions:
    - Linux: `$XDG_RUNTIME_DIR/portato-<uid>.sock` (`/run/user/<uid>`, a per-user
      tmpfs set by systemd/logind; falls back to `os.TempDir()` when unset).
    - macOS: `~/Library/Application Support/portato/portato-<uid>.sock` (the XDG
      state home). macOS has no reliable per-user runtime dir (`XDG_RUNTIME_DIR`
      is not set by the OS and varies across terminal/tmux sessions). The earlier
      choice of `$TMPDIR` was unstable: macOS periodically reaps and rotates
      `$TMPDIR`, unlinking the socket file under a running daemon and *wedging*
      it (alive — holding the single-instance lock and the local ports — but
      unreachable, since the listener fd stays open on the orphaned inode). The
      state home is stable and owner-only, so the socket survives temp-dir
      cleanup (Phase 40). The marker still resolves the per-session variance
      that remains on Linux, and `portato stop`/`doctor` recover from / diagnose
      a wedged daemon by its marker PID when the rare reap + restart wedges it.
    - Windows: a named pipe `\\.\pipe\portato` (no socket file). The marker
      still records it as the `socket` field; the PID and the IPC token live in
      `%LOCALAPPDATA%\portato\` (the pipe has no sibling file to chmod).
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
  On Windows the named pipe is the equivalent: an explicit security descriptor
  (SDDL) grants `GENERIC_ALL` to SYSTEM, Administrators, and the process
  token's user SID — so the pipe is reachable both from the boot-started SCM
  service (session 0) and from an unelevated interactive session of the same
  user (v1.6.1; a nil descriptor had let the service token's default DACL
  deny the interactive session). The Phase 18 bearer token still gates the
  protocol on top.
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

The daemon also persists its log to a size-rotated file under the XDG state
home — `daemon.log` for `portato daemon`, `portato.log` for the standalone
TUI — capped at `defaults.log.max_size_mb` with `retain` archives
(`.1`/`.2`/`.3`). The `GET /logs` ring above is the TUI's short live window;
for the full history from the shell, `portato logs` reads the persisted file
directly (no running daemon required), with `-f/--follow`, `-n/--lines`,
`--since`, `--tuber`, and `--all` (archives).

The Phase 10 config-editing endpoints (`GET /config`, `POST/PUT/DELETE
/tunnels`) make the daemon the single owner of config writes: an attached TUI
never touches the YAML directly, so a custom `--config` path on the daemon is
respected and concurrent clients cannot race. Persist is comment-preserving
(the file is edited as a `yaml.Node` tree, so comments on untouched tunnels
and on `defaults:` survive). Every mutation validates a prospective in-memory
config first, then patches the file, then reloads — on a validation error the
file is left untouched and a 4xx is returned.

**Key invariant:** every `enable/disable` writes `enabled` back to the YAML config. This is the foundation of the "leave in the background" hand-off: a fresh daemon reads the same config and brings up the same set of tunnels.

**Auto-start of enabled tunnels (both modes):** the daemon calls `engine.StartEnabledWith` at boot, and the standalone launcher calls the local controller's `StartEnabled` right after building it — so in *both* modes every `enabled: true` tunnel is up from the first moment. A hand-off to the daemon therefore starts exactly what the standalone already had running; no new tunnels appear. An enabled tunnel that cannot connect (no network, bad host, unknown host key) surfaces as `Reconnecting`/`Error` — the Engine's existing behaviour — rather than being silently skipped.

**Config reload (Phase 28):** the daemon watches `config.yaml` for changes and
applies an edit within ~1s without a restart, over the same `applyReload` path
as `POST /reload` and the `portato reload` CLI. The watcher is deliberately
polling-only (no fsnotify dependency): it stats the path every ~500ms and
debounces for ~300ms, which is robust to the atomic temp+rename saves editors
use and coalesces a save burst into one reload. Reload failures never crash the
daemon — `applyReload` returns before swapping the in-memory config on a parse
error, so a syntactically bad edit is logged and the last-good config and
running tunnels survive. A vanished config is skipped (not reloaded) — a reload
would hit `EnsureExample` and replace the live config with an empty example —
and the skip is logged once until the file reappears.

## 7. Config

Default path (via `xdg.ConfigHome`):

| OS     | Path                                              |
|--------|---------------------------------------------------|
| macOS  | `~/Library/Application Support/portato/config.yaml`  |
| Linux  | `~/.config/portato/config.yaml`                      |
| Windows | `%AppData%\portato\config.yaml` (via `xdg.ConfigHome`) |

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
  password_auth: false            # opt-OUT (Phase 35): disable the SSH password fallback (on by default); set false to keep a tunnel key-only
  ssh_password_store: false       # opt-in (Phase 35): persist SSH passwords in the OS keyring (per account)
  update_check: true              # optional (Phase 49): absent = not asked yet (one-time prompt on the
                                  # first interactive launch); true = daily anonymous GitHub check by the
                                  # daemon; false = no background checks ever. `portato update consent`
                                  # manages it; removing the key re-arms the question.
  log:                            # optional (Phase 22): persistent log-rotation knobs
    max_size_mb: 1                # rotate the log file at this size; 0 -> default (1 MiB)
    max_age_days: 0               # purge rotated archives older than N days; 0 -> disabled
    retain: 3                     # rotated archives to keep (.1 .. .retain); 0 -> default (3)

tubers:
  - name: db-stage                # unique, required
    type: local                   # "local" (-L), "remote" (-R), or "dynamic" (-D)
    local: 5432                   # port or host:port (defaults to 127.0.0.1)
    remote: 10.0.0.5:5432         # see below — meaning depends on type
    ssh: user@bastion.example.com:22   # required; user and port are optional.
                                      # May also be an `~/.ssh/config` alias (Phase 44):
                                      # HostName/User/Port/IdentityFile/ProxyJump are
                                      # resolved from it (explicit fields still win).
    identity: ~/.ssh/id_ed25519   # optional; overrides defaults
    enabled: false                # off by default; the daemon persists toggles here
    password_auth: false          # opt-OUT (Phase 35): keep this tunnel key-only (password fallback is on by default)
    # socks5_user / socks5_password (Phase 20): per-tunnel override of defaults,
    # honoured only by type=dynamic. Both empty (after fallback) -> NoAuth.
    jump: user@bastion.example.com   # optional (Phase 43): ProxyJump / OpenSSH `-J` — a single
                                     # `user@host[:port]` hop or a comma-separated chain
                                     # (`user@edge,user@bastion`); the target in `ssh:` is reached
                                     # through these intermediates in order.
    tags: [prod, db]                 # optional (Phase 46): grouping tags for --tag / #tag / a/x-over-
                                     # filter. Each tag is alphanumeric/-/_ (≤16 tags, ≤32 chars each).
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

### Tags (Phase 46)

- A tuber may carry a `tags:` list (`tags: [prod, db]`). Each tag reuses the
  `validName` alphabet (`[a-zA-Z0-9-_]`, the same rules as tuber names) so tags
  are shell-safe and completion-friendly; load validates non-empty, ≤32 chars
  per tag, ≤16 tags per tuber, and case-sensitive dedup.
- Tags are pure grouping metadata — they do not touch the dial path. They flow
  config → `forward.Status.Tags` → IPC, so `list` / `list --json` report them
  and the TUI filters on the live state rather than re-reading config.
- **`--tag` group op:** `enable` / `disable` / `restart` accept `--tag X` (and
  `--tag` TAB-completes the distinct values from `config.yaml`); it resolves
  every tuber whose `Tags` contain `X` (case-insensitive exact) and acts on
  each, printing one line per tuber. Exactly one of `--tag` / `<name>` is
  required. `forward` is intentionally excluded (ad-hoc, not daemon state).
- **`#tag` TUI filter:** a leading `#` in the `/` filter is an exact tag
  selector (case-insensitive) — `#db` matches a tuber tagged `db`, not one
  merely named `db-stage`. Plain queries keep the fuzzy-over-name/type/endpoint
  behaviour and never match on tags, so the two modes stay distinct.
- **`a` / `x` respect the filter:** enable-all / disable-all gate each
  candidate on the active filter; with no filter every tuber matches
  (unchanged), with a filter (incl. `#tag`) only the visible tubers toggle —
  turning any filtered view into an ad-hoc group.
- Tags render as `#tag` tokens (no space after `#`), byte-identical to the
  `#tag` filter token, in the CLI `list` (inline in the NAME cell) and the TUI
  detail strip (Phase 39: ≤1 line above the footer; an error wins, tags surface
  only when the selected row has no error — the strip never jitters between one
  and two lines). A width-aware TAGS column is a possible later refinement.

### Import from ssh_config (Phase 48)

`portato import [<host-pattern>…]` maps `LocalForward` / `RemoteForward` /
`DynamicForward` directives from `~/.ssh/config` (`--from` overrides the
location) to `enabled: false` tubers — a one-time **copy**, not a live link:
ssh_config is read (via the Phase-44 reader) and **never written**. The
scanner walks Host blocks per-block (each block's own directives, all
occurrences — `Get` would return only the first, and a literal
`GetAll(alias, …)` would leak `Host *` values into every host), skipping
`Host *`, `Match`, and negated-only blocks, uniformly through `Include`
files. A bare-port `RemoteForward` imports as `127.0.0.1:port` (OpenSSH
binds the remote side to loopback by default; Portato's bare port normalises
to `*`). `ssh:` keeps the raw pattern so Phase-44 resolution applies at
load. Dedup drops a forward an existing tuber already covers (same type +
normalised listen addresses + resolved ssh host — both sides resolved against
the scanned config). Flags: `--all`, `--dry-run`, `--yes` (required without
a TTY); names derive from the pattern + listen port, de-conflicted `-2`/`-3`.

A fresh install (config bootstrapped by portato — the `fresh_install` marker
in the state dir) gets a one-time `y/N` import-all offer on the first
**interactive** launch; the `import_offered` marker consumes it on any
outcome (decline included), and 0 candidates consume it silently. The daemon
never runs the offer (a daemon-first bootstrap does not consume it), a
non-TTY launch leaves it unconsumed, and upgrading installs (no fresh
marker) are never nudged.

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

**Server-side requirements.** Every tunnel type needs `AllowTcpForwarding yes`
on the server (the OpenSSH default). With `AllowTcpForwarding no` sshd rejects
the `direct-tcpip` open (`-L`/`-D`) and the `tcpip-forward` request (`-R`), so
the tunnel fails to forward; `portato doctor --probe` (Phase 41) detects this
non-interactively, and the `-L`/`-D` runtime dial surfaces an
`AllowTcpForwarding` hint in the `l` log. A `remote` (`-R`) tunnel that asks for
a non-loopback bind additionally needs `GatewayPorts yes|clientspecified`;
otherwise sshd silently binds loopback and the public address stays unreachable
(this silent downgrade is **not detectable from the client** — the
`tcpip-forward` reply carries only the port, per RFC 4254 §7.1).

## 9. SSH client (native)

- `ssh.Dial` to the server with an `ssh.ClientConfig`:
  - **Auth:** try `ssh.PublicKeysCallback` from the agent, then `ssh.PublicKeys` from the `identity`.
    - **Agent transport:** the agent is reached over the `SSH_AUTH_SOCK` unix-domain socket on darwin/linux; on Windows over the OpenSSH agent's named pipe `\\.\pipe\openssh-ssh-agent` (no `SSH_AUTH_SOCK` there). The dial is build-taged (`internal/forward/agentdial_*`); on failure it silently falls back to the identity key.
  - **Passphrase-protected identity (Phase 19):** if `ssh.ParsePrivateKey` reports a missing passphrase, the dial obtains one from the passphrase store (`internal/secret` — an in-memory cache backed by the OS keyring) and retries with `ssh.ParsePrivateKeyWithPassphrase`. With none available it surfaces `Status.PendingPassphrase` and **blocks** (the store's `Wait`) until the TUI/CLI provides one, instead of spinning the reconnect backoff. A wrong passphrase is invalidated and re-prompted.
  - **Password auth (Phase 35, on by default — opt-out):** when no usable key authenticates, the dial falls back to an interactive password prompt (OpenSSH-style). It is **on by default** so existing password-only hosts and servers that switch key→password just work with no config change; `password_auth: false` (per-tuber or in `defaults`) opts out — useful for deployments that only ever use keys and never want a prompt (including avoiding a premature prompt while an agent finishes starting at boot). Unlike a passphrase (validated locally), a password is only checked by the server, so the re-prompt loop is **dial-level**: it probes keys first (a working key never prompts), then for each password does a single dial with `ssh.Password(pw)` — on a wrong password it invalidates the value and re-prompts with no backoff, staying in `Connecting` with `Status.PendingPassword` set; a server that does not offer the `password` method at all bails out rather than looping. The password comes from the password store (in-memory cache, plus the OS keyring when `defaults.ssh_password_store` is on), keyed by server account `password:<user>@<host>:<port>`, and is supplied via the TUI modal / `POST /tubers/{name}/password` / the controller's `AcceptPassword`. **The password is never stored in config** (only the opt-out bool), preserving the §9 plaintext invariant.
  - **HostKeyCallback:** `knownhosts.New(hostsFile)`; with `accept_new_hosts: true` — a wrapper that appends a new key (TOFU).
  - **Timeout:** an explicit connect timeout (5s).
  - **ProxyJump / jump hosts (Phase 43):** a `jump:` field (single `user@host[:port]` or a comma-separated chain) dials the `ssh:` target through one or more intermediates — the OpenSSH `-J` equivalent. The dial already separates the TCP dial from the SSH handshake, so chaining reuses the handshake per hop: hop 0 is TCP-dialed via `net.Dialer`, each later hop via the previous hop's `ssh.Client.Dial` (a direct-tcpip channel), each wrapped in `ssh.NewClientConn`. The final client runs the tuber's forward (`Listen` / direct-tcpip `Dial`) exactly as today. Each hop verifies its own host key against known_hosts (with its own `HostKeyAlgorithms`), so a bastion key and the target key are both checked; TOFU/accept prompts fire per hop. **Auth split:** intermediate hops are key-only (the shared agent/identity), while the final hop uses whatever auth a no-jump tuber would (keys, or the Phase 35 password fallback). This deliberately avoids a chain of interactive password prompts across bastions — **a bastion that requires a key must accept the same key the target uses** (load it into `ssh-agent` or set `identity:`). Per-hop identity/password is a later refinement; a jump tuber with no usable key bails with a clear "bastion requires a key" error rather than prompting. The intermediate `ssh.Client`s are kept alive for the final client's lifetime and torn down by a leash goroutine once the final client disconnects (server drop, Stop, or a reconnect cycle), so a reconnect rebuilds the whole chain without leaking connections. `~/.ssh/config` resolution (populating `jump:` from an alias's `ProxyJump` directive) is **Phase 44** (below).
  - **`~/.ssh/config` resolution (Phase 44):** `ssh: <alias>` resolves HostName/User/Port/IdentityFile/ProxyJump from the user's `~/.ssh/config` (`kevinburke/ssh_config`, first-match-wins; multi-pattern `Host`, negation, and `Include` honoured), so host definitions aren't duplicated between ssh-config and `config.yaml`. This is a **config-layer** resolution: it fills a tuber's derived User/Host/Port/Identity/Jumps *before* any dial, so the forward/dial path (including the Phase 43 chain) is unchanged. **Precedence (openssh-faithful):** an explicit tuber value wins — `ssh: me@alias:2222` overrides the alias's User/Port; a tuber `identity:` / `jump:` overrides the config's IdentityFile / ProxyJump. ssh-config only fills the gaps. An alias's ProxyJump populates `jump:` and is itself expanded (each hop that is an alias is resolved to `user@host:port`, single-pass like OpenSSH `-J`; a visited-set + depth cap guard a pathological self-referential Host block). IdentityFile tokens (`~`, `%h`, `%u`, `%d`) are expanded. A derived IdentityFile/ProxyJump lives only on the non-persisted `Tuber` fields (`SSHIdentity`, `Jumps`) — `Config.Save` never writes them back as user content. **Errors (openssh-faithful):** no `~/.ssh/config`, or no Host block matches ⇒ the host is used literally and silently (matching OpenSSH); an existing but unreadable config, or a ProxyJump cycle/depth-cap ⇒ a clear load error. Out of scope v1: `Match exec/host` conditional blocks and `UserKnownHostsFile` (Portato keeps its own `ResolvedKnownHosts`).
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
| `o`            | enter the SSH password for a password-blocked selected tunnel (Phase 35; `p` is taken by the identity passphrase); the modal also auto-opens on block |
| `r`            | restart the selected tunnel                           |
| `a` / `x`      | enable all / disable all                              |
| `e` / `n` / `d`| edit / create / delete the selected tunnel            |
| `C`            | duplicate the selected tunnel (under `<name>-copy>`); the local port is auto-bumped past ports already used in the config (Phase 39)   |
| `l`            | view the selected tunnel's logs                       |
| `/`            | fuzzy (subsequence) filter over name/type/endpoint; `esc` clears (Phase 20; substring fallback) |
| `R`            | reload the config from disk                           |
| `?` / `esc`    | toggle help (`esc` also clears an active filter and cancels a confirm modal) |
| `q` / `ctrl+c` | quit (with the "background?" modal in standalone when there are live tunnels — Connecting/Connected/Reconnecting/Error) |

The header shows the mode: `standalone` or `attach` (Phase 39 dropped the
socket path that used to follow `attach`; `portato doctor` exposes it).

Layout (Phases 38–39): the footer is pinned to the bottom edge regardless of
how many rows the content occupies (no "jump" when the filter line appears or
disappears). Interactive prompts (delete / TOFU / passphrase / password /
quit) render in the footer zone with the header and table still visible above
them — the cursor keeps highlighting the row the prompt refers to (a true
dimmed overlay was ruled out: it cannot be theme-complete in mono/dark). The
selected row's full error shows in a one-line strip directly above the footer
(err-styled prefix + body), and the in-row error hint keeps the tail where the
conflicting port/address lives.

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

The Portato logo appears in three places — never on the working screen. Two
ASCII variants are embedded: a compact potato (24×12) and a combined
"potato + PORTATO" wordmark (70×12), each in braille and solid-block forms.

- **empty-list splash** — when the tunnel list is empty and the terminal is
  tall enough (≥ ~18 rows), the centered logo sits above the "no tunnels"
  hint. A wide terminal (≥ ~72 cols) shows the wordmark; a narrower one falls
  back to the compact potato. A short terminal shows the hint only.
- **help (`?`) overlay** — the compact potato is prepended above the hotkey
  list (same height gate), so the wider wordmark does not clutter the hotkeys.
- **`portato --version`** — the wordmark followed by a
  `portato <version> (<commit>, <date>)` line. Pipe-safe by construction: plain
  braille/block art with no ANSI and no inline-image escape, so
  `portato --version | head` stays clean.

A small potato emoji 🥔 marks the header before the title, on `GOOS=darwin`
only (where it renders cleanly at 2 cells); `PORTATO_LOGO_EMOJI=on|off`
overrides it, and `PORTATO_LOGO=off` hides it too.

Rendering (override with `PORTATO_LOGO=auto|braille|block|off`; `image` is
accepted as an alias for `auto`):

| Mode    | When                                                               |
|---------|--------------------------------------------------------------------|
| braille | default on macOS (Terminal.app) and Linux — outline-braille ASCII. |
| block   | `GOOS=windows` — solid block (robust on legacy conhost).           |
| off     | no big logo anywhere, no header emoji.                             |

The ASCII glyphs are tinted with the theme's title accent, except under the
mono theme / `NO_COLOR` (plain glyphs); the `--version` banner is untinted
(the CLI does not load the theme). The assets are `go:embed`ded in
`internal/logo/`, so the binary needs nothing on disk.

## 12. The "leave in the background" hand-off

When quitting standalone mode (`q`):

1. If there are no live (Connecting/Connected/Reconnecting/Error — Phase 39 counts an enabled tuber mid-retry in the Error state as live too) tunnels -> exit immediately with `StopAll()`.
2. If there are live tunnels -> modal: `"N tuber(s) still active. Leave them in the background? [y/N]"` (honestly pluralized — `1 tuber still active.` / `N tubers still active.`; the count includes the Error state).
3. **`y`**: the standalone moves its live tunnels to a detached daemon via the **seamless FD hand-off** (Phase 16) when possible, falling back to the close+rebind path otherwise:
   - **Seamless (default):** for each live `local`/`dynamic` tunnel the standalone dups its already-bound local listener (`(*net.TCPListener).File`), opens a one-shot SOCK_STREAM unix transfer socket, spawns `portato daemon --config <cfg> --listen-fds <sockpath>` (detached, `Setsid`) and sends the dup'd fds (SCM_RIGHTS) over the transfer socket. The daemon reconstructs each listener (`net.FileListener`) and adopts it — skipping its own `net.Listen` — so the kernel listening socket never closes: the standalone's and the daemon's dup'd fds reference the same socket, and the standalone closes its copy only after the daemon's `GET /healthz` answers. The local port therefore stays continuously available across the transition. The established SSH session is **not** moved — `golang.org/x/crypto/ssh` keeps the transport's crypto state in process memory and cannot resume it in another process — so the daemon re-dials; `type=remote` tunnels have no local listener and simply re-dial.
   - **Fallback** (no live local listeners, or any pre-spawn step fails): the standalone runs `StopAll()` (releasing the local ports), spawns the daemon without `--listen-fds`, and waits for `healthz`. The brief gap between release and the daemon's rebind is the legacy MVP blip.

   In both paths the standalone probes the advertised socket (§6) with `GET /healthz` every 100ms up to a 5s timeout, then exits; the fresh daemon reads `enabled: true` from the persisted config (the §6 invariant) and brings up the same set of tunnels. Because the standalone also auto-starts `enabled: true` tunnels on launch (§6), the daemon's set is exactly what was already running — the hand-off never surfaces "surprise" tunnels the user did not toggle.
4. **`n`** or `enter`: `StopAll()` + exit; **`Esc`**: cancel — close the modal and return to the list (without stopping the tunnels and without exiting).

Limitation: the seamless hand-off preserves continuous **local-port** availability (a `nc -z` to the local port never fails across the transition), but the underlying SSH session is re-established by the daemon — so a forwarded connection in flight at the moment of the hand-off is dropped (its SSH channel dies with the standalone's `*ssh.Client`); only new connections are seamless. This is a fundamental limit of FD-passing with `golang.org/x/crypto/ssh` (no cross-process session resume), not a defect.

## 13. Autostart

| OS     | Method          | Where we put it                                                     |
|--------|-----------------|---------------------------------------------------------------------|
| macOS  | launchd         | `~/Library/LaunchAgents/dev.portato.daemon.plist`, `RunAtLoad=true`, `KeepAlive=true` |
| Linux  | systemd --user  | `~/.config/systemd/user/portato.service` (+ `portato.socket`), `Restart=on-failure`, lingering enabled |
| Windows | SCM service     | `HKLM\SYSTEM\CurrentControlSet\Services\Portato` — `SERVICE_WIN32_OWN_PROCESS`, `StartType=Automatic`, `DelayedAutoStart=true`, depends on `Tcpip`, recovery = `SC_ACTION_RESTART` after 30 s, runs as the install-time user (password stored by SCM as an LSA secret); `portato install` starts it immediately |

`portato install` detects the OS and installs the right mechanism; `portato uninstall` reverses it.
Since tunnels are `enabled: false` by default, at system boot **only** the control daemon is brought up.

On Linux `install` also writes and enables `portato.socket` (Phase 22 socket
activation); the service `Requires`+`After`s it so systemd hands the daemon the
pre-bound IPC socket via `LISTEN_FDS`. macOS launchd does **not** get a `Sockets`
dict: claiming the handed fd needs a libc call that would require cgo, breaking
the pure-Go single binary — socket activation there is deferred.

On Windows (Phase 47) `install` registers a real Service Control Manager
service named `Portato`. SCM is the launchd/systemd equivalent: it owns the
lifecycle, restarts the daemon on failure (recovery action), and — by logging
on the install-time user account itself — gives the boot-time process a session
with full access to `%USERPROFILE%\.ssh\` and `%APPDATA%\portato\config.yaml`
without anyone logged in. The account defaults to the installing user
(`%USERDOMAIN%\%USERNAME%`); its password is collected once at `portato install`
(interactive no-echo prompt, or `--password-file` for CI) and handed straight to
SCM, which keeps it as an LSA secret (portato never persists it). A Windows
account password change therefore requires a fresh `portato install`. The
`--service-account LocalSystem` (a.k.a. `NT AUTHORITY\SYSTEM`) option skips the
password but runs without a user profile (headless / CI). `portato install`
needs administrator privileges (creating a service); a non-admin user should
use `--legacy-runkey`. Portato grants the account `SeServiceLogonRight` itself
(`CreateService` does not reliably) and validates the password up front via
`LogonUser`, so the password must be the account's **local** password — a
Windows Hello PIN or a Microsoft-account cloud password is rejected.
`portato install` starts the service immediately, so `portato list` works the
moment install returns — parity with macOS `launchctl bootstrap` and Linux
`systemctl --user enable --now`. `portato stop` sends `svc.Stop` (graceful drain) instead of
`TerminateProcess`. A Scoop-installed binary's version-pinned path is rewritten
to the stable `…\scoop\apps\portato\current\…` junction so the service survives
`scoop update portato`. The Phase-17 HKCU `Run`-key mechanism is kept behind
`--legacy-runkey` for locked-down environments (GPO/AV) where service creation
is blocked.

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

- **Cross-platform:** compiles and runs on darwin/amd64, darwin/arm64, linux/amd64, linux/arm64 (MVP) and windows/amd64 (Phase 17).
- **Single binary:** no external dependencies (the system `ssh` is not required).
- **Startup behavior:** on the first run, if there is no config — an example is created and the path is shown to the user.
- **Tests:** the key packages (`config`, `forward`, `controller`) are covered by unit tests (Phases 1, 2, 6).

## 16. Update check and self-update (Phases 49–50)

Portato can learn that a newer release exists — but never talks to the
network before the user has answered a one-time question, and never
applies anything on its own.

- **Source:** `GET https://api.github.com/repos/portuber/portato/releases/latest`
  (anonymous, 10s timeout, `portato/<version>` User-Agent). The base URL is
  a compile-time constant — **not** runtime-configurable (no env, no
  flag), so the checker can only ever talk to GitHub; in-repo tests
  redirect via a package-internal seam. `releases/latest` resolves to the
  newest non-draft non-prerelease release — exactly the project's
  strict-`vX.Y.Z` VERSIONING policy. A 403 with `X-RateLimit-Remaining: 0`
  is a temporary "try later" condition; version comparison is a
  hand-rolled integer-triple (no `golang.org/x/mod` dependency); a dev or
  snapshot build is "not comparable" rather than stale.
- **Consent** is a user setting: `defaults.update_check` in `config.yaml`.
  **Absent = "not asked yet"** — the first interactive surface (the
  standalone launcher right after the Phase-48 import offer, `portato
  install`, or a fully green `portato doctor`) asks once:
  `Check for portato updates in the background (GitHub, once a day)? [Y/n]`
  (Enter/y = yes — low stakes, one command to undo). Either answer is
  persisted through the comment-preserving AST patcher and never asked
  again; `portato update consent on|off|ask` manages it (ask removes the
  key, re-arming the question). The daemon, attach and every other CLI
  command never ask; a non-TTY launch does not consume the question. A
  failed persist leaves it pending.
- **The background poll** (daemon only, consent on): an hourly ticker
  re-reads the config — so `consent off` takes effect within one tick,
  no restart — and when the shared cache is past its 24h TTL performs one
  anonymous GET and records `{last_check, latest}`. Failures log at debug
  and never advance the clock (retry waits for tick+TTL; no storm). Off
  consent the checker is fully idle: zero requests, zero writes.
- **Cache:** `xdg.StateHome/portato/update.json` (`0600`, atomic
  tmp+rename) — machine state, deliberately not in `config.yaml` so the
  config is not rewritten on every poll. Missing or corrupt = "never
  checked" (the cache is disposable). Surfaces read it without network
  I/O: `portato doctor` (informational line in every consent/cache state)
  and the TUI header (`update: vX.Y.Z` segment next to `mode:`, shown
  only when the running version is a comparable release and the cache is
  strictly newer).
- **`portato update check`** is the explicit, consent-independent check:
  prints current/latest/verdict + release URL; exit 0 on "available" and
  "up to date", non-zero only on error. A successful check feeds the
  cache; a dev build reports "not comparable" without poisoning it.
- **`portato update apply`** (Phase 50) downloads the platform archive,
  verifies the SHA-256 against `checksums.txt` (both fetched into a temp
  dir; a mismatch — or an unlisted file — aborts with the installed
  binary untouched), extracts the single `portato` member (mode from the
  running binary, not the archive) and swaps it in place:
  - **unix:** two atomic renames — current → `portato.old` (the one-level
    rollback; a previous backup is replaced, not accumulated), staged new
    → current, best-effort restore if the second rename fails. A running
    daemon safely keeps its old inode; apply prints a restart hint when
    one is live (`portato stop`, start again — or reboot for autostart).
  - **windows:** a running `.exe` cannot be replaced — apply stages
    `portato.new` next to the binary and the **next launch** (the pre-cobra
    entry point, before SCM detection, the Phase-47 precedent) completes
    the swap once the previous holder has exited.
  - **Package-manager etiquette:** the install channel is detected
    (brew/scoop by path — including the Phase-47 scoop `current`
    junction — deb/rpm/apk by `dpkg-query`/`rpm -qf`/`apk info` ownership,
    go-install by GOBIN) and a managed install is **refused** in place:
    the channel's own upgrade command is printed instead (`brew upgrade
    --cask portuber/tap/portato`, `scoop update portato`, `apt/dnf/apk
    upgrade portato`, `go install …@latest`). `--force` overrides the
    refusal — never the checksum. A binary held by the Windows SCM
    service is never swapped, `--force` included. `doctor` names the
    channel in its update line.
  - **Flags:** `--dry-run` (plan only), `--yes` (skip the prompt; required
    in a non-TTY), `--force`. Explicit runs only — nothing is ever
    auto-applied; the Phase-49 consent gates *checking*, applying is
    always a deliberate command.
- **Privacy:** the check sends no identifiers — an anonymous GET to
  api.github.com is the entire footprint; `HTTPS_PROXY`/`NO_PROXY` are
  honoured by the default transport.

## 17. Open questions (to resolve as we go)

- IPC authorization: only filesystem permissions (0600) or a token? -> **resolved (Phase 18)**: a 32-byte bearer token in `<socketDir>/portato.token`, layered on the `0600` socket; `--ipc-token off` disables it. See §6.
- Where to store a passphrase for an identity when the agent is unavailable? -> **resolved (Phase 19)**: an in-memory cache (per process, so reconnects don't re-prompt) plus the OS keyring (macOS Keychain / Linux Secret Service / Windows Credential Manager via `zalando/go-keyring`) for cross-restart persistence. Opt-in keyring persistence via `defaults.identity_passphrase_store` (off by default); explicit `portato add-identity`/`forget-identity` always write/clear the keyring. Nothing is ever written to disk in plaintext. See §9.
- How to authenticate to a password-only SSH server (no usable key)? -> **resolved (Phase 35)**: password auth is **on by default** (OpenSSH-style) — when keys don't authenticate, the dial falls back to an interactive password prompt, so existing password-only hosts and servers that switch key→password need no config change. `password_auth: false` opts out (per-tuber or `defaults`). The password is supplied interactively (TUI modal / `POST /tubers/{name}/password` / `controller.AcceptPassword`) and held in an in-memory cache plus, opt-in, the OS keyring (`defaults.ssh_password_store`, keyed by account) — never in config. `golang.org/x/crypto/ssh` does not retry the password method within one handshake, so the re-prompt is a dial-level loop (no backoff, stays `Connecting`); a key-only server bails out cleanly. Keys stay the default and are tried first. See §9.
- Passing live listener FDs to the new daemon during hand-off (a seamless transition) -> **resolved (Phase 16)**: the standalone dups its local listeners and sends them (SCM_RIGHTS) over a one-shot transfer socket; the daemon adopts them via `net.FileListener`, so the local ports never go down. The SSH session itself is re-dialed (no cross-process resume in `golang.org/x/crypto/ssh`); only local-port availability is seamless. See §12.
- Windows support -> **resolved (Phase 17, refined in Phase 47)**: IPC over a named pipe (`\\.\pipe\portato` via `go-winio`); autostart via the HKCU registry Run key in Phase 17, superseded by a Service Control Manager service in Phase 47. See §6/§13.
