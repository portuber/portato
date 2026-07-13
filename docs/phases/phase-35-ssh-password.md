---
phase: 35
title: SSH password authentication
status: todo
depends_on: [19, 30]
---

## Goal

A tunnel whose SSH server requires a password (no usable key) authenticates
with a password supplied interactively (TUI modal / CLI) and, opt-in, persisted
to the OS keyring. **The password is never stored in the config in plaintext**
(the §9 invariant is preserved); only an opt-in `password_auth` boolean is
configured. Public-key auth remains the default and is always tried first, so a
working key never triggers a password prompt. This unblocks password-only SSH
servers (the case that surfaced while verifying Phase 17 on Windows) on every
platform.

## Background

The current `authMethods` (`internal/forward/ssh.go:85`) builds only
`ssh.PublicKeys*` methods (agent + identity). With neither, the dial fails with
"no ssh auth method available" — by design (SPEC §9). This phase adds a
`ssh.PasswordCallback` branch that mirrors the existing passphrase flow
(Phase 19/30): a `PasswordProvider` blocks the dial until a password arrives,
surfaces a pending state to the UI, and re-prompts on a wrong password.

## Tasks

- [ ] `internal/forward`: add a `PasswordProvider` interface and a `passwordSink`
      mirroring `PassphraseProvider`/`passphraseSink`
      (`internal/forward/passphrase.go:20-30,42-82`), plus a
      `loadPasswordWithPrompt` loop (Get → sink → Wait → callback → Delete on
      reject). `forward` must stay free of an `internal/secret` import — the
      provider is injected at `NewEngine` (`engine.go:99`) and threaded
      `Engine → NewTuber → dialSSH`, exactly like the passphrase provider.
- [ ] `internal/forward/ssh.go` `authMethods`: add a `ssh.PasswordCallback(cb)`
      branch, **after** the agent and identity methods (keys preferred), gated
      by the opt-in flag below. The callback surfaces `PendingPassword`, blocks
      in `provider.Wait`, returns the password; `ssh` retries it on rejection
      so `provider.Delete` drives the re-prompt.
- [ ] `internal/forward/state.go`: add a `Status.PendingPassword string` field
      (mirror `PendingPassphrase`, `state.go:85`); `State` stays `Connecting`
      while blocked.
- [ ] `internal/config`: add per-tuber `password_auth` and `defaults.password_auth`
      (bool, default false — opt-in, avoids surprise prompts); add
      `defaults.ssh_password_store` (bool, default false — opt-in keyring,
      mirror `IdentityPassphraseStore` at `config.go:53`). **No password field
      anywhere in the schema.** (Validation already only covers `Tubers`, so a
      bool needs no validation wiring.)
- [ ] `internal/secret`: serve passwords by reusing `secret.Store` with a
      namespaced key `password:<user>@<host>:<port>` (passwords are per-account,
      identities per-file); gate keyring persistence on `ssh_password_store`
      via a live closure (`secret.NewStore`, `server.go:181-184` /
      `local.go:45-47`).
- [ ] `internal/daemon/server.go`: `POST /tubers/{name}/password` handler
      mirroring `handlePassphrase` (`server.go:553`), body `{"password":"…"}`
      → `secrets.Set(accountKey, …)` (caches + wakes the blocked dial).
- [ ] `internal/client`: `SetPassword(name, password)` → the endpoint above
      (mirror `client.go:136`).
- [ ] `internal/controller`: `AcceptPassword(name, password)` on the interface
      (`controller.go:62`); `Local` writes the local store (`local.go:192`),
      `Remote` posts to the daemon (`remote.go:101`).
- [ ] `internal/tui`: auto-open a password modal on `PendingPassword` (mirror
      `autoOpenIfPending`, `update.go:402`), a manual key (e.g. `o` — `p` is
      taken by passphrase), and `openPasswordModal`/`handlePasswordKey`/
      `passwordView` + a "password?" row hint (mirror `update.go:323,423`,
      `view.go:445,264`).
- [ ] (optional) `internal/cmd`: `portato add-password <user@host>` /
      `forget-password` keyed by account (keyring), mirroring `add-identity`/
      `forget-identity` (`identity.go:23`).
- [ ] `docs/SPEC.md` §9/§16: document password auth (opt-in, key-preferred,
      never plaintext, keyring opt-in) and resolve the open question.
- [ ] Tests mirroring the passphrase tests: provider blocking + wake, wrong
      password re-prompt, key-preferred ordering (no prompt when a key works),
      opt-in gating, keyring opt-in.

## Definition of Done

- [ ] A tunnel with `password_auth: true` to a password-only SSH server
      authenticates after the password is supplied via the TUI (and via the
      CLI/HTTP endpoint); the password is never written to config or logs.
- [ ] Public-key auth stays the default and is tried first; a tunnel with a
      working key never prompts for a password.
- [ ] A wrong password is re-prompted; disabling/restarting a tunnel cancels a
      blocked prompt (ctx cancellation).
- [ ] With `defaults.ssh_password_store: true` the password persists across
      daemon restarts via the OS keyring; off (default) keeps it in-memory only.
- [ ] `make fmt && make vet && make test && make lint` green; the new flows are
      covered by unit tests.
- [ ] SPEC §9/§16 updated.

## Verification

```sh
make fmt && make vet && make test && make lint
# Manual: a password-only SSH server (e.g. extend the in-process sshtest
# fixture with ssh.PasswordCallback auth, or an external host):
# 1. config: a tuber with password_auth: true, no usable identity.
# 2. portato (TUI) -> space -> Connecting -> modal auto-opens -> type password
#    -> enter -> Connected; `nc -z 127.0.0.1 <local>` works.
# 3. wrong password -> re-prompted.
# 4. a tuber WITH a working key -> no password prompt ever.
# 5. defaults.ssh_password_store: true -> restart daemon -> password reused
#    from the keyring (no re-prompt).
```

## Technical details

- `golang.org/x/crypto/ssh` provides `ssh.PasswordCallback(func() (string, error))`
  for the `password` method. Servers that only offer `keyboard-interactive`
  (common with PAM) are **out of scope** for this phase (a follow-up may add
  `ssh.KeyboardInteractive`); most password servers accept the `password`
  method.
- Ordering: `Auth` slice order is agent → identity → password, so a successful
  key short-circuits before the password callback is ever invoked.
- Security: the password lives in process memory (the secret cache) and, only
  when opted in, in the OS keyring; it is sent to the server over SSH as a
  standard password auth — never to disk in plaintext.
- The provider key is the server account (`user@host:port`), not a file path,
  because a password is per-account while an identity passphrase is per-file.
- `forward` does not import `internal/secret`; the provider is an interface,
  keeping the dial unit-testable with a fake (the established pattern).
