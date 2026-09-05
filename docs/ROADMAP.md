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

### Post-MVP (phases 7–24)

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
| 16  | Seamless hand-off (FD-passing)    | `[x]`  | [phase-16-fd-passing-handoff.md](./phases/phase-16-fd-passing-handoff.md) |
| 17  | Windows support                   | `[x]`  | [phase-17-windows.md](./phases/phase-17-windows.md) |
| 18  | IPC authorization token           | `[x]`  | [phase-18-ipc-token.md](./phases/phase-18-ipc-token.md) |
| 19  | Identity passphrase storage       | `[x]`  | [phase-19-identity-passphrase.md](./phases/phase-19-identity-passphrase.md) |
| 20  | CLI/UX polish                     | `[x]`  | [phase-20-cli-ux-polish.md](./phases/phase-20-cli-ux-polish.md) |
| 21  | Packaging and releases            | `[x]`  | [phase-21-packaging.md](./phases/phase-21-packaging.md) |
| 22  | Robustness (socket activation…)   | `[x]`  | [phase-22-robustness.md](./phases/phase-22-robustness.md) |
| 23  | TUI list column alignment         | `[x]`  | [phase-23-tui-list-column-alignment.md](./phases/phase-23-tui-list-column-alignment.md) |
| 24  | TUI branding / logo               | `[x]`  | [phase-24-tui-logo.md](./phases/phase-24-tui-logo.md) |
| 25  | Easter egg — "pórtate bien" in --help | `[x]`  | [phase-25-easter-egg-portate-bien.md](./phases/phase-25-easter-egg-portate-bien.md) |
| 26  | Fix: renamed tunnel restarts under new name | `[x]`  | [phase-26-rename-restart-fix.md](./phases/phase-26-rename-restart-fix.md) |
| 27  | portato stop                     | `[x]`  | [phase-27-portato-stop.md](./phases/phase-27-portato-stop.md) |
| 28  | config reload (reload CLI + watch) | `[x]` | [phase-28-config-reload.md](./phases/phase-28-config-reload.md) |
| 29  | standalone/daemon enabled consistency | `[x]` | [phase-29-standalone-daemon-enabled-consistency.md](./phases/phase-29-standalone-daemon-enabled-consistency.md) |
| 30  | TUI toggle vs passphrase-prompt  | `[x]`  | [phase-30-tui-toggle-vs-passphrase.md](./phases/phase-30-tui-toggle-vs-passphrase.md) |
| 31  | TUI logo wordmark + drop PNG mode | `[x]` | [phase-31-logo-wordmark.md](./phases/phase-31-logo-wordmark.md) |
| 32  | Third-party license notices in releases | `[x]` | [phase-32-third-party-licenses.md](./phases/phase-32-third-party-licenses.md) |
| 33  | CodeFactor cleanup + golangci-lint guardrails | `[x]` | [phase-33-codefactor-cleanup.md](./phases/phase-33-codefactor-cleanup.md) |
| 34  | `portato license` command + `--license` flag | `[x]` | [phase-34-license-command.md](./phases/phase-34-license-command.md) |
| 35  | SSH password authentication (on by default) | `[x]` | [phase-35-ssh-password.md](./phases/phase-35-ssh-password.md) |
| 36  | CI security hardening (govulncheck + lint in CI) | `[x]` | [phase-36-ci-security.md](./phases/phase-36-ci-security.md) |
| 37  | TUI theme portability & color     | `[x]` | [phase-37-tui-theme-portability.md](./phases/phase-37-tui-theme-portability.md) |
| 38  | TUI responsive layout              | `[x]` | [phase-38-tui-responsive-layout.md](./phases/phase-38-tui-responsive-layout.md) |
| 39  | TUI polish (modals, microcopy)     | `[x]` | [phase-39-tui-polish.md](./phases/phase-39-tui-polish.md) |
| 40  | Recover from / prevent the wedged daemon | `[x]` | [phase-40-wedged-daemon-recovery.md](./phases/phase-40-wedged-daemon-recovery.md) |
| 41  | Forwarding-permission diagnostics | `[x]` | [phase-41-forwarding-permission-diagnostics.md](./phases/phase-41-forwarding-permission-diagnostics.md) |
| 42  | `portato logs` (tail/follow)      | `[x]` | [phase-42-portato-logs.md](./phases/phase-42-portato-logs.md) |
| 43  | ProxyJump (jump hosts)            | `[x]` | [phase-43-proxyjump.md](./phases/phase-43-proxyjump.md) |
| 44  | `~/.ssh/config` resolution        | `[x]` | [phase-44-ssh-config.md](./phases/phase-44-ssh-config.md) |
| 45  | Shell completions                 | `[x]` | [phase-45-shell-completions.md](./phases/phase-45-shell-completions.md) |
| 46  | Tunnel tags / groups              | `[x]` | [phase-46-tunnel-tags.md](./phases/phase-46-tunnel-tags.md) |
| 47  | Windows SCM autostart             | `[x]`  | [phase-47-windows-scm-autostart.md](./phases/phase-47-windows-scm-autostart.md) |
| 48  | Import forwards from ssh_config   | `[x]`  | [phase-48-ssh-config-import.md](./phases/phase-48-ssh-config-import.md) |
| 49  | Update checker (consent-gated)    | `[x]`  | [phase-49-update-checker.md](./phases/phase-49-update-checker.md) |
| 50  | Self-update (update apply)        | `[x]`  | [phase-50-self-update.md](./phases/phase-50-self-update.md) |
| 51  | TUI header version segment        | `[~]`  | [phase-51-tui-header-version.md](./phases/phase-51-tui-header-version.md) |

Legend: `[ ]` pending · `[~]` in progress · `[x]` done

## Rules (quick summary)

1. **Sequencing:** phase N does not start until every phase in its `depends_on` is `[x]`.
2. **Parallelism:** at most **one** phase may be in work (`[~]`) at a time.
3. **Definition of Done:** every "Definition of Done" item in the phase file must be `[x]` before the phase status becomes `[x]`.
4. **Who moves statuses:** the human says "start phase N" / "complete phase N"; the agent verifies the conditions and edits the phase file + this table.
5. **Level of detail:** phases 0–6 (MVP) and 7–15 (post-MVP) are described in detail above and complete (`[x]`); phases 16–22, 24–31 (post-MVP backlog) are planned in detail — all are done (`[x]`), including **17 (Windows)** (IPC over a named pipe via `go-winio`, autostart via the HKCU registry Run key — superseded by SCM in Phase 47, `windows-smoke` CI green). Phase 21 (packaging) is done (`[x]`): goreleaser publishes a GitHub Release + Homebrew cask + Scoop bucket + deb/rpm/apk (bundling `THIRD_PARTY_LICENSES.txt`).

## Current focus

**Phases 0–50 are all `[x]` — the update pair (49 checker, 50 self-update)
is complete.** The last one to close was 50: `portato update apply` with
SHA-256-verified downloads, an atomic swap with a one-level `portato.old`
rollback, and package-manager etiquette (managed installs get their own
upgrade command; a Windows SCM-held binary is never swapped). Phase 49
shipped in **v1.7.0**; the rest lives in
[Post-1.0 candidate features](#post-10-candidate-features). The core
includes ProxyJump (jump hosts), `~/.ssh/config` alias resolution and
forward import, shell completion, and tunnel tags/groups. The stability
surface (`config.yaml` + the CLI; see [`VERSIONING.md`](./VERSIONING.md)) has
no planned breaking changes; any future break goes through the deprecation
cycle defined there. For the most recent batch see [Current work](#current-work);
for per-phase status see the table above.

The single binary runs the smart launcher (attaches to a running daemon or
starts standalone), a background daemon with HTTP-over-unix-socket IPC, an
interactive TUI, the CLI commands, and system autostart (`install`/`uninstall`
via launchd / systemd --user / Windows SCM service). It supports `local` (`-L`),
`remote` (`-R`) and `dynamic` (`-D`, SOCKS5) tunnels, `jump:` (ProxyJump /
OpenSSH `-J` — reach a host through a bastion), `~/.ssh/config` alias
resolution for `ssh:`, push-based status events,
an in-TUI editor (`e`/`n`/`d`) and tunnel duplication (`Shift+C`), a per-tunnel
log screen (`l`), `portato logs`, `portato doctor` (incl. `--probe`),
`portato license`, an interactive unknown-host (TOFU) prompt, automatic
light/dark theming, robust IPC socket discovery, size-rotated logs, a `/` list
filter, goreleaser releases (Homebrew cask + Scoop + deb/rpm/apk +
`THIRD_PARTY_LICENSES.txt`), govulncheck + lint in CI, and IPC bearer-token
auth layered on the `0600` socket.

### Caveats / deviations
- **Behavior change (`feat(config)`, alongside Phase 13):** a `type: remote`
  tunnel's bare port or `:port` in `remote` normalises to `*:port` (all
  interfaces) instead of loopback; loopback-only is now opt-in via
  `127.0.0.1:port`, and a non-loopback bind still needs `GatewayPorts` on the
  server. See SPEC §7/§8.
- **Behavior change (`fix(forward)`, Phase 26):** on a config reload, a
  newly-appeared tunnel (a rename or an add) whose config has `enabled: true`
  is now started — mirroring `StartEnabled` at daemon boot. Previously such a
  tunnel was created `Off`, so a renamed live tunnel died and a newly-added
  `enabled: true` tunnel stayed off until manually toggled. See
  [phase-26-rename-restart-fix.md](./phases/phase-26-rename-restart-fix.md).

### Post-MVP backlog
All previously-backlogged items now have detailed phase plans (todo) — see
phases 16–22, 27–31 in the table above and in [`docs/phases/`](./phases/).
Phase 16 (seamless hand-off via FD-passing) is done and proved by
`make e2e-handoff`. Items not yet covered anywhere: seamless hand-off FD-passing
on Windows (Phase 17 will need a Windows-specific mechanism or skip), and
time-based (not just size-based) log rotation.

## Post-1.0 candidate features

Beyond Phase 48, the following are prioritised candidates — not yet formal
phases; each is promoted to a numbered phase (with a `phase-N-*.md` file)
when taken up. All are additive, so they ship as MINORs; patch releases are
fixes only.

1. **Per-tunnel stats** — bytes in/out, connection count, reconnect count
   (collected at the single `pipe()` chokepoint) shown in the TUI and
   `list --json`; folds in the deferred Phase-39 aggregate line
   (`n connected · n error · n off`).
2. **Unix-socket forwarding** — `-L /var/run/docker.sock:…` to reach a
   remote `docker.sock`; the forward direction is cheap via
   `client.Dial("unix", path)`, the reverse (`streamlocal-forward@openssh.com`)
   is harder.
3. **Lazy tunnels** — dial SSH only on the first connection + an idle
   timeout to disconnect; fits the FD-hand-off listener/client separation
   (Phase 16). Solves the "laptop with 20 tunnels" pain.
4. **State-change hooks / notifications** — `on_error:` / `on_connect:` cmd
   and/or a desktop notification when a tunnel drops or recovers; the daemon
   is headless, so this surfaces breaks without opening the TUI.
5. **Shared SSH client pool** — reuse one `*ssh.Client` per
   `user@host:port` with refcounting (fewer handshakes / password prompts
   for many tunnels to one bastion). The riskiest — it reworks the
   per-tuber reconnect / backoff / keepalive state machine onto a shared
   client — so it lands last.
 6. **Metadata-only refresh (no reconnect)** — editing *only* a tuber's
    `tags:` currently triggers a full `Reconfigure` (SSH reconnect) because
    `tuberChanged` lumps Tags with connection-affecting fields (the v1.4.1
    fix). Tags are pure metadata; split a metadata-refresh path in
    `Engine.Reload` (`UpdateMetadata`: `t.cfg = cfg` + notify, no `Restart`)
    for tags-only changes so live-tag-edit doesn't blip a connected tunnel.

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
- **Phase 15** — light-theme color tuning: the light theme's surface colour is baked into every style so each row stays readable.
- **Phase 16** — seamless hand-off via FD-passing: the standalone passes its already-bound local listeners to the spawned daemon over SCM_RIGHTS, so the local ports never go down across the transition (proved by `make e2e-handoff`).
- **Phase 17** — Windows support (done, `[x]`; IPC over a named pipe via `go-winio`, autostart via the HKCU Run key — superseded by the SCM service in Phase 47, `windows-smoke` CI green).
- **Phase 18** — IPC authorization token: a 32-byte bearer token layered on the `0600` socket; `--ipc-token off` disables.
- **Phase 19** — identity passphrase storage: an in-memory cache backed by the OS keyring; `portato add-identity`/`forget-identity`.
- **Phase 20** — CLI/UX polish: `--log-level`, `portato list --json`, SOCKS5 user/pass auth for `dynamic`, a fuzzy (subsequence) `/` filter.
- **Phase 21** — packaging and releases (done, `[x]`): goreleaser publishes a GitHub Release + Homebrew cask + Scoop bucket + deb/rpm.
- **Phase 22** — robustness: a single-instance flock + systemd socket activation for the IPC socket.
- **Phase 23** — TUI list column alignment.
- **Phase 24** — TUI branding / logo banner.
- **Phase 25** — easter egg: "pórtate bien" footer in `--help`.
- **Phase 26** — fix: a renamed tunnel now restarts under its new name on reload.
- **Phase 27** — `portato stop`: gracefully terminate the running daemon (SIGTERM via the marker PID).
- **Phase 28** — config reload: `portato reload` CLI + a polling file watcher that auto-reloads `config.yaml` on change (keeps last-good on a parse error).
- **Phase 29** — standalone/daemon enabled consistency: the standalone now auto-starts `enabled: true` tunnels on launch, matching the daemon.
- **Phase 30** — TUI toggle vs passphrase: `space` toggles purely by state; `p` enters a passphrase for a blocked tunnel.
- **Phase 31** — TUI logo wordmark: a combined potato+PORTATO wordmark in the empty-config splash and `--version` (compact-potato fallback on narrow terminals), the compact potato kept in the help overlay, and the inline-PNG image mode removed (iTerm2/WezTerm render braille).
- **Phase 32** — third-party license notices in releases: bundle each runtime dependency's LICENSE (MIT/Apache-2.0/BSD-3) into the GitHub Release archives and deb/rpm, generated at release time via `go-licenses`, closing the redistribution-notice obligation that phase 21 declared but didn't implement.
- **Phase 33** — clear codefactor.io's 12 issues (6 builtin-`max` shadowing + 6 complex methods, incl. 2 test funcs) and add a `golangci-lint` config + `make lint` so builtin shadowing and high-complexity production methods can't slip back in.
- **Phase 34** — `portato license` subcommand + `--license` root flag (parallel to `version`/`--version`): the binary self-reports its MIT license and points to the bundled `THIRD_PARTY_LICENSES.txt`; `license --full` prints the embedded MIT text. A MINOR; shipped in v0.2.0/v0.2.1.
- **Phase 35** — SSH password authentication (done, `[x]`): an on-by-default password-auth fallback (OpenSSH-style — keys tried first, then prompt when no key authenticates) with `password_auth: false` to opt out; the password is supplied interactively (TUI `o` modal / `POST /tubers/{name}/password` / `controller.AcceptPassword`) and, opt-in, persisted to the OS keyring (`defaults.ssh_password_store`), never to config. Keys stay the default; the re-prompt is a dial-level loop (golang.org/x/crypto/ssh does not retry the password method within one handshake). depends_on [19, 30]. Surfaced while verifying Phase 17 on a password-only server.
- **Phase 36** — CI security hardening (done, `[x]`): close two CI gaps — a `govulncheck` workflow (PR/push + weekly cron) scanning Go dependencies for reachable CVEs, and a `lint` job in `ci.yml` enforcing the existing `.golangci.yml` (which `make lint` runs locally but CI never did). Adding the workflow immediately surfaced 5 reachable stdlib CVEs in the pinned toolchain (crypto/tls, crypto/x509, net, net/http), fixed by bumping the toolchain 1.26.2 → 1.26.5; `govulncheck ./...` now exits 0. depends_on [33]. Out of scope: Dependabot, CodeQL, Go Reference badge.
- **Phase 37** — TUI theme portability & color correctness (done, `[x]`): pick the palette against the real terminal background (`tea.RequestBackgroundColor` → `BackgroundColorMsg`, with an explicit `PORTATO_THEME` → OSC 11 → `COLORFGBG` → dark degradation chain), move palette resolution off package init onto `Model`, fix the light surface fill under tmux, and correct the contrast-failing dark colors + the mono `connecting`/`connected` glyph split. depends_on [15].
- **Phase 38** — TUI responsive layout (done, `[x]`): the footer fits at 80/60 cols (`? help`/`q quit` visible), the `?` help overlay is reachable at 80×24, and the table columns shrink by priority (STATUS untouchable, ENDPOINT shrinks first, NAME flex, UPTIME right-aligned). depends_on [23].
- **Phase 39** — TUI polish (done, `[x]`): interactive modals render in the footer zone with the list visible (footer-zone replace, not a dimmed overlay — a true overlay can't be theme-complete in mono/dark); footer pinned to the bottom edge; empty-state CTA → `n` with footer keys filtered; `hasLiveTubers` counts the Error state with mode-aware q-quit microcopy; attach header drops its socket path; error text keeps the actionable tail (row tail-truncation + a full-error detail strip); `C` duplicate auto-bumps the local port. depends_on [].
- **Phase 40** — recover from / prevent the wedged daemon (done, `[x]`): on macOS the IPC socket lived under the reaped `$TMPDIR`, so when macOS unlinked it a running daemon was wedged (alive, holding the flock + local ports, but unreachable); `portato daemon` then said "already running", `portato stop` said "no daemon running", and the standalone TUI hit "address already in use". Prevention moves the darwin socket into the stable `xdg.StateHome/portato/` dir; recovery makes `stop` SIGTERM the wedged PID from the marker (guarded against PID reuse) and `doctor` diagnose it. Shipped in v0.4.2. depends_on [].
- **Phase 41** — forwarding-permission diagnostics (done, `[x]`): `portato doctor --probe` (opt-in) dials each configured host with key-only auth and classifies the server-side sshd gate — chiefly detecting `AllowTcpForwarding no` (a direct-tcpip open rejected with `ssh.Prohibited`), plus connectivity/auth; a non-loopback `-R` bind gets an honest "GatewayPorts not verifiable client-side" caveat (the silent-loopback downgrade is not client-detectable — RFC 4254 §7.1). The `-L`/`-D` runtime dial surfaces an `AllowTcpForwarding` hint on such a rejection. depends_on [11, 7, 8].
- **Phase 42** — `portato logs` (done, `[x]`): a CLI command to read the persisted daemon log (`daemon.log`, fallback `portato.log`) with `-f/--follow`, `-n/--lines`, `--since`, `--tuber`, `--all` — the missing `docker logs` / `journalctl` equivalent (the TUI `l` view is a live ring buffer only). depends_on [13].
- **Phase 43** — ProxyJump (jump hosts) (done, `[x]`): a `jump:` field (single hop or a comma-chain) dials a target through one or more bastions — the OpenSSH `-J` equivalent. The dial already separates the TCP dial from the SSH handshake, so chaining reuses the handshake per hop: hop 0 via `net.Dialer`, each later hop via the previous hop's `ssh.Client.Dial` wrapped in `ssh.NewClientConn`; the final client runs the tuber's forward unchanged. Intermediate hops are key-only (the shared agent/identity — the bastion must accept the same key); the Phase 35 password fallback applies only to the final target. Per-hop host keys are verified against `known_hosts`; a leash goroutine closes the intermediates once the final client disconnects (no reconnect leak). `~/.ssh/config` resolution and per-hop identity are follow-ups. depends_on [].
- **Phase 44** — `~/.ssh/config` resolution (done, `[x]`): `ssh: <alias>` resolves HostName/User/Port/IdentityFile/ProxyJump from the user's ssh config (via `kevinburke/ssh_config`, first-match-wins, patterns + `Include` honoured), so host definitions aren't duplicated in `config.yaml`; an alias's `ProxyJump` auto-populates Phase 43's `jump:` (resolved recursively, cycle-guarded), which Phase 43 then dials. Explicit tuber fields (`identity:` / `jump:` / `ssh: me@x:port`) override ssh-config; a missing alias is used literally (openssh-faithful), and only an unreadable `~/.ssh/config` or a ProxyJump cycle/depth is a clear load error. The dial path is unchanged — this is a config-layer resolution. depends_on [43].
- **Phase 45** — Shell completions (done, `[x]`): dynamic TAB completion of tuber names for `enable` / `disable` / `restart` / `forward` and `logs --tuber` (via `ValidArgsFunction` / `RegisterFlagCompletionFunc`; source = `config.yaml` load, so it works with no daemon running). Cobra's `completion bash/zsh/fish/powershell` already generates the shell script; the README documents per-shell sourcing (`eval` / `source`, document-only — no packaging changes). Additive ⇒ MINOR (`v1.3.0`). depends_on [].
- **Phase 46** — Tunnel tags / groups (done, `[x]`): `tags:` on a tuber; `enable` / `disable` / `restart --tag X` operate on every tuber with that tag (with `--tag` TAB-completion of the distinct values from `config.yaml`); a precise `#tag` filter in the TUI (a leading `#` is an exact tag selector — `#db` matches a tuber *tagged* `db`, not one merely *named* `db-stage`); and `a` / `x` (enable/disable-all) respect the active `/` filter for instant group ops (no filter ⇒ byte-identical to before). `forward.Status` carries tags so `list` / `list --json` and the TUI show and filter on them over IPC (no new IPC method); the editor gains a tags field. Tags render as `#tag` tokens everywhere (byte-identical to the filter syntax); in the TUI they live in the Phase-39 detail strip (≤1 line, an error wins) rather than a new column, keeping the Phase-38 responsive layout intact. Additive ⇒ MINOR (`v1.4.0`). depends_on [].
- **Phase 47** — Windows SCM autostart (done, `[x]`; shipped in v1.5.0, verified and hardened through v1.6.1): replaces the Phase-17 HKCU `Run`-key autostart with a real Service Control Manager service so the daemon starts at **boot** (not logon), runs **without anyone logged in** (SCM logs on the install-time user — the password is collected once at `portato install` and kept as an LSA secret), and `install` **starts the daemon immediately** (parity with macOS `launchctl bootstrap` / Linux `systemctl --user enable --now`). Also fixes the Scoop drift failure mode (`portato install` captures a version-pinned path that breaks on the next `scoop update`; the service path is rewritten to Scoop's stable `current` junction), makes `portato stop` graceful (sends `svc.Stop` instead of `TerminateProcess`), and keeps the Phase-17 Run-key mechanism behind `--legacy-runkey` for locked-down environments. The deferred refinement from the end of Phase 17. The final verification (real reboot, no login) surfaced and fixed three IPC gaps shipped as v1.6.1: the named pipe's explicit SDDL (an unelevated `list`/`doctor` reaches the boot-started service), pidAlive treating an unopenable session-0 PID as alive (the discovery marker is no longer culled), and `doctor` seeing the SCM service without elevation. depends_on [17].
- **Phase 48** — import forwards from `~/.ssh/config` (done, `[x]`): `portato import` (+ a fresh-install one-time offer) scans `LocalForward` / `RemoteForward` / `DynamicForward` via the Phase-44 config reader (`GetAll`, so multi-forward hosts import fully) and creates `enabled: false` tubers — a one-time copy that never modifies ssh_config; the nudge is marker-gated (`fresh_install` / `import_offered` in the state dir), interactive-only, never repeats, and never fires for upgrading users. depends_on [44].
- **Phase 49** — update checker (done, `[x]`): `internal/update` + `portato update check` / `update consent`; a consent-gated daily GitHub `releases/latest` poll (anonymous, 24h TTL, cache in `xdg.StateHome/portato/update.json`) surfacing "vX.Y.Z available" in the TUI header and `portato doctor`; the one-time consent ask (`defaults.update_check` absent = pending; `[Y/n]`, Enter = yes) follows the Phase-48 nudge pattern (interactive launcher + install + a green doctor; the daemon never asks), and consent lives in `config.yaml` (comment-preserving AST patch; a live edit reaches the daemon through the Phase-28 reload path). Hand-rolled semver under the strict-`vX.Y.Z` VERSIONING policy; the API base is compile-time-only (no runtime redirect); zero new dependencies. **Shipped in v1.7.0** (verified live: the released binary checks itself against releases/latest and reports "up to date"; the brew cask and scoop manifest published from the tag). depends_on [21].
- **Phase 51** — TUI header version segment (planned, `[ ]`): the running version next to `mode:` (`mode: attach  v1.8.1`, `dev` verbatim on dev builds), with the phase-49 update hint as the sibling segment; on narrow widths the hint shortens to `→ v1.9.0` so the header never wraps. Promoted from the Post-1.0 candidate list; `tui.Options.Version` is already plumbed (Phase 49). TUI internals ⇒ PATCH. depends_on [].
- **Phase 50** — self-update (done, `[x]`): `portato update apply [--yes|--force|--dry-run]` — download the GOOS/GOARCH archive, SHA256-verify against `checksums.txt`, atomic swap with a one-level `portato.old` rollback; package-managed installs (brew/scoop/deb/rpm/apk/go install) are detected and refused in place, printing the channel's own upgrade command (Windows Scoop/SCM included — the Phase-47 `current`-junction is never desynced); a live daemon is detected and prompted to restart. Windows direct installs swap via a staged `portato.new` completed at the next launch (pre-cobra, the Phase-47 precedent). depends_on [49].

## Current work

**Phases 0–50 are all `[x]`.** The most recent batch:

- **Phase 50** — self-update: `portato update apply` closes the loop the
  phase-49 checker opened. The platform archive (goreleaser-named) is
  downloaded into a temp dir and SHA-256-verified against the release
  `checksums.txt` — a mismatch or an unlisted file aborts with the install
  untouched — then the single `portato` member is extracted (mode from the
  running binary) and swapped: two atomic renames on unix with a one-level
  `portato.old` rollback, a staged next-launch swap on Windows (pre-cobra
  entry point, the Phase-47 precedent). Etiquette: brew/scoop/deb/rpm/apk/
  go-install channels are detected (path heuristics + dpkg/rpm/apk
  ownership) and refused in place with their own upgrade command printed;
  `--force` overrides the refusal but never the checksum, and a Windows
  SCM-held binary is never swapped. A live daemon gets a restart hint;
  doctor names the channel; nothing is ever auto-applied.

- **Phase 49** — update checker: portato learns a newer release exists —
  after a one-time consent question, never before. `internal/update`
  (GitHub `releases/latest` client with a compile-time-only base — no env,
  no flag — plus a strict integer-triple semver), `defaults.update_check`
  in config.yaml (absent = not asked; one `[Y/n]` on the first interactive
  launch / install / green doctor, Enter = yes, never repeated), the
  daemon's hourly-ticker/24h-TTL anonymous poll (consent re-read every
  tick, so flips need no restart; failures never advance the clock), a
  disposable cache in the state dir feeding `doctor` and the TUI header's
  `update: vX.Y.Z` segment (both network-free), and `portato update
  check` / `update consent on|off|ask`. Zero new dependencies; dev builds
  report "not comparable" instead of a bogus verdict.

- **Phase 36** — CI security hardening: a `govulncheck` workflow (PR/push +
  weekly cron) scanning dependencies for reachable CVEs, plus a `lint` job in
  CI enforcing `.golangci.yml`; it surfaced 5 reachable stdlib CVEs in the
  pinned toolchain (crypto/tls, crypto/x509, net, net/http), fixed by bumping
  1.26.2 → 1.26.5.
- **Phase 37** — TUI theme portability & color correctness: the palette is
  picked against the real terminal background (`tea.RequestBackgroundColor` →
  `BackgroundColorMsg`, with a `PORTATO_THEME` → OSC 11 → `COLORFGBG` → dark
  degradation chain), palette resolution moved off package init onto the
  model, the light surface fill is fixed under tmux, and the contrast-failing
  dark colors + the mono connecting/connected glyph split are corrected.
- **Phase 38** — TUI responsive layout: the footer fits at 80/60 cols (`? help`
  / `q quit` always reserved at the tail), `?` is a full-screen scrollable help
  view reachable at 80×24, and the table columns shrink by priority (STATUS
  untouchable, ENDPOINT shrinks first, NAME flex, UPTIME right-aligned).
- **Phase 39** — TUI polish: interactive modals (delete / TOFU / passphrase /
  password / quit) render in the footer zone with the list visible
  (footer-zone replace, not a dimmed overlay — a true overlay can't be
  theme-complete in mono/dark); the footer is pinned to the bottom edge; the
  empty state routes to `n` and the footer hides keys that don't apply;
  `hasLiveTubers` counts the Error state with mode-aware q-quit microcopy;
  the attach header drops its socket path (`portato doctor` exposes it); error
  text keeps the actionable tail (row tail-truncation + a one-line full-error
  detail strip above the footer); and `C` duplicate auto-bumps the local port.
- **Phase 40** — recover from / prevent the wedged daemon (done, `[x]`; shipped
  in v0.4.2): on macOS the IPC socket moved out of the reaped `$TMPDIR` into
  the stable `xdg.StateHome/portato/` dir, and `portato stop`/`doctor` recover
  from / diagnose the wedged state (an alive PID holding the flock + local
  ports but an unreachable socket) instead of deleting the marker and reporting
  "no daemon running".

- **Phase 41** — forwarding-permission diagnostics (done, `[x]`): an opt-in
  `portato doctor --probe` dials each configured host with key-only auth and
  classifies the server-side sshd gate, chiefly detecting `AllowTcpForwarding
  no` (a direct-tcpip open rejected with `ssh.Prohibited`); the `-L`/`-D`
  runtime dial surfaces an `AllowTcpForwarding` hint on such a rejection. A
  non-loopback `-R` bind gets an honest "GatewayPorts not verifiable
  client-side" caveat (the silent-loopback downgrade is not client-detectable —
  RFC 4254 §7.1).

- **Phase 42** — `portato logs` (done, `[x]`): a CLI command to read the
  persisted daemon log (`daemon.log`, fallback `portato.log`) with `-f/--follow`,
  `-n/--lines`, `--since`, `--tuber`, `--all` — the missing `docker logs` /
  `journalctl` equivalent (the TUI `l` view is a live ring buffer only).
  Verified end-to-end against the real daemon log: `--tuber` filters on the
  current `tuber=` attr, and `--follow` streams live.

- **Phase 43** — ProxyJump / jump hosts (done, `[x]`): a `jump:` field (single
  hop or a comma-chain, OpenSSH `-J` equivalent) dials a target through one or
  more bastions. The dial reuses the handshake per hop — hop 0 via `net.Dialer`,
  each later hop via the previous hop's `ssh.Client.Dial` wrapped in
  `ssh.NewClientConn` — so the final client runs the tuber's forward unchanged.
  Intermediate hops are key-only (the shared agent/identity; the bastion must
  accept the same key), the Phase 35 password fallback applies only to the
  final target, and each hop verifies its own host key. A leash goroutine
  closes the intermediates once the final client disconnects (proved by an
  `ActiveConns()→0` leak guard). `~/.ssh/config` resolution and per-hop
  identity are follow-ups. Additive → MINOR (`v1.1.0`).

- **Phase 44** — `~/.ssh/config` resolution (done, `[x]`): `ssh: <alias>`
  resolves HostName/User/Port/IdentityFile/ProxyJump from the user's ssh config
  (`kevinburke/ssh_config`, first-match-wins, patterns + `Include`), so host
  definitions aren't duplicated between ssh-config and `config.yaml`; an alias's
  ProxyJump auto-populates Phase 43's chain (resolved recursively, cycle-guarded).
  It's a pure config-layer resolution — the forward/dial path is unchanged.
  Precedence is openssh-faithful (explicit tuber fields win, ssh-config fills
  gaps); a derived IdentityFile/ProxyJump lives only on non-persisted fields, so
  `Save` never writes them back. No config / no match ⇒ literal silent; an
  unreadable config or a ProxyJump cycle ⇒ a clear load error. Proved by a Go
  black-box E2E (`make e2e-sshconfig`) and a real-Linux systemd-docker case
  (`make e2e-docker E2E_CASE=sshconfig`). Additive → MINOR (`v1.2.0`).

- **Phase 45** — Shell completions (done, `[x]`): `portato completion
  bash|zsh|fish|powershell` (cobra default) emits the script, and
  `enable`/`disable`/`restart`/`forward <name>` + `logs --tuber <name>` now
  TAB-complete tuber names from `config.yaml` via a `ValidArgsFunction` /
  `RegisterFlagCompletionFunc`. The completion helper reads the file directly
  (not `config.Load`), so it never creates a config as a TAB side effect and
  works with no daemon running. The README documents per-shell sourcing
  (document-only — no packaging changes). Additive → MINOR (`v1.3.0`).

- **Phase 46** — Tunnel tags / groups (done, `[x]`): a `tags:` field on a
  tuber; `enable` / `disable` / `restart --tag X` operate on every tuber with
  that tag (one line per tuber; exactly one of `--tag` / `<name>` required;
  `--tag` TAB-completes the distinct values from `config.yaml`); a precise
  `#tag` filter in the TUI (leading `#` = exact tag match — `#db` matches a
  tuber *tagged* `db`, not one *named* `db-stage`); `a` / `x` respect the
  active `/` filter so any filtered view is an instant group op (no filter ⇒
  byte-identical to before). Tags flow config → `forward.Status.Tags` → IPC,
  so `list` / `list --json` and the TUI show and filter on them with no new
  IPC method; the editor gains a comma-separated tags field with carry-through
  (Phase-43 jump/identity pattern). Tags render as `#tag` tokens everywhere;
  in the TUI they live in the Phase-39 detail strip (≤1 line, an error wins)
  rather than a new column, keeping the Phase-38 responsive layout intact.
  Additive ⇒ MINOR (`v1.4.0`).

- **Phase 47** — Windows SCM autostart (done, `[x]`): the final reboot
  verification on the office Windows PC closed the last open DoD — after a
  real reboot with no user logged in, the delayed-auto-start service brings
  the daemon up (log entries at boot+2min, discovery marker intact) and an
  unelevated `portato list` answers right after login. That verification
  surfaced three IPC gaps, all fixed and shipped in **v1.6.1**: the named
  pipe now carries an explicit SDDL (GENERIC_ALL for SYSTEM,
  Administrators, and the process user's SID — previously the service
  token's default DACL denied the same user's interactive session), a
  Windows pidAlive misread access-denied on the session-0 service process
  as dead and culled a live daemon's discovery marker, and
  `doctor`/`SCMInstalled` required SCM ALL_ACCESS (now minimal rights, no
  elevation). The `win-rdp` tunnel itself needed `GatewayPorts
  clientspecified` on the bastion (server-side, our phase-41 diagnostics
  pointed straight at it). The windows-smoke CI job now also runs the
  windows-tagged unit tests, which had never executed anywhere before.
  Shipped in v1.5.0; verification fixes in v1.6.1.

- **Phase 48** — import forwards from `~/.ssh/config` (done, `[x]`):
  `portato import [<pattern>…]` (with `--all` / `--from` / `--dry-run` /
  `--yes`) copies the `LocalForward` / `RemoteForward` / `DynamicForward`
  directives into `config.yaml` as `enabled: false` tubers — a one-time
  copy; ssh_config is read via the Phase-44 reader and never written
  (content + mtime test-asserted). The scan walks Host blocks per block (a
  literal `GetAll(alias, …)` would leak `Host *` forwards into every
  host), skips `Host *` / `Match` / negated-only blocks uniformly through
  `Include` files; a bare-port `RemoteForward` imports as
  `127.0.0.1:port` (OpenSSH binds loopback by default; Portato's bare port
  means `*`); names derive from pattern + listen port (`db-5432`),
  de-conflicted `-2`/`-3`; dedup drops a forward an existing tuber already
  covers, resolving both sides against the scanned config. A fresh install
  (config bootstrapped by portato) gets a one-time y/N import-all offer on
  the first interactive launch — markers `fresh_install` /
  `import_offered` in the state dir: a daemon-first bootstrap never
  consumes the offer, a non-TTY launch leaves it pending, upgrading
  installs are never nudged. Verified in-process with the real binary
  (`make e2e-import`) and on real Linux + real OpenSSH
  (`make e2e-docker E2E_CASE=import`). Additive → MINOR.

Earlier phases (33 CodeFactor cleanup + lint guardrails, 34 `portato license` +
`--license`, 17 Windows, 35 SSH password auth, …) are all `[x]`; see the phase
files in [`docs/phases/`](./phases/) for their detail.

## Final MVP E2E (on completing Phase 6)

1. `go build ./...` and `go test ./...` — green.
2. The config has one tunnel with `enabled: false`.
3. `portato install` -> the daemon starts on its own (launchctl/systemctl).
4. `portato list` shows the tunnel as `○ Disabled`.
5. `portato` (TUI) -> space -> `Connecting` -> `Connected`; `nc -z 127.0.0.1 <local>` succeeds, traffic flows.
6. space again -> `Disabled`, the port is closed.
7. **Hand-off:** `portato` with no daemon, space to enable the tunnel, `q`, answer `y` -> the daemon is spawned, the tunnel keeps running, `portato list` confirms it. (Seamless since Phase 16: the live local listeners are passed to the daemon, so the local port never goes down.)
8. SSH server dropped -> auto-reconnect restores `Connected`.
9. After a reboot/relogin — the daemon is up, the tunnels are `Disabled`.
10. `portato uninstall` -> the service is removed cleanly.
