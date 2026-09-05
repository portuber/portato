---
phase: 51
title: "TUI header version segment"
status: todo
depends_on: []
---

## Goal

The TUI header shows the running binary's version next to `mode:` —
`mode: attach  v1.8.1` — so both halves of the update story are visible in
one line: what you run, and (when the phase-49 cache knows better) what's
available. A user answering "what version are you on?" needs a screenshot
of the header, not a quit-and-run `--version`.

## Background

Promoted from the Post-1.0 candidate list (the v1.8.x verification-session
follow-up). The plumbing already exists: Phase 49 wired
`tui.Options.Version` through all three `tui.Run` call sites (used only to
derive the cached update hint), so this phase is rendering plus one
responsive rule. Precedent: Phase 39 dropped the socket path from the
header as noise — a 60+ char temp path; a stable ~7-char version segment
is a different class. TUI header content is TUI internals per VERSIONING
(free to change), so the release is a PATCH.

Design locked with the maintainer:

- The version renders through `ParseVersion → String()` for the canonical
  `v`-prefixed form, matching the update hint's spelling; a non-parseable
  build (`dev`) renders the raw string — honest, not hidden.
- Phase-38-style responsive rule: on narrow widths the update hint
  shortens (`update: v1.9.0` → `→ v1.9.0`) so the header never wraps; the
  version segment itself never shortens or hides.

## Tasks

- [ ] `internal/tui/view.go` `header()`: render the version segment (style
      `pal.mode`, the mode segment's sibling) between `mode:` and the
      update hint; add the width-driven hint shortening.
- [ ] Tests: the segment always renders (release build); a `dev` build
      renders `dev` verbatim; the hint pairs as the sibling segment; at a
      narrow width the shortened hint keeps the header to one line.
- [ ] SPEC §11: one line describing the header composition
      (mode + version + update hint).

## Definition of Done

- [ ] The header renders `mode: <mode>  v<version>` on every launch
      (release and `dev` builds), styled like the mode segment.
- [ ] With a cached newer release the hint follows as the sibling segment;
      at narrow widths it renders `→ v1.9.0` and the header stays a single
      line (test-asserted at 60 cols).
- [ ] `make fmt && make vet && make test && make lint` clean;
      `GOOS=windows go build ./...` clean.

## Verification

```sh
go test ./internal/tui/... -run 'TestHeader|TestUpdateHint' -v
make build && bin/portato attach   # header shows "mode: attach|standalone  v…"
```

## Technical details

- The version string arrives as `tui.Options.Version` (goreleaser embeds
  `1.8.1` without the `v`; `ParseVersion → String()` normalises both
  sides to the `v` form so the hint and the segment agree).
- Width budget: title ≈ 28 cols (emoji header) + `mode: attach` (12) +
  `v1.8.1` (6) fits 60; adding `update: v1.9.0` (14) overflows → the hint
  shortens to `→ v1.9.0` (9) below the measured threshold.
- Not in scope: the version anywhere else (footer, help overlay, logs) —
  `--version`, `doctor` and the help overlay already cover those.
