---
phase: 21
title: Packaging and releases
status: todo
depends_on: [13]
---

## Goal

Distributable packages and a tag-triggered release pipeline: Homebrew,
Scoop, and deb/rpm, on top of the existing goreleaser snapshot config — so
users can `brew install`, `scoop install`, or install a Linux package, and a
`git tag vX.Y.Z` cut produces all of them automatically.

## Tasks

- [ ] Extend `.goreleaser.yaml` (snapshot exists from Phase 13) with:
      `brews:` (Homebrew tap formula), `scoops:` (Scoop manifest), and
      `nfpms:` (deb + rpm via nfpm).
- [ ] Wire the external tap/bucket repos (the maintainer provides them); use
      goreleaser's publish hooks to push the formula/manifest on release.
- [ ] CI release workflow (`.github/workflows/release.yml`): on `v*` tag →
      `goreleaser release`; publish the GitHub Release + the tap/bucket
      commits; surface the needed tokens as CI secrets.
- [ ] Extend `portato doctor`: check the binary is on PATH, the config dir is
      writable, autostart is in place (per OS), and report the embedded
      version/commit/date.
- [ ] Version embedding: wire `main.version` / `main.commit` / `main.date`
      ldflags into the release builds (snapshot builds already inject
      placeholders).
- [ ] README: an "Install" section — `brew`/`scoop`/deb/rpm + direct download
      from the GitHub Release.

## Definition of Done

- [ ] A tag push produces darwin/linux × amd64/arm64 archives + a Homebrew
      formula + a Scoop manifest + a deb + an rpm.
- [ ] On a clean machine: `brew install <tap>/portato` and
      `scoop install portato` succeed and produce a working `portato`.
- [ ] `portato doctor` exits 0 on a healthy install and prints the version.
- [ ] `goreleaser check` is clean; a `--snapshot --clean` build reproduces
      locally; the release workflow dry-runs green in CI.

## Verification

```sh
goreleaser check
goreleaser release --snapshot --clean      # builds all archives/packages locally
# a real release: tag, push, watch CI produce the GitHub Release + tap/bucket.
```

## Technical details

- Requires the repo to be public and the Homebrew tap / Scoop bucket repos to
  exist (goreleaser pushes to them).
- CI secrets: a GitHub PAT with push rights to the tap/bucket repos
  (`HOMEBREW_TAP_GITHUB_TOKEN`, `SCOOP_BUCKET_GITHUB_TOKEN`).
- nfpm covers deb and rpm from one config; keep packaging metadata
  (description, license `MIT`, homepage, maintainer) in sync with README.
- This phase is mostly config + CI; little Go code beyond `doctor` and
  ldflag wiring.
