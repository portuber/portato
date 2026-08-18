<img src="logo.svg" width="128" align="right" alt="Portato logo">

# Portato

[![CI](https://github.com/portuber/portato/actions/workflows/ci.yml/badge.svg)](https://github.com/portuber/portato/actions/workflows/ci.yml)
[![security](https://github.com/portuber/portato/actions/workflows/security.yml/badge.svg)](https://github.com/portuber/portato/actions/workflows/security.yml)
[![Release](https://img.shields.io/github/v/release/portuber/portato)](https://github.com/portuber/portato/releases)
[![License: MIT](https://img.shields.io/github/license/portuber/portato)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/portuber/portato)](go.mod)
[![Codefactor](https://www.codefactor.io/repository/github/portuber/portato/badge)](https://www.codefactor.io/repository/github/portuber/portato)

**Portato** is an SSH port-forwarding manager with a TUI. It lets you turn
individual SSH tunnels on and off, restart them, and watch their status from a
single screen — either running standalone, or attached to a background daemon.

<p align="center"><img src="docs/landing/assets/hero.gif" alt="Portato TUI demo" width="720"></p>

The single binary works in several modes:

| Command                          | What it does                                                        |
|----------------------------------|---------------------------------------------------------------------|
| `portato`                    | Smart launcher: attach to a running daemon, or start standalone TUI |
| `portato daemon`             | Background process running tunnels + an IPC server (unix socket)    |
| `portato attach`             | TUI client connected to a running daemon                            |
| `portato list`               | Print status of all tunnels (stdout)                                |
| `portato logs`               | Print the daemon log (`-f` follow, `-n`, `--since`, `--tuber`, `--all`) |
| `portato enable <name>`      | Enable a tunnel on the daemon                                       |
| `portato disable <name>`     | Disable a tunnel on the daemon                                      |
| `portato restart <name>`     | Restart a tunnel                                                    |
| `portato reload`             | Reload the daemon's config from disk (also auto-reloads on change)  |
| `portato import [<pattern>…]` | Import forwards from `~/.ssh/config` as disabled tunnels (`--all`, `--dry-run`, `--yes`) |
| `portato stop`               | Stop the running daemon (graceful, via SIGTERM)                     |
| `portato install`            | Install system autostart (launchd / systemd --user / SCM on Windows)|
| `portato uninstall`          | Remove system autostart                                             |
| `portato add-identity <path>`| Cache a passphrase for a passphrase-protected SSH key (OS keyring)  |
| `portato forget-identity <path>` | Forget a cached identity passphrase                             |
| `portato doctor`             | Diagnose the setup (config, keys, agent, daemon); `--probe` also checks each server's forwarding permission |
| `portato version`            | Print the version                                                   |
| `portato license`            | Print license information (`--full` prints the full MIT text)       |

## Install

> **Prerequisites.** Portato forwards over SSH, so you need an SSH account on a
> reachable host and a key (or a password). `local` (`-L`) and `dynamic` (`-D`)
> tunnels work with default sshd settings; `remote` (`-R`) to a non-loopback
> address also needs `GatewayPorts yes` (or `clientspecified`) in the server's
> `sshd_config`. If a tunnel won't connect, run `portato doctor` first — see
> [Troubleshooting](#troubleshooting).

All channels are built from the same release for macOS, Linux, and Windows.

**Homebrew** (macOS / Linuxbrew):

```sh
brew install --cask portuber/tap/portato
```

**Scoop** (Windows):

```pwsh
scoop bucket add portuber https://github.com/portuber/scoop-bucket
scoop install portuber/portato
```

Scoop needs git for buckets — if it reports git is required, run `scoop install git` first.

**Binary** — download the archive for your platform from the
[latest release](https://github.com/portuber/portato/releases/latest), extract
it, and put the `portato` binary on your `PATH`:

```sh
# macOS / Linux (tar.gz)
tar -xzf portato_<version>_macOS_arm64.tar.gz   # or linux_amd64, linux_arm64, …
install -m 0755 portato ~/.local/bin/portato
portato version
```

```pwsh
# Windows (zip)
Expand-Archive portato_<version>_Windows_x86_64.zip
# move portato.exe into a directory on your PATH
portato version
```

**deb / rpm** (Linux) — from the
[latest release](https://github.com/portuber/portato/releases/latest):

```sh
sudo dpkg -i portato_<version>_linux_amd64.deb
# or: sudo rpm -i portato_<version>_linux_amd64.rpm
```

**Alpine** (apk) — from the
[latest release](https://github.com/portuber/portato/releases/latest):

```sh
sudo apk add --allow-untrusted portato_<version>_linux_amd64.apk
# arm64 (e.g. Raspberry Pi): portato_<version>_linux_arm64.apk
```

The apk is unsigned, so `--allow-untrusted` is required. It is the same static
build as the tarball/deb/rpm (CGO is disabled), so it runs on musl unchanged.

**go install** (needs Go 1.26+):

```sh
go install github.com/portuber/portato/cmd/portato@latest
```

The version baked into the binary comes from the git tag at build time.

## Shell completion

`portato` can TAB-complete subcommands, flags, and tuber names. Tubers are
completed from `config.yaml`, so it works with no daemon running —
`enable`/`disable`/`restart`/`forward <name>` and `logs --tuber <name>` all
complete. Generate and source the script for your shell:

**bash:**

```sh
echo 'eval "$(portato completion bash)"' >> ~/.bashrc
```

**zsh** — `compinit` is required, and a bare macOS zsh does **not** load it:

```sh
# ~/.zshrc — if compinit isn't already run (oh-my-zsh / a framework runs it),
# add the autoload line; otherwise just the source line:
autoload -Uz compinit && compinit
source <(portato completion zsh)
# or, without process substitution (faster shell start, but regenerate after
# each portato upgrade):  portato completion zsh > "${fpath[1]}/_portato"
```

**fish:**

```sh
portato completion fish > ~/.config/fish/completions/portato.fish
```

**PowerShell:**

```pwsh
portato completion powershell | Out-String | Invoke-Expression
```

Restart your shell (or re-`source` the file), then `portato enable <TAB>`
lists your tubers.

## Build

```sh
make build   # produces bin/portato
make run     # go run ./cmd/portato
make test    # go test ./...
make vet     # go vet ./...
make fmt     # gofmt -w .
```

Requires Go 1.26+.

## Releases

Releases are built with [goreleaser](https://goreleaser.com) across the
darwin/linux/windows × amd64/arm64 matrix, producing per-target tarballs and a
Windows zip, a Homebrew cask, a Scoop manifest, deb/rpm/apk packages, and a
`checksums.txt`. To build a local snapshot (no publish, writes to `dist/`):

```sh
make snapshot   # needs goreleaser: go install github.com/goreleaser/goreleaser/v2@latest
```

## Tunnel types

Each tunnel has a `type`:

| `type`    | SSH flag | Meaning                                                        |
|-----------|----------|----------------------------------------------------------------|
| `local`   | `-L`     | listen **here**, forward to `remote` on the host (`→` in UI).  |
| `remote`  | `-R`     | listen **on the host**, forward back here (`←` in UI).         |
| `dynamic` | `-D`     | a SOCKS5 proxy on `local`, all traffic via the host (`⇄ *`).  |

### Local (-L) tunnels

For a `local` tunnel, `local` is the address Portato listens on here, and
`remote` is the destination dialed **on the SSH server** — so `remote` needs a
`host:port` (a bare port is not enough; there is no host to dial). A bare
`local` port expands to loopback (`127.0.0.1:port`):

```yaml
tubers:
  - name: db
    type: local            # ssh -L 5432:10.0.0.5:5432 user@bastion.example.com
    local: 5432            # listen here (bare port -> 127.0.0.1:5432)
    remote: 10.0.0.5:5432  # destination on the host (host:port, not a bare port)
    ssh: user@bastion.example.com
```

### Remote (-R) tunnels

For a `remote` tunnel, `remote` is the address listened on the SSH server, and
`local` is the address connections are forwarded to on this machine. A bare
port (or `:port`) binds **all interfaces** on the host (`*:port`) — the default,
so a reverse forward exposes your local service through the server. Use an
explicit host for loopback-only (`127.0.0.1:port`) or a specific interface:

```yaml
tubers:
  - name: pull-redis
    type: remote
    remote: 16379        # listened on the server on all interfaces (*:16379)
    local: 6379          # forwarded to the local redis
    ssh: user@bastion.example.com
```

**A non-loopback bind on the host** — which now includes the bare-port default
— requires `GatewayPorts yes` (or `clientspecified`) in the server's
`sshd_config`, plus the port open in the host firewall. Otherwise sshd silently
binds loopback and the public address won't be reachable. For a server-internal
forward only, set `remote: 127.0.0.1:16379` explicitly.

### Dynamic (SOCKS5) tunnels

A `dynamic` tunnel runs a SOCKS5 proxy on `local`. There is no fixed `remote` —
each connection's destination is read from the SOCKS request and dialed on the
host side, so you can reach any internal address through the bastion without a
forward per port:

```yaml
tubers:
  - name: socks
    type: dynamic
    local: 1080          # SOCKS5 proxy -> 127.0.0.1:1080
    ssh: user@bastion.example.com
```

Use it like any SOCKS5 proxy (no auth, loopback bind):

```sh
curl --socks5 127.0.0.1:1080 http://internal-host.example.com
# or HTTP-through-SOCKS:
ALL_PROXY=socks5://127.0.0.1:1080 curl http://internal-host.example.com
```

For a browser, set the SOCKS5 host to `127.0.0.1` and port `1080` (enable "Proxy
DNS when using SOCKS v5" so names resolve on the bastion too). The proxy
reconnects automatically if the SSH session drops.

### Jump hosts (ProxyJump / `-J`)

When the target is only reachable through a bastion, set `jump:` to the
intermediate host (OpenSSH's `-J` / `ProxyJump`). `ssh:` is the final target;
`jump:` dials it through the bastion. A comma-separated chain
(`user@edge,user@bastion`) dials through each hop in order:

```yaml
tubers:
  - name: db-vpn
    type: local
    local: 5433
    remote: 10.0.0.5:5432          # destination on the TARGET host
    ssh: deploy@10.0.0.5:22        # target — reachable only via the bastion
    jump: user@bastion.example.com:22   # ssh -J user@bastion deploy@10.0.0.5
```

Each hop verifies its own host key against `known_hosts` (so the bastion key and
the target key are both checked). The bastion must accept **the same key** as the
target (ssh-agent / `identity:`) — intermediate hops are key-only; the Phase 35
password fallback applies only to the final target. Per-hop identity/password is
a later refinement.

### `~/.ssh/config` aliases

`ssh:` may be a Host alias from your `~/.ssh/config`. HostName / User / Port /
IdentityFile / ProxyJump are resolved from it, so a host defined once in your ssh
config doesn't have to be repeated in `config.yaml`. Explicit tuber fields still
win — `ssh: me@alias:2222`, `identity:`, or `jump:` override the alias's values;
ssh-config only fills the gaps. An alias with a `ProxyJump` reaches its target
through the bastion with **no `jump:` in `config.yaml` at all** (Phase 43 dials
the resolved chain):

```yaml
# ~/.ssh/config:
#   Host db-stage
#     HostName 10.0.0.5
#     User deploy
#     Port 2222
#     IdentityFile ~/.ssh/deploy_key
#     ProxyJump bastion

tubers:
  - name: db
    type: local
    local: 5434
    remote: 127.0.0.1:5432
    ssh: db-stage        # alias — HostName/User/Port/IdentityFile/ProxyJump resolved
```

A host with no matching `Host` block is used literally (matching OpenSSH), so
existing `user@host:port` values keep working unchanged. Only an unreadable
`~/.ssh/config` or a circular `ProxyJump` is a load error. `Match exec/host`
conditional blocks and `UserKnownHostsFile` are not resolved (Portato keeps its
own `known_hosts`).

### Importing from `~/.ssh/config`

Migrating from hand-rolled `ssh -N` setups? `portato import` copies the
`LocalForward` / `RemoteForward` / `DynamicForward` directives from your
`~/.ssh/config` into `config.yaml` as **disabled** tubers — a one-time copy;
your ssh config is read, never written.

```sh
portato import --dry-run          # list what would be imported (names included)
portato import db                 # import only the Host db block (exact pattern, case-insensitive)
portato import --all              # every block with forwards, with a y/N confirmation
portato import --all --yes        # non-interactive (no TTY)
portato import --from ~/ssh-backup --all   # a different ssh config file
```

- Imported tubers land `enabled: false` — nothing auto-connects.
- `ssh:` keeps the raw Host pattern (`ssh: db-stage`), so alias resolution
  (above) applies at load. A bare-port `RemoteForward` imports as
  `127.0.0.1:port` — OpenSSH binds the remote side to loopback by default.
- Names derive from the pattern + port (`db-5432`), de-conflicted with a
  `-2`/`-3` suffix; a forward an existing tuber already covers is listed as
  `already configured` and skipped.
- Blocks reached through `Include` are scanned with the same rules. `Host *`
  and `Match` blocks are skipped (the ssh-config library does not support
  `Match`), as are unix-socket forwards (`LocalForward /run/x.sock …`).

A **fresh install** (no config yet) gets a one-time offer: the first
interactive `portato` launch lists the forwards found in `~/.ssh/config` and
asks `y/N` — import them all or decline; either way the offer never repeats.
A daemon started first does not consume the offer, and upgrading installs are
never asked.

### Tags (grouping)

A tuber may carry `tags:` for ad-hoc grouping — by environment (`prod`,
`staging`), role (`db`, `web`), or owner:

```yaml
- name: db-prod
  tags: [prod, db]
```

Each tag is alphanumeric / `-` / `_` (the same alphabet as tuber names), ≤16
tags per tuber, ≤32 chars each. Tags are pure metadata — they don't affect the
dial.

```sh
portato enable --tag prod     # enable every prod tuber (one line per tuber)
portato disable --tag prod
portato restart --tag db
portato enable --tag pr<TAB>  # completes distinct tag values from config.yaml
```

`--tag` is mutually exclusive with a `<name>` positional (exactly one is
required). In the TUI, a `/` filter with a leading `#` is an exact tag selector
— `#db` matches a tuber *tagged* `db`, not one merely *named* `db-stage`. `a`
(enable-all) and `x` (disable-all) respect the active filter, so `/ #prod` then
`a` enables exactly the prod tubers.

## Autostart

`portato install` registers the daemon with your OS's service manager so it
starts automatically at login (or boot). `portato uninstall` removes it.
Tunnels are **disabled by default** — only the control daemon comes up; enable
the ones you need from the TUI or with `portato enable <name>`.

Both commands take an optional `--label` (default `dev.portato.daemon`) and
honour the global `--config` flag. Run them from a built binary
(`make build && ./bin/portato install`); running from `go run` works but
prints a warning, since the temp binary path is unstable.

### macOS (launchd)

`portato install` writes a per-user LaunchAgent and loads it:

- plist: `~/Library/LaunchAgents/dev.portato.daemon.plist`
- `RunAtLoad=true`, `KeepAlive=true` (the daemon is restarted after any exit)
- logs: `~/Library/Logs/portato.log` and `.err.log`

Inspect / control it directly:

```sh
launchctl print "gui/$(id -u)/dev.portato.daemon"   # status
launchctl bootout  "gui/$(id -u)/dev.portato.daemon" # stop (or `portato uninstall`)
```

### Linux (systemd --user)

`portato install` writes a `--user` unit and enables it:

- unit: `~/.config/systemd/user/portato.service`
- `Restart=on-failure` (restarted only on a crash, not a clean exit)
- lingering is enabled (`loginctl enable-linger`) so the daemon runs without an
  active session; logs go to the journal — `journalctl --user -u portato`

```sh
systemctl --user status portato      # status
systemctl --user disable --now portato   # stop (or `portato uninstall`)
```

### Windows (Service Control Manager)

`portato install` registers a real Windows service with the Service Control
Manager so the daemon starts at **boot** (not login), runs **without anyone
logged in**, and is **started immediately** by install (parity with macOS and
Linux):

- service: `Portato` (`HKLM\SYSTEM\CurrentControlSet\Services\Portato`)
- `StartType = Automatic`, `DelayedAutoStart = true`, depends on `Tcpip`
- recovery: restart after 30 s on failure (the `KeepAlive` / `Restart=on-failure`
  equivalent)
- runs as the install-time user (`DOMAIN\user`); SCM logs that user on at
  service start, so the process gets `%USERPROFILE%\.ssh\` and
  `%APPDATA%\portato\config.yaml` without an interactive login
- install prompts once for the Windows account password (no echo; or pass
  `--password-file <path>` for CI / automation). The password is stored by SCM
  as an LSA secret — **re-run `portato install` after changing your Windows
  account password**
- `--service-account LocalSystem` runs without a password (no user profile —
  for headless / CI)
- install needs **administrator** privileges (it creates a service). portato
  grants the account the `Log on as a service` right itself and validates the
  password up front — enter the account's **local password, not a Windows Hello
  PIN** (a Microsoft-account / PIN will be rejected). If you can't elevate, use
  `portato install --legacy-runkey` instead (per-user, no admin).
- the service starts at boot, and `portato list` / `attach` / `doctor` work
  from a regular (non-elevated) session of the same user — the IPC pipe's
  security descriptor grants the user explicitly (v1.6.1).
- `portato stop` stops the service gracefully (`svc.Stop`) instead of
  terminating the process

Inspect / control it directly:

```pwsh
Get-Service Portato                       # status + StartType
sc.exe qc Portato                         # SERVICE_START_NAME, BINARY_PATH_NAME
Stop-Service Portato ; Start-Service Portato   # (or `portato stop`)
```

A Scoop-installed binary's version-pinned path is rewritten to the stable
`…\scoop\apps\portato\current\…` junction so the service survives
`scoop update portato`.

#### `--legacy-runkey` (fallback)

In locked-down environments where service creation is blocked (GPO / AV), the
Phase-17 HKCU registry Run-key mechanism is still available:

```pwsh
portato install --legacy-runkey
# HKCU\Software\Microsoft\Windows\CurrentVersion\Run, value Portato
# → fires at login only (no SCM recovery, no boot start)
portato uninstall --legacy-runkey
```

## Windows specifics

Portato runs natively on Windows (built and shipped from the same release):

- **Config:** `%AppData%\portato\config.yaml`.
- **IPC:** the daemon listens on a named pipe, `\\.\pipe\portato` (no TCP, no
  socket file). The smart launcher / `attach` find it automatically.
- **ssh-agent:** a key loaded into the OpenSSH agent (the `ssh-agent` service)
  is reached over the agent's named pipe `\\.\pipe\openssh-ssh-agent`; there is
  no `SSH_AUTH_SOCK` on Windows. As elsewhere, a key in the agent is tried
  before identity and password.
- **`portato stop`:** under the SCM service it stops the daemon gracefully
  (`svc.Stop` → the daemon drains and reports `Stopped`). The `--legacy-runkey`
  path still terminates the process directly (no graceful signal).
- **Autostart** is a Windows Service registered with the SCM — see above.

## Logs, themes & diagnostics

- **Per-tunnel logs** — press `l` in the TUI to open the selected tunnel's
  live log (scrolling with `↑↓`/`pgup`/`pgdn`/`g`/`G`, `L` toggles the debug
  level, `esc`/`l` closes). Logs are kept in an in-memory ring buffer; on disk
  they go to `~/Library/Logs/portato.log` (macOS) or the journal (Linux).
- **Themes** — the TUI picks a palette automatically: `NO_COLOR` forces
  monochrome, `COLORFGBG="fg;bg"` selects dark (bg ≤ 6) vs light, default dark.
  Force one explicitly with `PORTATO_THEME=light|dark|mono` (or `auto` to fall
  back to the automatic detection). The light theme paints a light background
  across the whole surface (a real light mode), so it reads as a strong inverse
  of dark regardless of your terminal's own background.
- **`portato doctor`** — checks config validity, identity keys and `ssh-agent`,
  `known_hosts`, daemon reachability over the local IPC socket (or named pipe on
  Windows) and its owner-only permissions, the autostart entry (launchd plist /
  systemd unit / Windows SCM service), and (Linux) lingering. Prints a `✓`/`✗` line
  per check and exits non-zero on any failure.

### Unknown host keys (TOFU)

When a tunnel connects to a host not in `~/.ssh/known_hosts` and
`accept_new_hosts: false` (the default), the TUI shows the key fingerprint and
offers to accept it inline (`y` appends it to `known_hosts` and restarts the
tunnel). To trust new hosts automatically instead, set:

```yaml
defaults:
  accept_new_hosts: true
```

## Troubleshooting

If something doesn't work, run `portato doctor` first — it checks the config,
identity keys and `ssh-agent`, the daemon, autostart, and (on macOS) a wedged
daemon, and prints a `✓`/`✗` line per check.

| Symptom | Check |
|---------|-------|
| `portato list` / daemon start: `remote "X" is not a valid host:port for type: local` | For `type: local`, `remote` is the destination on the host and needs `host:port` (e.g. `127.0.0.1:1234`); a bare port is only accepted for `local` and (as a bind) for `type: remote`. |
| Tunnel stuck on `✗ host key not in known_hosts` | Accept the key in the TUI, or set `accept_new_hosts: true`. |
| `✗ listen ...: address already in use` | A local port is busy — `lsof -i :<port>` to find and stop the holder. |
| `portato list` errors with "daemon not running" | Start the daemon: `portato daemon`, or `portato install` to autostart it. |
| `✗ auth failed` | Start `ssh-agent` / `ssh-add`, or set an `identity:` key. Run `portato doctor`. |
| `✗ bastion requires a key` (a `jump:` tunnel) | Intermediate hops are key-only — load the key into `ssh-agent` (`ssh-add`) or set `identity:`. The bastion must accept the same key as the target; per-hop identity is not supported yet. |
| Tunnels die after logout (Linux) | Enable lingering: `loginctl enable-linger "$USER"`. |
| `✗ listen <addr> on server` (remote tunnel won't bind on the host) | `AllowTcpForwarding no` on the server, or the port is in use there. `AllowTcpForwarding yes` is the default — ask the admin to re-enable it. Run `portato doctor --probe` to confirm. |
| `remote` (`-R`) to a public address isn't reachable | `GatewayPorts no` on the server silently binds loopback. Set `GatewayPorts yes` (or `clientspecified`), or use `remote: 127.0.0.1:port` for server-internal only. (Not auto-detected client-side; `portato doctor --probe` covers the rest.) |
| macOS: "can't be opened — Apple cannot check it for malicious software" | Gatekeeper. Right-click → Open, or `xattr -dr com.apple.quarantine /path/to/portato`. |
| TUI shows a passphrase / password prompt | Press `o` to enter it inline. Cache a key passphrase in the OS keyring with `portato add-identity <key>`. |
| `✗ connection refused` / dial timeout to `remote` | Check `host:port` in the config, the host firewall, and that the service is running on the remote side. |
| Daemon says "already running" but `list`/`stop` say "not running" | Wedged daemon — `portato stop` recovers by PID. `portato doctor` reports "wedged: pid N". |

## Documentation

The source of truth lives in [`docs/`](./docs):

- [`docs/SPEC.md`](./docs/SPEC.md) — technical specification (stack, architecture, config, IPC, TUI).
- [`docs/ROADMAP.md`](./docs/ROADMAP.md) — phase status.
- [`docs/CONVENTIONS.md`](./docs/CONVENTIONS.md) — how phases are planned and implemented.

## License

Portato is licensed under the [MIT License](./LICENSE). All dependencies are
permissive (MIT / Apache-2.0 / BSD); there is no copyleft. Run `portato license`
(or `portato license --full` for the full MIT text) to see the same from the
binary; third-party notices ship in `THIRD_PARTY_LICENSES.txt`.

## Versioning

Portato uses [Semantic Versioning](./docs/VERSIONING.md). Releases are tagged
`vX.Y.Z`.
