---
phase: 19
title: Identity passphrase storage
status: todo
depends_on: [2]
---

## Goal

Allow passphrase-protected SSH identity keys to be used **without ssh-agent**,
by storing the passphrase in the OS keychain (macOS Keychain / Linux Secret
Service / Windows Credential Manager). Today the agent is the only way to use
a passphrase-protected key (SPEC §16 open question).

## Tasks

- [ ] `internal/secret` package: `Get/Set/Delete(service, key) string` over
      `github.com/zalando/go-keyring` (Keychain / Secret Service / Credential
      Manager). Injectable backend for tests.
- [ ] `forward/dialSSH`: when `ssh.ParsePrivateKey` fails with a passphrase
      error, retry with `ssh.ParsePrivateKeyWithPassphrase`; obtain the
      passphrase from (a) the keyring keyed by the identity path, then (b) a
      one-time prompt (TUI prompt / CLI `portato add-identity`), and
      optionally store it.
- [ ] Config: `defaults.identity_passphrase_store: true` (opt-in to keyring
      storage; default off so nothing is stored without consent).
- [ ] Daemon: in-memory passphrase cache per identity (reconnects must not
      re-prompt); the keyring provides cross-restart persistence.
- [ ] Unit tests: store round-trip with a mock keyring; the dialSSH passphrase
      retry path.

## Definition of Done

- [ ] A passphrase-protected identity connects with **no agent running**.
- [ ] The passphrase is NOT stored plaintext anywhere on disk — keychain only.
- [ ] After a daemon restart, when opt-in storage is on, the passphrase is
      reused from the keyring (no re-prompt); with opt-in off, the user is
      prompted again.
- [ ] `go vet ./...`, `gofmt -l .`, `go test ./...` clean; cross-compilation
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
