---
phase: 19
title: Identity passphrase storage
status: done
depends_on: [2]
---

## Goal

Allow passphrase-protected SSH identity keys to be used **without ssh-agent**,
by storing the passphrase in the OS keychain (macOS Keychain / Linux Secret
Service / Windows Credential Manager). Today the agent is the only way to use
a passphrase-protected key (SPEC §16 open question).

## Tasks

- [x] `internal/secret` package: `Get/Set/Delete(service, key) string` over
      `github.com/zalando/go-keyring` (Keychain / Secret Service / Credential
      Manager). Injectable backend for tests.
- [x] `forward/dialSSH`: when `ssh.ParsePrivateKey` fails with a passphrase
      error, retry with `ssh.ParsePrivateKeyWithPassphrase`; obtain the
      passphrase from (a) the keyring keyed by the identity path, then (b) a
      one-time prompt (TUI prompt / CLI `portato add-identity`), and
      optionally store it.
- [x] Config: `defaults.identity_passphrase_store: true` (opt-in to keyring
      storage; default off so nothing is stored without consent).
- [x] Daemon: in-memory passphrase cache per identity (reconnects must not
      re-prompt); the keyring provides cross-restart persistence.
- [x] Unit tests: store round-trip with a mock keyring; the dialSSH passphrase
      retry path.

## Definition of Done

- [x] A passphrase-protected identity connects with **no agent running**.
- [x] The passphrase is NOT stored plaintext anywhere on disk — keychain only.
- [x] After a daemon restart, when opt-in storage is on, the passphrase is
      reused from the keyring (no re-prompt); with opt-in off, the user is
      prompted again.
- [x] `go vet ./...`, `gofmt -l .`, `go test ./...` clean; cross-compilation
      clean (the keyring lib must not break other platforms).

## Verification

```sh
ssh-keygen -t ed25519 -N "secret" -f /tmp/passkey
# point a tunnel's identity at /tmp/passkey, ensure ssh-agent is unset
./bin/portato daemon &
./bin/portato enable <tunnel>          # prompted once; Connected
# restart the daemon -> Connected again with no prompt (opt-in storage on)
```

## Technical details

- `go-keyring` is the canonical cross-OS wrapper; on headless Linux it needs a
      D-Bus/Secret Service available, so tests MUST mock it.
- The prompt surface lives behind the `Controller` so both the TUI and CLI can
      drive it; the daemon must be able to request a passphrase asynchronously
      (event/request channel) since it has no terminal.
- Security review needed: keychain item naming, whether to lock to the
  identity's absolute path or a hash, and a `portato forget-identity` command.
- Out of scope: agent forwarding, per-tunnel passphrase overrides (later).

## Decisions during implementation

- **Blocking dial (approach C).** Rather than fail-fast + reconnect-backoff
  while the user types (the TOFU pattern), a dial that needs a passphrase
  surfaces `Status.PendingPassphrase` and blocks on `secret.Store.Wait` until
  one arrives. This avoids a chatty backoff spin and means `AcceptPassphrase`
  needs no `Restart` — the blocked dial wakes on the store. A wrong passphrase
  is invalidated and re-prompted.
- **Keychain key = the identity path** (~ expanded, via `config.ExpandTilde`,
  matching `ResolvedIdentity`). Readable/debuggable via the OS tools; a hash
  was rejected as harder to debug. (Resolves the "absolute path or a hash"
  security-review item.)
- **CLI commands included.** `portato add-identity <path>` / `forget-identity
  <path>` always write/clear the OS keyring (explicit consent, regardless of
  `identity_passphrase_store`) and best-effort notify a running daemon
  (`POST/DELETE /identities`) so a blocked dial wakes. The opt-in flag governs
  only the TUI-modal auto-store path.
- **Daemon prompt plumbing reuses the TOFU seams:** a `Status` field
  (`pending_passphrase`), a controller method (`AcceptPassphrase`), and an
  explicit RPC (`POST /tunnels/{name}/passphrase`). The SSE event stream is
  signal-only, so the new field reaches attached clients unchanged.

## Verification notes

- DoD #1 (connects with no agent) is covered end-to-end by
  `internal/forward/passphrase_integration_test.go` against the in-process SSH
  server, including the block→provide and wrong→right paths.
- `internal/secret` cross-compiles clean for linux/amd64 and windows/amd64
  (go-keyring is pure Go). The pre-existing `GOOS=windows` build break is in
  `internal/daemon/discovery.go` (`syscall.Kill`) — a Phase 17 issue, not from
  this phase.
