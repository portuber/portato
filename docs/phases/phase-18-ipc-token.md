---
phase: 18
title: IPC authorization token
status: done
depends_on: [4]
---

## Goal

Authenticate IPC on the unix socket with a **bearer token**, in addition to
the existing `0600` filesystem permission. A process that somehow gains access
to the socket path still cannot drive the daemon without the token (SPEC §16
open question).

## Tasks

- [x] Daemon: at startup, generate a 32-byte token with `crypto/rand`, hex
      encode it, and write it to `<socketDir>/portato.token` (mode `0600`,
      next to the socket). Lives in `internal/ipctoken` (leaf package, shared
      with the client).
- [x] HTTP middleware on the daemon: require
      `Authorization: Bearer <token>` on every route; reject with `401` on
      missing/invalid token (constant-time compare; active only when a token
      exists).
- [x] `healthz` policy: the discovery probe (`daemon.probeSocket`) reads the
      token file and sends it, so `healthz` stays protected like the rest.
      (Same-user callers can read the `0600` token file.)
- [x] `internal/client`: read the token file (best-effort) and attach the
      `Authorization` header to all requests via a `tokenTransport`
      `RoundTripper`; if no token file is present, no header is sent
      (backward compatible with an older daemon).
- [x] Unit tests: middleware rejects missing/wrong token, accepts correct;
      client attaches the header; discovery probe attaches the token;
      `--ipc-token off` disables generation.
- [x] Escape hatch: `--ipc-token off` (and `PORTATO_NO_IPC_TOKEN=1`) skip token
      generation and serve openly over the `0600` socket.

## Definition of Done

- [x] A request without/with a wrong token returns `401`; with the correct
      token, `200`. (Unit: `TestServer_AuthMiddleware` /
      `TestServer_AuthProtectsEveryRoute`. E2E: raw `curl --unix-socket`
      without header → 401, with token → 200.)
- [x] The existing CLI / `attach` / hand-off / smart-launcher all work
      unchanged (token is read automatically by the client). (E2E:
      `portato list` against an authed daemon succeeds with no call-site
      changes.)
- [x] The token is never logged; the token file is `0600` and lives next to the
      socket. (Only the token *path* is logged; `stat` shows `-rw-------`.)
- [x] `go vet ./...`, `gofmt -l .`, `go test ./...` clean.

## Verification

```sh
make build
E2E=$(mktemp -d /tmp/pt-e2e.XXXX); CFG="$E2E/config.yaml"; SOCK="$E2E/portato.sock"
printf 'defaults:\n  known_hosts: /dev/null\ntunnels: []\n' > "$CFG"
PORTATO_SOCKET="$SOCK" ./bin/portato daemon --config "$CFG" &
sleep 0.5
TOKEN=$(cat "$E2E/portato.token")     # next to the socket, mode 0600
# without token -> 401; with -> 200 (probe via the unix socket)
curl -s --unix-socket "$SOCK" -o /dev/null -w "%{http_code}\n" http://portato/healthz
curl -s --unix-socket "$SOCK" -H "Authorization: Bearer $TOKEN" -o /dev/null -w "%{http_code}\n" http://portato/healthz
PORTATO_SOCKET="$SOCK" ./bin/portato list    # 200 (client auto-attaches the token)
# escape hatch: no token, serves openly
PORTATO_SOCKET="$SOCK" ./bin/portato daemon --config "$CFG" --ipc-token off   # (after stopping the first)
```

## Technical details

- The middleware wraps `http.ServeMux` in `Server.routes`; a single `authmw`
  chain keeps it DRY.
- Backward compatibility is by token-file **absence**: an older client
  (no header) talking to a new daemon fails closed; a new client talking to an
  old daemon (no token file) simply sends no header. Within one install the
  versions match, so this is mainly a concern across upgrades.
- The `--ipc-token off` escape hatch (and `PORTATO_NO_IPC_TOKEN=1`) skip token
  generation and leave the daemon serving openly over the `0600` socket, for
  break-glass scenarios. The env wins over the flag so it can force a
  disabled run regardless of how the daemon was launched.
