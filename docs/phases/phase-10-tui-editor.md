---
phase: 10
title: TUI tunnel editor (e/n/d)
status: todo
depends_on: [6]
---

> Outline phase. Details to be filled in when work begins.

## Goal

Fully manage the configuration from the TUI: create, edit, and delete tunnels
without manually editing YAML. Changes are persisted to disk and applied (to a
running daemon or in-process).

## Scope (preliminary)

- `e` — edit form for the selected tunnel (all fields).
- `n` — new tunnel (empty form).
- `d` — delete with confirmation.
- Field validation directly in the form (error highlighting).
- Saving to YAML on disk.
- After saving — `Reload()` the config and apply the changes (new/deleted tunnels appear/disappear; running ones are not reset unless necessary).
- Sensitive fields (identity) — manual path entry with `~` expansion. No passwords in the form.

## Tasks (candidates)

- [ ] `internal/tui/editor.go` — a separate bubbletea Model/screen/form with text fields (`bubbles/textinput`) and a type selector list (`local` | `remote` | `dynamic`).
- [ ] Navigation: `Tab`/`Shift+Tab` between fields, `Ctrl+S` to save, `Esc` to cancel.
- [ ] Validation: name (required, unique, alphanumeric+dash), ssh (required, `user@host:port` format), local/remote (port or host:port).
- [ ] Highlight invalid fields in red + a message below the form.
- [ ] `internal/config/save.go` — saving while preserving structure. Decision regarding comments:
  - Option 1: Marshal from the struct (comments are lost). Simpler.
  - Option 2: editing via `yaml.Node` AST (comments are preserved, code is more complex).
  - Recommendation: start with (1), switch to (2) if users lose important comments.
- [ ] Integration: `controller.Reload()` after saving; works the same in-process and in the daemon (via HTTP `POST /reload`).
- [ ] Deletion: confirmation screen; removal from the config + `Disable(name)` if active.
- [ ] `n` — empty form, saved only on `Ctrl+S` (cancel with `Esc`).

## Definition of Done

- [ ] `e` opens a form with all tunnel fields, pre-filled with the current values.
- [ ] `n` opens an empty form for a new tunnel.
- [ ] All fields are editable; the type is selected from a list (local/remote/dynamic).
- [ ] Invalid values are highlighted; saving is not possible until fixed.
- [ ] `Ctrl+S` saves changes to YAML; the list in the TUI is updated.
- [ ] `Esc` cancels editing without saving.
- [ ] `d` deletes the tunnel after confirmation; an active tunnel is correctly stopped.
- [ ] The config file remains valid YAML after any operations (`portato list` works after editing).
- [ ] Editing works both standalone and in attach (via `Reload`).
- [ ] `go vet`, `gofmt` are clean.

## Technical details (preliminary)

- `bubbles/textinput` for text fields; for the type selection — a custom selector or `bubbles/list` with a single choice.
- The form is a separate `tea.Model`, returning a result (saved/cancelled) to the main screen.
- Identity file: manual path entry with `~` expansion; a file dialog is optional via a third-party library (not a blocker).
- Passwords are NOT added to the form — only identity/agent.
- In daemon mode the form calls `controller.Reload()` → POST `/reload` on the daemon. For standalone — `localController.Reload()` directly in-process.

## Open questions

- Preserving comments in YAML (option 1 vs 2) — to be decided based on usage results.
- Do we need a file dialog for identity, or is manual path entry sufficient?
