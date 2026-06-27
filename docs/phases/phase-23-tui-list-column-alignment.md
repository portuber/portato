---
phase: 23
title: TUI list column alignment (dynamic NAME width)
status: in-progress
depends_on: [15]
---

> Bug fix. Long tunnel names (e.g. `pntr-sberhealth-browser`, 24 cells) overflow
> the fixed 20-cell NAME column and push TYPE / ENDPOINT / STATUS / UPTIME to the
> right on their rows, breaking column alignment across the list. This phase
> makes the NAME column width dynamic: it grows to fit the longest name (clamped,
> and capped by the terminal budget), so columns line up regardless of name
> length.

## Goal

The tunnel list's NAME column adapts to its contents and to the terminal width.
On a wide terminal with long names, the names are shown in full (no truncation)
and every other column (TYPE / ENDPOINT / STATUS / UPTIME) stays aligned across
all rows. On a narrow terminal, NAME shrinks and long names are middle-truncated
(`prefix…suffix`), preserving the prefix so grouped tunnels (`pntr-*`, `tv-*`)
stay identifiable. Columns never drift again.

## Background

`internal/tui/view.go` renders the list by hand (no `lipgloss.Table`): each cell
is run through `pad(s, n)`, which pads to a fixed width `n` but, on overflow,
returns the full string plus one space (`view.go:380-386`). ENDPOINT is
protected by a dedicated `fitEndpoint()` middle-truncator (`view.go:401`), but
**NAME has no fitter** — it is passed raw into `pad(name, colName)` at
`view.go:220`, so any name longer than `colName` (20) shifts that row's trailing
columns. This is exactly what the screenshot shows.

Fixed widths are `colName=20`, `colType=7`, `colEndpoint=48`, `colStatus=14`
(`view.go:15-22`). `columnHeader()` (`view.go:178`) and `Model.row()`
(`view.go:189`) build the header/rows; both are called only from
`Model.table()` (`view.go:144`), so signature changes are local to `view.go`.
There are no external callers of `columnHeader`/`row` (verified across the
package and its tests).

## Approach

Dynamic NAME width (option "B3" from the design discussion):

- The NAME column width (`nameW`) is computed once per render in `table()` from
  the **longest name across all tunnels in `m.list`** (not only the visible /
  filtered set), so the column does not jump while the `/` filter narrows the
  view.
- `nameW` is clamped to `[minName, maxName]` and further capped by the
  terminal's available width so the row still fits; ENDPOINT stays fixed at
  `colEndpoint` (it already self-truncates via `fitEndpoint`).
- When `m.width == 0` (before the first `WindowSizeMsg`, and in unit tests),
  `nameW` falls back to `colName` (20) — the layout is identical to today's.
- Even at the computed `nameW`, individual names are still passed through a new
  `fitName` (a thin wrapper over the existing `middleTruncate`, in the spirit of
  `fitEndpoint`) as a defensive guard, so a name can never exceed its cell.

Truncation style: **middle** (`pntr-sberh…wser`), consistent with
`fitEndpoint` / `middleTruncate`.

## Tasks

- [x] `view.go` — add constants `minName = 12`, `maxName = 40`,
      `uptimeBudget = 7` next to the existing column constants; keep `colName`
      as the `width==0` fallback.
- [x] `view.go` — add `func (m Model) nameWidth() int`: iterate **all** of
      `m.list`, take the longest `lipgloss.Width(s.Name)`, clamp to
      `[minName, maxName]`, then (when `m.width > 0`) cap by the terminal
      budget
      `avail = m.width - sideMargin - 4 - 4*len(gutter) - colType - colEndpoint - colStatus - uptimeBudget`
      (`4` = cursor + sp + indicator + sp); return `colName` when `m.width == 0`.
- [x] `view.go` — add `func fitName(s string, max int) string` returning
      `middleTruncate(s, max)` (place near `fitEndpoint`, ~line 414).
- [x] `view.go` — change `columnHeader()` → `columnHeader(nameW int)`; use
      `pad("NAME", nameW)`.
- [x] `view.go` — change `Model.row(i, s)` → `Model.row(i, s, nameW int)`;
      compute `name := fitName(s.Name, nameW)` and pad with `pad(name, nameW)`.
- [x] `view.go` — in `table()`: `nameW := m.nameWidth()`; pass to
      `columnHeader(nameW)` and `m.row(i, m.list[i], nameW)`.
- [x] `model_test.go` — add `TestFitName` (short name unchanged; long name
      middle-truncated to exactly `n` cells).
- [x] `model_test.go` — add `TestNameWidth`:
      `width==0` → `colName`; wide terminal → `min(longest, maxName)`; names
      longer than `maxName` → `maxName`; all names short → `minName`; narrow
      terminal → capped by `avail` but not below `minName`.
- [x] `model_test.go` — add a regression test for column alignment: with a mix
      of short and long names and a wide `m.width`, render `View()` and assert
      that the TYPE column starts at the same display column on every row
      (measure with `lipgloss.Width` on the segment before `TYPE`).
- [x] `ROADMAP.md` — add the phase-23 row to the post-MVP table, status `[ ]`.

## Definition of Done

- [x] With long tunnel names present, every row's TYPE / ENDPOINT / STATUS /
      UPTIME columns line up with the header and with each other (no rightward
      drift on long-name rows).
- [x] On a wide terminal, names up to `maxName` (40) cells are shown in full
      (no ellipsis); only names longer than `maxName` are middle-truncated.
- [x] On a narrow terminal, NAME shrinks (down to `minName`, 12) and long names
      are middle-truncated (`prefix…suffix`); the row does not push past the
      terminal width more than before.
- [x] Changing the `/` filter does not change the NAME column width (computed
      over all tunnels, not only visible).
- [x] `m.width == 0` (pre-`WindowSizeMsg`, unit tests) renders identically to
      the previous fixed-`colName` layout.
- [x] `go build ./...`, `gofmt -l .`, `go vet ./...`, `go test ./...` are clean.
- [x] ROADMAP.md has the phase-23 row, with its status marker matching the
      file's frontmatter.

## Verification

```sh
make fmt && make vet && make test
go test ./internal/tui/... -run 'TestFitName|TestNameWidth' -v

# Manual:
make run
#   with a config containing a long-named tunnel (e.g. pntr-sberhealth-browser)
#   on a wide terminal: name shown in full, all columns aligned.
#   resize the terminal narrower: NAME column shrinks, long names middle-
#   truncated (prefix visible); columns stay aligned.
#   type `/` and a filter: the NAME column width does not change.
NO_COLOR=1 make run   # monochrome: alignment unchanged
```

## Technical details

- Row display-width budget (all in cells): `sideMargin(1) + cursor(1) + sp(1) +
  indicator(1) + sp(1) + nameW + 4*gutter(8) + colType(7) + colEndpoint(48) +
  colStatus(14) + uptimeBudget(7)`. The terminal-budget cap subtracts everything
  except `nameW`.
- `uptimeBudget = 7` reserves room for the longest realistic uptime
  (`999d23h`); UPTIME only renders for the `Connected` state (`view.go:265`),
  so on rows where it is absent the reserved space is just trailing blank —
  acceptable, and it keeps UPTIME from being pushed off on wide-content rows.
- `minName = 12` / `maxName = 40`: `minName` keeps the column usable on small
  terminals; `maxName` stops an absurdly long single name from shrinking the
  neighbouring columns visually. Both are easy to revisit.
- `fitName` is a one-line wrapper over `middleTruncate` (`view.go:437`); it
  exists for symmetry with `fitEndpoint` / `fitHostPort` and for a clear test
  surface, not for new logic. It is applied even though `nameW` is already
  `>=` the longest name in the common case, so the `width==0` fallback path
  and any future caller stay safe.
- The existing `pad()` contract ("pad to n; overflow returns value + one
  space") and its tests (`TestPad` / `TestPadAlignsColumns`) are **not**
  changed. Alignment now comes from `fitName` guaranteeing
  `lipgloss.Width(name) <= nameW`, so `pad` never hits its overflow branch for
  NAME.
- ENDPOINT intentionally stays fixed: it already middle-truncates long hosts
  (see the screenshot's `c-c9qmgaf6i…dexcloud.net:3306`), so making it dynamic
  is unnecessary for this fix. A future phase could make ENDPOINT shrink on
  very narrow terminals if needed.
- Out of scope: the STATUS-with-error overflow
  (`error` + an 18-char truncated message = 24 cells in a 14-wide column). It
  does not visibly drift UPTIME today because the `Error` state has no uptime,
  but it is the same class of bug and is left for a separate pass.

## Open questions (resolved)

- Truncation style for NAME? → **middle** (`prefix…suffix`), consistent with
  `fitEndpoint`.
- Over which rows is `nameW` computed? → **all** of `m.list`, so the column is
  stable while the `/` filter narrows the view (it does not jump as matches
  change).
- Does ENDPOINT become dynamic too? → **no** — it already self-truncates; only
  NAME changes in this phase.
