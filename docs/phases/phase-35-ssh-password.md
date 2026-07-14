---
phase: 35
title: SSH password authentication
status: in-progress
depends_on: [19, 30]
---

## Goal

A tunnel whose SSH server requires a password (no usable key) authenticates
with a password supplied interactively (TUI modal / CLI) and, opt-in, persisted
to the OS keyring. **The password is never stored in the config in plaintext**
(the §9 invariant is preserved); only an opt-out `password_auth` boolean is
configured. The password fallback is **on by default** (OpenSSH-style): when
keys don't authenticate, the dial prompts for a password, so existing
password-only hosts and servers that switch key→password need no config change.
`password_auth: false` opts out (per-tuber or `defaults`). Public-key auth is
always tried first, so a working key never triggers a password prompt. This
unblocks password-only SSH servers (the case that surfaced while verifying Phase
17 on Windows) on every platform. (The phase was originally specced as opt-in;
it was switched to on-by-default + opt-out before release for graceful handling
of existing users and server-side key→password changes — see Background.)

## Background

The current `authMethods` (`internal/forward/ssh.go:85`) builds only
`ssh.PublicKeys*` methods (agent + identity). With neither, the dial fails with
"no ssh auth method available" — by design (SPEC §9). This phase adds a password
branch that mirrors the existing passphrase flow (Phase 19/30): a
`PasswordProvider` blocks the dial until a password arrives, surfaces a pending
state to the UI, and re-prompts on a wrong password.

Unlike a passphrase (validated locally by `ssh.ParsePrivateKeyWithPassphrase`),
a password can only be validated by the server. `golang.org/x/crypto/ssh@v0.53.0`
calls `ssh.PasswordCallback` **once** per handshake (`client_auth.go:202`) and
`authenticate` dedupes methods via `tried` (`client_auth.go:97,137`), so there is
no within-handshake retry — and portato's 5s handshake deadline
(`internal/forward/ssh.go:67`) would time out an interactive prompt anyway. The
re-prompt loop is therefore **dial-level**: each iteration does a full `dialSSH`
with `ssh.Password(pw)`; an auth failure invalidates the password
(`provider.Delete`) and re-prompts with no backoff, and any other error returns
for the tuber's normal reconnect backoff. Keys are probed first, so a working key
never triggers a password prompt.

## Tasks

- [x] `internal/forward`: add a `PasswordProvider` interface and a `passwordSink`
      mirroring `PassphraseProvider`/`passphraseSink`
      (`internal/forward/passphrase.go:20-30`), plus a `dialWithPasswordPrompt`
      loop mirroring `loadIdentityWithPassphrase` (`passphrase.go:42-82`) whose
      validation step is a full `dialSSH` (Get → sink → Wait → dial → on
      auth-fail Delete + re-prompt with no backoff; on success sink("") +
      return; any other error returns for the tuber backoff). `forward` must
      stay free of an `internal/secret` import — the provider is injected at
      `NewEngine` (`engine.go:99`) and threaded `Engine → NewTuber → dialSSH`,
      exactly like the passphrase provider.
- [x] `internal/forward/ssh.go`: split the single-dial body of `dialSSH` into a
      reusable `dialOnce`, and make `dialSSH` a dispatcher — when `password_auth`
      is off it is today's key-only single dial (keeping the "no ssh auth method
      available" error); when on it runs `dialWithPasswordPrompt` (keys probed
      first, then the password loop). Add `isAuthFailed` reusing `mapDialError`'s
      auth-failed sentinel (`"unable to authenticate"` / `"no supported methods
      remain"`).
- [x] `internal/forward/state.go`: add a `Status.PendingPassword string` field
      (mirror `PendingPassphrase`, `state.go:85`); `State` stays `Connecting`
      while blocked.
- [x] `internal/config`: `password_auth` (a `*bool`, on by default) on tubers and
      `defaults`, plus `defaults.ssh_password_store` (bool, default false — opt-in
      keyring, mirror `IdentityPassphraseStore` at `config.go:53`). `nil`/true →
      on (OpenSSH-style prompt when keys fail); an explicit `false` opts out. A
      `*bool` distinguishes absent (on) from explicit false. ResolvedPasswordAuth
      is on unless either the tuber or defaults sets false; Defaults.Equal
      dereferences the pointer so a no-op reload doesn't spuriously restart
      tubers. **No password field anywhere in the schema.**
- [x] `internal/secret`: serve passwords by reusing `secret.Store` with a
      namespaced key `password:<user>@<host>:<port>` (passwords are per-account,
      identities per-file); gate keyring persistence on `ssh_password_store`
      via a live closure (`secret.NewStore`, `server.go:181-184` /
      `local.go:45-47`).
- [x] `internal/daemon/server.go`: `POST /tubers/{name}/password` handler
      mirroring `handlePassphrase` (`server.go:553`), body `{"password":"…"}`
      → `secrets.Set(accountKey, …)` (caches + wakes the blocked dial).
- [x] `internal/client`: `SetPassword(name, password)` → the endpoint above
      (mirror `client.go:136`).
- [x] `internal/controller`: `AcceptPassword(name, password)` on the interface
      (`controller.go:62`); `Local` writes the local store (`local.go:192`),
      `Remote` posts to the daemon (`remote.go:101`).
- [x] `internal/tui`: auto-open a password modal on `PendingPassword` (mirror
      `autoOpenIfPending`, `update.go:402`), a manual key (e.g. `o` — `p` is
      taken by passphrase), and `openPasswordModal`/`handlePasswordKey`/
      `passwordView` + a "password?" row hint (mirror `update.go:323,423`,
      `view.go:445,264`).
- [ ] (optional) `internal/cmd`: `portato add-password <user@host>` /
      `forget-password` keyed by account (keyring), mirroring `add-identity`/
      `forget-identity` (`identity.go:23`).
- [x] `docs/SPEC.md` §9/§16: document password auth (on by default/opt-out,
      key-preferred, never plaintext, keyring opt-in) and resolve the open
      question.
- [x] Tests mirroring the passphrase tests: provider blocking + wake, wrong
      password re-prompt, key-preferred ordering (no prompt when a key works),
      opt-out gating (auto-on default + password_auth:false → key-only),
      keyring opt-in.

## Definition of Done

- [x] A tunnel (on by default — no flag needed) to a password-only SSH server
      authenticates after the password is supplied via the TUI (and via the
      CLI/HTTP endpoint); the password is never written to config or logs.
- [x] Public-key auth is always tried first; a tunnel with a working key never
      prompts for a password.
- [x] `password_auth: false` opts a tunnel (or, in `defaults`, every tunnel) out
      of the password fallback (key-only, with reconnect retries; provider never
      consulted).
- [x] A wrong password is re-prompted; disabling/restarting a tunnel cancels a
      blocked prompt (ctx cancellation).
- [ ] With `defaults.ssh_password_store: true` the password persists across
      daemon restarts via the OS keyring; off (default) keeps it in-memory only.
      (Wiring + the in-memory path are covered by unit tests; the cross-restart
      keyring path is identical to the Phase 19 passphrase store and awaits a
      runtime/keyring verification — see the dual-[~] note below.)
- [x] `make fmt && make vet && make test && make lint` green; the new flows are
      covered by unit tests.
- [x] SPEC §9/§16 updated.

## Follow-ups from dogfooding

A real standalone run (connect a password host → quit → re-run) exposed a state
bug and two UX traps. Root cause: pressing `space` on a password-pending tuber
DISABLES it (space toggles; the modal opens via `o` or auto-open), which cancels
the dial — but `Tuber.Stop()` did not clear the `pending*` fields, so the tuber
still reported `PendingPassword`, the tick re-opened the modal over an already-
dead tuber, and submits went into the cache with no dial consuming them (the
"wrong password, try again" counter just climbed per enter). A later re-enable
connected from the cache without prompting.

- [x] **Fix 1 (must):** `Tuber.Stop()` (and `Reconfigure`, and the run-loop error
      branch) clear `pendingHost/Passphrase/Password`, so an Off/errored tuber
      shows no stale "password?" and no modal auto-opens over a dead tuber.
- [x] **Fix 2:** the password/passphrase modal ignores a leading space when the
      field is empty — an accidental space-press (masked, invisible) no longer
      corrupts the value. Tradeoff: a password/passphrase that genuinely starts
      with a space can't be entered as the first char (rare).
- [x] **Fix 3:** the "wrong password" hint is now driven by the dial's actual
      rejection count (`Status.PasswordAttempts`, derived in `passwordSink`),
      not a per-submit TUI counter — so it only appears when the server really
      rejected the password.
- [x] **Fix 4:** the help overlay documents `o` (and that the modal auto-opens);
      `space` is toggle-only.

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

- `golang.org/x/crypto/ssh` provides `ssh.Password(secret)` for the `password`
  method. Servers that only offer `keyboard-interactive` (common with PAM) are
  **out of scope** for this phase (a follow-up may add
  `ssh.KeyboardInteractive`); most password servers accept the `password`
  method.
- Re-prompt model: a wrong password cannot be detected locally — only the server
  knows. Because x/crypto/ssh does not retry the password method within one
  handshake (see Background), the loop spans multiple dials: a key-probe dial
  first (so a working key never prompts), then for each password a single dial
  with `ssh.Password(pw)`; auth-failed → `Delete` + re-prompt (no backoff, the
  tuber stays `Connecting` with `PendingPassword` set). A server that offers no
  `password` method must not loop forever — the error string carries the
  "attempted methods", which distinguishes a wrong password from "password not
  offered".
- Ordering: keys (agent → identity) are always tried before a password, so a
  successful key short-circuits before any password dial.
- Security: the password lives in process memory (the secret cache) and, only
  when opted in, in the OS keyring; it is sent to the server over SSH as a
  standard password auth — never to disk in plaintext.
- The provider key is the server account (`user@host:port`), not a file path,
  because a password is per-account while an identity passphrase is per-file.
- `forward` does not import `internal/secret`; the provider is an interface,
  keeping the dial unit-testable with a fake (the established pattern).
