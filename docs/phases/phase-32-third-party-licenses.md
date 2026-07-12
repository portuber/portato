---
phase: 32
title: Third-party license notices in binary releases
status: in-progress
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

- `.goreleaser.yml` `archives:` has no `files:` adding a notices file/tree.
- `.goreleaser.yml` `nfpms.contents` installs only Portato's own `LICENSE` to
  `/usr/share/doc/portato/LICENSE`; the dependencies' notices are absent.
- There is no `before.hooks` block.

So the already-published v0.1.x artifacts ship without the dependency license
texts — a real (if rarely enforced) compliance gap, and the decisive hook is
unambiguous: `golang.org/x/{crypto,sys,term}` and `github.com/spf13/pflag` are
BSD-3-Clause, which explicitly covers "binary form".

"Permissive / no copyleft" (accurate for the dep set — there is no GPL/AGPL/LGPL
anywhere in the tree) is **not** the same as "obligation-free": permissive
licenses still require notice retention on redistribution of binaries. Nothing
here requires crediting or "thanking" authors; BSD-3 in fact prohibits using
contributors' names to promote the product without permission. The obligation is
to carry license **text**, which is exactly what this phase adds.

## Design decisions (locked)

| Aspect | Decision |
|---|---|
| Generation timing | Generate the notices **at release time**, not committed to the repo. Stays in sync with `go.mod`; no drift, no repo bloat. |
| Tool | `github.com/google/go-licenses` (Apache-2.0 build tool; not linked into the binary, imposes nothing on it). |
| Placement | `before.hooks` in `.goreleaser.yml` — runs for both `release` and local `snapshot`, keeps everything in one config (no extra `release.yml` step). |
| Output layout | A `third_party/` tree mirroring module paths, one `LICENSE` per dependency (`go-licenses save ./cmd/portato --save_path third_party`). Self-describing and standard. |
| Archives | Add `third_party/**/*` to `archives[].files`; re-list `LICENSE` + `README.md` because setting `files:` overrides goreleaser's defaults. |
| deb/rpm | Add `src: third_party/` → `dst: /usr/share/doc/portato/third_party/` to `nfpms.contents` (nfpm expands a directory `src` recursively), alongside the existing own-LICENSE install. |
| Homebrew cask | Untouched — downloads the archive, notices ride along. |
| Binary itself | No embedded licenses; no Go code change (except an optional Makefile target). |
| Phase 21 | Stays `[x]`. This phase only implements what phase-21:39-41 declares; a cross-link note is added to the phase-21 doc, its status is not reopened (same pattern phase-31 used w.r.t. phase-24). |

## Tasks

### `.goreleaser.yml`
- [ ] Add a top-level `before.hooks` block:
      - `go install github.com/google/go-licenses@latest`
      - `go-licenses save ./cmd/portato --save_path third_party --force`
- [ ] `archives[].files`: set explicitly to `['LICENSE', 'README.md', 'third_party/**/*']`
      (overriding the default; re-list LICENSE/README so they remain packed).
- [ ] `nfpms.contents`: add
      `{ src: third_party/, dst: /usr/share/doc/portato/third_party/ }`
      next to the existing `{ src: ./LICENSE, dst: /usr/share/doc/portato/LICENSE }`.

### Docs
- [ ] `docs/phases/phase-21-packaging.md`: add a cross-link note pointing to
      phase 32 as the implementation of the "retain notices on redistribution"
      line (phase 21 status unchanged).
- [ ] Check `docs/SPEC.md`'s packaging/release section; if it enumerates release
      contents, note the bundled `third_party/` there too.

### Optional
- [ ] `Makefile`: a `licenses` target
      (`go-licenses save ./cmd/portato --save_path third_party --force`) for local review.
- [ ] README "License" section: optionally a one-liner pointing readers to the
      bundled `third_party/` in release artifacts.

## Definition of Done

- [ ] `goreleaser release --snapshot --clean` produces tar.gz archives whose
      contents include a `third_party/` tree with each runtime dependency's
      LICENSE (spot-check: cobra = Apache-2.0, golang.org/x/crypto = BSD-3-Clause,
      charm.land/bubbletea = MIT).
- [ ] `dpkg-deb -c dist/*.deb` lists files under
      `/usr/share/doc/portato/third_party/...`; the rpm likewise carries them.
- [ ] `goreleaser check` is clean.
- [ ] `go build ./...`, `make vet`, `make test`, `gofmt -l .` unchanged (no Go
      code change expected).
- [ ] ROADMAP + phase-21 cross-link updated; SPEC packaging section reconciled
      if needed.

## Verification

```sh
make fmt && make vet && make test
goreleaser check
goreleaser release --snapshot --clean
tar -tzf dist/*linux_amd64*.tar.gz | grep '^third_party/' | head      # tree present
tar -tzf dist/*linux_amd64*.tar.gz | grep -E 'x/crypto|cobra|bubbletea'  # control deps present
dpkg-deb -c dist/*linux_amd64*.deb | grep third_party                 # deb carries it
```

## Technical details / risks

- **go-licenses vs Go 1.26.** `go-licenses` is Google-maintained and
  occasionally lags a Go release. At phase start, verify it runs on Go 1.26
  (`go-licenses save ./cmd/portato --save_path /tmp/tp --force`). Fallback if it
  breaks: pin `go-licenses@<known-good commit>`, or collect LICENSE files
  manually from the module cache via `go list -m all` +
  `$(go env GOMODCACHE)/<module>@<ver>/LICENSE`.
- **`before.hooks` ordering.** goreleaser runs `before.hooks` once at pipeline
  start, before builds/archives/nfpm assemble — so `third_party/` exists by
  packaging time. `setup-go` + the build itself populate the module cache that
  `go-licenses` reads.
- **Network in CI.** `go install go-licenses` needs the module proxy; GitHub
  Actions runners reach it by default.
- **Retroactive.** v0.1.x is already published without notices; this fixes the
  **next** release onward.
- **Why a new phase, not a reopen of 21.** Phase 21 is `[x]`; CONVENTIONS
  forbids silently reopening a completed phase. Tracked as phase 32
  (`depends_on: [21]`); the phase-21 doc gets only a cross-link, status
  unchanged (mirrors phase-31 ↔ phase-24).

## Commit plan (per CONVENTIONS)

1. `docs(phase-32): plan` — create this file + the ROADMAP row (`[ ]`) (+ the
   phase-21 cross-link can ride here or in the start commit). This is the
   planning commit; may be made at plan time.
2. On "start phase 32": `docs(phase-32): start` — flip the frontmatter + ROADMAP
   row `[ ] -> [~]`.
3. `feat(build): bundle third-party license notices into releases` —
   `.goreleaser.yml` (+ optional Makefile).
4. `docs(phase-32): complete` — `[~] -> [x]` after the DoD passes.

## Start guard

This phase is `status: todo`. It starts only on an explicit **"start phase 32"**
command (per `docs/CONVENTIONS.md`). The first action then is to flip the
frontmatter + ROADMAP row `[ ] -> [~]` (commit `docs(phase-32): start`) — not
before.
