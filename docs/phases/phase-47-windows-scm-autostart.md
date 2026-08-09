---
phase: 47
title: "Windows autostart via Service Control Manager"
status: todo
depends_on: [17]
---

## Goal

Replace the HKCU registry `Run`-key autostart with a real **Windows Service**
registered with the Service Control Manager (SCM), so `portato install` on
Windows produces a daemon that:

1. **Starts at system boot** (not at user logon) — `StartType = StartAutomatic`,
   `DelayedAutoStart = true`.
2. **Runs without anyone logged in** — SCM logs on the install-time user
   account itself (password stored as an LSA secret), so the service process
   gets a logged-in session with full access to `%USERPROFILE%\.ssh\` and
   `%APPDATA%\portato\config.yaml`.
3. **Starts immediately on `portato install`** — closing the parity gap with
   macOS (`launchctl bootstrap`, `RunAtLoad=true`) and Linux
   (`systemctl --user enable --now`), where install already launches the
   daemon. Today `portato install` on Windows only writes the `Run` value and
   leaves the daemon unstarted, so `portato list` immediately after says
   "daemon is not running."

This is the "later refinement" explicitly deferred at the end of Phase 17
(`phase-17-windows.md` technical details: *"Out of scope here: Windows service
(Service Control Manager) autostart; the Run key is the MVP-equivalent. SCM
can be a later refinement."*).

## Background

Phase 17 (`[x]`) shipped the MVP-equivalent autostart for Windows:
`internal/service/service_windows.go` writes a `REG_SZ` value `Portato` into
`HKCU\Software\Microsoft\Windows\CurrentVersion\Run` pointing at
`"<binary>" daemon --config "<config>"` (SPEC §13). That key:

- Fires **only on interactive user logon** — never at boot, never without a
  logged-in user.
- Does **not start the daemon at install time** — it only registers a future
  trigger, so `portato list` right after install reports "not running".

Both diverge from launchd (`RunAtLoad=true`, `KeepAlive=true`, loaded and
started by `launchctl bootstrap`) and systemd --user (`enable --now` starts
the unit immediately; lingering keeps it alive without a session).

There is also a **Scoop drift** failure mode that compounds the
"didn't-start-after-reboot" symptom reported in the field: Scoop installs the
binary under `%USERPROFILE%\scoop\apps\portato\<version>\portato.exe` (with a
`current` junction that bumps atomically on `scoop update portato`). When
`portato install` captures `os.Executable()` and that resolves to a
**version-pinned** path, the autostart entry silently breaks on the next
`scoop update` (the old version directory is pruned). This phase fixes that
alongside the SCM switch by preferring the stable `current` junction path.

The architectural tension this phase resolves: SSH identities, `known_hosts`,
and the config all live in the **user profile**, which a boot-time process
under `LocalSystem` cannot read. The chosen design runs the service **as the
install-time user** (SCM stores the password as an LSA secret and logs the
user on at service start), preserving the per-user model with no path
migration and no `~/.ssh` copying.

## Tasks

- [ ] **Refactor the daemon run path into a reusable function.**
      `internal/cmd/daemon.go:45` (`daemonRunE`) currently inlines flag
      parsing → `routelog.Setup` → `daemon.New` → `srv.Start(ctx)`. Extract
      the body into `runDaemon(ctx, cfg, path, listenFdsPath, logger) error`
      so both the cobra command and the SCM service handler call the same
      code. Pure prepare commit — no behaviour change.
- [ ] **Detect SCM launch before cobra dispatch.**
      `cmd/portato/main.go:9` on Windows must branch *before* `cmd.Execute()`:
      `if isSvc, _ := svc.IsWindowsService(); isSvc { return service.RunAsService() }`.
      SCM launches services with no argv and no stdio; the cobra tree must
      not run in that mode. Build-tagged (`//go:build windows`).
- [ ] **SCM service handler** (`internal/service/svcmain_windows.go`,
      `//go:build windows`): `RunAsService()` calls `svc.Run(serviceName,
      &handler{})`. `handler.Execute(...)` maps SCM commands to the daemon
      lifecycle:
      - `svc.Interrogate` → report current state.
      - `svc.Start` / `svc.Continue` → build a cancellable `context`,
        `os.Chdir` to the user's home (so relative identity paths resolve),
        call `runDaemon(ctx, …)`, report `svc.Running`.
      - `svc.Stop` / `svc.Shutdown` → cancel the context (the daemon's
        `srv.Start(ctx)` already drains gracefully), report `svc.Stopped`.
      The service name is the constant `Portato` (matches the SCM display
      name; `EffectiveLabel` is reused for the launchd-style reverse-DNS on
      macOS only).
- [ ] **Rewrite `internal/service/service_windows.go` to use SCM** via
      `golang.org/x/sys/windows/svc/mgr` (already a transitive dep — see
      `go.mod` `golang.org/x/sys v0.46.0`; no new module needed):
      - **`Install(o Options)`**: `mgr.Connect()` → `mgr.CreateService(
        "Portato", binPath, mgr.Config{ServiceType:
        windows.SERVICE_WIN32_OWN_PROCESS, StartType: mgr.StartAutomatic,
        ServiceStartName: account, Password: password, DelayedAutoStart:
        true, Dependencies: []string{"Tcpip"}})`. `binPath` is
        `"<o.BinaryPath>" daemon` (SCM does its own quoting). Then
        `s.SetRecoveryActions(...)` with `SC_ACTION_RESTART` after 30 s
        (parity with launchd `KeepAlive` / systemd `Restart=on-failure`).
        Finally `s.Start()` so the daemon is up the moment install returns
        (parity with `--now`). Returns the SCM service path
        (`HKLM\SYSTEM\CurrentControlSet\Services\Portato`) for display.
      - **`Uninstall`**: `mgr.OpenService` → `s.Control(svc.Stop)` with a
        bounded wait → `s.Delete`. Idempotent: a missing service is a no-op.
      - **`Status`**: `mgr.QueryState` → `running / stopped / not installed`
        (+ last error / exit code when stopped).
- [ ] **Credentials collection on install.** Add to
      `internal/cmd/install.go`:
      - `--service-account string` (default: the current user —
        `%USERDOMAIN%\%USERNAME%`).
      - `--password-file string` (read the Windows password from a file for
        CI / automation).
      - With neither flag, `install` prompts interactively on the terminal
        via `golang.org/x/term.ReadPassword(stdin)` (no echo), matching the
        UX of `portato add-identity`. The password is passed straight to
        `mgr.CreateService` and **never persisted by portato** — SCM keeps
        it as an LSA secret under the service record.
      - A clear message at the end of install notes that **the password must
        be re-supplied (via a fresh `portato install`) after the Windows
        account password changes**, since SCM's stored secret goes stale.
- [ ] **`--legacy-runkey` fallback.** Keep the Phase-17 `Run`-key code as a
      fallback behind a `--legacy-runkey` flag on `install`/`uninstall` for
      locked-down environments where service creation is blocked by GPO /
      AV. When set, `Install` takes the old registry path instead of SCM.
      `Status` reports whichever mechanism is in effect (preferring SCM).
- [ ] **Stable Scoop `current` path.** In `buildServiceOptions`
      (`internal/cmd/service_opts.go`), if `os.Executable()` lives under
      `…\scoop\apps\portato\<version>\`, rewrite `Options.BinaryPath` to the
      `…\scoop\apps\portato\current\…` junction (which Scoop updates
      atomically on `scoop update portato`). Non-Scoop layouts and the
      `--legacy-runkey` path are untouched. The detection is path-shape-only
      — no registry / scoop-state reads.
- [ ] **`portato stop` parity under SCM.** Phase 17 left `stop` using
      `TerminateProcess` (no graceful SIGTERM equivalent). Under SCM,
      `portato stop` should instead open the service and send
      `svc.Stop` (the handler then cancels the daemon context, draining
      gracefully). `--legacy-runkey` mode keeps the existing
      `TerminateProcess` behaviour. The `stopKill` seam in
      `internal/cmd/stop.go` already abstracts the kill mechanism.
- [ ] **Update `portato doctor` autostart reporting on Windows**
      (`internal/cmd/autostart_windows.go`) to query the SCM service state
      instead of (or in addition to) the `Run` value.
- [ ] **Unit tests behind an SCM seam.** Introduce
      `internal/service/scm_windows.go` with a small interface
      (`scmClient`) wrapping the `mgr` calls used; the production
      implementation is a thin adapter over `golang.org/x/sys/windows/svc/mgr`,
      and tests inject a fake (mirroring the existing `execFunc` /
      `fakeexec_test.go` pattern already used by `service_linux_test.go` /
      `service_darwin_test.go`). Assert: correct `mgr.Config` (StartAutomatic,
      `ServiceStartName` = account, `DelayedAutoStart`), the
      CreateService → SetRecoveryActions → Start sequence, idempotent
      Uninstall on a missing service, `--legacy-runkey` routes to the old
      code path.
- [ ] **CI: rewrite `windows-smoke`** (`.github/workflows/ci.yml:74`). The
      current job asserts the HKCU `Run`-value install/uninstall; replace
      the autostart block with an SCM flow: `portato install
      --service-account <runner user> --password-file <(echo $PWD)>` →
      `Get-Service Portato` reports `Running` → `portato list` answers →
      `portato stop` → `Get-Service` reports `Stopped` → `portato uninstall`
      → `Get-Service Portato` throws "no service found". `windows-latest`
      runners run with admin privileges, so SCM install works there. Keep a
      short `--legacy-runkey` regression block.
- [ ] **SPEC §13.** Replace the Windows row of the autostart table with
      `SCM service (Start=Automatic, DelayedAutoStart, recovery=restart,
      runs as the install-time user)` and update the paragraph below it
      (currently SPEC.md:572–585) to describe the SCM mechanism, the
      install-time password prompt, the `--legacy-runkey` fallback, and the
      Scoop `current`-junction path note.
- [ ] **README.** Update the Windows install instructions to mention the
      one-time password prompt and the post-password-change re-install
      caveat. Add a note that `portato install` now starts the daemon
      immediately on Windows (matching macOS/Linux).
- [ ] **Phase-17 cross-link.** Append a note to
      `docs/phases/phase-17-windows.md` (Technical details) that the
      "deferred SCM refinement" landed in Phase 47.

## Definition of Done

- [ ] On a Windows host, `portato install` registers an SCM service named
      `Portato` (`StartAutomatic`, `DelayedAutoStart=true`,
      `SERVICE_WIN32_OWN_PROCESS`, runs as the install-time user, recovery =
      `SC_ACTION_RESTART` after 30 s) and the daemon is reachable via
      `portato list` immediately after install returns — no logoff/reboot
      needed.
- [ ] After a real Windows reboot with **no user logged in**, the daemon is
      up and `portato list` answers once a user logs in (proving the service
      started at boot under the stored credentials).
- [ ] `portato uninstall` stops and removes the service; a subsequent
      `Get-Service Portato` fails with "Cannot find any service".
- [ ] `portato stop` gracefully stops the running service (state → Stopped)
      rather than `TerminateProcess`-killing it.
- [ ] `portato doctor` reports the SCM service state (Running / Stopped /
      Not installed) on Windows.
- [ ] `portato install --legacy-runkey` reproduces the Phase-17 HKCU `Run`
      behaviour exactly (regression for locked-down environments).
- [ ] After `scoop update portato`, the previously-installed SCM service
      still starts (the binary path points at the stable `current`
      junction, not the pruned version directory).
- [ ] `GOOS=windows go build ./...`, `GOOS=windows go vet ./...`,
      `gofmt -l .`, and `golangci-lint run ./...` are all clean; new
      functions stay under gocyclo 15; the phase's unit tests are green.
- [ ] darwin/linux behaviour is byte-identical (all SCM code is
      `//go:build windows`; the `runDaemon` refactor is pure extraction).
- [ ] The `windows-smoke` CI job runs green with the SCM install/start/stop/
      uninstall sequence and the `--legacy-runkey` regression block.
- [ ] SPEC §13, README, and `phase-17-windows.md` cross-link updated; this
      file's status and the ROADMAP row flip together on completion.

## Verification

```sh
# Cross-platform build hygiene (run on macOS/Linux dev host):
GOOS=windows GOARCH=amd64 go build ./...
GOOS=windows GOARCH=amd64 go vet ./...
gofmt -l .
golangci-lint run ./...
go test ./internal/service/... -run Windows -v   # SCM seam tests

# On a real Windows host (or windows-latest CI runner):
portato.exe install                  # prompts for Windows password
Get-Service Portato                  # Status = Running, StartType = Automatic
portato.exe list                     # answers immediately
sc.exe qc Portato                    # SERVICE_START_NAME = <user>, BINARY_PATH_NAME = …current\portato.exe
portato.exe stop                     # graceful: service state -> Stopped
portato.exe uninstall                # removes the service
Get-Service Portato                  # -> "Cannot find any service ..."

# Boot survival:
portato.exe install
Restart-Computer                     # reboot, do not log in interactively for ~60 s
# After reboot + login:
portato.exe list                     # answers — the service started at boot
Get-WinEvent -LogName System -MaxEvents 50 | ? { $_.Message -like '*Portato*' }

# Scoop drift:
scoop install portuber/portato
portato install
scoop update portato                 # bumps version, prunes old dir
Restart-Computer
# After reboot:
portato list                         # still works — service path was the `current` junction

# Legacy fallback (locked-down host):
portato install --legacy-runkey
reg query HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v Portato
```

## Technical details

- **SCM, not Task Scheduler.** SCM is the true Windows equivalent of
  launchd / systemd — it owns the service lifecycle, restarts on failure
  (recovery actions), reports a clean state machine, and integrates with
  `services.msc` / `sc.exe` / event logs. A Scheduled Task with an "At
  startup" trigger was considered (lighter: no binary refactor, just
  `schtasks.exe /Create /SC ONSTART /RU <user> /RP <pwd>`), but it loses
  SCM's recovery / state reporting and is awkward to query for status.
- **Credentials.** The service runs as `DOMAIN\user` (default
  `%USERDOMAIN%\%USERNAME%`); SCM stores the password as an LSA secret and
  logs the user on at service start, producing a session with full profile
  access (`%USERPROFILE%\.ssh\`, `%APPDATA%\portato\`). This is what makes
  "starts at boot without login" viable without relocating the config or
  copying SSH keys. `LocalSystem` was rejected: it has no user profile and
  cannot read `%USERPROFILE%\.ssh\`.
- **Binary dispatch.** `golang.org/x/sys/windows/svc.IsWindowsService()`
  distinguishes an SCM launch from an interactive one. The check must run
  in `main()` *before* `cmd.Execute()`, because SCM- launched processes
  have no argv (cobra would see `os.Args = [portato.exe]`) and no
  stdin/stdout/stderr — any cobra path that touches stdio would misbehave.
  The `cmd/portato/main.go` edit is a single build-tagged branch.
- **Dependencies on existing modules.** `golang.org/x/sys v0.46.0`
  (already in `go.mod`) provides `windows/svc` and `windows/svc/mgr` — no
  new dependency. `golang.org/x/term` (for the no-echo password prompt) is
  a transitive dep already present via `golang.org/x/...`; verify with
  `go mod tidy` after the import is added.
- **Recovery actions.** `mgr.Config` does not carry recovery policy; call
  `s.SetRecoveryActions([]mgr.RecoveryAction{{Type:
  mgr.ActionRestart, Delay: 30 * time.Second}}, 1 * time.Minute)` after
  `CreateService` — restart on failure, with a 1-minute reset window
  (mirrors systemd's default backoff window and launchd's `KeepAlive`).
- **DelayedAutoStart.** `DelayedAutoStart = true` makes the service start
  ~30 s after boot, after the boot-critical services settle; this avoids
  races with `Tcpip` / DNS on fast-booting hosts. Marked as a dependency
  so SCM refuses to start Portato before the network stack is up.
- **`runDaemon` extraction.** The daemon entry point
  (`internal/cmd/daemon.go:45`) builds the logger, the daemon server, the
  signal context, and calls `srv.Start(ctx)`. The SCM handler needs the
  same sequence but with the SCM-provided stop signal (rather than
  `signal.NotifyContext`). Extracting `runDaemon(ctx, …)` lets both
  callers share everything except the context source. The standalone-only
  `--listen-fds` hand-off path is unix-only (Phase 16 / Phase 17 already
  skip it on Windows) and is passed through as empty in the SCM path.
- **Scoop `current` junction.** Scoop creates
      `%USERPROFILE%\scoop\apps\portato\current` as a directory junction
      pointing at the active version directory, and updates it atomically
      on `scoop update portato`. The version-pinned path
      (`…\portato\<version>\portato.exe`) becomes stale the moment Scoop
      prunes the old version. `buildServiceOptions` rewrites the
      version-segment to literal `current` when the path matches the Scoop
      layout, so a `portato install` survives later `scoop update`s. This
      also matches the `os.Executable()` semantics when invoked through the
      Scoop shim (which forwards to the `current` path).
- **Out of scope (deferred).** Windows Event Log integration (the file
  logger is sufficient), per-user service infrastructure via
  `SERVICE_SID_TYPE_UNRESTRICTED`, and a named-pipe "socket activation"
  analog (the named pipe already gives implicit activation semantics once
  the daemon is up). The seamless FD hand-off (Phase 16) stays unix-only.
- **Cross-platform invariant.** All new SCM code is behind
  `//go:build windows`. The `runDaemon` refactor in `internal/cmd/daemon.go`
  is a pure extraction — the cobra `daemon` command's observable behaviour
  on macOS / Linux does not change.
