---
phase: 41
title: "Forwarding-permission diagnostics"
status: done
depends_on: [11, 7, 8]
---

## Goal

When a tunnel fails because of a **server-side** sshd setting —
`AllowTcpForwarding no` (rejects every `-L`/`-R`/`-D`) or `GatewayPorts no`
(silently downgrades a non-loopback `-R` bind to loopback) — give the user a
precise, actionable diagnosis instead of a generic dial/listen error. Today
only the `-R` listen-failure hint exists; `-L`/`-D` failures surface as
per-connection log lines with no hint, and `portato doctor` checks only the
client side.

## Background

`golang.org/x/crypto/ssh` exposes the two server-side gates differently:

- `AllowTcpForwarding no` makes sshd **reject** the `direct-tcpip` channel open
  (`-L`/`-D`, surfaced by `client.Dial`) and the `tcpip-forward` global request
  (`-R`, surfaced by `client.Listen`) — both come back as errors
  (administratively prohibited / request failed).
- `GatewayPorts no` does **not** error: sshd honours the `tcpip-forward`
  request but binds the listener to loopback regardless of the requested
  address. So a `*:port` `-R` "succeeds" yet is unreachable publicly. This is
  **not detectable from the client**: the `tcpip-forward` reply carries only the
  bound port (RFC 4254 §7.1), so `ssh.Client.Listen` builds its listener address
  from the **requested** host — `ln.Addr()` always reflects what was asked for,
  never what sshd actually bound. The silent-loopback case is therefore a
  documentation concern (README Prerequisites + Troubleshooting), not something
  this phase auto-detects.

The `tuber.go` remote listen-error hint (widened to `AllowTcpForwarding` in the
preceding docs/fix batch) names the right knob for an error; Phase 41 adds the
*missing* detection: the silent-loopback case, the `-L`/`-D` dial hint, and a
doctor command that proves it from the outside.

`portato doctor` is a separate process and is currently purely client-side
(config, keys, agent, daemon, autostart). Adding a real SSH probe means real
network/auth side-effects, so it lands behind an opt-in `--probe` flag — the
default `doctor` stays fast and side-effect-free. The probe is
**non-interactive** (doctor can't show the TUI passphrase/password modal), so it
authenticates with configured identity keys + ssh-agent + keyring-cached
passphrases, and skips password-auth tubers with an informational line.

## Tasks

- [x] Phase file + ROADMAP row/summary/current-work updated; status `[~]`.
- [x] `internal/sshtest`: extend the in-process server with two fixtures the
      probe tests against — (a) reject `direct-tcpip` opens + `tcpip-forward`
      requests (simulating `AllowTcpForwarding no`), (b) accept `tcpip-forward`
      but bind loopback regardless of the requested address (simulating
      `GatewayPorts no` silent downgrade). Reuse the existing direct-tcpip /
      tcpip-forward plumbing.
- [x] `internal/forward`: a non-interactive probe path (no passphrase/password
      sinks) reusing `dialSSH`'s transport; a classifier returning, per tuber,
      the server-side outcome — `client.Dial` probe for `-L`/`-D`,
      `client.Listen` + `ln.Addr()` inspection for `-R` — distinguishing
      healthy / `AllowTcpForwarding`-no / `GatewayPorts`-silent-loopback /
      connectivity / auth.
- [x] `internal/forward` runtime: in `handleConn`/`handleDynamicConn`, detect
      the administratively-prohibited signature
      (`*ssh.OpenChannelError{Reason: ssh.Prohibited}`) on a `client.Dial`
      failure and append an `AllowTcpForwarding` hint to the log line. (A `-R`
      silent-loopback warning is **not** achievable client-side — see Background
      — and stays a docs concern.)
- [x] `internal/cmd/doctor.go`: a `--probe` flag; when set, run the classifier
      per configured tuber (key/agent + keyring-passphrase; skip password-auth
      with an info line)       under a per-host timeout (~5s), printing `✓ forwarding` /
      `✗ forwarding  AllowTcpForwarding no on <host>` /
      `· forwarding  <connectivity/auth>`; for a `-R` non-loopback bind it
      additionally notes `GatewayPorts not verifiable client-side`. Default
      `doctor` (no `--probe`) is unchanged.
- [x] Tests: sshtest fixtures covered; the classifier unit-tested against all
      outcomes; `doctor --probe` integration against the sshtest fixtures
      (permission denied, silent loopback, healthy); runtime `-R`
      silent-loopback warning + `-L`/`-D` dial-hint covered by tuber
      integration tests.
- [x] `docs/SPEC.md`: a short "Server-side requirements" note
      (`AllowTcpForwarding` default-on for all types; `GatewayPorts` for
      non-loopback `-R`) + the doctor `--probe` description. README
      Troubleshooting's AllowTcpForwarding / GatewayPorts rows already point at
      `portato doctor`; cross-link `--probe`.

## Definition of Done

- [x] `portato doctor --probe` distinguishes, per configured tuber, the classes
      (healthy / `AllowTcpForwarding`-no / `GatewayPorts`-silent-loopback /
      connectivity-or-auth), verified against `sshtest` fixtures.
- [x] Default `portato doctor` (no `--probe`) makes **no** SSH connections —
      unchanged behaviour and output.
- [x] `doctor --probe` prints an honest `GatewayPorts not verifiable
      client-side` note for `-R` non-loopback binds (auto-detection is
      impossible client-side — see Background); the silent-loopback case stays
      covered by README docs.
- [x] `-L`/`-D` dial failures under `AllowTcpForwarding no` log a hint pointing
      at the server knob.
- [x] The remote listen-error hint stays consistent with the new detection
      (AllowTcpForwarding first, GatewayPorts for the non-loopback case).
- [x] `go build ./...`, `gofmt -l .`, `go vet ./...`, `golangci-lint run ./...`
      are clean; new functions sit under gocyclo 15; the phase's tests are green.

## Verification

```sh
make fmt && make vet && make test && make lint

# manual (needs a reachable sshd you control):
# - AllowTcpForwarding no:  in sshd_config set `AllowTcpForwarding no`, reload
#   sshd, then `./bin/portato doctor --probe` -> "✗ forwarding  AllowTcpForwarding no".
# - GatewayPorts no:        with `GatewayPorts no` and a `*:port` -R tuber,
#   `./bin/portato doctor --probe` -> "✗ forwarding  GatewayPorts no: bound loopback",
#   and the TUI shows the silent-loopback warning instead of a bare Connected.
```

## Technical details (sketch)

- `internal/sshtest/sshd.go`: add a rejected-forward server (reject direct-tcpip
  + tcpip-forward) and a silent-loopback knob on the existing server (honour
  `tcpip-forward` but force-bind 127.0.0.1). Reuse `serveDirect` / `serveForwards`.
- `internal/forward/probe.go` (new): a non-interactive dial (keyring-backed
  passphrase provider, no password sink) + the classify logic, shared with the
  runtime `-R` `ln.Addr()` check where possible.
- `internal/cmd/doctor.go`: `doctorCmd` gains `--probe`; `doctorRunE` runs
  `checkForwarding(d, cfg)` only when `--probe` is set. Per-host timeout via
  `context.WithTimeout`.
- Administratively-prohibited detection: `errors.As(err, &oce)` where `oce` is
  `*ssh.OpenChannelError` and `oce.Reason == ssh.Prohibited`. The probe opens a
  `direct-tcpip` to a throwaway target (`127.0.0.1:1`) — `Prohibited` ⇒
  `AllowTcpForwarding no`; any other outcome ⇒ forwarding permitted
  (AllowTcpForwarding applies uniformly to direct-tcpip and tcpip-forward, so
  one probe suffices).
