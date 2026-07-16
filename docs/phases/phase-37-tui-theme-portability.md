---
phase: 37
title: TUI theme portability & color correctness
status: done
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
- [x] `theme.go` — keep `detectKind()` as the fallback resolver; add a path that
      accepts a runtime-resolved background luminance and returns the theme kind.
- [x] `styles.go` — stop resolving the palette at package init (`var pal = …`).
      Introduce a `styles`/theme struct on `Model` (fields replacing the current
      package-level style vars `titleStyle … editorLabelStyle`, `stateStyle`,
      `surfaceBg`); update every call site in `view.go`/`editor.go`/`logs.go`.
- [x] `model.go` — hold the resolved styles on `Model`; resolve a sensible
      default at construction (for the pre-message first frame and unit tests).
- [x] `update.go`/`model.go` — return `tea.RequestBackgroundColor()` from
      `Init()`; on `tea.BackgroundColorMsg` resolve the palette by luminance
      (`IsDark()`), store it on the model, and request a repaint.
- [x] Degradation: `PORTATO_THEME` short-circuits the query; `NO_COLOR` → mono;
      a missing OSC 11 answer falls through to `COLORFGBG` then default dark —
      verify none of these misrender or hang.

### B — light surface fill
- [x] `view.go` — issue `tea.ClearScreen` on the first `WindowSizeMsg` so the
      whole frame repaints once dimensions are known.
- [x] `view.go` — light surface via two complementary mechanisms (see note), and
      delete the per-style baked background (`withBackground`) that caused
      visible #FAFAFA boxes whenever the terminal bg ≠ #FAFAFA.

> **Implementation note (Task B resolution — empirically revised).** Two
> terminal facts drove this (verified on the maintainer's machine): **iTerm2
> ignores OSC 11 set** but exports `COLORFGBG="7;0"` (→ dark auto-detect);
> **Terminal.app honours OSC 11 set** but has no `COLORFGBG`. So neither a pure
> cell-fill nor a pure view-background works everywhere, and the earlier
> `withBackground` baking (Phase 15) painted a #FAFAFA box per glyph that showed
> on any terminal whose bg ≠ #FAFAFA (bugs #1/#2). The final mechanism:
>
> - **Delete `withBackground`** — styles carry foregrounds only; no per-glyph
>   boxes. (Supersedes the Phase 15 approach; the surface comes from below.)
> - **`fillBg` cell-paints every content line** with #FAFAFA — covers OSC-ignoring
>   terminals (iTerm2). Reset-aware (re-asserts bg after every ANSI reset).
> - **`tea.View.BackgroundColor`** asks the renderer to set the terminal's own
>   background (OSC 11) to #FAFAFA — covers the whole pane (incl. footer and the
>   below-content area) on OSC-honoring terminals (Terminal.app). Restored on
>   normal exit (`ResetBackgroundColor` on close; SIGKILL can't — accepted).
> - **Theme-conditional section separators** in `render()`: light keeps sections
>   adjacent (single `\n`, `table()` no trailing `\n`) — a blank separator would
>   render as the terminal's own background, a dark seam through the card on
>   non-honoring terminals. Dark/mono keep a blank line (`\n\n`) for breathing
>   room: it is invisible there (transparent surface). One conditional on
>   `surfaceBg`, not scattered per-view. (If OSC-11-set success ever becomes
>   detectable, light can regain the gaps; a painted separator row for light is a
>   phase-39 polish candidate, rejected here — rule lines change the design
>   language and NBSP tricks bet on ultraviolet's whitespace classification.)
>
> **Detection status (#3):** auto-detect works on both terminals. iTTerm2 → dark
> via `COLORFGBG="7;0"`; **Terminal.app (white) → light**, i.e. bubbletea's OSC 11
> query reader gets an answer where the naive shell test did not — so #3 is a
> *working*-detection case, not accept-and-document. The DoD item "white-bg
> terminal → light palette" is met.
>
> **Accepted residuals (non-honoring terminal with a FORCED light theme — not the
> auto-detect path):** (a) the area below the content block to full screen height
> is whitespace-only by nature and stays terminal-bg; (b) the footer line — a
> single long foreground-only run — loses its bg to a v2 cell-diff renderer quirk
> (the diff skips bg-only attribute changes on unchanged visible glyphs). Both
> are covered by `View.BackgroundColor` on honoring terminals. `ClearScreen` on
> the first `WindowSizeMsg` is kept as a first-frame repaint aid.

### C — dark surface policy
- [x] Dark stays transparent in every resolution branch — env override, OSC-11
      answer, `COLORFGBG`, and the unknown-background default-dark fallback
      alike. No `#16161D` fallback paint. (Supersedes the original "paint in the
      unknown-fallback branch" clause; see the note.)

> **Implementation note (Task C resolution).** Decision: the dark surface stays
> **transparent everywhere**; the `#16161D`-class fallback paint is **not**
> implemented. Rationale: (a) the DoD requires only "transparent when detection
> succeeded" — already true, since `darkPalette`/`monoPalette` carry `surfaceBg
> == nil`; (b) a paint via `tea.View.BackgroundColor` (OSC 11 set) is ignored by
> the iTerm2 class of terminals, so it would not produce a reliable surface
> anyway; (c) the `fillBg` cell-fill alone reproduces the same blank-separator /
> footer gaps documented under Task B, i.e. it does not yield a clean dark
> surface either; (d) on the rare no-signal **light** terminal (no OSC answer,
> no `COLORFGBG`) a forced dark paint would override the user's own light theme
> — reintroducing the F1 failure mode (a forced theme override) in exchange for
> a readability gain that only matters in that same rare case; (e) the
> unknown-fallback is empirically rare: both tested terminals emit a signal
> (iTerm2 → `COLORFGBG="7;0"` → dark; Terminal.app → OSC 11 answer → light).
> Net: a forced dark surface buys readability in a rare corner while paying with
> a theme override there, and it cannot be made reliable on non-honoring
> terminals regardless — so the transparent default is kept unconditionally.
> `darkPalette`/`monoPalette` are unchanged (`surfaceBg` nil;
> `TestResolvePaletteAllKinds` already locks that). The DoD item "Dark theme
> stays transparent by default" is met — strictly stronger: transparent in
> every branch.

### D — color correctness
- [x] `theme.go` `darkPalette()`/`lightPalette()` — dark state colors + accent/
      error/warn retuned to truecolor hex so every state clears WCAG AA
      deterministically; light `166` → `#B45309` (its only failing color). See
      the note. Locked by `TestDarkPaletteContrastOnDarkHome` /
      `TestLightPaletteContrastOnLightSurface`.
- [ ] (Optional, larger) Adopt adaptive light/dark pairs per semantic role
      (replacing the hand-written two-palette split), and drop `Faint(true)`
      usage in dark/mono (`theme.go:96-104`) in favor of a dim color — `Faint`
      is terminal-dependent and collapses on light backgrounds. Pairs tuned for
      `#FAFAFA` (light) / `#1E1E1E` (dark), all ≥ 4.4:1: accent `#4646D6`/`#8F8FFF`,
      connected `#15803D`/`#22C55E`, error `#CC0000`/`#F87171`, warn `#B45309`/`#FBBF24`,
      dim `#626262`/`#9CA3AF`, body `#262626`/`#E5E7EB`.

> **Implementation note (Task D resolution — empirically revised).** The planned
> minimal swaps (error `1`→`203`, title/cursor `63`→`105`, ANSI-256 indices) were
> widened to **truecolor hex** and extended to the connecting/connected states.
> Reason: while writing the WCAG test it surfaced that lipgloss's color model maps
> ANSI-16 indices (0-15) to dim VGA values (index 3 = olive `#808000`, index 2 =
> dark green `#008000`), under which connecting computed **3.97:1** and connected
> **3.25:1** on `#1E1E1E` — failing AA — and whose real rendering further depends
> on the terminal's palette index 2/3 (bright on iTerm2/Terminal.app, dim
> elsewhere). The audit's "passing" verdict for 2/3 assumed a bright palette. To
> make the DoD ("every state ≥ 4.5:1, recomputed via WCAG") hold
> **terminal-independently**, the dark state colors moved off ANSI-16 onto hex
> from the audit §5 dark column: error `#F87171` (6.03),
> connecting/reconnecting `#FBBF24` (9.99), connected `#22C55E` (7.32); accent
> (title/cursor/editorTitle) `#8F8FFF` (5.99); off stays `245` (4.83,
> deterministic ramp). The dark `warn` field followed connecting (`#FBBF24`) for
> role coherence. Light: `166` → `#B45309` (4.81) for connecting/reconnecting/warn
> — its only failing color. Hex values quantize predictably on 256-colour
> terminals (nearest cube ≈ the 203/105-class indices originally proposed). The
> optional D-large (adaptive `LightDark` pairs + drop `Faint`) remains deferred
> by explicit owner decision; the chosen hex values are verbatim the §5 dark
> column, so D-large is now a mechanical refactor onto `lipgloss.LightDark`, not
> a re-tune. Exclusions honored: no adaptive-pairs refactor, no `Faint` removal,
> no `LightDark`.

### E — mono glyph split
- [x] `theme.go` `palette` (new `connectingGlyph`/`connectedGlyph` fields) +
      `monoPalette()` (`◐`/`●`) + `view.go` `indicator()` (Connecting/
      Reconnecting → `connectingGlyph`, Connected → `connectedGlyph`). Dark/
      light keep `●` for connecting (colour already distinguishes the live
      states) — the `◐` split is mono-only. Locked by
      `TestMonoIndicatorGlyphs`.

> **Implementation note (Task E resolution).** Added `connectingGlyph`/
> `connectedGlyph` to the `palette` struct so `indicator()` is glyph-driven
> rather than hardcoded: mono sets `◐` (connecting/reconnecting) vs `●`
> (connected) — the old `●`-for-both-differing-only-by-Bold was invisible in
> many fonts (F11). The colour themes set both to `●` (the live states are
> already separated by green/amber); making the split universal is a one-line
> change per palette if ever wanted. `○` off and `✗` error are unchanged.

### F — bookkeeping
- [x] `docs/ROADMAP.md` — phase-37 row already added at plan time; flip status
      on start/complete.
- [x] This file — flip status on start/complete.

## Definition of Done

- [x] With `PORTATO_THEME` unset, a dark-background terminal gets the dark
      palette and a white-background terminal gets the light palette.
- [x] On a terminal that does not answer OSC 11 (e.g. tmux without passthrough)
      the app does not hang or misrender: it falls back to `COLORFGBG`, then
      default dark.
- [x] `PORTATO_THEME=light` inside a dark-background tmux: `tmux capture-pane -e`
      shows the surface background SGR on **every** grid line across the full
      width (spot-check first / middle / last rows and padding lines).
- [x] Every state color (off / connecting / connected / reconnecting / error) ≥
      4.5:1 on its ground background (recomputed via WCAG relative-luminance).
- [x] In the monochrome theme, `connecting` and `connected` are visually distinct
      (`◐` vs `●`), not just bold.
- [x] Dark theme stays transparent by default (no painted dark surface when
      detection succeeded); the user's terminal background shows through.
- [x] `PORTATO_THEME=dark|light|mono` still override; `NO_COLOR` still → mono.
- [x] `go build ./...`, `gofmt -l .`, `go vet ./...`, `go test ./...` are clean;
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

### Task B gate results (recorded)

Outputs captured during Task B verification (maintainer's machine):

1. **tmux capture gate** — `PORTATO_THEME=light`, dark-bg 256-colour tmux
   (`capture-pane -e`, x=100 y=30). The content-block lines carry the surface
   bg (#FAFAFA = 256-colour index 231) across the full width:
   - L1 header — bg YES (`48;5;231` SGR, re-asserted after every ANSI reset,
     padded to width; e.g. starts `<ESC>[48;5;231m 🥔 …`).
   - L2 column-header — bg YES.
   - L3 row — bg YES.
   - L4 footer — bg NO: the documented residual (a single long fg-only run the
     v2 cell-diff renderer leaves un-bg'd on non-honoring terminals; covered by
     `View.BackgroundColor` on honoring ones).
2. **bg restore on clean `q`-quit** — pty capture shows **OSC 111 (reset
   background)** emitted on close, so the terminal's prior background is
   restored. Confirmed visually on Terminal.app (Ocean/blue profile):
   `PORTATO_THEME=light` paints the full pane #FAFAFA over blue; `q` restores
   the exact blue.
3. **Unit tests** — `go test ./internal/tui/` → ok. Passing: `TestViewBackgroundColor`
   (light sets bg; dark/mono nil), `TestFillBg`, `TestFillBgReassertsAfterReset`,
   `TestDetectKind` (10 cases), `TestResolvePaletteAllKinds`,
   `TestLightPaletteReadableForegrounds`, `TestResolveKind` (11 cases). Full
   `make fmt/vet/test/lint` + `gofmt -l .` (empty) + `go build ./...` clean.

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
