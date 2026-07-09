---
phase: 31
title: TUI logo wordmark + drop inline-PNG image mode
status: done
depends_on: [24]
---

## Goal

Show a combined "potato + PORTATO" wordmark in the empty-config splash and in
`portato --version` (falling back to the compact potato on a narrow terminal),
keep the compact potato in the help (`?`) overlay so it does not clutter the
hotkeys, and remove the inline-PNG image mode entirely so iTerm2/WezTerm render
the braille wordmark like every other terminal.

## Background

Phase 24 delivered a single 28×12 potato logo in three places (empty-list
splash, help overlay, `--version`) with a render cascade: inline PNG via the
OSC 1337 sequence for iTerm2/WezTerm, braille ASCII otherwise, solid block on
Windows. The user has since redrawn the assets:

- `internal/logo/assets/logo.braille.txt` and `logo-block.txt` — updated
  compact potato (frame **24×12**, uniform line width).
- `internal/logo/assets/logo-portato.braille.txt` and
  `logo-portato-block.txt` — **new** combined wordmark (potato + the letters
  PORTATO); frame **70×12**, uniform line width.
- `internal/logo/assets/logo.png` and root `logo.svg` — updated (the PNG is no
  longer wanted; see "drop image mode" below).

All four txt files have a **uniform raw line width** (each line padded to the
same frame), so the existing `centerBlock` centering works on them without any
art rework.

Current code pointers:

- `internal/logo/logo.go` — `Mode{Image,Braille,Block,Off}`, `Detect()`,
  `Render(mode, accent, mono)`, `Banner(accent, mono)`, `VersionBanner(...)`,
  embeds `brailleArt`/`blockArt`/`pngBytes`, `isImageTerm()`.
- `internal/logo/logo_image.go` — `inlineImage(png)` builds the OSC 1337
  sequence at the hardcoded `logoWidth=28`/`logoHeight=12` cell size.
- `internal/tui/view.go` — `table()` gates the splash on `m.height >=
  splashMinH` (18); `splash(hint)` renders `logo.Banner(...)` centered; the
  `helpBlock()` prepends `logo.Banner(...)` above the hotkeys (same height
  gate). Constants `splashMinH=18`, `splashLogoW=28`, `sideMargin=1`.
- `internal/cmd/version.go` — `printVersion(w, tty)` calls
  `logo.VersionBanner(version, commit, date, tty)`; `isTerminal(os.Stdout)`
  gates pipe-safety; `versionCmd.RunE` and `root.go:60` (`printVersion(...,
  isTerminal(os.Stdout))`) are the two call sites.
- `docs/SPEC.md` §11 "Branding / logo" (lines ~425–456) and
  `docs/phases/phase-24-tui-logo.md` describe the old single-size + PNG
  behaviour and must be updated when the code lands.

## Design decisions (locked)

| Aspect | Decision |
|---|---|
| Wordmark placements | empty-config splash + `portato --version`. NOT the help overlay (keep it uncluttered). |
| Help (`?`) overlay | unchanged — compact potato above the hotkeys (existing `helpBlock()`). |
| Narrow terminal | when `avail = m.width - 2*sideMargin < 70`, the splash falls back to the compact potato; very short terminal (`< splashMinH`) still shows the hint text only. |
| Image mode | removed entirely. iTerm2/WezTerm no longer auto-select the inline PNG; they render the braille wordmark. `PORTATO_LOGO=image` becomes unknown → auto → braille. |
| `logo.png` | deleted; `pngBytes` embed, `inlineImage`, `logo_image.go`, `isImageTerm`, `ModeImage` all removed. |
| `logo.svg` | kept (repo root) — canonical art source + README asset (`README.md` line 1 references `logo.svg`, not the PNG). |
| Tinting | unchanged: ASCII glyphs tinted with the theme title accent unless mono/`NO_COLOR`. The `--version` banner stays untinted (the CLI does not load the theme). |
| Heights | both potato (24×12) and wordmark (70×12) are 12 rows tall, so the height gate `splashMinH=18` is unchanged. |

## Asset inventory (frame = uniform raw line width, 12 rows)

| File | Frame | Role |
|---|---|---|
| `logo.braille.txt` | 24×12 | compact potato, braille |
| `logo-block.txt` | 24×12 | compact potato, block (Windows) |
| `logo-portato.braille.txt` | 70×12 | wordmark, braille |
| `logo-portato-block.txt` | 70×12 | wordmark, block (Windows) |

## Tasks

### `internal/logo/logo.go`
- [x] Remove `ModeImage` from the `Mode` enum (keep `ModeBraille`, `ModeBlock`,
      `ModeOff`).
- [x] Remove `pngBytes` and its `//go:embed assets/logo.png`.
- [x] Remove `isImageTerm()` and the `case "image": return ModeImage` branch in
      `Detect()`; remove the `if isImageTerm() { return ModeImage }` auto-branch
      so iTerm2/WezTerm fall through to braille.
- [x] Embed the two wordmark assets:
      `//go:embed assets/logo-portato.braille.txt` → `wordmarkBraille`;
      `//go:embed assets/logo-portato-block.txt` → `wordmarkBlock`.
- [x] Replace constants `logoWidth=28`/`logoHeight=12` (only used by the removed
      `inlineImage`) with `artHeight = 12`, `potatoW = 24`, `wordmarkW = 70`.
- [x] `Render(mode, accent, mono)` — compact potato (braille/block/off); drop
      the `ModeImage` case.
- [x] Add `RenderWordmark(mode, accent, mono)` (wordmarkBraille/wordmarkBlock/
      off) and `Wordmark(accent, mono) = RenderWordmark(Detect(), …)`.
- [x] `Banner(accent, mono)` — unchanged (compact potato via `Render`).
- [x] `VersionBanner(version, commit, date string)` — drop the `tty` parameter;
      render the **wordmark** untinted (`strings.TrimRight(wordmarkBraille,"\n")`
      / `wordmarkBlock` / `""`) + the version line. Inherently pipe-safe (raw
      braille/block, no ANSI, no OSC).

### `internal/logo/logo_image.go`
- [x] Delete the file (`inlineImage` was its only content and only consumer).

### `internal/logo/assets/logo.png`
- [x] Delete the file.

### `internal/cmd/version.go`
- [x] `printVersion(w io.Writer)` — drop the `tty` parameter; body becomes
      `fmt.Fprintln(w, logo.VersionBanner(version, commit, date))`.
- [x] Delete `isTerminal()`.
- [x] `versionCmd.RunE`: `printVersion(cmd.OutOrStdout())`.
- [x] Drop the now-unused `"os"` import if nothing else in the file uses it.

### `internal/cmd/root.go`
- [x] Line ~60: `printVersion(cmd.OutOrStdout(), isTerminal(os.Stdout))` →
      `printVersion(cmd.OutOrStdout())`. (Keep the `"os"` import if used
      elsewhere in the file.)

### `internal/tui/view.go`
- [x] Add constant `splashWordmarkW = 70` next to `splashLogoW = 28`.
- [x] Rewrite `splash(hint)` to pick the wordmark when
      `avail := m.width - 2*sideMargin >= splashWordmarkW`, else the compact
      potato (`logo.Banner`); keep the `splashLogoW` floor and the existing
      `centerBlock` calls. `helpBlock()` is left untouched.
- [x] The height gate in `table()` (`m.height >= splashMinH`) is unchanged.

### Tests
- [x] `internal/logo/logo_test.go`:
      - `TestEmbeddedAssets`: drop the `pngBytes`/PNG-signature checks; add
        non-empty + 12-line (`artHeight`) checks for `wordmarkBraille` and
        `wordmarkBlock`.
      - `TestDetectMatrix`: remove all `ModeImage` cases; iTerm2/WezTerm →
        `ModeBraille`; add `PORTATO_LOGO=image → braille` (image is now
        unknown → auto).
      - `TestRenderPerMode`: drop the `ModeImage` case.
      - Delete `TestInlineImageWellFormed` and `TestRenderImageViaMode`.
      - `TestTintAppliedUnlessMono`: keep (compact braille).
      - `TestVersionBanner`: rewrite — no `tty` parameter; braille mode shows
        the wordmark + version line and contains no OSC; `off` shows only the
        version line.
      - Add `TestRenderWordmark` / `TestWordmark` (non-empty for Braille/Block,
        `""` for Off).
- [x] `internal/tui/logo_test.go`:
      - `TestEmptyListSplashShowsLogo`: keep (the wordmark still contains
        braille glyphs).
      - Add `TestEmptyListSplashWideUsesWordmark`: `width=80` → the rendered
        splash's longest line is ~70 cells (wordmark).
      - Add `TestEmptyListSplashNarrowUsesPotato`: `width=60` → longest line
        ~24 cells (compact potato fallback). Distinguish wordmark vs potato by
        the longest rendered line width.
      - `TestHelpShowsLogo`, `TestSmallHeightOmitsLogo`,
        `TestNonEmptyListHasNoLogo`, `TestLogoOffHidesBranding`: unchanged.
- [x] `internal/cmd/version_test.go`:
      - All `printVersion(&b, true|false)` → `printVersion(&b)`.
      - Replace `TestPrintVersion_PipedImageNoOSC` with
        `TestPrintVersion_ImageFallsBackToBraille` (`PORTATO_LOGO=image`, no
        OSC, has braille, has the version line).
      - Delete `TestIsTerminal`; remove the now-unused `"os"` import.

### Docs (reality diverged from the spec — fix and mention in the commit)
- [x] `docs/SPEC.md` §11 "Branding / logo" (≈ lines 425–456): rewrite — drop
      the image mode / OSC 1337 / inline-PNG and the "all variants 28×12" claim;
      describe the wordmark (70×12) for splash + `--version`, the compact
      potato (24×12) for the help overlay and the narrow-terminal fallback, the
      width gate (`avail ≥ 70`), and that the `--version` banner is plain
      braille/block (pipe-safe by construction).
- [x] `docs/phases/phase-24-tui-logo.md`: update the Design Decisions table,
      Definition of Done, Verification and Out-of-scope sections to note the
      wordmark + potato fallback and the removal of the image mode (cross-link
      this phase). Phase 24 stays `[x]` — this is a post-hoc refinement.

## Definition of Done

- [x] Empty-config splash shows the wordmark on a wide terminal (≥ ~72 cols)
      and the compact potato on a narrow one; a short terminal shows the hint
      only.
- [x] `portato --version` prints the wordmark + the `portato <version>
      (<commit>, <date>)` line; pipe-safe (no ANSI, no OSC).
- [x] The help (`?`) overlay still shows the compact potato (unchanged).
- [x] No `ModeImage` / `pngBytes` / `inlineImage` / `isImageTerm` / `logo.png` /
      `logo_image.go` remain; iTerm2/WezTerm render braille.
- [x] `PORTATO_LOGO=off` hides the big logo everywhere (splash, help, version)
      and the header emoji.
- [x] `PORTATO_LOGO=image` no longer errors and renders braille.
- [x] `go build ./...`, `make fmt`, `make vet`, `make test` all clean;
      `gofmt -l .` empty.
- [x] SPEC §11 and the phase-24 doc reflect the new behaviour.

## Verification

```sh
make fmt && make vet && make test
go build ./...
gofmt -l .                       # must be empty

./bin/portato --version          # wordmark + version line
PORTATO_LOGO=braille ./bin/portato   # empty config -> wordmark (wide term)
# narrow the terminal to < 72 cols -> compact potato fallback
PORTATO_LOGO=off ./bin/portato   # no logo, hint only
PORTATO_LOGO=image ./bin/portato # braille wordmark (image mode gone)
# in iTerm2: empty list shows the braille wordmark, not a PNG
```

## Commit plan

One commit at the end, bundling the asset changes with the code/tests/docs (the
user's instruction: commit together with `./logo.svg` and
`internal/logo/assets/*`). Stage the updated/new assets
(`logo.svg`, `logo.braille.txt`, `logo-block.txt`, `logo-portato.braille.txt`,
`logo-portato-block.txt`), the `logo.png` **deletion**, and all code/test/doc
changes. Suggested message:

```
feat(logo): potato+PORTATO wordmark for splash and --version; drop inline-PNG image mode

Replace the single 28x12 potato (with iTerm/WezTerm inline-PNG via OSC
1337) with two ASCII variants: a compact 24x12 potato and a 70x12
"potato + PORTATO" wordmark. The empty-list splash and `portato
--version` now show the wordmark (falling back to the compact potato on
narrow terminals); the help (?) overlay keeps the compact potato so it
does not clutter the hotkeys. The inline-PNG image mode is removed
entirely -- iTerm2/WezTerm render the braille wordmark like everyone
else. Assets updated/added accordingly.
```

Per `AGENTS.md`: local commit only, no push. Phase lifecycle commits
(`docs(phase-31): start` / `complete`) are separate, per CONVENTIONS.

## Technical details

- **Width math.** `avail = m.width - 2*sideMargin` (= `m.width - 2`). The
  wordmark (70-cell frame) fits when `m.width >= 72`. Below that the compact
  potato (24-cell frame) is used; the existing `splashLogoW` floor keeps
  `centerBlock`'s pad non-negative. `centerBlock` measures visible width via
  `lipgloss.Width` (ANSI-stripped), so a tinted wordmark still centers on its
  70-cell footprint.
- **Why centering works on the new art.** All four txt files have a uniform raw
  line width (24 or 70) — each line is padded to the frame with braille-blank
  (U+2800) / space cells — so `centerBlock` pads every line identically and the
  art stays aligned. No per-line normalisation needed.
- **`--version` pipe-safety.** With the image mode gone, `VersionBanner` emits
  raw braille/block glyphs + the version line (no ANSI, no OSC), so
  `portato --version | head` is clean by construction. The `tty` parameter and
  `isTerminal` are no longer needed.
- **No-cycle constraint.** `internal/logo` imports only `embed`, lipgloss, os,
  runtime, strings — no portato packages. Both `tui` and `cmd` import it
  safely (unchanged from Phase 24).
- **Why a new phase, not a reopen of 24.** Phase 24 is `[x]` (done) and
  CONVENTIONS forbids silently reopening a completed phase. This is tracked as
  Phase 31 (`depends_on: [24]`); the phase-24 doc is edited only to note the
  divergence (cross-link), not re-marked.

## Start guard

This phase is `status: todo`. It starts only on an explicit **"start phase 31"**
command (per `docs/CONVENTIONS.md`). The first action then is to flip the
frontmatter + ROADMAP row `[ ]->[~]` (commit `docs(phase-31): start`) — not
before.
