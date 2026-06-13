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

Project skeleton (Phase 0). Tunneling, the TUI, the daemon, and autostart are
stubs that respond with `not implemented yet`.

## Documentation

The source of truth lives in [`docs/`](./docs):

- [`docs/SPEC.md`](./docs/SPEC.md) — technical specification (stack, architecture, config, IPC, TUI).
- [`docs/ROADMAP.md`](./docs/ROADMAP.md) — phase status.
- [`docs/CONVENTIONS.md`](./docs/CONVENTIONS.md) — how phases are planned and implemented.
