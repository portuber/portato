---
phase: 38
title: TUI responsive layout (footer, help, columns)
status: todo
depends_on: [23]
---

> TUI responsiveness phase. On real terminal sizes (80×24, 60×16) the footer
> hides its most important keys, the `?` help overlay shows only the logo, and
> the STATUS column is clipped before anything else. This phase makes the
> footer fit, the help reachable, and the table columns shrink by priority.

## Goal

At 80 columns (and down to 60) the user always sees the keys that unlock the
rest of the UI (`? help`, `q quit`), can reach every help binding, and sees the
full status word for every tunnel — with the least-important column (ENDPOINT)
shrinking first instead of the most-important one dying.

## Background / why

- **Footer.** `footer()` (`internal/tui/view.go:376-378`) renders one fixed
  string:
  ```
  ↑↓/jk move · space toggle · p passphrase · o password · r restart · a/x all · e edit · n new · C duplicate · d delete · l logs · / filter · R reload · ? help · q quit
  ```
  ~150 cells wide. At 120 cols the visible part ends at `d delete ·` — `l logs`,
  `/ filter`, `R reload`, **`? help`**, **`q quit`** are never seen. At 60 cols
  it cuts mid-word (`r r`). The keys that reveal the rest of the UI are exactly
  the ones lost.
- **Help overlay.** The help panel is appended *below* the table and footer
  (`render`, `view.go:146-149`). Its height (logo + title + 17 bindings +
  borders) exceeds what remains of a 24-row terminal, and the overflow is
  simply cut. The `splashMinH = 18` gate (`view.go:29`) only drops the logo
  below 18 rows of *total* terminal height; it does not account for the panel's
  position under the table. At 80×24, pressing `?` shows the potato logo +
  `Help` title and **zero** bindings.
- **Columns.** Only NAME is width-aware (`nameWidth`, `view.go:233-250`,
  clamped `[12, 40]`, added in phase 23). TYPE, ENDPOINT, STATUS are fixed
  (`colType=7`, `colEndpoint=48`, `colStatus=14`); overflow is clipped by the
  terminal. At 80 cols STATUS is reduced to `co` and UPTIME disappears, while
  ENDPOINT keeps its full 48 columns — the least-important column survives and
  the most-important one dies. A middle-truncate helper already exists
  (`fitEndpoint` / `middleTruncate`, `view.go:587-638`).

## Design decisions (locked at plan time)

| Aspect | Decision |
|---|---|
| Footer | `bubbles/help` driven by a `key.Map` (auto-fits width, short/full modes), or a hand-rolled priority-fit that emits `? help · q quit · space toggle` first and appends the rest while they fit. Keep the existing visual style. |
| Help overlay | Render help as its own **full-screen view** (the same way logs render) or as a centered, scrollable overlay; include the logo only when the full binding list fits below it. |
| Column shrink priority | Indicator glyph + STATUS are untouchable; ENDPOINT shrinks first (it already middle-truncates); TYPE can degrade to `L`/`R`/`D`; NAME is the flex column; UPTIME is right-aligned numeric. |
| Flex column | NAME stays the flexible column (phase 23's `nameWidth`); ENDPOINT becomes droppable/shrinkable instead of fixed 48. |
| Right-aligned numerics | UPTIME right-aligned (`fmt.Sprintf("%*s", w, s)`). |

## Tasks

### A — footer that fits
- [ ] `view.go` `footer()` — replace the single fixed string with a width-aware
      build: either `bubbles/help` + a `key.Map`, or a priority-fit (`? help`,
      `q quit`, `space toggle` first, append the rest while they fit). Must
      honor `m.width`.

### B — reachable help
- [ ] `view.go` / new help model — render `?` help as a full-screen view (or a
      centered scrollable overlay) so all 17 bindings are reachable at 80×24.
- [ ] Drop or relocate the `splashMinH`-gated logo so it never pushes the
      binding list off-screen; show the logo only when the list fits beneath it.

### C — column shrink priority
- [ ] `view.go` — introduce a shrink order: STATUS + indicator untouchable,
      ENDPOINT shrinks first (reuse `fitEndpoint`/`middleTruncate`), TYPE
      degrades to `L/R/D` when very tight, NAME stays flex, UPTIME
      right-aligned.
- [ ] `view.go` `nameWidth()` / `row()` / `columnHeader()` — make ENDPOINT
      width-aware (currently fixed `colEndpoint=48`); hand it a budget derived
      from `m.width` after the untouchable columns are reserved.

### D — bookkeeping
- [ ] `docs/ROADMAP.md` — phase-38 row added at plan time; flip status on
      start/complete.
- [ ] This file — flip status on start/complete.

## Definition of Done

- [ ] At 80 cols, `? help` and `q quit` are visible in the footer; nothing is
      cut mid-word at 60 cols.
- [ ] At 80×24, pressing `?` shows all 17 bindings (scroll is acceptable); the
      logo does not push them off-screen.
- [ ] At 80 cols the full status word (`connected` / `error` / `off`) is visible
      for every row; ENDPOINT middle-truncates instead.
- [ ] The right edge is never ragged: slack goes to the flex (NAME) column.
- [ ] No regression at 120×35 (the reference size) — full layout as today.
- [ ] `go build ./...`, `gofmt -l .`, `go vet ./...`, `go test ./...` are clean;
      `make lint` is clean.

## Verification

```sh
make fmt && make vet && make test && make lint

# capture at the problem sizes:
for s in "80 24" "60 16" "120 35"; do
  set -- $s
  tmux new-session -d -s t -x $1 -y $2
  tmux send-keys -t t './bin/portato --force-standalone --config <isolated>' Enter
  tmux capture-pane -ep -t t    # footer: ? help + q quit visible at 80; STATUS word intact
  tmux send-keys -t t '?'       # help: all 17 bindings reachable at 80x24
  tmux capture-pane -ep -t t
  tmux kill-session -t t
done
```

Safety: use an isolated config (`--config`) with `ssh:` hosts set to
`127.0.0.1:9` or a local test sshd; never enable the user's real tubers.

## Technical details / risks

- **Footer choice.** `bubbles/help` gives short/full modes and width fitting
  for free, but changes the visual; a priority-fit string preserves the current
  look. Either is acceptable; pick the one that keeps the existing footer style.
- **Help as a view** mirrors how `logs` (`internal/tui/logs.go`) already takes
  over the frame — a proven pattern in this codebase, lower risk than a layered
  overlay.
- **Column shrink** reuses phase 23's `nameWidth` machinery and the existing
  `fitEndpoint`/`middleTruncate` helpers; the new work is computing an ENDPOINT
  budget from `m.width` and a TYPE degradation. Untouched: the `pad()` contract
  and its tests.
- This phase is layout-only and independent of phase 37 (theme) — the two can
  land in either order.

## Commit plan (per CONVENTIONS)

1. `docs(phase-38): plan` — this file + ROADMAP row `[ ]`.
2. `docs(phase-38): start` — flip frontmatter + ROADMAP `[ ] -> [~]`.
3. `feat(tui): width-aware footer` — task A.
4. `feat(tui): full-screen help overlay` — task B.
5. `feat(tui): column shrink priority + right-aligned uptime` — task C.
6. `docs(phase-38): complete` — `[~] -> [x]` after the DoD passes.

## Start guard

This phase is `status: todo`. It may start only on an explicit "start phase 38"
command, after its `depends_on` ([23], `[x]`) is satisfied, and while no other
phase is `[~]`.
