---
phase: 7
title: Remote (-R) tunnels
status: done
depends_on: [6]
---

## Goal

Add support for `type: remote` (`-R`): listen on the remote host, forward to a
local address. Covers "forward a port from the server to me" scenarios (e.g.
exposing a local dev server to a production machine, or pulling a remote
service's port back to localhost).

## Semantics

For a `type: remote` tunnel:

- `remote` — the address listened on **on the SSH server** (`host:port` or a
  bare `port`, in which case the bind host defaults to `127.0.0.1`). A non-
  loopback bind requires `GatewayPorts yes` in `sshd_config`.
- `local` — the local address connections are forwarded to (a bare `port`
  defaults to `127.0.0.1`; same parsing as the `local` tunnel type).

This is the inverse of `type: local`: the listener lives on the server side and
is tied to the lifetime of the SSH client (re-established on every reconnect),
while the data path dials a local address.

## Scope

- `type: remote` is valid in `config.Validate`.
- `Tunnel.Start` branches by type: for `remote` — `client.Listen("tcp",
  remote)` on the server side after each successful dial, accept-loop →
  `net.Dial(local)` → bidirectional `io.Copy`.
- TUI/CLI display direction via a per-row arrow: `local` → `{Local} → {Remote}`,
  `remote` → `{Local} ← {Remote}`; the column header becomes `ENDPOINT`.
- Reconnect/backoff/keepalive — shared logic from Phase 2, not duplicated.
- Friendly error on a forbidden server-side bind, pointing at `GatewayPorts`.
- Documentation: `GatewayPorts yes` is required for a non-loopback bind.

## Out of scope

- `type: dynamic` (`-D` SOCKS5) — Phase 8.
- Seamless FD-passing for remote listeners during the standalone→daemon
  hand-off (the existing MVP limitation on the local side now has a mirror on
  the remote side: there is a brief window where the server-side port is
  unavailable during hand-off). No code change; documented as a known caveat.
- IPv6/UDP forwarding — not in SSH `-R` semantics for TCP anyway.

## Tasks

- [x] `internal/config/config.go`:
  - [x] `Validate` accepts `type == "local" | "remote"`; unknown types still
    rejected with a clear message.
  - [x] `(Tunnel) RemoteListenAddr() string` — normalises the server-side bind
    address (bare port → `127.0.0.1:<port>`).
  - [x] `local` for a remote tunnel keeps using `ListenAddr()` (the local
    forward target).
- [x] `internal/forward/tunnel.go`:
  - [x] `Start` branches by `cfg.Type`: local keeps binding the local listener
    synchronously; remote skips it (no local listener) and runs `runRemote`.
  - [x] `runRemote`: `dialSSH` → `client.Listen("tcp", RemoteListenAddr())` →
    `Connected` → accept-loop on the SSH listener + keepalive + `client.Wait`.
    On drop: close listener + client, backoff, reconnect.
  - [x] accept: for each remote conn — `net.Dial("tcp", ListenAddr())` +
    `io.Copy` in both directions.
  - [x] Friendly error mapping for a forbidden bind
    (`"listen ...: check GatewayPorts in sshd_config"`).
- [x] `internal/tui/view.go` and `internal/cmd/list.go`:
  - [x] endpoint string built by type (arrow `→` for local, `←` for remote).
  - [x] column header `LOCAL → REMOTE` → `ENDPOINT`.
- [x] `config.example.yaml`: a commented `type: remote` example.
- [x] `README.md`: a "Remote tunnels" section + the `GatewayPorts` requirement.
- [x] `docs/SPEC.md` §7/§8: clarify field semantics for `type: remote`.
- [x] Tests:
  - [x] `config_test.go`: `type: remote` is valid; an unknown type is rejected
    (re-baseline the existing `not supported yet` case to a truly-unsupported
    type).
  - [x] `tunnel_integration_test.go`: extend the test sshd to handle the global
    `tcpip-forward` / `cancel-tcpip-forward` requests and the `forwarded-tcpip`
    channel; an integration test for a remote tunnel covering traffic flow and
    auto-reconnect after an sshd drop/restart.

## Definition of Done

- [x] `type: remote` works: the port is listened on the remote host, traffic
  arrives at the local address (integration test + manual verification).
- [x] Direction is displayed correctly in the TUI and `portato list`
  (`←` for remote).
- [x] Reconnect works for a remote tunnel (sshd drop → recovery), covered by an
  integration test.
- [x] A forbidden server-side bind surfaces a friendly `GatewayPorts` hint.
- [x] README describes the remote scenario + the `GatewayPorts` requirement.
- [x] Tests (unit + integration) are green; `go vet ./...` and `gofmt -l .` are
  clean; `go build ./...` succeeds.

## Verification

```sh
cd portato
make fmt && make vet && make test
go test ./internal/forward/... -run 'Remote' -v
go test ./internal/config/... -v

# Manual (needs a reachable sshd with `GatewayPorts yes` for a non-loopback bind):
# 1. Put a remote tunnel in the config, e.g.:
#    tunnels:
#      - name: pull-db
#        type: remote
#        remote: 15432        # listened on the server (loopback)
#        local:  5432         # forwarded to here
#        ssh: user@bastion
#        enabled: true
# 2. Start the daemon (or `portato` standalone).
# 3. On the server: `nc 127.0.0.1 15432` reaches the local 5432.
# 4. Drop sshd → the tunnel reconnects and the server-side port is re-bound.
```

## Technical details

- **Listener lifetime (the core difference):** for `local`, `net.Listen` is
  bound once in `Start` and outlives reconnects; the accept-loop is independent.
  For `remote`, `client.Listen` returns a `net.Listener` whose lifetime is bound
  to the SSH client — so it must be created inside the reconnect loop right
  after a successful `dialSSH`, and closed when the client drops. The shared
  scaffolding (dial → backoff → keepalive → `client.Wait`) is reused verbatim.
- **`client.Listen` failure:** when sshd refuses the bind (e.g. `GatewayPorts
  no` and a non-loopback address, or the port is privileged/in use),
  `client.Listen` returns an error. Map it to a readable message recommending
  `GatewayPorts`.
- **No new abstraction:** per the Phase 2 note ("Do not introduce abstractions
  for remote/dynamic"), the divergence is a type switch inside `Tunnel`, not a
  strategy interface.
- **Status carries everything the UI needs:** `Status.Type` already lets the
  TUI/CLI pick the arrow; no struct change is required.
- **`forwarded-tcpip` (test server):** remote forwarding is driven by a global
  `tcpip-forward` request (server replies with the bound port) followed by
  `forwarded-tcpip` channels pushed from the server when a connection arrives
  on the bound port. The test sshd binds a real loopback port, remembers it,
  and on each accepted connection opens a `forwarded-tcpip` channel back to the
  client.

## Resolved open questions

- **GatewayPorts:** documentation + a friendly error message. No automatic
  workaround.
- **Direction display:** a per-row arrow in the endpoint column (`←` for
  remote), with the column header renamed to `ENDPOINT`.
