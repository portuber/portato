# Phase 15 — Light-theme color tuning

**Status:** [x]
**Dependencies:** Phase 12 ([x])

## Goal

Make the light-theme palette genuinely readable on light terminal
backgrounds. The root cause of the dimness was that most user-facing text in
the TUI was rendered **unstyled** (plain terminal default), which on a light
surface reads as a faint grey. This phase adds a readable `body` text style,
darkens the muted grey, removes the redundant selection band (bold is enough),
and swaps the unreadable lime-green Type-active colour for the app accent.

## Scope

What we do (all colour/contrast tuning; layout untouched):

- `internal/tui/theme.go` — add a `body` palette style; darken `dim` and
  `state[Off]`; remove the `selected` background.
- `internal/tui/styles.go` — expose `bodyStyle`.
- `internal/tui/view.go` — render non-selected row cells (`name/type/endpoint/
  uptime`) with `bodyStyle` instead of plain.
- `internal/tui/logs.go` — render log `time` with `dimStyle`, `msg`+attrs with
  `bodyStyle` (level tags keep their colour).
- `internal/tui/editor.go` — set the textinput `Text` style (focused + blurred)
  to `bodyStyle`; render the active Type value with the accent `cursorStyle`
  instead of the hardcoded lime `"2"`.

What we do NOT do:

- No change to `darkPalette()` or `monoPalette()` colours (a default/no-fg
  `body` is added to keep the field set complete; on dark/mono it is a no-op).
- No change to `detectKind()` / theme auto-detection.
- No change to layout, keybindings, or the selection model (selection stays
  bold + the `❯` cursor glyph — no background band).

## Tasks

- [x] `theme.go` — add `body lipgloss.Style` to `palette`; bake the surface
      background into it via `withBackground`.
- [x] `theme.go` — `lightPalette()`: `body` = `"235"` (#262626, primary text).
- [x] `theme.go` — `lightPalette()`: `dim` and `state[Off]` `245 → 241`
      (#8a8a8a → #626262; tagline, notes, INF/DBG, off status).
- [x] `theme.go` — drop the `selected` background (`153`): selection reverts to
      bold + `235`, no band.
- [x] `styles.go` — `bodyStyle = pal.body`.
- [x] `view.go` — `row()`: wrap non-selected `name/typ/ep/up` in `bodyStyle`.
- [x] `logs.go` — `renderLogs()`: `time` → `dimStyle`, `msg`+attrs → `bodyStyle`.
- [x] `editor.go` — `newInput()`: set `Focused.Text`/`Blurred.Text` to
      `bodyStyle`.
- [x] `editor.go` — `renderType()`: active value via `cursorStyle` (accent),
      not `Foreground("2")`.

### Resulting `lightPalette` colour values

```
title / cursor / editorTitle      "26"   (accent, unchanged)
header / mode / footer            "240"  (unchanged)
dim                               "241"  ← was "245"
body                              "235"  (NEW — primary text)
selected                          bold + "235", no background (band removed)
state[Off]                        "241"  ← was "245"
state[Connecting] / Reconnecting  "166"  (phase-15 brightened orange)
state[Connected]                  "28"   (unchanged)
state[Error]                      "124"  (unchanged)
warn                              "166"  (phase-15)
```

## Definition of Done (DoD)

- [x] Non-selected tunnel rows (name/type/endpoint/uptime) read dark and clear
      on a light terminal, not faint grey.
- [x] Log text (timestamps and messages) is readable on a light terminal.
- [x] Editor input fields show entered text in a readable dark colour (focused
      and blurred).
- [x] The active Type value is the blue accent, not lime green.
- [x] The selected row is marked only by bold text + the `❯` cursor — no blue
      background band.
- [x] The tagline / off status / notes (`dim`, `241`) are visibly darker than
      before but still secondary to body text.
- [x] Dark and monochrome themes show no visible regression.
- [x] `make test && make vet` are green and `gofmt -l .` is empty.

## Verification

```sh
make build
PORTATO_THEME=light ./bin/portato
# list: non-selected rows readable; selected row = bold only (no band);
#       off status / tagline darker than before but secondary.
# open a tunnel (e): input text readable; focus Type → blue accent value.
# open logs (l): timestamps + messages readable.
PORTATO_THEME=dark ./bin/portato   # no regression
NO_COLOR=1 ./bin/portato           # monochrome, unchanged
make test && make vet && test -z "$(gofmt -l .)"
```

## Technical details

- All colours are ANSI-256 codes via `lipgloss.Color`. Hex references:
  `26`=`#005fff`, `235`=`#262626`, `241`=`#626262`, `240`=`#585858`,
  `166`=`#d75f00`, `28`=`#008700`, `124`=`#af0000`.
- `body` has no foreground on dark/mono (a no-op there — default terminal fg);
  on light it is `235`. `withBackground` bakes `#FAFAFA` into it like every
  other light style.
- Selection: `view.go` renders the selected row's cells individually with
  `selectedStyle` (bold + `235`, no background); the `❯` cursor is the only
  other cue. A full-width background band was tried and dropped as redundant.
- `textinput` (bubbles v2) is styled via `Styles()`/`SetStyles` — the entered
  text lives in `Focused.Text` / `Blurred.Text` (the model has no `TextStyle`
  field). Both are set to `bodyStyle` so the value is readable regardless of
  the terminal's default foreground.
- `TestLightPaletteBakesBackground` now also asserts `body` carries the surface
  background and is not faint.
