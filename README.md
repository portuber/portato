<img src="logo.svg" width="128" align="right" alt="Portato logo">

# Portato

[![CI](https://github.com/portuber/portato/actions/workflows/ci.yml/badge.svg)](https://github.com/portuber/portato/actions/workflows/ci.yml)

**Portato** is an SSH port-forwarding manager with a TUI. It lets you turn
individual SSH tunnels on and off, restart them, and watch their status from a
single screen — either running standalone, or attached to a background daemon.

The single binary works in several modes:

| Command                          | What it does                                                        |
|----------------------------------|---------------------------------------------------------------------|
| `portato`                    | Smart launcher: attach to a running daemon, or start standalone TUI |
| `portato daemon`             | Background process running tunnels + an IPC server (unix socket)    |
| `portato attach`             | TUI client connected to a running daemon                            |
| `portato list`               | Print status of all tunnels (stdout)                                |
| `portato enable <name>`      | Enable a tunnel on the daemon                                       |
| `portato disable <name>`     | Disable a tunnel on the daemon                                      |
| `portato restart <name>`     | Restart a tunnel                                                    |
| `portato install`            | Install system autostart (launchd / systemd --user)                 |
| `portato uninstall`          | Remove system autostart                                             |
| `portato doctor`             | Diagnose the setup (config, keys, agent, daemon, logs)             |
| `portato version`            | Print the version                                                   |

## Build

```sh
make build   # produces bin/portato
make run     # go run ./cmd/portato
make test    # go test ./...
make vet     # go vet ./...
make fmt     # gofmt -w .
```

Requires Go 1.22+.

## Releases

Releases are built with [goreleaser](https://goreleaser.com) across the
darwin/linux × amd64/arm64 matrix, producing per-target tarballs and a
`checksums.txt`. To build a local snapshot (no publish, writes to `dist/`):

```sh
make snapshot   # needs goreleaser: go install github.com/goreleaser/goreleaser/v2@latest
```

Install from a released tarball by extracting it and putting the `portato`
binary on your `PATH`:

```sh
tar -xzf portato_<version>_macOS_arm64.tar.gz
install -m 0755 portato ~/.local/bin/portato
portato version
```

The version baked into the binary comes from the git tag at build time.

## Status

Phases 0–15 are done. The single binary runs the smart launcher, a background
daemon with HTTP-over-unix-socket IPC, an interactive TUI, the CLI commands,
and system autostart (`portato install` / `uninstall`) for macOS (launchd) and
Linux (systemd --user). It supports `local` (`-L`), `remote` (`-R`) and
`dynamic` (`-D`, SOCKS5) tunnels, push-based status events, an in-TUI tunnel
editor (`e`/`n`/`d`) and duplication (`Shift+C`), a per-tunnel log screen (`l`),
an interactive unknown-host (TOFU) prompt, automatic light/dark theming, a
`portato doctor` diagnostics command, robust IPC socket discovery, size-rotated
logs with a `/` list filter, and goreleaser release tooling.

See [`docs/ROADMAP.md`](./docs/ROADMAP.md) for the phase-by-phase status.

## Tunnel types

Each tunnel has a `type`:

| `type`    | SSH flag | Meaning                                                        |
|-----------|----------|----------------------------------------------------------------|
| `local`   | `-L`     | listen **here**, forward to `remote` on the host (`→` in UI).  |
| `remote`  | `-R`     | listen **on the host**, forward back here (`←` in UI).         |
| `dynamic` | `-D`     | a SOCKS5 proxy on `local`, all traffic via the host (`⇄ *`).  |

For a `remote` tunnel, `remote` is the address listened on the SSH server, and
`local` is the address connections are forwarded to on this machine. A bare
port (or `:port`) binds **all interfaces** on the host (`*:port`) — the default,
so a reverse forward exposes your local service through the server. Use an
explicit host for loopback-only (`127.0.0.1:port`) or a specific interface:

```yaml
tunnels:
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
tunnels:
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
  `known_hosts`, daemon reachability and socket permissions, and (Linux)
  lingering. Prints a `✓`/`✗` line per check and exits non-zero on any failure.

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

| Symptom | Check |
|---------|-------|
| Tunnel stuck on `✗ host key not in known_hosts` | Accept the key in the TUI, or set `accept_new_hosts: true`. |
| `✗ listen ...: address already in use` | A local port is busy — `lsof -i :<port>` to find and stop the holder. |
| `portato list` errors with "daemon not running" | Start the daemon: `portato daemon`, or `portato install` to autostart it. |
| `✗ auth failed` | Start `ssh-agent` / `ssh-add`, or set an `identity:` key. Run `portato doctor`. |
| Tunnels die after logout (Linux) | Enable lingering: `loginctl enable-linger "$USER"`. |

## Documentation

The source of truth lives in [`docs/`](./docs):

- [`docs/SPEC.md`](./docs/SPEC.md) — technical specification (stack, architecture, config, IPC, TUI).
- [`docs/ROADMAP.md`](./docs/ROADMAP.md) — phase status.
- [`docs/CONVENTIONS.md`](./docs/CONVENTIONS.md) — how phases are planned and implemented.

## License

Portato is licensed under the [MIT License](./LICENSE). All dependencies are
permissive (MIT / Apache-2.0 / BSD); there is no copyleft.
