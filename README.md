# Portato

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

## Build

```sh
make build   # produces bin/portato
make run     # go run ./cmd/portato
make test    # go test ./...
make vet     # go vet ./...
make fmt     # gofmt -w .
```

Requires Go 1.22+.

## Status

Phases 0–5 are done: config, native-SSH forwarding, standalone TUI, the daemon
with HTTP-over-unix-socket IPC, and the CLI + smart launcher + background
hand-off all work. Phase 6 adds system autostart (`portato install` /
`uninstall`) for macOS (launchd) and Linux (systemd --user).

See [`docs/ROADMAP.md`](./docs/ROADMAP.md) for the phase-by-phase status.

## Tunnel types

Each tunnel has a `type`:

| `type`    | SSH flag | Meaning                                                        |
|-----------|----------|----------------------------------------------------------------|
| `local`   | `-L`     | listen **here**, forward to `remote` on the host (`→` in UI).  |
| `remote`  | `-R`     | listen **on the host**, forward back here (`←` in UI).         |

For a `remote` tunnel, `remote` is the address listened on the SSH server (a
bare port binds loopback, the OpenSSH default), and `local` is the address
connections are forwarded to on this machine:

```yaml
tunnels:
  - name: pull-redis
    type: remote
    remote: 16379        # listened on the server (127.0.0.1:16379)
    local: 6379          # forwarded to the local redis
    ssh: user@bastion.example.com
```

**Binding a non-loopback address on the host** (e.g. `remote: 0.0.0.0:16379`)
requires `GatewayPorts yes` in the server's `sshd_config`. Otherwise the server
refuses the bind and the tunnel reports a `GatewayPorts` error.

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

## Documentation

The source of truth lives in [`docs/`](./docs):

- [`docs/SPEC.md`](./docs/SPEC.md) — technical specification (stack, architecture, config, IPC, TUI).
- [`docs/ROADMAP.md`](./docs/ROADMAP.md) — phase status.
- [`docs/CONVENTIONS.md`](./docs/CONVENTIONS.md) — how phases are planned and implemented.
