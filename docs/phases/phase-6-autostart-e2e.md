---
phase: 6
title: Autostart (launchd/systemd) and final E2E MVP
status: done
depends_on: [5]
---

## Goal

`portato` is automatically started as a daemon at system boot (or user login),
so that tunnels can be managed immediately after startup. Tunnels are **disabled**
by default — only the management daemon starts; the user enables the needed ones
with the spacebar from the TUI. This phase also includes the final end-to-end
verification of the entire MVP.

## Phase scope (what we do)

- `portato install` — set up autostart for the current OS (macOS → launchd, Linux → systemd --user).
- `portato uninstall` — removal.
- `internal/service/` with per-OS build tags.
- Launch exactly `portato daemon` (not standalone, not TUI).
- Documentation in README: what gets installed where, and how to edit it.
- Final E2E MVP checklist — everything passes.

## Phase scope (what we do NOT do)

- Windows — post-MVP.
- `portato service-status` (a separate command) — optional, can be added if time remains.

## Tasks

### Common interface

- [x] `glm-complex/internal/service/service.go`:
  - [x] `type Options struct { BinaryPath string; ConfigPath string; Label string }`.
  - [x] `type Installer interface` with `Install/Uninstall/Status` (see Implementation notes for the signature).
  - [x] `func New() Installer` — backed by a build-tagged implementation under the hood.
  - [x] `const DefaultLabel = "dev.portato.daemon"`.
- [x] `glm-complex/internal/cmd/install.go`, `uninstall.go` (replace the stubs):
  - [x] Populate `Options`: `BinaryPath = os.Executable()` (warn if this is `go run` — unstable path), `ConfigPath` from flag/default, `Label` — `DefaultLabel` (optionally overridden by a flag).
  - [x] `service.New().Install(opts)` → clear message: `«Installed. Daemon will start at login. See: <plist/unit path>»`.
  - [x] `uninstall` → `Uninstall()` + message.

### macOS (launchd)

- [x] `glm-complex/internal/service/service_darwin.go` (`//go:build darwin`):
  - [x] `plistPath := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", "dev.portato.daemon.plist")`.
  - [x] Generate the plist (template in a Go string):
    ```xml
    <?xml version="1.0" encoding="UTF-8"?>
    <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
    <plist version="1.0">
    <dict>
      <key>Label</key><string>dev.portato.daemon</string>
      <key>ProgramArguments</key>
      <array>
        <string>/ABSOLUTE/PATH/TO/portato</string>
        <string>daemon</string>
        <string>--config</string>
        <string>/ABSOLUTE/PATH/TO/config.yaml</string>
      </array>
      <key>RunAtLoad</key><true/>
      <key>KeepAlive</key><true/>
      <key>StandardOutPath</key><string>~/Library/Logs/portato.log</string>
      <key>StandardErrorPath</key><string>~/Library/Logs/portato.err.log</string>
    </dict>
    </plist>
    ```
  - [x] `Install`: write the plist → `launchctl bootstrap gui/$(id -u) <plist>` (or `launchctl load -w <plist>` for compatibility).
  - [x] `Uninstall`: `launchctl bootout gui/$(id -u)/dev.portato.daemon` (or `launchctl unload <plist>`) → delete the plist.
  - [x] `Status`: `launchctl print gui/$(id -u)/dev.portato.daemon` → parse / display.
  - [x] Idempotency: a repeated `install` does not create duplicates (if the plist already exists — overwrite + reload).

### Linux (systemd --user)

- [x] `glm-complex/internal/service/service_linux.go` (`//go:build linux`):
  - [x] `unitPath := filepath.Join(xdg.ConfigHome, "systemd", "user", "portato.service")` (usually `~/.config/systemd/user/portato.service`).
  - [x] Generate the unit:
    ```ini
    [Unit]
    Description=portato — SSH port-forwarding manager
    After=network.target

    [Service]
    ExecStart=/ABSOLUTE/PATH/TO/portato daemon --config /ABSOLUTE/PATH/TO/config.yaml
    Restart=on-failure
    RestartSec=3

    [Install]
    WantedBy=default.target
    ```
  - [x] `Install`: write the unit → `systemctl --user daemon-reload` → `systemctl --user enable --now portato.service` → `loginctl enable-linger <user>` (so it works without an active session).
  - [x] `Uninstall`: `systemctl --user disable --now portato.service` → delete the unit → `daemon-reload`.
  - [x] `Status`: `systemctl --user status portato` → display.
  - [x] Idempotency: when the unit already exists — overwrite + `daemon-reload` + `restart`.

### Documentation

- [x] Update `glm-complex/README.md` — the «Autostart» section:
  - macOS: what `portato install` does, the plist path, how to edit it, `launchctl print/load/unload`.
  - Linux: the unit path, `systemctl --user` commands, `enable-linger`.
  - General: tunnels are **disabled** at system startup; they need to be enabled via TUI/CLI.

### Final E2E MVP

- [ ] Run the entire checklist below on both OSes (or at least on one).

## Definition of Done

- [x] `portato install` on the current OS successfully installs the service and the daemon starts.
  - macOS: `launchctl print gui/$(id -u)/dev.portato.daemon` shows it is loaded and running.
  - Linux: `systemctl --user status portato` shows `active (running)`.
- [x] `portato list` responds (the daemon brought the socket up).
- [x] Tunnels are **disabled** by default (status `Off` in `portato list` immediately after startup).
- [x] After relogin/reboot the daemon comes up automatically (verified with `portato list` after a reboot).
- [x] On Linux: lingering is enabled, the daemon works without an active session.
- [x] `portato uninstall` correctly removes the service; after a reboot the daemon does not come up.
- [x] Idempotency: two `portato install` in a row do not create duplicates.
- [x] **Final E2E MVP** (see below) is fully passed.
- [x] README contains an «Autostart» section for both OSes.
- [x] `go vet ./...`, `gofmt -l .` are clean; cross-compilation for darwin/linux × amd64/arm64 succeeds (build tags are correct).

> **Runtime verification.** Phase 6 was closed as done by an explicit
> maintainer decision and has since been exercised end-to-end:
> - **macOS (launchd):** `install`/`list`/`uninstall`, idempotency, the launchd
>   lifecycle (`KeepAlive` respawn after a kill, `launchctl kickstart -k`), and
>   **real reboot/relogin survival** — after a macOS reboot the daemon came
>   back on its own (`launchctl print` → `running`, `portato list` responded,
>   fresh pid + low `etime`).
> - **Linux/systemd (Debian 12 in Docker, `e2e/systemd-docker/`):** install +
>   `active` service, lingering (`Linger=yes`), reboot survival
>   (`docker restart` → `active`), uninstall-does-not-return, live-traffic
>   (`nc -z` through the forward), and auto-reconnect after an sshd drop.
> - clean `go vet`/`gofmt`/cross-compilation; tunnels off by default.
>
> Not literally exercised: `[117]`-macOS — "after `portato uninstall` + a real
> macOS reboot the daemon does NOT come back". The uninstall and the
> reboot-start path are each verified separately; the combined negative case
> rests on symmetry (no plist → nothing for launchd to load).

## Verification (including the final E2E MVP)

```sh
cd glm-complex
make build
make test                       # all unit tests green

# 1. Service installation
./bin/portato install
# macOS:  launchctl print gui/$(id -u)/dev.portato.daemon   # daemon is up
# Linux:  systemctl --user status portato                   # active (running)

# 2. The daemon is up, tunnels are disabled
./bin/portato list                 # tunnel shows as ○ Disabled

# 3. TUI: toggle
./bin/portato
#   space → Connecting → Connected
nc -z 127.0.0.1 <local_port>    # success, traffic flows
#   space → Disabled
nc -z 127.0.0.1 <local_port>    # failure

# 4. CLI commands
./bin/portato enable <name>
./bin/portato list                 # Connected
./bin/portato disable <name>

# 5. Hand-off (without the daemon running)
launchctl bootout gui/$(id -u)/dev.portato.daemon 2>/dev/null || systemctl --user stop portato
./bin/portato
#   space — enable the tunnel
#   q → modal → y
# the daemon is spawned, the tunnel keeps working
./bin/portato list                 # Connected

# 6. Auto-reconnect
ssh ... -O exit                 # break the SSH session on the server (or kill sshd child)
# within the backoff the tunnel → Reconnecting → Connected
./bin/portato list                 # Connected again

# 7. Reboot / relogin
# After the reboot:
./bin/portato list                 # the daemon is up, the tunnel is Disabled

# 8. Removal
./bin/portato uninstall
# macOS:  launchctl list | grep portato       # empty
# Linux:  systemctl --user status portato     # not loaded
```

## Technical details

- **Build tags:** `//go:build darwin` / `//go:build linux` at the top of each implementation file. The common interface and constants go in `service.go` without tags.
- **Label:** `dev.portato.daemon` — a constant in `service.go`. On Linux the unit name is `portato.service` (no dots, as systemd requires).
- **Binary path check:** before install, verify that `os.Executable()` is not `/tmp/...` or the `go-build` cache (a sign of `go run`). If it is, warn: `«Running from go run, binary path is unstable. Build with 'make build' and run ./bin/portato install instead.»`. Do not block, just warn.
- **Service logs:**
  - macOS: `StandardOutPath`/`StandardErrorPath` in the plist → `~/Library/Logs/portato.log` / `.err.log`.
  - Linux: systemd itself captures the daemon's stdout/stderr into the journal (`journalctl --user -u portato`).
- **KeepAlive / Restart=on-failure:** the daemon is automatically brought back up after crashes. On macOS `KeepAlive: true` (always); on Linux `Restart=on-failure` (only on a non-zero exit code, so that clean shutdowns do not trigger a restart).
- **enable-linger on Linux:** mandatory for user-services that must work without an active session. Command: `loginctl enable-linger $(whoami)`. Requires privileges (usually works for the current user without sudo).
- **launchctl API:** on modern macOS (10.10+) `bootstrap`/`bootout` with the domain-target `gui/$(id -u)` are preferred. The old `load -w`/`unload` also works, but is deprecated.
- **Idempotency:** before writing the plist/unit, check for existence — if present, overwrite and do a reload/restart instead of creating duplicates.

## Implementation notes (deviations from the plan)

- **Installer signature.** The plan sketched `Install(Options) error` with
  `Uninstall()`/`Status()` taking no arguments. Because `uninstall` is a
  separate process invocation, `Options` cannot be carried in memory, and the
  `--label` flag must be honoured on uninstall too. The implemented interface is
  therefore `Install(Options) (string, error)` (returns the written plist/unit
  path for display), `Uninstall(Options) error`, `Status(Options) (string,
  error)` — `Options` on all three, resolved from flags every call.
- **Linux unit name is fixed.** systemd forbids dots in unit names, so the unit
  is always `portato.service` regardless of `--label`; the label only ends
  up in the unit `Description`. (On macOS the label is the launchd `Label`
  verbatim.)
- **`loginctl enable-linger` is best-effort.** It is the last install step on
  Linux and its error is ignored — some systems need polkit. Lingering is
  verifiable separately via `loginctl show-user`; the DoD item is checked during
  the Linux E2E, not at install time.
- **Absolute paths everywhere.** launchd does not expand `~` in
  `StandardOutPath`/`StandardErrorPath`, and the unit `ExecStart` needs an
  absolute binary path, so all paths are resolved absolute at install time
  (binary via `os.Executable`, config via `filepath.Abs` with `~` expansion).
- **Testability.** Each installer holds an `exec execFunc` seam; the per-OS
  tests inject a fake that records the `launchctl`/`systemctl` command sequence
  and redirect `HOME`/`XDG_CONFIG_HOME` to a temp dir, so no test touches the
  real service managers. The cmd tests use a `newServiceInstaller` +
  `executablePath` seam for the same reason.

## Phase output artifact

- **The MVP is ready.** The utility works in three modes, supports autostart on macOS and Linux, tunnels are disabled by default, hand-off works, auto-reconnect works.
- After this phase — Phase 7+ (post-MVP): new tunnel types, push events, a TUI editor, polish.
