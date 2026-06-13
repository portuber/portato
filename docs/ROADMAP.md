# `portato` — Roadmap

> Summary status of all phases. Statuses are duplicated in the phase files and must match.
> Rules for working with statuses and sequencing — see [`CONVENTIONS.md`](./CONVENTIONS.md).
> Technical specification — see [`SPEC.md`](./SPEC.md).

## Phase status

### MVP (phases 0–6)

| #  | Name                                  | Status | File                                                  |
|----|---------------------------------------|--------|-------------------------------------------------------|
| 0  | Project skeleton + GSD                | `[~]`  | [phase-0-skeleton.md](./phases/phase-0-skeleton.md)   |
| 1  | Config                                | `[ ]`  | [phase-1-config.md](./phases/phase-1-config.md)       |
| 2  | Forward engine (native SSH, -L)       | `[ ]`  | [phase-2-forward-engine.md](./phases/phase-2-forward-engine.md) |
| 3  | Standalone TUI                        | `[ ]`  | [phase-3-standalone-tui.md](./phases/phase-3-standalone-tui.md) |
| 4  | Daemon and HTTP-over-unix-socket IPC  | `[ ]`  | [phase-4-daemon-ipc.md](./phases/phase-4-daemon-ipc.md) |
| 5  | CLI commands + smart launcher + hand-off | `[ ]`  | [phase-5-cli-smart-launcher.md](./phases/phase-5-cli-smart-launcher.md) |
| 6  | Autostart (launchd/systemd) + E2E     | `[ ]`  | [phase-6-autostart-e2e.md](./phases/phase-6-autostart-e2e.md) |

### Post-MVP (phases 7–11, outline — detailed when approaching)

| #   | Name                              | Status | File                                                  |
|-----|-----------------------------------|--------|-------------------------------------------------------|
| 7   | Remote (-R) tunnels               | `[ ]`  | [phase-7-remote-R.md](./phases/phase-7-remote-R.md)   |
| 8   | Dynamic (-D) SOCKS5               | `[ ]`  | [phase-8-dynamic-D.md](./phases/phase-8-dynamic-D.md) |
| 9   | Push events instead of polling    | `[ ]`  | [phase-9-push-events.md](./phases/phase-9-push-events.md) |
| 10  | TUI tunnel editor (e/n/d)         | `[ ]`  | [phase-10-tui-editor.md](./phases/phase-10-tui-editor.md) |
| 11  | Polish (logs, themes, CI, doctor) | `[ ]`  | [phase-11-polish.md](./phases/phase-11-polish.md)     |

Legend: `[ ]` pending · `[~]` in progress · `[x]` done

## Rules (quick summary)

1. **Sequencing:** phase N does not start until all phases in its `depends_on` are `[x]`.
2. **Parallelism:** only **one** phase may be in progress (`[~]`) at a time.
3. **Definition of Done:** all "Completion criteria" items in the phase file must be `[x]` before the phase status becomes `[x]`.
4. **Who moves statuses:** the human says "start phase N" / "complete phase N"; the agent checks the conditions and edits the phase file + this table.
5. **Detail level:** phases 0–6 (MVP) are described in detail; phases 7–11 (post-MVP) are outlined, filled in as they are approached.

## Current focus

**Phase 0 — Project skeleton** (pending). We'll start with it on the user's command.

Phases 1–6 — detailed MVP plan; 7–11 — outline (goal + DoD), refined as they are approached.

## Phase summary

- **Phase 0** — `go.mod`, cobra skeleton of all subcommands (stubs), directory tree, Makefile.
- **Phase 1** — YAML load/save, XDG paths, persist `enabled`, defaults, validation, unit tests.
- **Phase 2** — `Tunnel` + `Engine`: native ssh, ssh-agent + identity, TOFU known_hosts, reconnect+backoff, keepalive, only `-L`.
- **Phase 3** — `Controller` (local) + bubbletea list, hotkeys, run in standalone mode.
- **Phase 4** — `portato daemon` (HTTP over unix-socket), `Controller` (remote), `portato attach`, PID file, permissions 0600.
- **Phase 5** — CLI (`list/enable/disable/restart`), smart launcher `portato`, "to background?" modal + hand-off to daemon.
- **Phase 6** — `portato install/uninstall` (launchd + systemd --user), final MVP E2E checklist.
- **Phase 7** — `type: remote` (`-R`), `ssh.Client.Listen` on the remote side.
- **Phase 8** — `type: dynamic` (`-D`), SOCKS5 proxy.
- **Phase 9** — push events (`GET /events` SSE/chunked) instead of 1s polling.
- **Phase 10** — tunnel editor in TUI (`e`/`n`/`d`).
- **Phase 11** — logs in TUI (`l`), themes, `portato doctor`, tests, CI.

## Final MVP E2E (upon completion of Phase 6)

1. `go build ./...` and `go test ./...` — green.
2. In the config, one tunnel with `enabled: false`.
3. `portato install` → the daemon starts itself (launchctl/systemctl).
4. `portato list` shows the tunnel `○ Disabled`.
5. `portato` (TUI) → space → `Connecting` → `Connected`; `nc -z 127.0.0.1 <local>` succeeds, traffic flows.
6. space again → `Disabled`, port closed.
7. **Hand-off:** `portato` without daemon, space to enable the tunnel, `q`, answer `y` → the daemon is spawned, the tunnel keeps running, `portato list` confirms.
8. SSH server disconnect → auto-reconnect restores `Connected`.
9. After reboot/re-login — the daemon is up, tunnels `Disabled`.
10. `portato uninstall` → the service is removed cleanly.
