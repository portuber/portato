---
phase: 24
title: TUI branding / logo (empty-state splash, help, --version)
status: todo
depends_on: [3, 11]
---

## Goal

Render the Portato potato logo in three places — the empty-list splash, the top
of the help (`?`) overlay, and the `portato --version` CLI banner — using the
best rendering the terminal supports: an inline PNG (iTerm2/WezTerm via OSC
1337), falling back to braille ASCII, then to block ASCII for legacy Windows
consoles. A small potato emoji marks the header on macOS only. NO startup splash.

## Background

Researched facts (verified in the planning session — do not re-derive):

- `logo.svg` lives at the **repo root** (canonical project logo + README asset;
  it is the source both ASCII variants regenerate from). `assets/` (repo root)
  currently holds six files (regenerated at **28x12** during planning):
  `logo.braille.txt` (filled braille — NOT kept), `logo-outline.braille.txt`
  (outline braille — kept, -> `internal/logo/assets/logo.braille.txt`),
  `logo-block.txt` (outline-source block — NOT kept), `logo-solid-block.txt`
  (solid block, Windows — kept, -> `internal/logo/assets/logo-block.txt`),
  `logo.png` (1024x1024 RGBA, **alpha-transparent**, single brown fill #955e30 —
  composites cleanly on dark AND light backgrounds, so no dark/light variants
  needed; -> `internal/logo/assets/logo.png`), `logo-solid.png` (1 MB, the solid
  raster source — deleted in commit 2). See "Asset disposition" for the full
  keep/rename/remove split.
- The text logos were generated from `logo.svg`/the PNG via ImageMagick + chafa
  at `--size 28x12` (see the regeneration commands in "Asset disposition").
- TUI rendering (`internal/tui/view.go`): `header()` emits
  `"Portato — Port Forwarding"` left + `"mode: …"` right via `joinRight`;
  `table()` empty-state currently prints one dim hint line; `helpBlock()`
  builds the `?` panel; `centered()` centers modals and can place a splash.
  `View()` uses `tea.NewView(...)` with `AltScreen=true`; `fillBg` paints the
  surface for the light theme.
- Theme/detect (`internal/tui/theme.go`): `PORTATO_THEME`/`NO_COLOR`/`COLORFGBG`
  resolve a dark/light/mono palette; styles are lipgloss. `lipgloss.Width`
  counts display cells (the potato emoji 🥔 = 2 on darwin).
- Deps already in the graph: `github.com/xo/terminfo` (via lipgloss/bubbletea,
  indirect). iTerm2 OSC 1337 needs no new dependency — hand-roll ~20 lines.
- `go:embed` cannot escape a package dir (`../../assets` is illegal), so the
  assets MUST live under the embedding package.

## Design decisions (locked at phase start)

| Aspect | Decision |
|---|---|
| Placements | empty-list splash + help (`?`) top + `portato --version` banner. NO startup splash. NO always-on big logo on the working screen. |
| Header mark | potato emoji 🥔 before "Portato" **only on GOOS=darwin**; plain text elsewhere. Override: `PORTATO_LOGO_EMOJI=on\|off`. |
| Render cascade | iTerm2/WezTerm (`TERM_PROGRAM in {iTerm.app, WezTerm}`) -> inline PNG (OSC 1337); otherwise -> braille; `GOOS=windows` -> block. |
| Override | `PORTATO_LOGO=auto\|image\|braille\|block\|off`. `off` hides the big logo everywhere AND the header emoji. |
| ASCII size | ONE size, **28x12** (cells), used for empty-state, help, and the `--version` banner. User regenerates the two txt files at this size before/during impl. |
| PNG | already alpha-transparent -> works in dark/light unchanged; one PNG, no theme variants. |
| Small terminal | gate the big logo on `m.height >= ~18` rows; below that, show the hint text only (current behaviour). Help panel similarly omits the logo if height < threshold. |
| Colour | braille/block glyphs tinted with the theme title/accent foreground; mono/`NO_COLOR` -> plain glyphs (no tint). PNG renders with its own colours. |
| Assets in repo | MINIMAL: keep **outline**-braille (rename -> `logo.braille.txt`) + **solid**-block (rename -> `logo-block.txt`) + `logo.png` + `logo.svg`; remove `logo-solid.png` and the filled/outline-source duplicates. Variant choice evaluated at 28x12 (outline reads as a potato; filled reads as a generic egg; solid block is robust on legacy conhost, sparse outline-block looks fragmented). |
| Packaging | new leaf package `internal/logo/` (so `tui` and `cmd` import it without cycles); assets at `internal/logo/assets/`; `go:embed` the two txt + the png. |
| Phase | #24, `depends_on: [3, 11]` (main screen + theme/detect — both `[x]`). |

## Asset disposition

Variant selection (locked after eyeballing the regenerated 28x12 outputs):
outline-braille reads as a lumpy potato (filled reads as a generic egg/oval);
solid-block is robust on legacy Windows conhost (sparse outline-block looks
fragmented there).

- KEEP (4):
  - `logo-outline.braille.txt` -> `internal/logo/assets/logo.braille.txt`
    (outline braille, primary ASCII variant; from `logo.svg` via erode+threshold).
  - `logo-solid-block.txt` -> `internal/logo/assets/logo-block.txt` (solid
    block, Windows/legacy fallback; from the solid PNG).
  - `assets/logo.png` -> `internal/logo/assets/logo.png` (inline PNG for
    iTerm2/WezTerm; embedded in the binary).
  - `logo.svg` stays at the **repo root** (canonical project logo + README
    asset; NOT moved into `internal/logo/assets/`, NOT embedded — it is the
    source both ASCII variants regenerate from).
- REMOVE (3): `logo-solid.png` (1 MB; only the solid-block regen used it, see
      below), the filled `logo.braille.txt`, the outline-source `logo-block.txt`.
- MOVE: the txt files + `logo.png` -> `internal/logo/assets/` (`assets/` is
      untracked, so a plain `mv` + `git add` is fine; `go:embed` is package-
      relative). After the move `assets/` is empty and is removed; the repo
      root keeps `logo.svg`.

Regeneration commands (28x12), run from repo root. Both ASCII variants derive
from `logo.svg` at the repo root (the canonical project logo + README asset, so
it lives outside `internal/logo/assets/`), and the repo needs no large PNG to
reproduce them:
```
# outline braille — primary ASCII variant (from logo.svg, eroded + thresholded)
magick logo.svg -background white -flatten -colorspace gray \
  -morphology Erode Disk:7 -resize 88x88 -threshold 80% -depth 8 -type truecolor png:- \
  | chafa -f symbols --symbols braille --colors none --invert --size 28x12 - \
  > internal/logo/assets/logo.braille.txt
# solid block — Windows / legacy fallback (from logo.svg, solid fill, no erode)
magick logo.svg -background white -flatten -resize 88x88 -depth 8 -type truecolor png:- \
  | chafa -f symbols --symbols block+space --colors none --invert --size 28x12 - \
  > internal/logo/assets/logo-block.txt
```
(Adjust the magick/chafa flags to taste; the contract is just "28x12 cells,
recognisable potato". The currently-committed `logo-block.txt` was generated
from `logo-solid.png` before its deletion — the `logo.svg` path above is the
reproducible equivalent and may differ slightly; tweak to match if needed.)

## Tasks

- [ ] Move assets: `assets/` is currently untracked at the repo root (minus
      `logo.svg`, which already moved to the repo root for the README). Move
      `logo-outline.braille.txt` -> `internal/logo/assets/logo.braille.txt` and
      `logo-solid-block.txt` -> `internal/logo/assets/logo-block.txt`, and
      `logo.png` -> `internal/logo/assets/logo.png`; delete `logo-solid.png`,
      the filled `logo.braille.txt`, and the outline-source `logo-block.txt`;
      remove the now-empty `assets/` dir. `logo.svg` stays at the repo root.
      The two committed txt files are the source of truth — no regeneration
      needed unless the art is being tweaked.
- [ ] `internal/logo/logo.go`: package `logo`.
  - `type Mode int { ModeImage, ModeBraille, ModeBlock, ModeOff }`.
  - `Detect() Mode` — read `PORTATO_LOGO` (auto/image/braille/block/off); auto
    = `TERM_PROGRAM in {iTerm.app, WezTerm}` -> Image, else `GOOS==windows` ->
    Block, else Braille.
  - `EmojiEnabled() bool` — `PORTATO_LOGO_EMOJI` override; default true on
    darwin, false elsewhere; false when `PORTATO_LOGO=off`.
  - `Render(mode Mode, accent lipgloss.Style, mono bool) string` — returns the
    splash string for the active mode: OSC 1337 (Image), embedded braille txt
    tinted with accent unless mono (Braille), embedded block txt likewise
    (Block), "" (Off).
  - `Banner(accent, mono) string` — convenience = `Render(Detect(), …)` for the
    TUI; plus `VersionBanner(version, commit, date)` for `--version` (the
    28x12 logo + a version line).
  - `go:embed` the two txt + `logo.png`.
- [ ] OSC 1337 writer (in `logo.go` or `logo_image.go`): build
      `ESC ]1337;File=inline=1;width=28cells;height=12cells;preserveAspectRatio=1:<base64(png)> BEL`
      (base64 of the embedded PNG bytes; size in cells so the PNG occupies the
      same footprint as the ASCII variant).
- [ ] `internal/tui/view.go`:
  - `header()`: when `logo.EmojiEnabled()`, prefix the title with the potato
    emoji + space (account for the 2-cell width in `joinRight` —
    `lipgloss.Width` already returns 2 on darwin).
  - `table()` empty-state: when `len(m.list)==0` and `m.height >= splashMinH`,
    render `centered(logo.Render(...))` + the hint line below; else the current
    hint-only line.
  - `helpBlock()`: prepend the 28x12 logo (same height gate) above the hotkey
    list, centered within the panel width.
- [ ] `internal/cmd/version.go`: print `logo.VersionBanner(...)` (the 28x12
      logo via the detected mode) before the version line. Keep it usable in a
      pipe (no ANSI when stdout is not a TTY -> braille/emoji only, no colour,
      no inline image).
- [ ] Tests:
  - `internal/logo/logo_test.go`: embed round-trip; `Detect()` matrix
    (PORTATO_LOGO each value x TERM_PROGRAM x GOOS); `Render` returns non-empty
    for Image/Braille/Block and "" for Off; OSC 1337 sequence well-formed
    (prefix + base64 + BEL); tint applied unless mono.
  - `internal/tui` tests: empty-list renders the logo; help renders the logo;
    `PORTATO_LOGO=off` hides both; header shows the emoji on darwin, plain on
    linux (gate via the logo pkg, tested directly); small height -> hint only.
  - `internal/cmd` test: `--version` output contains the version string and,
    on a TTY, the logo banner.
- [ ] Docs: SPEC §11 (TUI) — add a "Branding / logo" subsection; §3 — note
      `--version` banner; ROADMAP post-MVP table — add phase 24 row.

## Definition of Done

- [ ] Empty-list splash shows the potato (PNG in iTerm2 with `PORTATO_LOGO=image`,
      braille in Terminal.app/Linux, block under `PORTATO_LOGO=block`).
- [ ] Help (`?`) overlay shows the compact logo at the top.
- [ ] `portato --version` prints the logo banner + version/commit/date.
- [ ] Header shows the emoji 🥔 before "Portato" on darwin; plain text on
      linux/windows; `PORTATO_LOGO_EMOJI=off` hides it; `PORTATO_LOGO=off`
      hides everything.
- [ ] Small terminal (height < ~18): big logo is omitted, hint still shows; no
      layout breakage.
- [ ] mono / `NO_COLOR`: ASCII logo renders without colour tint.
- [ ] The non-empty working list renders identically to pre-phase (logo only in
      empty/help/--version/header-mark — no regression on the main screen).
- [ ] Binary works without `assets/` on disk (everything embedded).
- [ ] `go vet ./...`, `gofmt -l .`, `go test ./...` clean.

## Verification

```sh
# braille (default on linux/Terminal.app):
PORTATO_LOGO=braille ./bin/portato        # empty list -> centered potato
# force the PNG path (even outside iTerm2, to test the OSC):
PORTATO_LOGO=image ./bin/portato
# block (legacy Windows look):
PORTATO_LOGO=block ./bin/portato
# off — plain, no branding:
PORTATO_LOGO=off ./bin/portato
# macOS header emoji + linux plain:
./bin/portato                              # darwin: "🥔 Portato — …"
PORTATO_LOGO_EMOJI=off ./bin/portato       # darwin, forced plain
# version banner:
./bin/portato --version
# in iTerm2: empty list + ? help show the real PNG potato
```

## Technical details

- **OSC 1337 (iTerm2 inline image):**
  `"\x1b]1337;File=inline=1;width=28cells;height=12cells;preserveAspectRatio=1:" + base64(pngBytes) + "\x07"`.
  Modern iTerm2 + WezTerm honour `Ncells` units; if an older iTerm2 mis-sizes,
  fall back to `width=NNpx` derived from the cell size (not needed for the
  target environment). bubbletea passes raw OSC through in the `View()` string.
- **Inline-image redraw stability:** the empty-list and help frames are static
  (logo at fixed cells), so re-emitting the OSC each frame is stable in iTerm2
  (it caches same-size images at the same position). The header never uses PNG
  (emoji only) — avoids flicker on every redraw. If any flicker appears in
  practice, render the image once on entering the state and emit only cursor
  positioning afterward (deferred refinement).
- **Emoji width:** `lipgloss.Width("🥔 ")` = 3 on darwin (2 + space); bake into
  `joinRight`'s budget so the right-aligned `mode:` does not drift. On
  non-darwin the emoji is absent, so no width issue.
- **No-cycle constraint:** `internal/logo` imports only `embed`, lipgloss, os,
  runtime — no portato packages. Both `tui` and `cmd` import it safely.
- **`--version` in a pipe:** detect a TTY on stdout (e.g. `golang.org/x/term`
  or an `os.Stdout.Stat()` mode check); if not a TTY, skip colour AND skip the
  inline image (emit the braille/plain banner) so `portato --version | head`
  stays clean.

## Out of scope

- kitty graphics protocol, sixel (Linux fragmentation; revisit on demand).
- A startup splash screen (decided against — the tool must open instantly).
- A second, larger empty-state-only logo size (single 28x12 size kept for
  minimalism; can add later).
- Tinting the inline PNG (it renders with its own brown fill; acceptable).
- Windows Terminal sixel support (defer with the rest of Phase 17).

## Commit plan

1. `docs(phase-24): start` — flip frontmatter todo->in-progress + ROADMAP row `[ ]->[~]`.
2. `chore(assets): move logo assets under internal/logo; trim to minimal set` — mv assets -> internal/logo/assets; rename outline-braille -> logo.braille.txt + solid-block -> logo-block.txt; rm logo-solid.png + the filled/outline-source duplicates (no regeneration needed).
3. `feat(logo): detect + render cascade (image/braille/block) with go:embed` — new `internal/logo` package + tests.
4. `feat(tui): logo in empty-list splash and help overlay` — view.go wiring + height gate + tests.
5. `feat(cmd): --version logo banner` — version.go + pipe-safe handling + test.
6. `feat(tui): header emoji (darwin) + PORTATO_LOGO/PORTATO_LOGO_EMOJI overrides` — header wiring + tests.
7. `docs(spec): branding/logo subsection + --version note`.
8. `docs(phase-24): complete` — DoD checklist + `[~]->[x]`.

## Start guard

This phase is `status: todo`. It starts only on an explicit **"start phase 24"**
command (per docs/CONVENTIONS.md). The first action then is to flip the
frontmatter + ROADMAP (commit 1) — not before.
