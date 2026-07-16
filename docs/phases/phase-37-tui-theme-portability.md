---
phase: 37
title: TUI theme portability & color correctness
status: in-progress
depends_on: [15]
---

> TUI accessibility phase. The default theme (dark) is selected even on a
> white terminal background, where its light foreground colors fall below every
> WCAG contrast threshold — the single biggest "looks washed-out" complaint.
> This phase makes theme selection follow the *actual* terminal background,
> fixes the light surface fill under multiplexers, and corrects the few
> contrast-failing colors and the mono glyph ambiguity.

## Goal

The active palette is chosen against the real terminal background (queried at
runtime), not a package-init default; on a white-background terminal the light
palette is picked automatically. The light theme paints a solid full-width
background reliably even under tmux. Every state color passes WCAG AA (≥ 4.5:1)
on its ground, and the monochrome theme distinguishes `connected` from
`connecting` by glyph, not just by bold.

## Background / why

- `detectKind()` (`internal/tui/theme.go:29-53`) resolves the theme at
  priority `PORTATO_THEME` → `NO_COLOR` → `COLORFGBG` heuristic → **default
  dark** (`theme.go:52`). macOS Terminal.app does **not** set `COLORFGBG`, so a
  white-background terminal loads the dark palette.
- Dark and mono never paint a background (`surfaceBg` is nil,
  `internal/tui/styles.go:35`), so the dark palette's light foregrounds land on
  the terminal's own (white) background.
- The palette is resolved once at package init (`styles.go:11`), before any
  runtime background query could arrive.

Measured contrast of the dark palette on a white background (WCAG AA normal
text = 4.5:1; large text = 3:1):

| Role (ANSI index) | on white | verdict |
|---|---|---|
| off / dim text (245, #8A8A8A) | 3.45:1 | FAIL |
| connecting / reconnecting (3, yellow) | 1.70:1 | FAIL |
| connected (2, green) | 2.16:1 | FAIL |
| faint-attribute text (footer/header) | ~4.0:1 | FAIL |
| error (1, red) | 5.84:1 | pass |
| title / cursor (63, indigo) | 4.60:1 | pass |

The failing values are below even the 3:1 large-text threshold — for an app
whose only job is to show tunnel status at a glance, that is a functional
breakage, not "low contrast".

On the dark palette's *home* background the error color also fails: ANSI 1
(#CD0000) on #1E1E1E = **2.85:1** — the most attention-critical state is the
least readable. (Connecting 9.79:1, connected 7.73:1, off 4.83:1 are fine.)

The monochrome palette renders both `connecting` and `connected` as `●`,
differing only by `Bold(true)` on connected (`theme.go:209-210`) — invisible in
many fonts.

## Design decisions (locked at plan time)

| Aspect | Decision |
|---|---|
| Background detection | `tea.RequestBackgroundColor()` on startup → handle `tea.BackgroundColorMsg`; pick the palette from `BackgroundColorMsg.IsDark()`. Both exist in `charm.land/bubbletea/v2 v2.0.7` (verified). Use `lipgloss.LightDark` (`charm.land/lipgloss/v2 v2.0.4`) for per-role pairs. |
| Degradation chain (explicit) | `PORTATO_THEME` set → use it, ask nothing; OSC 11 answered → pick by luminance; no answer → `COLORFGBG` heuristic; nothing → default dark. OSC 11 is not universally answered (macOS Terminal.app historically unreliable; tmux may not pass it through), but bubbletea v2 brackets the query with a Device Attributes request so "no answer" resolves and the program never hangs. |
| Override precedence | `PORTATO_THEME` stays the top override; `NO_COLOR`/mono stays. |
| Palette location | Move palette resolution out of package init (`styles.go:11`) into a **theme/styles struct held on `Model`**, because `BackgroundColorMsg` arrives after `Init`. Package-level style vars become fields/lookups on the model. |
| Dark surface policy | Dark stays **transparent** by default (respects the user's terminal theme; once runtime detection works the right palette is chosen for the actual background anyway). Painting a dark surface is only a fallback for the "background could not be determined" case. |
| Light surface fill | Replace string-level SGR post-processing (`fillBg`/`paintLine` in `view.go:58-103`) with structural full-width row styles (`lipgloss.NewStyle().Width(m.width).Background(bg)`) or the compositor/view background; force a full repaint once dimensions are known. |

## Tasks

### A — runtime theme resolution (the core change)
- [ ] `theme.go` — keep `detectKind()` as the fallback resolver; add a path that
      accepts a runtime-resolved background luminance and returns the theme kind.
- [ ] `styles.go` — stop resolving the palette at package init (`var pal = …`).
      Introduce a `styles`/theme struct on `Model` (fields replacing the current
      package-level style vars `titleStyle … editorLabelStyle`, `stateStyle`,
      `surfaceBg`); update every call site in `view.go`/`editor.go`/`logs.go`.
- [ ] `model.go` — hold the resolved styles on `Model`; resolve a sensible
      default at construction (for the pre-message first frame and unit tests).
- [ ] `update.go`/`model.go` — return `tea.RequestBackgroundColor()` from
      `Init()`; on `tea.BackgroundColorMsg` resolve the palette by luminance
      (`IsDark()`), store it on the model, and request a repaint.
- [ ] Degradation: `PORTATO_THEME` short-circuits the query; `NO_COLOR` → mono;
      a missing OSC 11 answer falls through to `COLORFGBG` then default dark —
      verify none of these misrender or hang.

### B — light surface fill
- [ ] `view.go` — issue `tea.ClearScreen` on the first `WindowSizeMsg` so the
      whole frame repaints once dimensions are known (the first frames render
      with `m.width == 0`, where `fillBg` is a documented no-op,
      `view.go:59-61`).
- [ ] `view.go` — replace `fillBg`/`paintLine` string-level SGR injection with
      structural full-width backgrounds (full-width row styles or the view
      background) so every grid cell carries the surface color regardless of the
      cell-diff renderer.

### C — dark surface policy
- [ ] With A+B in place, confirm dark is transparent by default; paint a dark
      surface (`#16161D`-class) **only** in the unknown-background fallback
      branch. Do not override the user's terminal theme when detection worked.

### D — color correctness
- [ ] `theme.go` `darkPalette()` — replace error `1` → `203` (#FF5F5F, 2.85 →
      5.60:1); replace title/cursor `63` → `105` (#8787FF, 3.63 → 5.51:1).
- [ ] (Optional, larger) Adopt adaptive light/dark pairs per semantic role
      (replacing the hand-written two-palette split), and drop `Faint(true)`
      usage in dark/mono (`theme.go:96-104`) in favor of a dim color — `Faint`
      is terminal-dependent and collapses on light backgrounds. Pairs tuned for
      `#FAFAFA` (light) / `#1E1E1E` (dark), all ≥ 4.4:1: accent `#4646D6`/`#8F8FFF`,
      connected `#15803D`/`#22C55E`, error `#CC0000`/`#F87171`, warn `#B45309`/`#FBBF24`,
      dim `#626262`/`#9CA3AF`, body `#262626`/`#E5E7EB`.

### E — mono glyph split
- [ ] `theme.go` `monoPalette()` / `view.go` `indicator()` — differentiate
      `connecting`/`reconnecting` as `◐` vs `●` connected (keep `○` off, `✗`
      error). (Mono is a daily-driver theme for some users, not an edge case.)

### F — bookkeeping
- [ ] `docs/ROADMAP.md` — phase-37 row already added at plan time; flip status
      on start/complete.
- [ ] This file — flip status on start/complete.

## Definition of Done

- [ ] With `PORTATO_THEME` unset, a dark-background terminal gets the dark
      palette and a white-background terminal gets the light palette.
- [ ] On a terminal that does not answer OSC 11 (e.g. tmux without passthrough)
      the app does not hang or misrender: it falls back to `COLORFGBG`, then
      default dark.
- [ ] `PORTATO_THEME=light` inside a dark-background tmux: `tmux capture-pane -e`
      shows the surface background SGR on **every** grid line across the full
      width (spot-check first / middle / last rows and padding lines).
- [ ] Every state color (off / connecting / connected / reconnecting / error) ≥
      4.5:1 on its ground background (recomputed via WCAG relative-luminance).
- [ ] In the monochrome theme, `connecting` and `connected` are visually distinct
      (`◐` vs `●`), not just bold.
- [ ] Dark theme stays transparent by default (no painted dark surface when
      detection succeeded); the user's terminal background shows through.
- [ ] `PORTATO_THEME=dark|light|mono` still override; `NO_COLOR` still → mono.
- [ ] `go build ./...`, `gofmt -l .`, `go vet ./...`, `go test ./...` are clean;
      `make lint` is clean.

## Verification

```sh
make fmt && make vet && make test && make lint

# theme detection (unset override):
unset PORTATO_THEME
make run        # in a dark-bg terminal → dark palette; white-bg → light palette

# OSC 11 fallback (must not hang under tmux):
tmux new-session -d -s t -x 120 -y 35
tmux send-keys -t t 'unset PORTATO_THEME; ./bin/portato --force-standalone --config <isolated>' Enter
tmux capture-pane -ep -t t   # -e includes SGR attributes; verify it rendered

# light surface fill under tmux (every line carries bg across full width):
PORTATO_THEME=light ./bin/portato --force-standalone --config <isolated>   # run inside tmux
tmux capture-pane -e -t t

# mono glyph split:
PORTATO_THEME=mono ./bin/portato   # connecting ◐ vs connected ●
```

Safety: when capturing against the real binary, never enable tubers from the
user's real `config.yaml` (it dials real hosts). Use a copy with `ssh:` hosts
replaced by `127.0.0.1:9` (instant local failure) or a local test sshd.

## Technical details / risks

- **bubbletea v2 API** (verified present in v2.0.7): `tea.RequestBackgroundColor()`
  returns a `Msg` to send from `Init`; `tea.BackgroundColorMsg` carries the
  answered `color.Color` with an `IsDark()` helper. lipgloss v2.0.4 provides
  `lipgloss.LightDark(isDark bool)` and `lipgloss.HasDarkBackground(in, out)`.
- **The theme-struct refactor is load-bearing**, not cosmetic: package-init
  resolution is fundamentally incompatible with a runtime-arriving message, so
  moving styles onto `Model` is a prerequisite for the whole phase. It also
  unblocks the adaptive-pair option in task D.
- **OSC 11 reliability.** Not all terminals answer; tmux can swallow the query.
  The degradation chain (task A) is the safety net — never assume the query
  succeeds. bubbletea v2 brackets the request with Device Attributes so the
  wait always resolves (no hang).
- **Dark surface trade-off.** Painting a dark background makes the theme
  terminal-independent but overrides the user's own terminal theme. Decision
  (task C): transparent by default, painted only as the unknown-background
  fallback.
- **Light fill root cause (hypothesis).** First frames render before the first
  `WindowSizeMsg` when `m.width == 0` and `fillBg` is a no-op; the v2 cell-diff
  renderer then never repaints rows whose content did not change, leaving
  unpainted rows. `tea.ClearScreen` on the first `WindowSizeMsg` forces a full
  repaint; structural row backgrounds make the fill robust against the
  compositor. Verify against `capture-pane -e`.

## Commit plan (per CONVENTIONS)

1. `docs(phase-37): plan` — this file + ROADMAP row `[ ]`.
2. `docs(phase-37): start` — flip frontmatter + ROADMAP `[ ] -> [~]`.
3. `refactor(tui): move palette resolution onto Model` — task A (styles struct +
   runtime resolution) — the load-bearing change.
4. `fix(tui): reliable light surface fill` — task B.
5. `feat(tui): adaptive theme colors + mono glyph split` — tasks D, E (+ C as
   policy).
6. `docs(phase-37): complete` — `[~] -> [x]` after the DoD passes.

## Start guard

This phase is `status: todo`. It may start only on an explicit "start phase 37"
command, after its `depends_on` ([15], `[x]`) is satisfied, and while no other
phase is `[~]`.
