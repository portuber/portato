---
phase: 17
title: Windows support
status: todo
depends_on: [4, 6]
---

## Goal

A native Windows build of portato: the daemon and its IPC server run over a
**named pipe**, `portato install`/`uninstall` manage a **registry Run-key**
autostart entry, and the TUI/CLI work as on macOS/Linux. Windows joins the
supported platform matrix (SPEC §15/§16).

## Tasks

- [ ] Abstract the IPC transport behind a build-tagged interface
      (`internal/daemon/transport`): unix-domain socket on darwin/linux
      (`net.Listen("unix", ...)`), named pipe on windows
      (`\\.\pipe\portato` via `github.com/Microsoft/go-winio` or
      `gopkg.in/natefinch/npipe.v2`).
- [ ] `internal/client`: build-tagged dialer (unix socket vs named pipe) behind
      the existing `client.New` surface.
- [ ] `internal/daemon/paths.go`: per-OS paths — Windows has no socket file;
      the PID file and the discovery marker live in `%LOCALAPPDATA%\portato\`.
- [ ] `internal/service/service_windows.go` (`//go:build windows`): autostart
      via `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` — write/remove a
      `REG_SZ` value `Portato` pointing at `portato daemon --config <abs>`.
      Implement `Install/Uninstall/Status`.
- [ ] Update `service_other.go` build tag to `//go:build !darwin && !linux &&
      !windows` so exactly one `newInstaller` exists per platform.
- [ ] Build-tag the hand-off `Setsid` (`syscall.SysProcAttr{Setsid: true}` is
      unix-only) — Windows uses `SysProcAttr{CreationFlags:
      windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}`.
- [ ] Build-tag the two `syscall.Kill` call sites (unix-only) found during the
      phase-21 windows build attempt: `pidAlive` in
      `internal/daemon/discovery.go` (PID-liveness check) and `stopKill` in
      `internal/cmd/stop.go` (`portato stop` sends SIGTERM). Windows needs an
      equivalent `OpenProcess`+`TerminateProcess` (or `taskkill`) path behind a
      build-tagged seam (the `stopKill` var in stop.go is already a seam).
- [ ] Packaging follow-up (phase 21): re-add `windows` to the `.goreleaser.yml`
      build matrix, restore the zip `format_overrides`, and add back the
      `scoops:` section (`portuber/scoop-bucket`) so a Scoop manifest publishes
      alongside the windows archive.
- [ ] CI: add `windows/amd64` (and `windows/arm64` when Go supports it) to the
      cross-compile matrix; add a Windows smoke test (`portato daemon` +
      `portato list` round-trip).

## Definition of Done

- [ ] `GOOS=windows go build ./...` is clean.
- [ ] On a Windows host/CI runner: `portato daemon` + `portato list`
      round-trip works over the named pipe; the smart launcher attaches.
- [ ] `portato install` adds the HKCU `Run` value; `uninstall` removes it;
      `portato doctor` reports the autostart state.
- [ ] darwin/linux are unaffected (build tags fully isolated).
- [ ] `go vet ./...`, `gofmt -l .` clean on all platforms.

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
