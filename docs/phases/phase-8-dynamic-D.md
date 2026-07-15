---
phase: 8
title: Dynamic (-D) SOCKS5
status: done
depends_on: [7]
---

## Goal

Add support for `type: dynamic` (`-D`): a SOCKS5 proxy on `local`, with all
traffic flowing through `host`. Covers "reach internal resources via a bastion
without a forward per port" — point a browser or `curl --socks5` at `local` and
let the SSH server do the dialing.

## Semantics

For a `type: dynamic` tunnel:

- `local` — the SOCKS5 listen address (`host:port` or a bare `port`, in which
  case the bind host defaults to `127.0.0.1`). Same parsing as the `local`
  tunnel type.
- `remote` — **unused** (ignored if present). Unlike `local`/`remote`, there is
  no fixed forward target: each SOCKS request carries its own destination, which
  is passed to `ssh.Client.Dial` on the server side.
- No auth. The proxy binds to loopback by default; auth is a later concern.

This reuses the Phase 2 listener/accept-loop scaffolding verbatim: the local
listener is bound once in `Start` and outlives reconnects; the only divergence
from `local` is the per-connection handler (a SOCKS5 server instead of a fixed
`client.Dial(remote)`).

## Scope

- `type: dynamic` is valid in `config.Validate`; for dynamic the `remote` field
  is **not** required (its presence/absence is ignored, only `local` is
  required).
- `Tunnel` accept-loop branches by type: for `dynamic` each accepted connection
  is handed to a SOCKS5 server whose `Dial` calls `ssh.Client.Dial`.
- TUI/CLI display: a per-row arrow `⇄ *` for dynamic (endpoint reads
  `{Local} ⇄ *`), alongside the existing `→`/`←` for local/remote.
- Reconnect/backoff/keepalive — shared logic from Phase 2, not duplicated.
- Documentation: `config.example.yaml` example, README scenario + curl/browser
  usage, SPEC field semantics for `type: dynamic`.

## Out of scope

- SOCKS5 username/password auth (MVP: none; loopback bind only).
- SOCKS5 UDP `ASSOCIATE` / BIND commands beyond what the library provides by
  default (TCP `CONNECT` is the meaningful case).
- A DNS-resolution policy (the library resolves/dials via the injected `Dial`,
  so DNS happens on the server side, as with OpenSSH `-D`).

## Tasks

- [x] Dependency: add `github.com/armon/go-socks5`.
- [x] `internal/config/config.go`:
  - [x] `Validate` accepts `type == "local" | "remote" | "dynamic"`; unknown
    types still rejected with a clear message.
  - [x] For `dynamic`, `local` is required but `remote` is **not** (skip the
    empty-`remote` check for dynamic); SSH host/port checks still apply.
- [x] `internal/forward/tunnel.go`:
  - [x] accept-loop branches by type: `dynamic` → `handleDynamicConn`, local →
    `handleConn` (unchanged). `Start`/`run`/`serveConnected` reused unchanged
    (dynamic already takes the local-listener path).
  - [x] `handleDynamicConn(client, conn)`: `socks5.New(&socks5.Config{ Dial:
    func(ctx, network, addr) (net.Conn, error) { return client.Dial(network,
    addr) } })` then `srv.ServeConn(conn)`.
- [x] `internal/forward/state.go`:
  - [x] `Status.Endpoint()` gains a dynamic branch: `dynamic` → `{Local} ⇄ *`.
- [x] `config.example.yaml`: a commented `type: dynamic` example; update the
  `type` comment.
- [x] `README.md`: a "Dynamic (SOCKS5) tunnels" section + `curl --socks5` /
  browser configuration.
- [x] `docs/SPEC.md` §5/§7/§8: mark `dynamic` implemented, document field
  semantics (local = SOCKS5 listen addr, remote unused).
- [x] Tests:
  - [x] `config_test.go`: `type: dynamic` is valid; `dynamic` with empty
    `local` is rejected; `dynamic` with empty `remote` is valid; `local`/`remote`
    with empty `remote` still rejected; an unknown type is rejected
    (re-baseline the existing `not supported` case from `dynamic` to a truly-
    unsupported type).
  - [x] `state_test.go`: add a dynamic case to `TestStatusEndpointDirection`
    (`{Local} ⇄ *`).
  - [x] `tunnel_integration_test.go`: a dynamic-tunnel integration test that
    hand-rolls a minimal SOCKS5 client (no-auth + CONNECT), connects through
    the local proxy to the test echo server, verifies a round-trip, then drops
    and restarts sshd and verifies reconnect + a second round-trip. The test
    sshd's `direct-tcpip` handling (already present for `-L`) is exactly what
    `client.Dial` needs — no server-side change.

## Definition of Done

- [x] `type: dynamic` works: a SOCKS5 proxy on `local`; a request through it
  (`curl --socks5 127.0.0.1:<local>`) reaches the destination via the bastion
  (integration test + manual verification).
- [x] Reconnect works for a dynamic tunnel (sshd drop → recovery), covered by an
  integration test.
- [x] Direction is displayed correctly in the TUI and `portato list`
  (`⇄ *` for dynamic).
- [x] README describes the dynamic scenario + curl/browser configuration.
- [x] Tests (unit + integration) are green; `go vet ./...` and `gofmt -l .` are
  clean; `go build ./...` succeeds.

## Verification

```sh
cd portato
make fmt && make vet && make test
go test ./internal/forward/... -run 'Dynamic' -v
go test ./internal/config/... -v

# Manual (needs a reachable sshd):
# 1. Put a dynamic tunnel in the config, e.g.:
#    tunnels:
#      - name: socks
#        type: dynamic
#        local: 1080
#        ssh: user@bastion
#        enabled: true
# 2. Start the daemon (or `portato` standalone).
# 3. `curl --socks5 127.0.0.1:1080 http://internal-host` reaches it via the bastion.
# 4. Point a browser at SOCKS5 host 127.0.0.1 port 1080 — traffic flows through.
# 5. Drop sshd → the tunnel reconnects and the proxy works again.
```

## Technical details

- **Library: `armon/go-socks5`.** Chosen over `txthinking/socks5` (actively
  maintained but heavy: TCP+UDP, pulls `go-cache`/`runnergroup`/`dns`, and dials
  via process-global `DialTCP` which breaks per-tunnel SSH clients). `armon` has
  exactly the API we need — `socks5.New(conf)`, `Config.Dial`, `ServeConn` — no
  transitive deps, 423 importers, MIT. Unmaintained since 2016 but SOCKS5 is a
  frozen, small protocol.
- **Per-connection handler is the only divergence.** `Start`/`run`/`acceptLoop`
  are shared with `local`: the listener is bound once in `Start`, the accept-loop
  outlives reconnects, and each accepted conn is dispatched by type. Per the
  Phase 2 note ("Do not introduce abstractions for remote/dynamic"), this is a
  type switch inside `Tunnel`, not a strategy interface.
- **Dial routing.** The SOCKS5 `Config.Dial` closes over the current
  `ssh.Client`, so a reconnect (new client) is picked up automatically: each
  accepted connection reads the live client from the connection-handling path
  that already checks `state == Connected && client != nil`.
- **No server-side test change.** The integration-test sshd already serves
  `direct-tcpip` channels by dialing the requested address and piping — which is
  precisely what `ssh.Client.Dial` (used by the SOCKS5 `Dial`) requests.

## Resolved open questions

- **Library choice:** `armon/go-socks5` (see Technical details).
- **Auth:** none for the MVP (loopback bind only); user/pass is a later concern.
- **Endpoint display:** `{Local} ⇄ *` — the `⇄` matches the existing `→`/`←`
  arrow idiom; `*` conveys "any destination".
