---
phase: 49
title: "Update checker (GitHub Releases check, consent-gated background polling)"
status: done
depends_on: [21]
---

## Goal

Portato learns, on its own, when a newer release is out — without a single
network request until the user explicitly agrees. Three surfaces:

1. **`portato update check`** — an explicit one-shot check (works always,
   ignores consent and the cache).
2. **A consent-gated background check** — only when the user opted in, the
   daemon polls GitHub at most once per 24h and caches the result; the TUI
   header and `portato doctor` show "vX.Y.Z available" from the cache, with
   zero network I/O of their own.
3. **A one-time consent ask** — on the first *interactive* launch (and at
   `install` / in `doctor`), following the Phase-48 nudge pattern: ask once,
   remember the answer, never nag again. The daemon never prompts.

Zero new dependencies (hand-rolled semver under the project's strict-`vX.Y.Z`
policy), zero changes to the IPC/transport/SCM surfaces.

## Background

Everything the checker needs already exists:

- The version is embedded at release time (`internal/cmd/version.go:17`,
  goreleaser ldflags); dev/snapshot builds carry `dev` / `vX.Y.Z-next`.
- Phase 21 publishes a GitHub Release per tag with `checksums.txt` and
  archives named `portato_<v>_<macOS|Windows|linux>_<x86_64|arm64>` — the
  update source is `GET /repos/portuber/portato/releases/latest`.
- `VERSIONING.md` pins the policy: tags are always stable `vX.Y.Z`, **never**
  pre-releases. GitHub's `releases/latest` already excludes drafts and
  pre-releases, so "stable-only" needs zero filtering code, and semver
  comparison degenerates to comparing three integers — no `golang.org/x/mod`
  (not in the module graph; the project prefers stdlib — the Phase-28
  no-fsnotify precedent).

Design locked with the maintainer:

- **Off by default, explicit consent.** No background network traffic happens
  until the user answers the one-time ask (or runs
  `portato update consent on`). The ask reuses the Phase-48 nudge skeleton:
  interactive launcher only (after the import offer), `term.IsTerminal` gate,
  a non-TTY launch leaves the question pending, the daemon never asks.
- **Consent + cache live in one state file** — `xdg.StateHome/portato/`
  `update.json` (`{consent, last_check, latest}`) — next to the log files,
  the Phase-40 socket and the Phase-48 markers. Rationale: consent is state,
  not a cache (it must survive cache clears), the directory already exists on
  every platform (including Windows `%LOCALAPPDATA%\portato\`), and the
  `PORTATO_STATE_HOME` test seam (`internal/config/markers.go:28`) applies.
- **Privacy:** the background check is an anonymous `GET` to
  `api.github.com` — no identifiers, no version-of-the-day telemetry beyond
  the request itself; `HTTPS_PROXY`/`NO_PROXY` honoured via the default
  transport. Anonymous GitHub API rate limit (60/h per IP) is a non-issue at
  one request per day.

## Tasks

- [x] **`internal/update` package — client**: `Release{Version, URL,
      PublishedAt, Assets}`; `Latest(ctx)` does
      `GET {base}/repos/portuber/portato/releases/latest` with a 10s timeout,
      `Accept: application/vnd.github+json`, a `portato/<version>` User-Agent;
      the base URL is the compile-time `DefaultBase` (`https://api.github.com`)
      — deliberately **not** runtime-configurable (no env, no flag), so the
      checker (and the Phase-50 `apply`) can only ever talk to GitHub. The
      in-repo test seam is `SetBaseForTest(t, base)` — a package-level
      setter taking a testing-style hook; production code cannot call it.
      Error taxonomy: network / rate-limited (403 + `X-RateLimit-Remaining:
      0`) / malformed — all surfaced as plain errors, never panic.
- [x] **Compare**: `ParseVersion("vX.Y.Z") → ([3]int, bool)` (strip the `v`,
      exactly three numeric components — the VERSIONING.md guarantee makes
      pre-release grammar dead code); `Compare(a, b) → -1|0|+1`. A
      non-parseable *current* build (`dev`, `unknown`, `*-next` snapshots) is
      "not comparable": commands report `current dev (not comparable); latest
      vX.Y.Z` instead of a bogus verdict.
- [x] **Consent + check cache**: consent is a *user setting* —
      `defaults.update_check: true|false` in `config.yaml`; **absent = "not
      asked yet"** (the tri-state without a third value). Persisted through
      the comment-preserving AST patcher (`SetDefaultsBoolNode`); `ask` = the
      key is removed, re-arming the question. The Phase-28 reload watcher
      picks a live edit up for free. The *machine* half — `CheckCache
      {last_check, latest}` — lives in `xdg.StateHome/portato/update.json`
      (atomic tmp+rename, 0600, `PORTATO_STATE_HOME` seam): written only on a
      successful check, corrupt = zero (the cache is disposable).
- [x] **One-time consent ask**: `maybeAskUpdateConsent()` in
      `runStandalone` (root.go:80) *after* `maybeOfferImport` (root.go:94) —
      the import offer owns the very first screen. Gate:
- [x] **One-time consent ask**: `maybeAskUpdateConsent()` in
      `runStandalone` (root.go:80) *after* `maybeOfferImport` (root.go:94) —
      the import offer owns the very first screen. Gate:
      `defaults.update_check == nil` && `term.IsTerminal(stdin)`. Ask
      "Check for portato updates in the background (GitHub, once a day)?
      [Y/n]" — Enter/y defaults to **yes** (low-stakes, reversible with one
      command); either answer is final, persisted via `SetDefaultsBoolNode`,
      and never asked again. The attach branch, every CLI command and the
      daemon never ask — a daemon-first bootstrap leaves the key absent for
      the next interactive launch (the Phase-48 fresh/import marker pair
      behaviour).
- [x] **Ask at `install`**: after a successful install (TTY only), the same
      question with the same persistence — install is the natural moment
      (the daemon it starts will do the polling).
- [x] **`doctor`**: an `update` check line — `update_check: false` →
      "checks off (`portato update consent on`)"; cache newer →
      "vX.Y.Z available (checked 2h ago)"; up to date → "up to date
      (checked 5h ago)"; never checked → the consent hint. When stdin is a
      TTY and the key is still absent, ask the same one-time question;
      otherwise just print the hint (doctor must stay scriptable).
- [x] **Daemon background check**: only when `update_check: true` — a ticker
      (1h) re-reads the config (a live `consent` edit reaches the daemon via
      the existing reload path) and checks the cache; `last_check` older
      than 24h → `Latest(ctx)`, write the cache. Failures log at debug and
      leave the cache untouched; no retry storm (a failed check does not
      advance `last_check`, so retries wait for tick + TTL). Consent flipped
      to `off` at runtime → the ticker sees it and goes idle.
- [x] **TUI header hint**: when the cached `latest` is newer than the
      running version, the header line gains a short `update: vX.Y.Z`
      segment (existing hint styling, theme-aware, one segment — no new
      rows). The TUI never performs network I/O; it reads the cache at
      launch. Hidden when up to date / not comparable / checks off.
- [x] **Commands**: `portato update check` (explicit check, ignores consent
      and cache age, prints current / latest / release URL; exit 0 on
      "up to date" and on "available", non-zero only on error) and
      `portato update consent on|off|ask` (on/off set the config key, ask
      removes it; prints what changed; `ask` re-arms the one-time question).
- [x] **Tests**: `ParseVersion`/`Compare` table (equal/major/minor/patch,
      `v`-prefix optional, garbage); `Latest` against `httptest` (200 parse,
      403 rate-limit, network refusal); cache round-trip + 0600 + corrupt →
      zero + `NeedsCheck` TTL table; `SetDefaultsBoolNode` (set/replace/
      remove-re-arms, absent-noop keeps bytes, defaults created, comments
      preserved); consent ask gating (non-TTY does not consume the absent
      key; y/n persist; daemon path never asks); daemon TTL logic (clock
      seam: fresh → checks, 23h-old → skips, 25h-old → checks); doctor line
      in all states; TUI header shows/hides per cache.
- [x] **Docs**: SPEC §3 command list (`update check`, `update consent`) and
      the new SPEC §17 "Update check and self-update" (checker half; the
      apply half is marked Phase 50); README "Updating" section.

## Definition of Done

- [x] `portato update check` against the real repo prints current vs latest
      (+ URL) and exits 0; with the network cut it fails cleanly non-zero;
      the fixture path through `SetBaseForTest` works with zero real network
      (test), and `NewClient` dials the compile-time `DefaultBase` when no
      seam is installed (test).
- [x] A config with no `defaults.update_check` + first interactive launch
      asks the consent question exactly once; `y`/Enter enables the daemon's
      daily check (proved by a fake-clock test), `n` never asks again and no
      background request ever happens; `update consent on|off|ask`
      round-trips (ask = key removed, question re-armed).
- [x] A daemon-first bootstrap does not consume the ask.
- [x] `doctor` prints the update line in all three consent states.
- [x] The TUI header shows the hint only when the cache holds a newer
      version; the TUI path performs no network I/O (test).
- [x] `go.mod` require block unchanged (zero new dependencies).
- [x] `make fmt && make vet && make test && make lint` clean;
      `GOOS=windows go build ./...` succeeds and the windows-tagged unit
      tests (the set the `windows-smoke` job runs) are green.

## Verification

```sh
make build
bin/portato update check                                             # real check against api.github.com
bin/portato update consent on                                        # then: doctor shows the line
go test ./internal/update/... -v
GOOS=windows go build ./... && GOOS=windows go test -tags windows ./internal/cmd/... -run TestUpdate -v
```

Manual: set `defaults.update_check: true` (or answer `y` on a first launch
without the key) → `doctor` reports the check once the daemon has polled;
`update consent off` → the daemon ticker goes idle (debug log).

## Technical details

- **Why `releases/latest` and not tag listing:** GitHub resolves it to the
  newest non-draft non-prerelease release — exactly the project's VERSIONING
  policy, for free. It 404s only before the first public release
  (v1.6.1 exists, so it won't).
- **Why hand-rolled compare:** `golang.org/x/mod/semver` is not in the module
  graph and pulls a module for what is, under the no-prerelease policy, an
  integer-triple comparison (~20 lines, fully table-tested).
- **Consent lives in config, cache in state** (a maintainer decision that
  revised the original all-in-state plan): consent is a *user setting* —
  visible and hand-editable in `config.yaml`, persisted through the same
  comment-preserving AST patching as `enabled:`, and a live edit reaches a
  running daemon through the existing Phase-28 reload watcher with zero new
  plumbing. The tri-state needs no third value: **absent = "not asked yet"**,
  `true` = daily checks, `false` = off; `update consent ask` removes the key.
  The cache (`last_check`/`latest`) is *machine state* — it stays in
  `xdg.StateHome/portato/update.json` so the config is not rewritten on every
  poll. Trade-off accepted: copying `config.yaml` to a new machine carries
  the answered consent (no second ask there).
- **Ordering with the Phase-48 nudge:** import offer first (it edits the
  config the TUI then loads), consent ask second — both one-shot, both
  standalone-only; two independent mechanisms mean no cross-coupling.
- **The TUI reads the cache at launch**, not an IPC call: works in
  standalone (no daemon) and stays consistent with "no new IPC methods".
- **Rate-limit handling:** a 403 with `X-RateLimit-Remaining: 0` is a
  temporary error — the cache keeps the last good `latest` and `last_check`
  stays fresh for TTL purposes only on success (a failed check does not
  reset the 24h clock, so a flaky network retries at most hourly, not
  per-request).
- **Not in scope:** downloading or applying anything (Phase 50), desktop
  notifications (the Post-1.0 hooks candidate), release channels
  (stable-only, per VERSIONING.md).
