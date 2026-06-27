# `portato` — Roadmap

> The summary state of all phases. The statuses are mirrored in the phase files and must match.
> For the rules on statuses and sequencing see [`CONVENTIONS.md`](./CONVENTIONS.md).
> For the technical specification see [`SPEC.md`](./SPEC.md).

## Phase status

### MVP (phases 0–6)

| #   | Name                                  | Status | File                                                  |
|-----|---------------------------------------|--------|-------------------------------------------------------|
| 0   | Project skeleton + GSD                | `[x]`  | [phase-0-skeleton.md](./phases/phase-0-skeleton.md)   |
| 1   | Config                                | `[x]`  | [phase-1-config.md](./phases/phase-1-config.md)       |
| 2   | Forward engine (native SSH, -L)       | `[x]`  | [phase-2-forward-engine.md](./phases/phase-2-forward-engine.md) |
| 3   | Standalone TUI                        | `[x]`  | [phase-3-standalone-tui.md](./phases/phase-3-standalone-tui.md) |
| 4   | Daemon and HTTP-over-unix-socket IPC  | `[x]`  | [phase-4-daemon-ipc.md](./phases/phase-4-daemon-ipc.md) |
| 5   | CLI commands + smart launcher + hand-off | `[x]`  | [phase-5-cli-smart-launcher.md](./phases/phase-5-cli-smart-launcher.md) |
| 6   | Autostart (launchd/systemd) + E2E     | `[x]`  | [phase-6-autostart-e2e.md](./phases/phase-6-autostart-e2e.md) |

### Post-MVP (phases 7–15)

| #   | Name                              | Status | File                                                  |
|-----|-----------------------------------|--------|-------------------------------------------------------|
| 7   | Remote (-R) tunnels               | `[x]`  | [phase-7-remote-R.md](./phases/phase-7-remote-R.md)   |
| 8   | Dynamic (-D) SOCKS5               | `[x]`  | [phase-8-dynamic-D.md](./phases/phase-8-dynamic-D.md) |
| 9   | Push events instead of polling    | `[x]`  | [phase-9-push-events.md](./phases/phase-9-push-events.md) |
| 10  | TUI tunnel editor (e/n/d)         | `[x]`  | [phase-10-tui-editor.md](./phases/phase-10-tui-editor.md) |
| 11  | Polish (logs, themes, CI, doctor) | `[x]`  | [phase-11-polish.md](./phases/phase-11-polish.md)     |
| 12  | Robust IPC socket discovery       | `[x]`  | [phase-12-ipc-discovery.md](./phases/phase-12-ipc-discovery.md) |
| 13  | Polish 2 (log rotation, `/` filter, goreleaser) | `[x]`  | [phase-13-polish-2.md](./phases/phase-13-polish-2.md) |
| 14  | TUI: duplicate tunnel (Shift+C)   | `[x]`  | [phase-14-tui-duplicate.md](./phases/phase-14-tui-duplicate.md) |
| 15  | Light-theme color tuning          | `[x]`  | [phase-15-light-theme-colors.md](./phases/phase-15-light-theme-colors.md) |

Legend: `[ ]` pending · `[~]` in progress · `[x]` done

## Rules (quick summary)

1. **Sequencing:** phase N does not start until every phase in its `depends_on` is `[x]`.
2. **Parallelism:** at most **one** phase may be in work (`[~]`) at a time.
3. **Definition of Done:** every "Definition of Done" item in the phase file must be `[x]` before the phase status becomes `[x]`.
4. **Who moves statuses:** the human says "start phase N" / "complete phase N"; the agent verifies the conditions and edits the phase file + this table.
5. **Level of detail:** phases 0–6 (MVP) and 7–15 (post-MVP) are all described in detail above and complete (`[x]`).

## Current focus

**All phases 0–15 are `[x]`.** The single binary runs the smart launcher
(attaches to a running daemon or starts standalone), a background daemon with
HTTP-over-unix-socket IPC, an interactive TUI, the CLI commands, and system
autostart (`install`/`uninstall` via launchd / systemd --user). It supports
`local` (`-L`), `remote` (`-R`) and `dynamic` (`-D`, SOCKS5) tunnels, push-based
status events, an in-TUI editor (`e`/`n`/`d`) and tunnel duplication
(`Shift+C`), a per-tunnel log screen (`l`), an interactive unknown-host (TOFU)
prompt, automatic light/dark theming, `portato doctor`, robust IPC socket
discovery, size-rotated logs, a `/` list filter, and goreleaser release tooling.

### Caveats / deviations
- **Phase 6 autostart — runtime-verified.** macOS launchd: `install`/`list`/
  `uninstall`, idempotency, `KeepAlive` respawn, and **real reboot/relogin
  survival** (after a macOS reboot the daemon was back up). Linux/systemd
  (Debian 12 in Docker, `e2e/systemd-docker/`): lingering, `docker restart`
  survival, uninstall-does-not-return, live-traffic, auto-reconnect. See
  [phase-6-autostart-e2e.md](./phases/phase-6-autostart-e2e.md).
- **Behavior change (`feat(config)`, alongside Phase 13):** a `type: remote`
  tunnel's bare port or `:port` in `remote` normalises to `*:port` (all
  interfaces) instead of loopback; loopback-only is now opt-in via
  `127.0.0.1:port`, and a non-loopback bind still needs `GatewayPorts` on the
  server. See SPEC §7/§8.

### Post-MVP backlog (candidates, not yet phased)
- Windows support (named pipe + the registry Run key).
- Seamless standalone→daemon hand-off via FD-passing (no port-availability gap).
- IPC authorization token (currently 0600 filesystem perms only).
- Identity passphrase storage when ssh-agent is unavailable.
- `--log-level` flag; `portato list --json`; SOCKS5 user/pass auth.
- Fuzzy (`fzf`-style) list filter; Homebrew/scoop/deb-rpm packaging;
  config-driven log-rotation knobs.
- launchd/systemd socket activation; concurrent-start hardening (flock on the
  discovery marker).

## Phase summary

- **Phase 0** — `go.mod`, the cobra skeleton of all subcommands (stubs), the directory tree, the Makefile.
- **Phase 1** — YAML load/save, XDG paths, `enabled` persistence, defaults, validation, unit tests.
- **Phase 2** — `Tunnel` + `Engine`: native ssh, ssh-agent + identity, TOFU known_hosts, reconnect + backoff, keepalive, `-L` only.
- **Phase 3** — `Controller` (local) + the bubbletea list, hotkeys, running in standalone mode.
- **Phase 4** — `portato daemon` (HTTP over a unix socket), `Controller` (remote), `portato attach`, the PID file, 0600 permissions.
- **Phase 5** — the CLI (`list/enable/disable/restart`), the smart launcher `portato`, the "background?" modal + hand-off to the daemon.
- **Phase 6** — `portato install/uninstall` (launchd + systemd --user), the final MVP E2E checklist.
- **Phase 7** — `type: remote` (`-R`), `ssh.Client.Listen` on the remote side.
- **Phase 8** — `type: dynamic` (`-D`), a SOCKS5 proxy.
- **Phase 9** — push events (`GET /events` SSE/chunked) instead of 1s polling.
- **Phase 10** — a tunnel editor in the TUI (`e`/`n`/`d`).
- **Phase 11** — logs in the TUI (`l`), themes, `portato doctor`, tests, CI.
- **Phase 12** — robust IPC socket discovery: the daemon advertises its socket path via a stable discovery file; clients read it (socket lives in `$TMPDIR` / `$XDG_RUNTIME_DIR`).
- **Phase 13** — polish 2 (deferred phase-11 items): persistent rotated log file, the `/` tunnel-list filter, goreleaser release tooling.
- **Phase 14** — duplicate the selected tunnel in the TUI (`Shift+C`): opens the Phase 10 editor in create mode, prefilled under a fresh `<name>-copy`; commits via `AddTunnel`.

## Final MVP E2E (on completing Phase 6)

1. `go build ./...` and `go test ./...` — green.
2. The config has one tunnel with `enabled: false`.
3. `portato install` -> the daemon starts on its own (launchctl/systemctl).
4. `portato list` shows the tunnel as `○ Disabled`.
5. `portato` (TUI) -> space -> `Connecting` -> `Connected`; `nc -z 127.0.0.1 <local>` succeeds, traffic flows.
6. space again -> `Disabled`, the port is closed.
7. **Hand-off:** `portato` with no daemon, space to enable the tunnel, `q`, answer `y` -> the daemon is spawned, the tunnel keeps running, `portato list` confirms it.
8. SSH server dropped -> auto-reconnect restores `Connected`.
9. After a reboot/relogin — the daemon is up, the tunnels are `Disabled`.
10. `portato uninstall` -> the service is removed cleanly.
