---
phase: 49
title: "Update checker (GitHub Releases check, consent-gated background polling)"
status: in-progress
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

- [ ] **`internal/update` package — client**: `Release{Version, URL,
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
- [ ] **Compare**: `ParseVersion("vX.Y.Z") → ([3]int, bool)` (strip the `v`,
      exactly three numeric components — the VERSIONING.md guarantee makes
      pre-release grammar dead code); `Compare(a, b) → -1|0|+1`. A
      non-parseable *current* build (`dev`, `unknown`, `*-next` snapshots) is
      "not comparable": commands report `current dev (not comparable); latest
      vX.Y.Z` instead of a bogus verdict.
- [ ] **State file**: `Load/Save` of `update.json` — atomic (tmp+rename,
      mode `0600`), schema `{"consent":"ask|on|off","last_check":<RFC3339>,
      "latest":"vX.Y.Z"}`. Written fields: consent on ask/answer;
      last_check+latest on every successful network check. The daemon is the
      only background writer; `update check` / `update consent` write it in
      the foreground. Reuses the `PORTATO_STATE_HOME` seam.
- [ ] **One-time consent ask**: `maybeAskUpdateConsent()` in
      `runStandalone` (root.go:80) *after* `maybeOfferImport` (root.go:94) —
      the import offer owns the very first screen. Gate:
      `consent == "ask"` && `term.IsTerminal(stdin)`. Ask
      "check for updates in the background (GitHub, once a day)? [y/N]";
      either answer is final and persisted. The attach branch, every CLI
      command and the daemon never ask — a daemon-first bootstrap leaves the
      question pending for the next interactive launch (the Phase-48
      fresh/import marker pair behaviour).
- [ ] **Ask at `install`**: after a successful install (TTY only), the same
      question with the same persistence — install is the natural moment
      (the daemon it starts will do the polling).
- [ ] **`doctor`**: a `update` check line — `consent: off` →
      "checks off (`portato update consent on`)"; cache newer →
      "vX.Y.Z available (checked 2h ago)"; up to date → "up to date
      (checked 5h ago)"; never checked → the consent hint. When stdin is a
      TTY and consent is still `ask`, ask the same one-time question;
      otherwise just print the hint (doctor must stay scriptable).
- [ ] **Daemon background check**: only when consent is `on` — a ticker
      (1h) checks `last_check`; older than 24h → `Latest(ctx)`, write the
      cache. Failures log at debug and leave the cache untouched; no retry
      storm (the next attempt waits for the next tick + TTL). Consent
      flipped to `off` at runtime → the ticker observes the state file and
      goes idle (re-read on tick; no restart needed).
- [ ] **TUI header hint**: when the cached `latest` is newer than the
      running version, the header line gains a short `update: vX.Y.Z`
      segment (existing hint styling, theme-aware, one segment — no new
      rows). The TUI never performs network I/O; it reads the state file at
      launch. Hidden when up to date / not comparable / consent off.
- [ ] **Commands**: `portato update check` (explicit check, ignores consent
      and cache age, prints current / latest / release URL; exit 0 on
      "up to date" and on "available", non-zero only on error) and
      `portato update consent on|off|ask` (writes the state file, prints
      what changed; `ask` re-arms the one-time question).
- [ ] **Tests**: `ParseVersion`/`Compare` table (equal/major/minor/patch,
      `v`-prefix optional, garbage); `Latest` against `httptest` (200 parse,
      403 rate-limit, network refusal); state-file round-trip + atomic write
      + `PORTATO_STATE_HOME`; consent ask gating (non-TTY does not consume
      `ask`; y/n persist; daemon path never asks); daemon TTL logic (clock
      seam: fresh → checks, 23h-old → skips, 25h-old → checks); doctor line
      in all states; TUI header shows/hides per cache.
- [ ] **Docs**: SPEC §3 command list (`update check`, `update consent`) and
      the new SPEC §17 "Update check and self-update" (checker half; the
      apply half is marked Phase 50); README "Updating" section.

## Definition of Done

- [ ] `portato update check` against the real repo prints current vs latest
      (+ URL) and exits 0; with the network cut it fails cleanly non-zero;
      the fixture path through `SetBaseForTest` works with zero real network
      (test), and `NewClient` dials the compile-time `DefaultBase` when no
      seam is installed (test).
- [ ] A fresh state file + first interactive launch asks the consent
      question exactly once; `y` enables the daemon's daily check (proved by
      a fake-clock test), `n` never asks again and no background request
      ever happens; `update consent on|off|ask` round-trips.
- [ ] A daemon-first bootstrap does not consume the ask.
- [ ] `doctor` prints the update line in all three consent states.
- [ ] The TUI header shows the hint only when the cache holds a newer
      version; the TUI path performs no network I/O (test).
- [ ] `go.mod` require block unchanged (zero new dependencies).
- [ ] `make fmt && make vet && make test && make lint` clean;
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

Manual: remove `update.json` from the state dir → first TUI launch asks →
answer `y` → `doctor` reports the check; `update consent off` → the daemon
ticker goes idle (debug log).

## Technical details

- **Why `releases/latest` and not tag listing:** GitHub resolves it to the
  newest non-draft non-prerelease release — exactly the project's VERSIONING
  policy, for free. It 404s only before the first public release
  (v1.6.1 exists, so it won't).
- **Why hand-rolled compare:** `golang.org/x/mod/semver` is not in the module
  graph and pulls a module for what is, under the no-prerelease policy, an
  integer-triple comparison (~20 lines, fully table-tested).
- **Consent is `"ask"` initially**, not absent: the tri-state in one field
  keeps the file schema stable (`ask` = question pending; `off` = refused;
  `on` = opted in). Missing file ⇒ `ask` (fresh install); corrupt file ⇒
  treated as `ask` and rewritten on the next persist.
- **Ordering with the Phase-48 nudge:** import offer first (it edits the
  config the TUI then loads), consent ask second — both one-shot, both
  standalone-only; two independent markers mean no cross-coupling.
- **The TUI reads the state file at launch**, not an IPC call: works in
  standalone (no daemon) and stays consistent with "no new IPC methods".
- **Rate-limit handling:** a 403 with `X-RateLimit-Remaining: 0` is a
  temporary error — the cache keeps the last good `latest` and `last_check`
  stays fresh for TTL purposes only on success (a failed check does not
  reset the 24h clock, so a flaky network retries at most hourly, not
  per-request).
- **Not in scope:** downloading or applying anything (Phase 50), desktop
  notifications (the Post-1.0 hooks candidate), release channels
  (stable-only, per VERSIONING.md).
