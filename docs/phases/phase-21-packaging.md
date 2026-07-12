---
phase: 21
title: Packaging and releases
status: done
depends_on: [13]
---

## Goal

Distribute portato through four complementary channels — pre-built binaries on
GitHub Releases (primary), `go install` from source, Homebrew (macOS), and
Scoop/deb/rpm — driven by one goreleaser config, so a `git tag vX.Y.Z` cut
publishes all of them automatically. Also add the project LICENSE (MIT) as the
publishing prerequisite.

## Tasks

- [x] Extend `.goreleaser.yaml` (snapshot exists from Phase 13) with the
      packaging sections. NOTE: `brews` is hard-deprecated in goreleaser
      v2.16+ (`goreleaser check` fails), so the Homebrew channel uses
      `homebrew_casks` (a Cask, not a Formula); `nfpms:` adds deb + rpm.
      `scoops:` (Scoop) is **deferred to phase 17** — the windows build does
      not compile (`syscall.Kill` in discovery.go/stop.go, `Setsid` in
      handoff.go) and would not run (unix-socket IPC, fd-passing).
- [x] Wire the external tap repo: the `homebrew_casks.repository`
      (owner/name/branch/token) + `HOMEBREW_TAP_GITHUB_TOKEN` push the cask to
      portuber/homebrew-tap on release. (The Scoop bucket is deferred to
      phase 17 with windows.)
- [x] CI release workflow (`.github/workflows/release.yml`): on `v*` tag →
      `goreleaser release`; publishes the GitHub Release + the tap cask commit;
      surfaces `GITHUB_TOKEN` and `HOMEBREW_TAP_GITHUB_TOKEN` as CI secrets.
- [x] Extend `portato doctor`: check the binary is on PATH, the config dir is
      writable, autostart is in place (per OS), and report the embedded
      version/commit/date.
- [x] Version embedding: wire `main.version` / `main.commit` / `main.date`
      ldflags into the release builds (snapshot builds already inject
      placeholders).
- [x] Add a `LICENSE` file (**MIT**) at the repo root — publishing prerequisite.
      All dependencies are permissive (MIT / Apache-2.0 / BSD), there is no
      copyleft and therefore no conflict; the only obligation is to retain the
      deps' notices on redistribution (the same for any permissive choice). That
      retention is implemented for binary releases in Phase 32 — a bundled
      `THIRD_PARTY_LICENSES.txt` per archive + deb/rpm (see
      `docs/phases/phase-32-third-party-licenses.md`).
- [x] README: an "Install" section listing the channels —
      `go install github.com/portuber/portato/cmd/portato@latest` (note: requires
      Go 1.25+), direct download from the GitHub Release,
      `brew install --cask portuber/tap/portato`, and deb/rpm. (Scoop is
      deferred to phase 17.)

## Definition of Done

- [x] A tag push produces darwin/linux × amd64/arm64 archives + a Homebrew
      **cask** + a deb + an rpm. (goreleaser v2.16 hard-deprecated `brews`/
      formulae, so the Homebrew channel ships a Cask; Scoop is deferred to
      phase 17 with windows.)
- [x] On a clean machine: `brew install --cask portuber/tap/portato` succeeds
      and produces a working `portato` (verified on macOS). (Scoop deferred to
      phase 17.)
- [x] `portato doctor` exits 0 on a healthy install and prints the version.
- [x] `goreleaser check` is clean; a `--snapshot --clean` build reproduces
      locally; the release workflow ran green in CI and published v0.1.0.

## Verification

```sh
goreleaser check
goreleaser release --snapshot --clean      # builds all archives/packages locally
# a real release: tag, push, watch CI produce the GitHub Release + tap/bucket.
```

## Technical details

- **Distribution channels (complementary, not either/or):** GitHub Releases
  (no Go needed, primary) · `go install` (Go users/CI; needs Go 1.25+) ·
  Homebrew (macOS UX) · Scoop (Windows, after Phase 17) · deb/rpm (Linux). All
  unblock with one step — a **public git remote + the first `vX.Y.Z` tag**: the
  Go module proxy serves `go install`, goreleaser publishes the rest.
- **License:** MIT — permissive; every dependency is MIT / Apache-2.0 / BSD (no
  copyleft), so there is no conflict and no obligation beyond retaining the
  deps' notices on redistribution (the same for any permissive choice). MIT
  needs no source-header comments and no NOTICE file; a single `LICENSE` at the
  repo root (plus the license line in `go.mod`/README) is sufficient.
- Requires the repo to be public and the Homebrew tap / Scoop bucket repos to
  exist (goreleaser pushes to them).
- CI secrets: a GitHub PAT with push rights to the tap/bucket repos
  (`HOMEBREW_TAP_GITHUB_TOKEN`, `SCOOP_BUCKET_GITHUB_TOKEN`).
- nfpm covers deb and rpm from one config; keep packaging metadata
  (description, license `MIT`, homepage, maintainer) in sync with README.
- This phase is mostly config + CI; little Go code beyond `doctor` and
  ldflag wiring.
