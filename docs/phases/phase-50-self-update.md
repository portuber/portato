---
phase: 50
title: "Self-update (portato update apply: checksum-verified swap, package-manager etiquette)"
status: in-progress
depends_on: [49]
---

## Goal

One command updates a direct-download install end-to-end:

```
portato update apply            # download → verify → swap, with a y/N confirm
portato update apply --yes      # scripted consent (required in a non-TTY)
portato update apply --dry-run  # what would happen, nothing touched
```

For package-managed installs (brew / scoop / deb / rpm / apk / go install)
`apply` **refuses the in-place swap and prints the channel's own upgrade
command instead** — a package manager must never discover its files replaced
behind its back (`--force` overrides, loudly). The swap itself is
checksum-verified, atomic, and leaves a one-level rollback
(`portato.old`).

## Background

Built entirely on Phase 49's machinery (`internal/update.Latest`, the state
file, the in-repo `SetBaseForTest` seam) and Phase 21's release shape:

- Every release publishes archives named by the goreleaser template
  (`.goreleaser.yml:51`): `portato_<v>_<macOS|Windows|linux>_<x86_64|arm64>`
  — tar.gz (zip on Windows) — plus `checksums.txt` (SHA-256 per file).
- The maintainer chose **full in-place + PM etiquette** over hint-only: the
  risk budget is closed by SHA-256 verification, the one-level backup, and
  the PM refusal, and the privacy stance is preserved because `apply` is
  always an explicit user action (no auto-apply, no scheduled updates).
- **Windows carries three Phase-47 constraints:** a running `.exe` cannot be
  overwritten; the SCM service may hold the binary open at any moment; and a
  Scoop install's service path points through Scoop's stable `current`
  junction — replacing the versioned copy in place would desync it. So on
  Windows: Scoop → hint only; SCM-service installs → hint; direct-download →
  staged swap completed on the next launch (details below). If the Windows
  direct path proves hairier than planned, it is cut per CONVENTIONS §"If a
  phase is blocked" (a `## Blockers` note), not silently de-scoped.
- **Unix rename semantics make the live daemon safe:** `os.Rename` swaps the
  directory entry, the running daemon keeps its old inode until restart —
  no downtime, no corruption; `apply` just *tells* the user the daemon is
  still on the old version and how to restart it (`portato stop` + start,
  or a reboot for autostart).

## Tasks

- [ ] **Asset selection**: map `runtime.GOOS`/`runtime.GOARCH` → the
      goreleaser archive name (darwin→`macOS`, windows→`Windows`, else the
      GOOS; amd64→`x86_64`, arm64→`arm64`; `.zip` on Windows, `.tar.gz`
      elsewhere) and find it among `Release.Assets`; a missing asset is a
      clear error naming what was looked for.
- [ ] **Download + verify**: stream the asset to a temp file (size-capped,
      e.g. 100 MiB); fetch `checksums.txt`, find the
      `<sha256> <filename>` line, verify. Any mismatch → delete the temp
      file, abort with a scary message, exit non-zero, **the installed
      binary is untouched**. Verify *before* any staging near the install
      dir.
- [ ] **Extract**: pull the single `portato` member out of the tar.gz/zip
      (no full unpack), preserving the current binary's file mode (typically
      0755) rather than trusting archive bits.
- [ ] **Channel detection** (`internal/update/channel.go`): classify
      `os.Executable()` into `brew | scoop | deb | rpm | apk | goinstall |
      direct` — path heuristics (`/opt/homebrew/`, `/usr/local/Cellar/`,
      `~/scoop/`, `gopath/bin`) plus `dpkg-query`/`rpm -q`/`apk info`
      lookups where the tool exists. Each channel carries its upgrade hint
      (`brew upgrade --cask portuber/tap/portato`,
      `scoop update portato/portato`, `sudo apt upgrade portato`, …).
      Detection is advisory in `update check` (printed as a hint) and
      blocking in `apply` (any PM channel → refuse in-place; `--force`
      overrides after a loud warning).
- [ ] **Apply flow (unix)**: stage `portato.new` in the install dir (same
      filesystem) → `os.Rename(cur, cur+".old")` → `os.Rename(new, cur)` →
      print `vX → vY` + the backup path + the rollback command
      (`mv portato.old portato`). A previous `portato.old` is replaced
      (one level of rollback, not an archive). Failure at any step leaves
      the installed binary in place (the `.old`/`.new` intermediates are
      cleaned up best-effort).
- [ ] **Apply flow (Windows)**: Scoop / SCM-service installs → hint only
      (refuse regardless of `--force` for SCM — the service holds the file).
      Direct download → write `portato.new` next to the binary and complete
      the swap at the *next launch*: an early pre-cobra check (the
      Phase-47 precedent — SCM dispatch already runs before cobra,
      internal/cmd) sees `portato.new`, waits briefly if the current process
      is short-lived, and performs the rename dance on startup when the old
      file is not yet held. `apply` prints exactly this ("restart to
      finish the update").
- [ ] **Live-daemon hint**: after a successful swap, probe
      `daemon.ResolveSocket()` + `Healthz` (the doctor pattern,
      doctor.go:162) — if a daemon answers, print "the daemon still runs
      vX until restarted (`portato stop`, then start/reboot)".
- [ ] **`doctor`**: extend the Phase-49 update line with the detected
      channel — e.g. "v1.7.0 available · brew install (`apply` defers to
      the package manager)".
- [ ] **Tests**: asset-name mapping table; checksum mismatch → abort with
      the binary untouched; tar.gz and zip extraction (fixture archives);
      channel classification from path fixtures (+ the dpkg/rpm lookup
      behind a seam); full apply against an `httptest` "release" serving a
      fixture binary + checksums (via `update.SetBaseForTest`): old file
      swapped, `portato.old` exists, mode preserved, second run says "up to
      date"; PM channel → refusal text; `--yes` in a non-TTY; the daemon
      hint behind a probe seam.
- [ ] **Docs**: finish SPEC §17 (apply semantics, rollback, PM etiquette,
      Windows caveats); README "Updating" — the per-channel command table.

## Definition of Done

- [ ] On a macOS/Linux direct install: `apply` vX→vY swaps the binary
      (`--version` reports the new one), `portato.old` exists and the
      documented `mv` restores it; a second `apply` reports "up to date".
- [ ] A checksum mismatch aborts with the installed binary untouched
      (test-asserted).
- [ ] brew / scoop / deb / rpm / apk / go-install are detected and refused
      with the exact channel upgrade command; `--force` overrides (except
      Windows SCM).
- [ ] A non-TTY `apply` without `--yes` refuses.
- [ ] After a swap with a live daemon, the restart hint prints (probe-seam
      test).
- [ ] Windows: `GOOS=windows go build ./...` and the windows-tagged tests
      green; the scoop path → hint (test); direct-download + SCM behaviour
      either implemented and tested or recorded under `## Blockers`.
- [ ] `make fmt && make vet && make test && make lint` clean.

## Verification

```sh
make build
bin/portato update check            # shows latest + detected channel
bin/portato update apply --dry-run  # plan only
go test ./internal/update/... -v    # incl. the fixture-release apply E2E
```

Manual (the maintainer's machine): install a real older direct-download
release, `apply`, verify `--version`, rollback via `portato.old`, and on a
brew install confirm the refusal + `brew upgrade` hint.

## Technical details

- **Checksums over API digests:** `checksums.txt` is a release artefact
  with one stable format; the GitHub asset `digest` field is API-versioned
  and has flipped formats before. Parsing `<sha256>  <name>` is boring and
  stays boring.
- **Same-filesystem staging:** `portato.new` is created in the install
  directory itself, so the final `os.Rename` cannot hit an EXDEV — a
  `/tmp`-staged binary would.
- **Why not `MOVEFILE_DELAY_UNTIL_REBOOT` on Windows:** it needs privileges
  and defers to a reboot nobody wants to schedule; the next-launch swap
  needs neither and matches how Scoop itself stages updates.
- **`--force` scope:** overrides the PM refusal *only* — it never skips the
  checksum verification, and it never touches a Windows SCM-held binary.
- **go-install detection is a heuristic** (path under `gopath/bin`): the
  hint is `go install github.com/portuber/portato/cmd/portato@latest`; a
  wrong guess costs a hint line, nothing more.
- **Not in scope:** auto-apply or scheduled updates (explicit runs only —
  the Phase-49 consent gates *checking*, applying is always a deliberate
  command), signature verification beyond SHA-256 (releases are not GPG-
  signed today), delta patches, updating anything but the single binary.
