---
phase: 8
title: Dynamic (-D) SOCKS5
status: todo
depends_on: [7]
---

> Outline phase. Will be detailed as we approach it.

## Goal

Add support for `type: dynamic` (`-D`): a SOCKS5 proxy on `local`, with traffic
flowing through `host`. Useful for accessing internal resources via a bastion
without per-port forwards.

## Scope (preliminary)

- `type: dynamic` is valid.
- A SOCKS5 server on `local` that, for each incoming connection, calls `ssh.Client.Dial("tcp", <address from the SOCKS request>)`.
- Use an existing library (do not write our own SOCKS5 implementation). Candidates: `armon/go-socks5` or similar.
- Testing via `curl --socks5 127.0.0.1:<local> http://internal-host`.

## Tasks (candidates)

- [ ] Add the dependency (choose a SOCKS5 library).
- [ ] Implement `Tunnel.serveDynamic(ctx)`: create a SOCKS5 server with a custom `Dialer` that internally calls `ssh.Client.Dial`.
- [ ] `config.Validate` accepts `type == "dynamic"`; for dynamic, the `remote` field is not required — validate its absence / ignore it.
- [ ] TUI/`portato list` correctly displays the SOCKS tunnel.
- [ ] Test: bring up a local SOCKS proxy, then via `curl --socks5` reach an internal resource through the bastion.

## Definition of Done

- [ ] `type: dynamic` tunnel works: SOCKS5 proxy on `local`, `curl --socks5 127.0.0.1:<local> http://host` reaches `host` through `host`.
- [ ] Reconnect works (shared mechanism).
- [ ] A browser can be configured to use this SOCKS proxy — traffic flows through the bastion.
- [ ] README describes the dynamic scenario + an example of curl/browser configuration.
- [ ] Tests are green; `go vet`, `gofmt` are clean.

## Technical details (preliminary)

- `armon/go-socks5` — a mature implementation; it accepts a `Config.Dial` function into which we pass `ssh.Client.Dial`.
- The SOCKS5 server listens on `local`, parses the SOCKS request for each conn, extracts the dst address, and calls `ssh.Client.Dial("tcp", dst)`.
- The listen-loop and accept-handler are shared with local/remote; the only difference is the handler for the incoming conn.

## Open questions

- SOCKS5 + auth (user/pass) — is it needed? MVP: no auth (localhost-bind only).
- Which library to choose — `armon/go-socks5` or another?
