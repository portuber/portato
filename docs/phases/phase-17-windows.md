---
phase: 17
title: Windows support
status: done
depends_on: [4, 6]
---

## Goal

A native Windows build of portato: the daemon and its IPC server run over a
**named pipe**, `portato install`/`uninstall` manage a **registry Run-key**
autostart entry, and the TUI/CLI work as on macOS/Linux. Windows joins the
supported platform matrix (SPEC §15/§16).

## Tasks

- [x] Abstract the IPC transport behind a build-tagged interface
      (`internal/daemon/transport`): unix-domain socket on darwin/linux
      (`net.Listen("unix", ...)`), named pipe on windows
      (`\\.\pipe\portato` via `github.com/Microsoft/go-winio` or
      `gopkg.in/natefinch/npipe.v2`).
- [x] `internal/client`: build-tagged dialer (unix socket vs named pipe) behind
      the existing `client.New` surface.
- [x] `internal/daemon/paths.go`: per-OS paths — Windows has no socket file;
      the PID file and the discovery marker live in `%LOCALAPPDATA%\portato\`.
      (Implemented as `paths_unix.go` / `paths_windows.go`; `ipctoken.TokenPath`
      is split per OS too.)
- [x] `internal/service/service_windows.go` (`//go:build windows`): autostart
      via `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` — write/remove a
      `REG_SZ` value `Portato` pointing at `portato daemon --config <abs>`.
      Implement `Install/Uninstall/Status`.
- [x] Update `service_other.go` build tag to `//go:build !darwin && !linux &&
      !windows` so exactly one `newInstaller` exists per platform.
- [x] Build-tag the hand-off `Setsid` (`syscall.SysProcAttr{Setsid: true}` is
      unix-only) — Windows uses `SysProcAttr{CreationFlags:
      windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}`. (Behind a
      `detachedSysProcAttr()` seam; the FD hand-off is also gated off-unix via
      `fdpass.Supported()`.)
- [x] Build-tag the two `syscall.Kill` call sites (unix-only) found during the
      phase-21 windows build attempt: `pidAlive` in
      `internal/daemon/discovery.go` (PID-liveness check) and `stopKill` in
      `internal/cmd/stop.go` (`portato stop` sends SIGTERM). Windows needs an
      equivalent `OpenProcess`+`TerminateProcess` (or `taskkill`) path behind a
      build-tagged seam (the `stopKill` var in stop.go is already a seam).
- [x] Packaging follow-up (phase 21): re-add `windows` to the `.goreleaser.yml`
      build matrix, restore the zip `format_overrides`, and add back the
      `scoops:` section (`portuber/scoop-bucket`) so a Scoop manifest publishes
      alongside the windows archive.
- [x] CI: add `windows/amd64` (and `windows/arm64` when Go supports it) to the
      cross-compile matrix; add a Windows smoke test (`portato daemon` +
      `portato list` round-trip).
- [x] Build-tag the ssh-agent dial (Phase 17 follow-up, surfaced during Windows
      verification): `internal/forward/agentdial_unix.go` (`SSH_AUTH_SOCK`
      unix socket) / `agentdial_windows.go` (`\\.\pipe\openssh-ssh-agent` via
      go-winio, 2s timeout), so a tunnel with a key in the OpenSSH ssh-agent
      authenticates on Windows. `authMethods` calls the `dialAgent` seam.

## Definition of Done

- [x] `GOOS=windows go build ./...` is clean.
- [x] On a Windows host/CI runner: `portato daemon` + `portato list`
      round-trip works over the named pipe; the smart launcher attaches.
      (Verified by Phase 35 dogfooding on a real Windows host: the daemon/TUI
      run over the named pipe. The `windows-smoke` CI job asserts this
      automatically and will run on the next push.)
- [x] `portato install` adds the HKCU `Run` value; `uninstall` removes it;
      `portato doctor` reports the autostart state.
      (The `windows-smoke` CI job covers install/uninstall and runs on the next
      push; `doctor` is implemented and maintainer-accepted.)
- [x] darwin/linux are unaffected (build tags fully isolated).
- [x] `go vet ./...`, `gofmt -l .` clean on all platforms.
- [x] On a Windows host with a key loaded into the OpenSSH ssh-agent, a tunnel
      authenticates via the agent (`agentdial_windows.go`, named pipe).
      (Maintainer-accepted: the code cross-compiles/vets clean; not exercised by
      the Phase 35 dogfooding, which used password auth with no agent loaded.)

## Verification

```sh
GOOS=windows GOARCH=amd64 go build ./...      # cross-compiles
# On a Windows VM / CI runner:
portato daemon &
portato list
portato install
reg query HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v Portato
portato uninstall
```

## Technical details

- Named-pipe libs: `github.com/Microsoft/go-winio` (mature) is the usual
  choice; `npipe` is a lighter alternative. Pick one and pin it.
- Win10 1803+ also supports `AF_UNIX`, which would let the unix-socket path
  mostly carry over — evaluate, but named pipes remain the idiomatic Windows
  choice and avoid version gating.
- Registry access: `golang.org/x/sys/windows/registry`.
- This is the largest post-MVP phase and **requires a Windows environment**
  (VM or CI runner) to verify honestly — the build compiling is necessary but
  not sufficient.
- Out of scope here: Windows service (Service Control Manager) autostart; the
  Run key is the MVP-equivalent. SCM can be a later refinement.

## Verification status

All code, packaging and CI config for the phase is implemented. The status is
now `[x]` (done): the Windows runtime was verified by dogfooding Phase 35 on a
real Windows host (the daemon and TUI run over the named pipe, password auth
works end to end, the TOFU host-key prompt and the reconnect loop behave).

Verified locally (macOS dev host):

- `GOOS=windows GOARCH=amd64 go build ./...` — clean.
- `GOOS=windows GOARCH=amd64 go vet ./...` (with and without `-tags=e2e`) —
  clean.
- A real `PE32+ executable (console) x86-64, for MS Windows` binary is
  produced from `./cmd/portato`.
- `gofmt -l .` empty; `make vet`, `make test`, `make lint` green on darwin.
- darwin/linux behaviour unchanged (the named-pipe / registry / process
  code is fully build-tagged out there).

Accepted by the maintainer (pending their automated/manual check on the next
push/release):

1. **`windows-smoke` CI job** (`.github/workflows/ci.yml`) asserts the
   `portato daemon` + `portato list` round-trip over `\\.\pipe\portato` and the
   HKCU Run-key install/uninstall; it runs only on push/PR, so it will execute
   on the next push (the runtime path it asserts was covered by the manual
   dogfooding).
2. **`portato doctor` autostart reporting on Windows** is implemented
   (`autostart_windows.go` queries the Run value); maintainer-accepted.
3. **ssh-agent over the Windows named pipe** (`agentdial_windows.go` dials
   `\\.\pipe\openssh-ssh-agent`) cross-compiles/vets clean; not exercised by the
   Phase 35 dogfooding (which used password auth with no agent loaded) —
   maintainer-accepted.
4. **Known limitations already accepted** (documented in commit bodies and
   SPEC caveats): `portato stop` terminates rather than SIGTERMs (no
   `portato stop` graceful path); a detached daemon gets no ctrl-C at logout;
   named-pipe ACL relies on the Phase 18 bearer token (no per-user SDDL
   hardening yet); the seamless FD hand-off is intentionally skipped (clean
   close+rebind instead).
