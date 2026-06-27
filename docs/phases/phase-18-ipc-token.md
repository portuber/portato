---
phase: 18
title: IPC authorization token
status: todo
depends_on: [4]
---

## Goal

Authenticate IPC on the unix socket with a **bearer token**, in addition to
the existing `0600` filesystem permission. A process that somehow gains access
to the socket path still cannot drive the daemon without the token (SPEC §16
open question).

## Tasks

- [ ] Daemon: at startup, generate a 32-byte token with `crypto/rand`, hex
      encode it, and write it to `<socketDir>/portato.token` (mode `0600`,
      next to the socket / discovery marker).
- [ ] HTTP middleware on the daemon: require
      `Authorization: Bearer <token>` on every route; reject with `401` on
      missing/invalid token.
- [ ] `healthz` policy: the discovery probe (smart launcher, hand-off) reads
      the token file and sends it, so `healthz` can stay protected like the
      rest. (Same-user callers can read the `0600` token file.)
- [ ] `internal/client`: read the token file (best-effort) and attach the
      `Authorization` header to all requests; if no token file is present,
      talk to an older daemon that does not require one (backward compatible).
- [ ] Unit tests: middleware rejects missing/wrong token, accepts correct;
      client attaches the header.

## Definition of Done

- [ ] A request without/with a wrong token returns `401`; with the correct
      token, `200`.
- [ ] The existing CLI / `attach` / hand-off / smart-launcher all work
      unchanged (token is read automatically by the client).
- [ ] The token is never logged; the token file is `0600` and lives next to the
      socket.
- [ ] `go vet ./...`, `gofmt -l .`, `go test ./...` clean.

## Verification

```sh
./bin/portato daemon &
TOKEN=$(cat "$(./bin/portato __socket-dir 2>/dev/null || echo ~/.portato)/portato.token")
# without token -> 401; with -> 200 (probe via the unix socket with curl --unix-socket)
./bin/portato list            # works (client attaches the token automatically)
```

## Technical details

- The middleware wraps `http.ServeMux` in `Server.routes`; a single `authmw`
  chain keeps it DRY.
- Backward compatibility is by token-file **absence**: an older client
  (no header) talking to a new daemon fails closed; a new client talking to an
  old daemon (no token file) simply sends no header. Within one install the
  versions match, so this is mainly a concern across upgrades.
- Consider an `--ipc-token off` escape hatch for break-glass scenarios
  (optional).
