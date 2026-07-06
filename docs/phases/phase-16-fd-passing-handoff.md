---
phase: 16
title: Seamless hand-off via FD-passing
status: done
depends_on: [5]
---

## Goal

Eliminate the MVP port-availability gap during the standalone→daemon
hand-off. Instead of `StopAll()` (releasing local ports) → spawn daemon →
rebind, the standalone passes its **already-bound local listeners** to the
spawned daemon as file descriptors (SCM_RIGHTS), and the daemon **adopts**
them — so the local port never goes down across the transition (new
connections are seamless) and the daemon never rebinds. The established SSH
session itself is **not** moved across processes: `golang.org/x/crypto/ssh`
keeps the transport's crypto state in memory and cannot resume a session in
another process, so the daemon re-dials; what survives is continuous **port
availability**, not in-flight data on the old SSH channels.

## Background

The Phase 5 hand-off (`internal/tui/handoff.go::handoffToDaemon`) runs
`ctrl.Close()` first (which `StopAll`s the tunnels and closes their
`net.Listener`s), then spawns `portato daemon` and waits for its socket.
`forward.Tunnel.Start` binds its `net.Listener` synchronously and does not
retry a failed bind, so the ports must be free when the daemon starts — this
creates the accepted MVP "blip" (SPEC §12). This phase removes it.

## Tasks

- [x] `forward.Tunnel`: add `ListenerFile() (*os.File, error)` —
      `t.listener.(*net.TCPListener).File()` — without closing the listener;
      return a typed error for `remote` (no local listener) and when stopped.
- [x] `forward.Engine`: add `LiveListenerFiles() (map[string]*os.File, error)`
      — one entry per running `type=local`/`dynamic` tunnel, keyed by name.
- [x] FD-transfer protocol: a small framing over a dedicated unix socket
      (separate from the HTTP control socket). Per tunnel: a JSON header
      `{"name": "...", "type": "..."}` followed by the FD sent with
      `(*net.UnixConn).WriteMsgUnix(..., unix.UnixRights(fds), ...)`.
- [x] `portato daemon`: accept `--listen-fds <unixsock>`; at startup read the
      offered FDs and reconstruct each listener via `net.FileListener`,
      adopting it into the engine (skip the `net.Listen` bind for that tunnel).
- [x] `tui/handoff.go`: open the transfer socket, spawn the daemon with
      `--listen-fds`, **send the live FDs before `ctrl.Close()`**, and keep the
      standalone alive until the daemon acks adoption (its `healthz` answers)
      — then exit.
- [x] Fallback: if FD-passing fails at any step, fall back to the current
      Phase 5 close→rebind path and log the degradation.
- [x] Unit tests: FD round-trip (`net.FileListener` reconstructs a working
      listener from the passed `*os.File`); the adoption path; the fallback
      path.

## Definition of Done

- [x] During hand-off, a long-lived TCP connection through a local tunnel
      survives (data flows continuously across the transition), or at minimum
      `nc -z 127.0.0.1 <local>` never fails during the hand-off window.
- [x] `type=remote` tunnels reconnect normally after hand-off (no FD to pass);
      `type=dynamic` passes its local listener like `local`.
- [x] FD-passing failure degrades gracefully to the Phase 5 hand-off (no crash,
      tunnels still come up after the brief MVP blip).
- [x] `go vet ./...`, `gofmt -l .`, `go test ./...` clean; cross-compilation
      darwin/linux × amd64/arm64 green.

## Verification

```sh
make build

# Automated (recommended): the black-box hand-off E2E proves the invariant:
make e2e-handoff

# Manual confirmation:
# 1. Ensure no daemon is running and exactly one enabled local tunnel is up.
# 2. Terminal 1 -- poll the local port; it must NEVER refuse across the hand-off:
while nc -z -w1 127.0.0.1 <local> >/dev/null 2>&1; do printf .; sleep 0.05; done; echo REFUSED
# 3. Terminal 2 -- standalone TUI, enable the tunnel, then hand off (q -> y):
./bin/portato
# 4. Expect no REFUSED in Terminal 1. The daemon re-dials SSH (uptime is fresh,
#    NOT carried over) -- only the LOCAL PORT is seamless, not in-flight data:
./bin/portato list           # daemon: Connected (uptime fresh -- SSH re-dialled)
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
