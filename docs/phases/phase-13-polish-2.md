---
phase: 13
title: Polish 2 (deferred phase-11): log rotation, list filter, release tooling
status: todo
depends_on: [11]
---

> Post-MVP polish. The three items explicitly deferred from Phase 11's DoD
> (phase-11-polish.md §Design decisions): persistent log rotation, the `/`
> tunnel-list filter, and goreleaser release tooling.

## Goal

Three deferred conveniences: (1) logs persist to a rotated file so they survive
restarts and can be read outside the TUI; (2) `/` opens a substring filter over
the tunnel list; (3) `goreleaser` produces versioned cross-platform release
artifacts from the matrix already used by `make build-all`.

## Background

Phase 11 deliberately scoped its baseline DoD to the in-memory ring buffer,
theme detection, the TOFU prompt, `portato doctor`, CI, and docs. Three
candidate features were recorded as deferred (not DoD): *log rotation*, *the
`/` list filter*, and *`goreleaser`*. This phase picks them up. Each is
independent and may land in its own commit.

## Tasks

- [ ] Log rotation
  - [ ] `internal/log/file.go`: a rotating file `slog.Handler` writing to
        `xdg.StateHome/portato/portato.log`, rotating by size and keeping N
        archives (e.g. `portato.log`, `portato.log.1`, …).
  - [ ] Rotation knobs: max size (default ~1 MiB), keep (default 3). Constants
        for now; config later if needed.
  - [ ] Daemon wires the file handler alongside the ring handler so persisted
        logs are identical to what the TUI shows, in both standalone and attach.
  - [ ] `portato doctor` reports the log path and the last rotation.
- [ ] List filter (`/`)
  - [ ] `Model.filter` (string) + `Model.filtering` (bool); `/` opens the input,
        `esc` clears, live-filter as you type.
  - [ ] `table()` narrows `m.list` by a case-insensitive substring over
        name / endpoint / type.
  - [ ] Render a one-line filter input (footer area) showing the query and a
        matched/total count (e.g. `3/12`).
  - [ ] Pure view-state: works identically in standalone and `attach` (filter is
        applied client-side over the status list; no IPC change).
- [ ] goreleaser
  - [ ] `.goreleaser.yml`: build the darwin/linux × amd64/arm64 matrix already
        used by `make build-all`, with archives + checksums and a changelog.
  - [ ] `make snapshot` target → `goreleaser release --snapshot --clean`.
  - [ ] README: a short "Releases / install from tarball" note.
- [ ] Docs: note in phase-11-polish.md that these items moved to phase 13; SPEC
      §logs/hotkeys updated to the new behaviour.

## Definition of Done

- [ ] Logs are written to a file under the state dir; rotation triggers at the
      size cap and keeps the configured number of archives; recent lines are
      still queryable from the TUI's in-memory ring.
- [ ] `/foo` narrows the list to matching tunnels; `esc` restores the full list;
      the filter survives a redraw tick and works in both standalone and
      `attach`.
- [ ] `goreleaser release --snapshot --clean` builds all four targets and writes
      archives + checksums to `dist/`.
- [ ] `go build ./...`, `gofmt -l .`, `go vet ./...`, `go test ./...` are clean.
- [ ] phase-11-polish.md is annotated that these items moved here; SPEC updated.

## Verification

1. Start the daemon, enable a tunnel; `cat "$XDG_STATE_HOME/portato/portato.log"`
   shows entries; force a rotation (exceed the size cap) → an archived
   `portato.log.1` appears and the current log resets.
2. With ≥3 tunnels, `/db` shows only matches; `esc` restores the full list; in a
   second `attach` session the filter works too.
3. `make snapshot` → `dist/` contains the four archives and `checksums.txt`.

## Technical details

- File handler: reuse `internal/log` setup. The rotator can be hand-rolled
  (write → size check → rename chain) to avoid a new dependency; a
  `gopkg.in/natefinch/lumberjack.v2` dependency is the alternative. The write
  goes through the same `slog` pipeline as the ring, so both see identical
  records.
- Filter: keep it a substring match first; fuzzy (`fzf`-style ranking) is a later
  option. The filter lives entirely on `Model`, so the controller/IPC are
  untouched.
- goreleaser: mirror `make build-all`'s `GOOS`/`GOARCH` list; `-ldflags` injects
  the version from git tags. Homebrew/scoop/deb-rpm are out of scope for now.

## Open questions

- Rotation: hand-rolled vs a dependency (`lumberjack`)? (lean: hand-rolled.)
- Filter: live-filter-as-you-type vs `enter` to apply? (lean: live.)
- goreleaser: also publish a Homebrew tap / scoop now or later? (lean: later —
  archives + checksums only for now.)
