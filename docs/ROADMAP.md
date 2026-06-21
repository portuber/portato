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

### Post-MVP (phases 7–12, outline — detailed when reached)

| #   | Name                              | Status | File                                                  |
|-----|-----------------------------------|--------|-------------------------------------------------------|
| 7   | Remote (-R) tunnels               | `[x]`  | [phase-7-remote-R.md](./phases/phase-7-remote-R.md)   |
| 8   | Dynamic (-D) SOCKS5               | `[x]`  | [phase-8-dynamic-D.md](./phases/phase-8-dynamic-D.md) |
| 9   | Push events instead of polling    | `[x]`  | [phase-9-push-events.md](./phases/phase-9-push-events.md) |
| 10  | TUI tunnel editor (e/n/d)         | `[~]`  | [phase-10-tui-editor.md](./phases/phase-10-tui-editor.md) |
| 11  | Polish (logs, themes, CI, doctor) | `[ ]`  | [phase-11-polish.md](./phases/phase-11-polish.md)     |
| 12  | Robust IPC socket discovery       | `[ ]`  | [phase-12-ipc-discovery.md](./phases/phase-12-ipc-discovery.md) |

Legend: `[ ]` pending · `[~]` in progress · `[x]` done

## Rules (quick summary)

1. **Sequencing:** phase N does not start until every phase in its `depends_on` is `[x]`.
2. **Parallelism:** at most **one** phase may be in work (`[~]`) at a time.
3. **Definition of Done:** every "Definition of Done" item in the phase file must be `[x]` before the phase status becomes `[x]`.
4. **Who moves statuses:** the human says "start phase N" / "complete phase N"; the agent verifies the conditions and edits the phase file + this table.
5. **Level of detail:** phases 0–6 (MVP) are described in detail; phases 7–11 (post-MVP) are outline only, filled in when reached.

## Current focus

**MVP complete (Phases 0–6).** All six MVP phases are `[x]`: config, native-SSH
forwarding, standalone TUI, the daemon with HTTP-over-unix-socket IPC, the CLI
+ smart launcher + background hand-off, and system autostart
(`portato install/uninstall` via launchd / systemd --user).

Phase 6 was closed by an explicit maintainer decision; the runtime-verified
parts are `install`/`list`/`uninstall` on macOS, idempotency, tunnels-off by
default, and clean vet/gofmt/cross-compilation. The reboot/relogin survival,
Linux lingering, and the full live-traffic/auto-reconnect MVP E2E were **not**
exercised and are recorded as a deferred-verification deviation in
[phase-6-autostart-e2e.md](./phases/phase-6-autostart-e2e.md) — recommended
manual checks before relying on autostart in production.

**Phase 7 — Remote (`-R`) tunnels — done.** `type: remote` tunnels now work: the
port is listened on the SSH server via `ssh.Client.Listen` and forwarded back to
a local address, with the shared dial/backoff/keepalive scaffolding reused and
the listener re-established on every reconnect. Direction is shown in the TUI and
`portato list` (`←` for remote), and a forbidden server-side bind surfaces a
`GatewayPorts` hint.

**Phase 8 — Dynamic (`-D`) SOCKS5 — done.** A `type: dynamic` tunnel runs a
SOCKS5 proxy on `local` whose per-connection dial is routed through
`ssh.Client.Dial`, reusing the Phase 2 listener/accept-loop scaffolding. The
local listener and accept-loop are shared with the `-L` path; the only
divergence is the per-connection handler (a `armon/go-socks5` server).
Direction shows as `⇄ *` in the TUI and `portato list`; reconnect is covered by
an integration test (drop/restart sshd → proxy works again).

**Phase 9 — Push events — done.** The 1s polling on both `localController`
and `remoteController` is gone. The Engine now fans every tunnel state change
to subscribers via a drop-old broker; `localController.Changes()` forwards it,
and the daemon's new `GET /events` SSE stream forwards it to attached clients
(`remoteController` reads the stream and reconnects with exponential backoff).
The Controller interface is unchanged, so the TUI redraws instantly on
`space`, on reconnect, and across two concurrent `attach` sessions — with zero
idle load. Also landed two follow-ups during the phase: `fix(daemon)` made the
macOS socket path deterministic (a build-tagged `~/Library/Application
Support/portato/` location, no longer depending on `XDG_RUNTIME_DIR`), and
`fix(tui)` gave errored tunnels a distinct `✗` indicator.

Next up: **Phase 10 — TUI tunnel editor** (`e`/`n`/`d`) — **in progress.**
Create, edit, and delete tunnels from the TUI without touching YAML. The daemon
owns all config I/O (new `GET /config` + per-tunnel `POST/PUT/DELETE /tunnels`
endpoints, comment-preserving via `yaml.Node` AST patching), so standalone and
attach behave identically. **Phase 12 — Robust IPC socket discovery** is planned
to replace the phase-9 `fix(daemon)` patch with a discovery-file + runtime-socket
design.

Phases 1–6 are the detailed MVP plan; 7–12 are outline (goal + DoD), refined as we approach them. Phase 12 (Robust IPC socket discovery) is planned to replace the phase-9 `fix(daemon)` socket-path patch with a discovery-file + runtime-socket design.

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
