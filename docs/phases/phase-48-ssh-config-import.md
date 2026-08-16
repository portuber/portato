---
phase: 48
title: "Import forwards from ~/.ssh/config (portato import + first-run offer)"
status: in-progress
depends_on: [44]
---

## Goal

A one-shot migration path for people whose port forwards already live in
`~/.ssh/config`: `portato import` reads the `LocalForward` / `RemoteForward` /
`DynamicForward` directives and creates matching `enabled: false` tubers —
and a fresh install offers to do it once (interactive y/N) on the first
*interactive* launch, never again after that.

Two deliverables:

1. **The machinery — `portato import`** (works on any install, any time):
   `portato import [<host-pattern>…]` imports the matching host blocks, or
   every block with `--all`; `--from <path>` overrides the config location
   (default `~/.ssh/config`); `--dry-run` lists the candidates without
   touching `config.yaml`; `--yes` skips the confirmation (required in a
   non-TTY).
2. **The nudge — first-run offer**: on the first interactive launch of a
   *fresh* install, if ssh_config has forwards, show them and ask y/N.
   Fresh-install-only (state markers), one-shot, never appears for upgrading
   users.

## Background

Phase 44 made `ssh: <alias>` *resolve* against `~/.ssh/config`
(HostName/User/Port/IdentityFile/ProxyJump), but Portato never *reads* the
`*Forward` directives — verified: 0 code matches for `LocalForward` /
`RemoteForward` / `DynamicForward`, and no `import` command exists. People
migrating from hand-rolled `ssh -N` setups re-type every tunnel into
`config.yaml` by hand; issue #1 (avdotius) was exactly this audience — an
`ssh -L` user translating his command manually.

This is deliberately NOT another resolution feature: it is a one-time COPY
(a snapshot into `config.yaml`), not a live link. `~/.ssh/config` is never
modified — read-only parse, zero network, explicit consent. The privacy
envelope is the Phase-44 trust boundary (Portato already reads ssh_config
for alias resolution); importing adds no new data access, only a new use.

Design locked with the maintainer:

- UX = **machinery (`portato import`) + first-run nudge**. A `doctor` hint
  was dropped — existing users don't need the nudge; an every-start prompt
  was rejected — the daemon is non-interactive and repeating prompts are
  naggy.
- The nudge is **fresh-install-only and one-shot**: markers `fresh_install`
  + `import_offered` in the state dir. `portato daemon` creating the config
  first (non-interactive) must NOT consume the moment — the next interactive
  run still offers; upgrading users (whose config predates this phase) have
  no `fresh` marker and are never nudged.
- Import-all on `y` (the list is shown first); imported tubers land
  `enabled: false` — nothing auto-connects without consent.

## Tasks

- [ ] **Scanner** (`internal/importer/`): load ssh_config via the Phase-44
      reader (`config.loadUserSSHConfig`, config.go:551) and collect
      candidates: for each host block (skipping `Host *` and `Match`), read
      `LocalForward`, `RemoteForward`, `DynamicForward` **via `cfg.GetAll`**
      (`Get` returns only the first match; a host may carry several
      forwards). Candidate = {sshHost (the block's pattern), type, local,
      remote}.
- [ ] **Semantics mapping**:
      - `LocalForward [bind:]port host:port` → `type: local`,
        `local: [bind:]port`, `remote: host:port`.
      - `RemoteForward [bind:]port host:port` → `type: remote`,
        `remote: [bind:]port`, `local: host:port`. **Bare-port expansion:**
        OpenSSH binds the remote side to loopback by default, while
        Portato's bare port normalises to `*:port` (all interfaces) — so a
        bare `RemoteForward` port must import as `127.0.0.1:port` to
        preserve behaviour (the Phase-13 normalisation caveat).
      - `DynamicForward [bind:]port` → `type: dynamic`,
        `local: [bind:]port`.
- [ ] **Dedup**: drop a candidate an existing tuber already covers (same
      type + local + remote + resolved ssh host) and duplicates within one
      import; the CLI lists skipped ones as `already configured`.
- [ ] **Naming**: derive a valid tuber name (`validName`, config.go:807)
      from the host pattern + local port (`db-5432`), de-conflicted with a
      `-2`/`-3` suffix. Names appear in the preview list.
- [ ] **`portato import` command** (`internal/cmd/import.go`): optional
      positional host patterns + `--all` / `--from` / `--dry-run` / `--yes`;
      default = show the list, ask y/N; non-TTY without `--yes` fails with
      a hint. Writes through the existing config save path, prints one line
      per created tuber. No completion surface changes (patterns, not
      names).
- [ ] **First-run nudge**: the `fresh_install` marker is written when
      `EnsureExample` (config.go:335) creates the config (created=true);
      `import_offered` is written when the offer is shown (either outcome)
      or when a fresh install's scan finds 0 candidates (silent, still
      one-shot). The nudge runs in the **interactive launcher branch**
      (`rootRunE` → `runStandalone`, root.go:57/80 — the path that ends in
      the TUI), never in `daemonRunE`. Markers live in the daemon state dir
      (`internal/daemon/paths_unix.go` / `paths_windows.go`;
      `xdg.StateHome/portato/`).
- [ ] **Resolution reuse**: imported tubers keep `ssh: <pattern>` verbatim
      so Phase 44 resolves HostName/User/Port/IdentityFile/ProxyJump at
      load — the importer maps directives, it must not re-implement
      resolution.
- [ ] **Tests**: scanner (multi-forward per host, `Host *`/`Match` skip,
      semantics swap, bare-port expansion, dedup), CLI (`--dry-run`,
      `--yes`, non-TTY guard, host-pattern filtering), markers (fresh →
      offered once; daemon-first does not consume the offer; upgrade user
      never nudged), ssh_config NOT modified (content/mtime asserted
      unchanged).
- [ ] **Docs**: README "Importing from ~/.ssh/config" section; SPEC §7
      note (import = copy, ssh_config never written).

## Definition of Done

- [ ] `portato import --dry-run` lists candidates from a fixture ssh_config;
      `portato import --yes` creates them `enabled: false`, and the config
      round-trips (`portato list` shows them off).
- [ ] A fresh install (no config) with a forwarded ssh_config gets the
      one-time y/N offer on first interactive launch; accepting or
      declining never re-prompts; 0 candidates ⇒ no prompt at all, still
      never again.
- [ ] An upgraded install (config exists, no markers) is never nudged.
- [ ] `portato daemon` running first does not suppress a later interactive
      offer.
- [ ] `~/.ssh/config` is byte-identical after an import (test-asserted).
- [ ] A bare-port `RemoteForward` imports as `127.0.0.1:port`
      (loopback-preserving, not `*:port`).
- [ ] Multi-forward hosts import all their forwards (`GetAll`, not `Get`).
- [ ] `make fmt && make vet && make test && make lint` clean.

## Verification

1. Fixture ssh_config: `Host db` with `LocalForward 5432 10.0.0.5:5432` +
   `DynamicForward 1080`; `Host ci` with `RemoteForward 8080 127.0.0.1:80`;
   a `Host *` block carrying a forward (must be skipped).
2. `portato import --dry-run` → three candidates listed; the `Host *`
   forward is absent.
3. `portato import --yes` → tubers created `enabled: false`; `portato list`
   shows them; the ssh_config file is unchanged.
4. Marker flow: remove state markers + config → run `portato daemon` →
   stop it → run the TUI → the offer appears once; re-run → never again.

## Technical details

- **Why per-block iteration (a `GetAll` refinement)**: a literal
  `cfg.GetAll(pattern, key)` also collects values from every *other* block
  whose patterns match the alias (notably `Host *`), leaking global forwards
  into each host's candidates and breaking the skip rule. The scanner
  instead walks `cfg.Hosts` and reads each block's own KV nodes for the
  three directives (all occurrences — the multi-forward intent of "GetAll,
  not Get"), and `Include` nodes within a block via `Include.GetAll`. The
  skip rules (`Host *` / implicit / `Match` / negated-only) apply uniformly
  to blocks enumerated from Include files too (the library does not expose
  those via `cfg.Hosts`, so the scanner decodes them itself).
- **Markers**: empty regular files next to the daemon's state files (the
  `xdg.StateHome/portato/` dir); presence = truth. The pair exists so a
  non-interactive first run (the daemon creating the config) does not
  consume the one-time offer.
- **Why `GetAll`**: ssh_config `Get` returns only the first value;
  `LocalForward` is a repeatable directive — a host with three forwards
  must import three tubers.
- **`Match` blocks**: `kevinburke/ssh_config` does not support `Match`
  (Phase 44's documented limitation) — forwards under `Match` are invisible
  to the scanner; the README says so.
- **The `ssh:` field keeps the raw pattern** (`db`, not the resolved
  `db.internal.corp:2222`) so load-time Phase-44 resolution and the
  precedence rules (explicit tuber fields win) apply unchanged, and `Save`
  round-trips the user's own spelling.
- **Not in scope**: per-forward cherry-picking inside the nudge
  (import-all or nothing — `portato import <pattern>` remains available
  for selective imports), and merging beyond the dedup skip into a
  heavily-edited existing config.
