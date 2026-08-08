---
phase: 44
title: "~/.ssh/config resolution"
status: in-progress
depends_on: [43]
---

## Goal

Resolve a tuber's `ssh: <alias>` against the user's `~/.ssh/config` —
HostName / User / Port / IdentityFile / ProxyJump — so host definitions
aren't duplicated between ssh-config and `config.yaml`, and an alias's
ProxyJump auto-populates Phase 43's `jump:`. The top user-requested item
after ProxyJump (NixNicks on the Reddit launch post: "use stuff configured
in ~/.ssh/config").

## Background

`parseSSH` (`internal/config/config.go:508`) parses `ssh:` into
user/host/port and the tuber dials that directly — `~/.ssh/config` is never
read (verified: zero mentions of `ssh_config` in the codebase). Phase 43's
`jump:` is config-file-only; `SPEC.md:404` explicitly left "~/.ssh/config
resolution (populating `jump:` from an alias's `ProxyJump` directive) … as a
follow-up" — this is that follow-up.

The resolution is a **config-layer** concern: it fills the tuber's
User/Host/Port/Identity/Jump *before* any dial, so the forward/dial path
(including Phase 43's chain) is unchanged.

## Resolution model (openssh-faithful + precedence)

- `ssh:` is parsed by the existing `parseSSH` into user/host/port, tracking
  which parts were **explicit vs defaulted** (no `@` ⇒ user defaulted; no `:`
  ⇒ port defaulted).
- The host is resolved against `~/.ssh/config` (first-match-wins, patterns
  honoured via `github.com/kevinburke/ssh_config`): HostName (real address),
  User, Port, IdentityFile, ProxyJump.
- **Precedence:** explicit tuber values win — `ssh: me@alias:2222` overrides
  User/Port; tuber `identity:` / `jump:` override the config's IdentityFile /
  ProxyJump. ssh-config fills the gaps.
- **ProxyJump:** an alias's ProxyJump populates `jump:` and is resolved
  recursively — each hop that is itself an alias is expanded (cycle/depth
  guard). Phase 43 then dials the chain.
- No `~/.ssh/config`, or no Host block matches ⇒ unchanged (literal
  user@host:port).

## Tasks

- [ ] Add `github.com/kevinburke/ssh_config`; `go mod tidy`.
- [ ] `prepare()`: resolve the host against ssh-config (first-match-wins);
      extend `parseSSH` (or a thin wrapper) to surface explicit-vs-defaulted
      user/port so precedence is honoured.
- [ ] ProxyJump recursion: the alias's ProxyJump → `jump:` chain, expanding
      alias hops, with a visited-set + depth cap (cycle guard).
- [ ] IdentityFile token expansion: `~`, `%h` / `%u` / `%d` (openssh-style).
- [ ] Validate (openssh-faithful): error only if `~/.ssh/config` exists but is
      unreadable/unparseable, or a ProxyJump cycle/depth cap is hit; no-config
      or no Host match ⇒ the host is used literally, silently (matching OpenSSH).
- [ ] `known_hosts`: keep Portato's `ResolvedKnownHosts` (ssh-config
      `UserKnownHostsFile` out of scope for v1).
- [ ] Out of scope v1: `Match exec/host` conditional blocks (document the
      limitation).
- [ ] `docs/SPEC.md` (update the `:404` "out of scope" note → done; add a
      section on ssh-config resolution + the precedence model) +
      `config.example.yaml` + `README.md` (a `ssh: myalias` example).
- [ ] Tests: alias resolution (HostName/User/Port/IdentityFile);
      ProxyJump-from-config (recursive chain → Phase 43 dial carries traffic);
      precedence (tuber overrides); alias-not-found error; no-config graceful;
      cycle guard; IdentityFile token expansion.
- [ ] E2E: a black-box Go E2E (`make e2e-sshconfig`) — real binary, a temp
      `~/.ssh/config` whose alias carries a ProxyJump, `ssh: <alias>` dials
      through the chain and a `-L` forward carries traffic (macOS-verifiable);
      and a real-Linux systemd-docker case (`e2e.sh sshconfig`) against real
      OpenSSH (the Linux-compatibility gate, mirroring Phase 43's `jump`).

## Definition of Done

- [ ] `ssh: myalias` resolves HostName/User/Port/IdentityFile from
      `~/.ssh/config`.
- [ ] An alias with ProxyJump populates `jump:` and dials via Phase 43's chain
      (multi-hop, zero duplication).
- [ ] Explicit tuber `identity:` / `jump:` / `ssh: me@x:port` override
      ssh-config.
- [ ] `~/.ssh/config` exists but unreadable/unparseable, or a ProxyJump
      cycle/depth cap ⇒ clear load error; no `~/.ssh/config` or no Host match ⇒
      unchanged/literal behaviour (openssh-faithful).
- [ ] `go build ./...`, `gofmt -l .`, `go vet ./...`, `golangci-lint run ./...`
      clean; new functions under gocyclo 15; the phase's tests green.
- [ ] SPEC + README + `config.example.yaml` updated; the new dep is
      auto-bundled in `THIRD_PARTY_LICENSES.txt` (verified at release time).

## Verification

```sh
make fmt && make vet && make test && make lint

# manual: a temp ~/.ssh/config with a Host alias (+ a ProxyJump chain) →
# `portato list` shows the resolved endpoint; a -L forward through it carries
# traffic end to end.
```

## Technical details (sketch)

- `kevinburke/ssh_config` does first-match-wins pattern matching +
  `Get(alias, key)`; handles `Include`, multi-pattern `Host` lines, and
  negation.
- Resolution lives in `prepare()` (config layer); the dial path is untouched —
  ProxyJump-from-config fills the derived `Jumps` (the persisted `jump:` stays
  empty so Save never writes it back), which Phase 43 dials.
- `parseSSH` must surface explicit-vs-defaulted user/port so precedence
  honours `ssh: me@alias:2222` overrides.
- IdentityFile: expand `~` and `%h`/`%u`/`%d` tokens ourselves (kevinburke
  returns literals).
- Additive (a new optional resolution layer; no existing field's meaning
  changes for direct `user@host:port` values) ⇒ MINOR.
