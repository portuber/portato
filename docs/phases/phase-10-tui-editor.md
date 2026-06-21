---
phase: 10
title: TUI tunnel editor (e/n/d)
status: in-progress
depends_on: [6]
---

## Goal

Fully manage the configuration from the TUI: create, edit, and delete tunnels
without manually editing YAML. Changes are persisted to disk and applied (to a
running daemon or in-process), in both standalone and attach modes.

## Design decisions (locked at phase start)

1. **Daemon owns config I/O.** Config read/writes go through the Controller
   abstraction and the daemon's REST API — the TUI never writes the YAML file
   directly. This makes standalone and attach behave identically, handles a
   custom `--config` path on the daemon correctly, and is race-safe across
   multiple attached TUIs. It also fixes the attach-mode gap (the
   `remoteController` had neither `cfgPath` nor any way to read the current
   config to prefill an edit form).
2. **Per-tunnel REST operations**, not a whole-config replace. Each operation
   maps to a single `yaml.Node` edit, which keeps comments on every untouched
   tunnel / `defaults:` / top-level intact.
3. **Comment-preserving save** via `yaml.Node` AST patching (yaml.v3 is already
   a dependency). Semantics: editing a tunnel rewrites *that tunnel's* block
   (its own inline comments are not preserved); comments on all other tunnels
   and on `defaults:` survive. Add = append node; delete = remove node.
4. **`charm.land/bubbles/v2`** is added as a dependency for `textinput`. The
   type selector is a 3-value cycler (no `bubbles/list`).

## Scope

- `e` — edit form for the selected tunnel (all editable fields, prefilled).
- `n` — new tunnel (empty form).
- `d` — delete with confirmation.
- Field validation directly in the form (per-field red highlight + message).
- Saving to YAML on disk (comment-preserving) and applying the change via
  `Reload()` (new tunnels appear, deleted tunnels disappear and are stopped,
  changed tunnels restart only if their connection-affecting fields changed).
- Identity — manual path entry. No passwords in the form (agent / identity only).

## Architecture

### `internal/config/patch.go` (new) — comment-preserving AST edits

Shared by `localController` and the daemon (the two config owners). Operates on
the file's `yaml.Node` tree so comments survive:

- `LoadNode(path) (*yaml.Node, error)` — read file → document node.
- `(*documentNode) AddTunnel(t Tunnel) error` — append a marshaled tunnel node
  to the `tunnels:` sequence (create the sequence if absent).
- `(*documentNode) ReplaceTunnel(name string, t Tunnel) error` — find the
  sequence element whose `name` == `name`, replace it with a freshly-marshaled
  node (allows rename; error if not found).
- `(*documentNode) DeleteTunnel(name string) error` — remove the matching
  element (error if not found).
- `SaveNode(path, *yaml.Node) error` — encode + write (mode 0600).

Authority validation reuses the existing `Config.Validate()` / `prepare()` —
no new validation rules. The flow is: build the prospective in-memory `*Config`,
`prepare()` + `Validate()`; only on success, AST-patch the file.

### `internal/controller/controller.go` — interface extension

```go
type Controller interface {
    // ...existing...
    Config() (*config.Config, error)                    // deep copy; prefill + uniqueness
    AddTunnel(t config.Tunnel) error
    UpdateTunnel(name string, t config.Tunnel) error    // name = original; allows rename
    DeleteTunnel(name string) error
}
```

- `Local.Config()` returns a clone of `l.cfg`. `Add/Update/Delete` build the
  prospective config, `prepare()+Validate()`, AST-patch `l.cfgPath`, then reuse
  `Reload()` (load → `engine.Reload` → swap).
- `Remote.*` delegate to the new client methods.
- The test `fakeCtrl` is extended with all four methods.

### `internal/daemon/server.go` — new endpoints

| Method | Path | Action |
|--------|------|--------|
| `GET` | `/config` | JSON of the current `s.cfg` |
| `POST` | `/tunnels` (body `Tunnel`) | validate, AST-patch, reload (409 if name exists) |
| `PUT` | `/tunnels/{name}` (body `Tunnel`) | validate (exclude `{name}`), patch, reload (404 if absent; rename allowed) |
| `DELETE` | `/tunnels/{name}` | validate remaining, patch, reload (404 if absent) |

A shared `s.applyReload()` (load → `engine.Reload` → swap `s.cfg`) is factored
out of `handleReload` and reused by the three new mutation handlers. A
validation error → 400 with a message and the file is left untouched.

### `internal/client/client.go`

New methods `Config()`, `AddTunnel()`, `UpdateTunnel()`, `DeleteTunnel()`.

### `internal/tui/editor.go` (new) — `tunnelEditor` sub-model

The first sub-model in the TUI. Held by the main `Model` as `m.editor
*tunnelEditor` (nil when inactive):

- Fields: `name`, `type` (cycler over local/remote/dynamic), `ssh`, `local`,
  `remote` (unused for dynamic), `identity` (optional). `textinput` for text.
- Keys: `Tab`/`Shift+Tab` cycle focus; `←`/`→` cycle type; `Ctrl+S` save;
  `Esc` cancel; `Enter` = next field.
- Client-side validation → per-field red highlight + a status line; on
  `Ctrl+S` it calls the passed-in `ctrl.AddTunnel`/`UpdateTunnel`, closes only
  on success, surfaces server errors in the status line.
- Receives the existing-name list (for uniqueness, excluding the original) +
  the `original` name + mode (edit/new).

### Main TUI (`model.go`, `update.go`, `view.go`, `styles.go`)

- `Model`: add `editor *tunnelEditor`, `confirmDelete bool`, `deleteTarget string`.
- `Update`: route `WindowSizeMsg` + `KeyPressMsg` to the editor when active; on
  its `done` flag, clear it.
- `handleKey`: add `e` (fetch raw tunnel via `ctrl.Config()`, open editor),
  `n` (new), `d` (set confirmDelete) — only when no editor/modal is active.
- Delete confirm modal (`y`→`ctrl.DeleteTunnel`, `n`/`enter`/`esc`→cancel),
  reusing `modalStyle`.
- Refresh after save/delete flows through the existing Phase-9 `Changes()`
  channel (no manual list refresh).
- Update the footer (`view.go:167`) + help block (`view.go:170-185`).

## Tasks

- [ ] `docs(phase-10): start` — flip status, detail this file.
- [ ] Add `charm.land/bubbles/v2`; verify `textinput` v2 path/API; `make build`.
- [ ] `internal/config/patch.go` + `patch_test.go` (AST add/update/delete, comment preservation, validate-before-patch).
- [ ] Controller interface + `Local`/`Remote` impls + `fakeCtrl`; tests.
- [ ] Daemon `GET /config`, `POST/PUT/DELETE /tunnels`, `applyReload` refactor; `server_test.go`.
- [ ] Client methods; tests.
- [ ] `tui/editor.go` sub-model + `editor_test.go`.
- [ ] Main model routing + `e/n/d` keys + delete modal + footer/help; `model_test.go`.
- [ ] `make fmt && make vet && go build ./... && go test ./...` clean.
- [ ] `docs(phase-10): complete`.

## Definition of Done

- [ ] `e` opens a form with all tunnel fields, pre-filled with the current values.
- [ ] `n` opens an empty form for a new tunnel.
- [ ] All fields are editable; the type is selected from a list (local/remote/dynamic).
- [ ] Invalid values are highlighted; saving is not possible until fixed.
- [ ] `Ctrl+S` saves changes to YAML; the list in the TUI is updated.
- [ ] `Esc` cancels editing without saving.
- [ ] `d` deletes the tunnel after confirmation; an active tunnel is correctly stopped.
- [ ] The config file remains valid YAML after any operations (`portato list` works after editing).
- [ ] Comments on untouched tunnels / `defaults:` survive a save.
- [ ] Editing works both standalone and in attach.
- [ ] `go vet`, `gofmt` are clean; `go test ./...` is green.

## Verification

```
make fmt && make vet && make build && go test ./...
```

Manual smoke (standalone via `portato`, attach via `portato attach`):
- `n` → create a tunnel → `Ctrl+S` → it appears in the list and in `portato list`.
- `e` → edit fields / rename → `Ctrl+S` → list + `portato list` reflect it.
- `d` → confirm → tunnel gone; if it was active, the port is released.
- A config that has comments keeps them on untouched tunnels after an edit.
- Same flows work in attach mode against a running daemon.

## Technical notes / risks

- **`bubbles/v2` textinput** is less documented than v1. If the v2 API proves
  unusable, fallback is hand-rolled text fields (more work, no dep). To be
  verified first.
- **Rename = restart.** Renaming a live tunnel stops it and starts it under the
  new name (`engine.Reload` sees the old name gone, a new one present). This is
  expected and noted in the commit.
- An **edited tunnel's own inline comments are rewritten**, not preserved —
  inherent to per-node patching; comments elsewhere survive.

## Open questions

- File dialog for identity vs. manual path entry? → **manual path entry** for
  this phase (a file dialog is optional and not a blocker).
