---
phase: 7
title: Remote (-R) tunnels
status: todo
depends_on: [6]
---

> Outline phase. To be detailed when approaching it (see CONVENTIONS §"When and how to change SPEC").

## Goal

Add support for `type: remote` (`-R`): listen on the remote `host`, forward to
a local `remote` address. Covers "forward a port from the server to me"
scenarios (e.g. exposing a local dev server to a production machine).

## Scope (preliminary)

- `type: remote` is valid in `config.Validate`.
- `Tunnel.Start` branches by type: for `remote` — `ssh.Client.Listen("tcp", remote)` on the server side, accept-loop → local `net.Dial(local)`.
- TUI/CLI correctly display direction (`local:5432 ← host:5432` for remote).
- Reconnect/backoff — shared logic from Phase 2, not duplicated.
- Documentation: `GatewayPorts yes` in `sshd_config` is required if the bind is not on loopback.

## Tasks (candidates)

- [ ] Generalize `forward/tunnel.go`: a common SSH-client lifecycle framework; the only difference is in `serve(ctx)` for local/remote.
- [ ] Implement the remote direction: `client.Listen` → accept → `net.Dial(local)` → `io.Copy`.
- [ ] `config.Validate` — accept `type == "remote"`; new field requirements (`remote` for a remote tunnel = the address on the server, `local` = where to forward locally).
- [ ] TUI: a direction symbol (`→` for local, `←` for remote) and address labels.
- [ ] Test: an integration test against a localhost sshd for the remote tunnel.

## Definition of Done

- [ ] The `type: remote` tunnel works: the port is listened on the remote host, traffic arrives at the local address (manual verification).
- [ ] Direction is displayed correctly in TUI/`portato list`.
- [ ] Reconnect works for remote too (sshd drop → recovery).
- [ ] README describes the remote scenario + the `GatewayPorts` requirement.
- [ ] Tests (unit + integration) are green; `go vet`, `gofmt` are clean.

## Technical details (preliminary)

- `ssh.Client.Listen("tcp", remote)` returns a `net.Listener` on the server side — we accept incoming connections from it.
- For each connection — `net.Dial("tcp", local)` + `io.Copy` in both directions.
- If sshd does not allow binding to the required address → error `"listen: port forward not permitted, check GatewayPorts in sshd_config"`.
- SSH-client states (Connecting/Connected/Reconnecting/Error) — shared across all types; the only difference is in listen/dial.

## Open questions

- Support for `GatewayPorts`-sensitive scenarios — document them or automatically suggest a workaround?
- How should remote tunnels be displayed in the TUI table so that the direction is obvious?
