---
phase: 14
title: TUI: duplicate the selected tunnel (Shift+C)
status: todo
depends_on: [10]
---

> Post-MVP convenience. Add a "duplicate the tunnel under the cursor" action to
> the TUI list, reusing the Phase 10 editor machinery verbatim — no controller,
> daemon, client, or config changes.

## Goal

Pressing `C` (Shift+C) on the selected tunnel opens the tunnel editor in
**create mode**, prefilled with the source's fields under a fresh unique name
(`<name>-copy`). Saving appends a new tunnel via `Controller.AddTunnel` (not
`UpdateTunnel`), so the original is untouched. The common use case — "same SSH
host, a second local port" — becomes two keystrokes plus a small edit.

## Background

Phase 10 introduced the in-TUI tunnel editor (`e` edit / `n` new / `d` delete).
Its constructor `newTunnelEditor(mode, t, existing, ctrl)` already prefills a
form from any `config.Tunnel`, and `trySave` commits via `AddTunnel` when
`mode == modeNew`. So "duplicate" is exactly "open the editor in create mode,
prefilled from the selected tunnel, with a fresh name" — the whole pipeline
(validation, name uniqueness, comment-preserving persist, list refresh through
the Phase 9 `Changes()` broker) is reused unchanged.

The hotkey is `C` (capital), not `c`: duplicating is non-destructive (unlike
`d` delete, which is guarded by a `y/N` modal), but it opens a form — guarding
it behind Shift avoids an accidental keypress interrupting the session. Lowercase
`c` is intentionally left unbound.

## Tasks

- [ ] `internal/tui/update.go`: a `case "C":` in `handleKey` (next to the `d`
      case), guarded by `m.hasCurrent()` → `openDuplicateEditor(m.ctrl,
      m.list[m.cursor].Name, m.width, m.height)`. Lowercase `c` stays unbound.
- [ ] `internal/tui/update.go`: `openDuplicateEditor(ctrl, selected, w, h)`
      next to `openEditor` — fetch `ctrl.Config()`, collect existing names,
      find the source tunnel, set `src.Name = freshName(selected, names)` and
      `src.Enabled = false`, then `newTunnelEditor(modeNew, src, names, ctrl)`;
      set `e.original = ""` so the modeNew uniqueness check is clean, and
      `e.width/height = w/h`; return `(e, e.setFocus(fName))`.
- [ ] `freshName(base, existing) string` helper (`update.go` or `editor.go`):
      `db` → `db-copy` → `db-copy-2` → `db-copy-3` … (satisfies
      `validEditorName` `[a-zA-Z0-9_-]`).
- [ ] `internal/tui/view.go`: `footer()` adds `· C duplicate`; `helpBlock()`
      adds `C            duplicate the selected tunnel` after the `n …` line.
- [ ] `internal/tui/model_test.go`: `TestModel_DuplicateKeyOpensEditor`
      (mirror of `TestModel_EditKeyOpensEditor`) — `C` opens the editor,
      `mode == modeNew`, `original == ""`, the name field equals `<src>-copy`,
      and ssh/local/remote/type/identity equal the source's.

## Definition of Done

- [ ] On the tunnel under the cursor, `C` opens the editor prefilled with all
      of the source's fields, in create mode, focus on Name, name
      `<src>-copy`.
- [ ] `ctrl+s` persists the duplicate via `AddTunnel` (not `UpdateTunnel`); the
      new tunnel appears in the list without a manual refresh; the original is
      unchanged.
- [ ] The duplicate is created `enabled: false`; enabling it fails predictably
      if its `local` port collides with the source's (the user is expected to
      change it in the form).
- [ ] Lowercase `c` does nothing.
- [ ] `portato list` and the attach-mode TUI show the duplicate identically to
      any other tunnel (no IPC/controller/daemon change).
- [ ] `go build ./...`, `gofmt -l .`, `go vet ./...`, `go test ./...` are clean.

## Verification

1. `portato` (standalone) with ≥1 tunnel → move to it, press `C` → the editor
   opens with Name `<tunnel>-copy` and ssh/local/remote/type/identity matching
   the source, Enabled off.
2. Rename it and change `local`, `ctrl+s` → the list now shows both tunnels;
   the original is untouched.
3. `c` (lowercase) → nothing happens.
4. Duplicate the same source twice → the generated names are `<tunnel>-copy`
   then `<tunnel>-copy-2`.

## Technical details

- Reuses the Phase 10 editor (`newTunnelEditor` / `openEditor` / `trySave`) and
  `Controller.AddTunnel`; no controller, daemon, client, or config changes.
- `modeNew` is essential: it makes `trySave` call `AddTunnel` (a real new
  tunnel) rather than `UpdateTunnel` (which would overwrite the source).
- `Enabled = false` mirrors the `n` (new) convention and avoids an immediate
  local-port bind conflict on a verbatim duplicate.
- The `local`-port collision is deliberately NOT auto-resolved: a duplicate is
  usually "same host, different local port", but guessing an increment is
  brittle for `host:port` forms and for `-R`/`-D` types; the editor + validator
  surface the conflict instead. (Accepted trade-off: the user edits the port.)
- Name scheme `<name>-copy`[-N] is explicit and matches `validEditorName`.
- Total surface: ~1 switch case, ~1 ~12-line opener, ~1 ~15-line `freshName`
  helper, 2 doc-string edits, 1 test.

## Open questions (resolved)

- Hotkey `C` vs `c`? → **`C` (Shift+C)**; `c` left unbound as an accidental-press
  guard.
- Auto-increment the duplicate's `local` port? → **no**; leave to the user
  (brittle for `host:port` and non-local types).
- Name suffix `-copy` vs Finder-style `-2`? → **`-copy`** (explicit).
