---
phase: 16
title: Seamless hand-off via FD-passing
status: todo
depends_on: [5]
---

## Goal

Eliminate the MVP port-availability gap during the standalone→daemon
hand-off. Instead of `StopAll()` (releasing local ports) → spawn daemon →
rebind, the standalone passes its **already-bound local listeners** to the
spawned daemon as file descriptors (SCM_RIGHTS), and the daemon **adopts**
them — so the local port never goes down and in-flight connections survive.

## Background

The Phase 5 hand-off (`internal/tui/handoff.go::handoffToDaemon`) runs
`ctrl.Close()` first (which `StopAll`s the tunnels and closes their
`net.Listener`s), then spawns `portato daemon` and waits for its socket.
`forward.Tunnel.Start` binds its `net.Listener` synchronously and does not
retry a failed bind, so the ports must be free when the daemon starts — this
creates the accepted MVP "blip" (SPEC §12). This phase removes it.

## Tasks

- [ ] `forward.Tunnel`: add `ListenerFile() (*os.File, error)` —
      `t.listener.(*net.TCPListener).File()` — without closing the listener;
      return a typed error for `remote` (no local listener) and when stopped.
- [ ] `forward.Engine`: add `LiveListenerFiles() (map[string]*os.File, error)`
      — one entry per running `type=local`/`dynamic` tunnel, keyed by name.
- [ ] FD-transfer protocol: a small framing over a dedicated unix socket
      (separate from the HTTP control socket). Per tunnel: a JSON header
      `{"name": "...", "type": "..."}` followed by the FD sent with
      `(*net.UnixConn).WriteMsgUnix(..., unix.UnixRights(fds), ...)`.
- [ ] `portato daemon`: accept `--listen-fds <unixsock>`; at startup read the
      offered FDs and reconstruct each listener via `net.FileListener`,
      adopting it into the engine (skip the `net.Listen` bind for that tunnel).
- [ ] `tui/handoff.go`: open the transfer socket, spawn the daemon with
      `--listen-fds`, **send the live FDs before `ctrl.Close()`**, and keep the
      standalone alive until the daemon acks adoption (its `healthz` answers)
      — then exit.
- [ ] Fallback: if FD-passing fails at any step, fall back to the current
      Phase 5 close→rebind path and log the degradation.
- [ ] Unit tests: FD round-trip (`net.FileListener` reconstructs a working
      listener from the passed `*os.File`); the adoption path; the fallback
      path.

## Definition of Done

- [ ] During hand-off, a long-lived TCP connection through a local tunnel
      survives (data flows continuously across the transition), or at minimum
      `nc -z 127.0.0.1 <local>` never fails during the hand-off window.
- [ ] `type=remote` tunnels reconnect normally after hand-off (no FD to pass);
      `type=dynamic` passes its local listener like `local`.
- [ ] FD-passing failure degrades gracefully to the Phase 5 hand-off (no crash,
      tunnels still come up after the brief MVP blip).
- [ ] `go vet ./...`, `gofmt -l .`, `go test ./...` clean; cross-compilation
      darwin/linux × amd64/arm64 green.

## Verification

```sh
make build
# 1. In one terminal, keep a transfer going through a local tunnel:
nc 127.0.0.1 <local>          # or a curl loop
# 2. Standalone TUI, enable the tunnel, then hand off (q → y).
./bin/portato
# 3. The nc connection / port must stay up across the hand-off.
./bin/portato list            # daemon: Connected, uptime not reset
```

## Technical details

- Go APIs: `(*net.TCPListener).File()`, `net.FileListener(*os.File)`,
  `(*net.UnixConn).WriteMsgUnix`/`ReadMsgUnix`, `golang.org/x/sys/unix.UnixRights`.
- The FDs are sent over a **dedicated unix socket**, not via exec inheritance,
  to avoid `FD_CLOEXEC` issues across `Setsid`. `net.FileListener` dups the FD
  in the daemon, so the standalone may close its own copy after the daemon acks.
- macOS + Linux only (SCM_RIGHTS over a unix-domain socket). Windows (Phase 17)
  will need its own mechanism or skip FD-passing.
- Keep the change behind the existing hand-off seam (`startCmd`, `probeSocket`)
  so the tests can exercise both the FD path and the fallback.
