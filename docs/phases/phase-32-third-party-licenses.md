---
phase: 32
title: Third-party license notices in binary releases
status: done
depends_on: [21]
---

## Goal

Bundle the license texts of Portato's runtime dependencies (MIT / Apache-2.0 /
BSD-3) into every binary release artifact — the GitHub Release tar.gz archives
and the deb/rpm packages — so redistribution of compiled binaries carries the
dependency notices as required by those licenses (most explicitly BSD-3-Clause's
"Redistributions in binary form must reproduce the above copyright notice… in
the documentation and/or other materials provided with the distribution"). The
Homebrew cask needs no separate handling: it downloads the archive from the
GitHub Release, so the notices ride along.

## Background

Phase 21 (packaging) already states the obligation in passing —
`docs/phases/phase-21-packaging.md:39-41`: *"All dependencies are permissive
(MIT / Apache-2.0 / BSD), there is no copyleft and therefore no conflict; the
only obligation is to retain the deps' notices on redistribution (the same for
any permissive choice)."* — but the release tooling never actually implemented
that retention:

- `.goreleaser.yml` `archives:` had no `files:` adding a notices file/tree.
- `.goreleaser.yml` `nfpms.contents` installed only Portato's own `LICENSE` to
  `/usr/share/doc/portato/LICENSE`; the dependencies' notices were absent.
- There was no `before.hooks` block.

So the already-published v0.1.x artifacts shipped without the dependency license
texts — a real (if rarely enforced) compliance gap, and the decisive hook is
unambiguous: `golang.org/x/{crypto,sys,term}` and `github.com/spf13/pflag` are
BSD-3-Clause, which explicitly covers "binary form".

"Permissive / no copyleft" (accurate for the dep set — there is no GPL/AGPL/LGPL
anywhere in the tree) is **not** the same as "obligation-free": permissive
licenses still require notice retention on redistribution of binaries. Nothing
here requires crediting or "thanking" authors; BSD-3 in fact prohibits using
contributors' names to promote the product without permission. The obligation is
to carry license **text**, which is exactly what this phase adds.

## Design decisions (locked, then revised — see "Deviation from plan")

| Aspect | Decision |
|---|---|
| Generation timing | Generate the notices **at release time**, not committed to the repo. Stays in sync with `go.mod`; no drift, no repo bloat. |
| Tool | `github.com/google/go-licenses` (Apache-2.0 build tool; not linked into the binary, imposes nothing on it). |
| Placement | A `before.hooks: make third-party-licenses` step in `.goreleaser.yml` — runs for both `release` and local `snapshot`; the generation logic lives in the Makefile. |
| Output layout | A single concatenated `THIRD_PARTY_LICENSES.txt` — one block per dependency: its module path as a header, then the license text. Produced by the Makefile target (`go-licenses save` → concat → drop the tree). A per-module tree was the original plan but collides inside deb/rpm (see below). |
| Archives | Add `THIRD_PARTY_LICENSES.txt` to `archives[].files`; re-list `LICENSE` + `README.md` because setting `files:` overrides goreleaser's defaults. |
| deb/rpm | Add `src: THIRD_PARTY_LICENSES.txt` → `dst: /usr/share/doc/portato/THIRD_PARTY_LICENSES.txt` to `nfpms.contents`, alongside the existing own-LICENSE install. |
| Homebrew cask | Untouched — downloads the archive, notices ride along. |
| Binary itself | No embedded licenses; no Go code change. |
| Phase 21 | Stays `[x]`. This phase only implements what phase-21:39-41 declares; a cross-link note is added to the phase-21 doc, its status is not reopened (same pattern phase-31 used w.r.t. phase-24). |

## Tasks

### `Makefile`
- [x] Add a `third-party-licenses` target: `go install github.com/google/go-licenses@latest`,
      `go-licenses save ./cmd/portato --save_path third_party --force`, then
      concatenate every file under `third_party/` (module-path header + text)
      into `THIRD_PARTY_LICENSES.txt` and `rm -rf third_party`.

### `.goreleaser.yml`
- [x] Add a top-level `before.hooks` with a single step
      `make third-party-licenses` (replaces an earlier two-step inline form).
- [x] `archives[].files`: set explicitly to
      `['LICENSE', 'README.md', 'THIRD_PARTY_LICENSES.txt']` (re-list the
      goreleaser defaults we rely on, since `files:` overrides them).
- [x] `nfpms.contents`: add
      `{ src: THIRD_PARTY_LICENSES.txt, dst: /usr/share/doc/portato/THIRD_PARTY_LICENSES.txt }`
      next to the existing own-LICENSE entry.

### Docs
- [x] `docs/phases/phase-21-packaging.md`: add a cross-link note pointing to
      phase 32 as the implementation of the "retain notices on redistribution"
      line (phase 21 status unchanged).
- [x] `docs/SPEC.md`: checked — it does not enumerate release/archive contents,
      so no SPEC change is needed.

## Definition of Done

- [x] `goreleaser release --snapshot --clean` produces tar.gz archives that
      contain `THIRD_PARTY_LICENSES.txt` (verified across all 4 darwin/linux ×
      amd64/arm64 archives), and that file holds every runtime dependency's
      license text (spot-check: cobra = Apache-2.0, golang.org/x/crypto =
      BSD-3-Clause, charm.land/bubbletea = MIT; yaml.v3 also carries its Apache
      NOTICE).
- [x] The deb's `data.tar.gz` lists
      `/usr/share/doc/portato/THIRD_PARTY_LICENSES.txt`; the rpm uses the same
      `nfpms.contents` (shared config, build succeeded without collision).
- [x] `goreleaser check` is clean.
- [x] `go build ./...`, `go vet ./...`, `gofmt -l .` unchanged — no Go code
      changed in this phase. (`make test` is green under CI's env; a pre-existing
      `tui` test-isolation flake surfaced locally only because the dev shell
      exports `PORTATO_THEME=mono` — unrelated to this phase, to be fixed
      separately.)
- [x] ROADMAP + phase-21 cross-link updated; SPEC packaging section reconciled
      (no change needed).

## Verification

```sh
make third-party-licenses                 # regenerates THIRD_PARTY_LICENSES.txt
goreleaser check
goreleaser release --snapshot --clean
tar -tzf dist/*linux_x86_64*.tar.gz       # lists LICENSE, README.md, portato, THIRD_PARTY_LICENSES.txt
# inspect a deb without dpkg-deb (macOS):
ar x dist/*linux_amd64*.deb && tar -tzf data.tar.gz | grep THIRD_PARTY   # /usr/share/doc/portato/THIRD_PARTY_LICENSES.txt
```

## Deviation from plan (during implementation)

The plan locked a per-module `third_party/` tree packed into archives and
expanded into deb/rpm via `nfpms.contents: { src: third_party/, dst:
.../third_party/ }`. That failed in practice:

```
⨯ nfpm failed … add globbed files from "third_party/": adding at destination
  /usr/share/doc/portato/third_party/LICENSE: file with source
  third_party/github.com/charmbracelet/colorprofile/LICENSE is already present
  at this destination: content collision
```

nfpm **flattens** a directory/glob `src` to basenames under `dst`, and the dep
tree has many files all named `LICENSE` (or `LICENSE.txt`), so they collide.
(The tar.gz archives handled the tree fine — the collision is deb/rpm only.)

Resolution: generate one concatenated `THIRD_PARTY_LICENSES.txt` (module-path
header + license text per block) and pack that single file into both archives
and deb/rpm. This is deterministic, avoids the collision entirely, is uniform
across all three artifact kinds, and is the conventional shape for a bundled
notices file. The Makefile target owns the generation; goreleaser calls it via
`before.hooks: make third-party-licenses`. `go-licenses` itself works on Go 1.26
(verified: 33 module notices; its warnings about non-Go `.s` files are
informational).

## Technical details / risks

- **go-licenses on Go 1.26.** Verified working at phase start
  (`go-licenses save ./cmd/portato --save_path /tmp/tp --force`, exit 0, 33
  notices). Fallback if a future Go release breaks it: pin
  `go-licenses@<known-good commit>`, or collect LICENSE files manually from the
  module cache via `go list -m all` + `$(go env GOMODCACHE)/<module>@<ver>/LICENSE`.
- **`before.hooks` ordering.** goreleaser runs `before.hooks` once at pipeline
  start, before builds/archives/nfpm assemble — so `THIRD_PARTY_LICENSES.txt`
  exists by packaging time. `setup-go` + the build populate the module cache
  that `go-licenses` reads.
- **Network in CI.** `go install go-licenses` needs the module proxy; GitHub
  Actions runners reach it by default.
- **Retroactive.** v0.1.x is already published without notices; this fixes the
  **next** release onward.
- **Pre-existing test-isolation flake (not this phase).** `internal/tui`
  `TestDetectKind` does not unset `PORTATO_THEME` for its COLORFGBG/default
  cases, so a dev shell exporting `PORTATO_THEME=mono` makes them fail. CI is
  unaffected (clean env). To be fixed in a separate `fix(tui):` commit.
- **Why a new phase, not a reopen of 21.** Phase 21 is `[x]`; CONVENTIONS
  forbids silently reopening a completed phase. Tracked as phase 32
  (`depends_on: [21]`); the phase-21 doc gets only a cross-link, status
  unchanged (mirrors phase-31 ↔ phase-24).

## Commit plan (per CONVENTIONS)

1. `docs(phase-32): plan` — create this file + the ROADMAP row (`[ ]`). ✅
2. `docs(phase-32): start` — flip the frontmatter + ROADMAP row `[ ] -> [~]`. ✅
3. `feat(build): bundle third-party license notices into releases` —
   `Makefile` + `.goreleaser.yml` + this phase file's deviation note +
   phase-21 cross-link. ✅
4. `docs(phase-32): complete` — `[~] -> [x]` after the DoD passes. ✅

## Start guard

This phase is `status: done`. It was completed by an explicit
**"complete phase 32"** command (per `docs/CONVENTIONS.md`) after every DoD
item was verified. A pre-existing `tui` test-isolation flake surfaced during
the phase (unrelated ambient `PORTATO_THEME` leaking into `TestDetectKind`)
was fixed separately in `e18b0c5` (`fix(tui): make TestDetectKind hermetic to
PORTATO_THEME`).
