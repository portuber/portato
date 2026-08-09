---
phase: 46
title: "Tunnel tags / groups"
status: done
depends_on: []
---

## Goal

Let users organise tubers with `tags:` and operate on them as a group:
`enable` / `disable` / `restart --tag X`, a precise `#tag` filter in the
TUI, and `a` / `x` (enable-all / disable-all) that respect the active `/`
filter — instant group operations without a new schema. Tags also surface
in `list` / `list --json` and the TUI, so a fleet of tunnels can be sliced
by environment (`prod`, `staging`), role (`db`, `web`), or owner.

## Background

Today every tuber is addressed by its unique `name`. With more than a
handful of tunnels, naming conventions (`db-prod-1`, `web-staging-canary`)
start to encode what is really grouping metadata. Phase 13's `/` list
filter already narrows the TUI by a fuzzy query over name/type/endpoint,
and `a` / `x` toggle every tuber (`enableAll` / `disableAll` in
`internal/tui/update.go` iterate `m.ctrl.List()` — the whole list). This
phase adds a first-class `tags:` field and makes the existing filter +
bulk-toggle machinery tag-aware, so a filtered view becomes an ad-hoc
group with no new grouping concept to learn.

Tag values reuse the existing `validName` alphabet (`[a-zA-Z0-9-_]`,
`internal/config/config.go`) — the same rules as tuber names — so tags are
shell-safe, completion-friendly, and consistent with the rest of the config.

## Tasks

- [x] **Config** (`internal/config`): add `Tags []string
      \`yaml:"tags,omitempty" json:"tags,omitempty"\`` to `Tuber`.
      `Validate()` — every tag must pass `validName`, be non-empty, and
      unique within the tuber (dedup, case-sensitive); cap at ≤16 tags per
      tuber and ≤32 chars per tag. Add a tagged tuber to `exampleConfig()`
      and `config.example.yaml`.
- [x] **State carries tags** (`internal/forward/state.go` + controller):
      add `Tags []string \`json:"tags,omitempty"\`` to the State struct,
      populated from `config.Tuber.Tags`, so the TUI, `list`, and
      `list --json` can show and filter on tags over IPC.
- [x] **CLI `--tag`** on `enable` / `disable` / `restart`
      (`internal/cmd`): change `Args: cobra.ExactArgs(1)` →
      `cobra.MaximumNArgs(1)` and add a `--tag string` flag. `--tag X`
      resolves every tuber whose `Tags` contain `X` and calls
      Enable/Disable/Restart on each, printing one line per tuber; a name
      positional keeps the current single-tuber behaviour. Exactly one of
      `--tag` / `<name>` is required (both or neither ⇒ error). `forward`
      is intentionally excluded (it is ad-hoc, not daemon state).
- [x] **`--tag` completion**: `RegisterFlagCompletionFunc("tag", ...)`
      returning the distinct tag values from `config.yaml` (reusing the
      Phase-45 file-read helper pattern — not `config.Load`).
- [x] **TUI `#tag` filter** (`internal/tui`): extend `matches()` so a
      filter query with a leading `#` matches tubers whose `Tags` contain
      the value after `#` (exact, case-insensitive). Non-`#` queries keep
      the current fuzzy-over-name/type/endpoint behaviour. Update
      `bindings.go` help + footer hint to mention `#tag`.
- [x] **`a` / `x` respect the filter** (`internal/tui/update.go`:
      `enableAll` / `disableAll`): gate each candidate on `m.matches(s)`
      instead of iterating all tubers unconditionally. With no active
      filter every tuber matches (behaviour unchanged); with a filter only
      the visible tubers toggle — turning any filtered view (incl. `#tag`)
      into a group operation.
- [x] **Editor** (`internal/tui/editor.go`): add a comma-separated `tags`
      input, parse to `[]string` (trim, validate each via the same rules,
      dedup), and carry it through the `Tuber` rebuild on save — the same
      per-field carry-through added for `jump` / `identity` in Phase 43 (no
      silent wipe of advanced fields).
- [x] **Display**: `list` shows tags compactly; `list --json` includes the
      `tags` array. In the TUI, v1 shows tags in the selected row's detail
      strip (Phase 39) rather than a new column — keeps the Phase-38
      responsive layout intact; a width-aware TAGS column is a possible
      later refinement.
- [x] **Docs**: README (a tags example + `--tag` + the `#tag` filter note),
      SPEC (the `tags:` field + semantics + precedence/limits),
      `config.example.yaml`, and command help text.
- [x] **Tests**: config validation (valid/invalid tags, dedup, length +
      count caps); `--tag` CLI (multi-tuber enable/disable/restart, both
      and neither ⇒ error, one line per tuber); `--tag` completion returns
      distinct values; TUI `#tag` filter narrows correctly (`#db` matches a
      tagged tuber, not one merely named `db-stage`); `a` / `x` with and
      without an active filter (regression: no-filter byte-identical to
      today); editor tags input + carry-through (no silent wipe); State
      carries tags over the IPC round-trip; `list --json` `tags` field.

## Definition of Done

- [x] `tags:` parses and validates (`validName` per tag, dedup, ≤16/≤32
      caps); the example config demonstrates it.
- [x] `portato enable|disable|restart --tag X` operates on every matching
      tuber; `<name>` still works; both/neither is rejected. `forward` is
      unchanged.
- [x] `portato enable|disable|restart --tag <TAB>` completes distinct tag
      values from `config.yaml`.
- [x] The TUI `#tag` filter narrows to tagged tubers; `a` / `x` respect the
      active `/` filter (no filter ⇒ unchanged behaviour).
- [x] The editor edits/creates tags and carries them through (no silent
      wipe of tags on save).
- [x] `list` and `list --json` show tags; `controller.State` carries tags
      over IPC.
- [x] README + SPEC + `config.example.yaml` document tags, `--tag`, and
      `#tag`.
- [x] `go build ./...`, `gofmt -l .`, `go vet ./...`, `golangci-lint run
      ./...` clean; new functions under gocyclo 15; the phase's tests green.
- [x] ROADMAP row + status mirrored (phase file frontmatter + table).

## Verification

```sh
make fmt && make vet && make test && make lint

# config:
#   - name: db-prod
#     tags: [prod, db]
#     ...

# CLI group op:
#   portato enable --tag prod     # enables every prod tuber

# TUI:
#   / # prod   <Enter>            # narrows to prod tubers
#   a                              # enables only the visible (prod) tubers
```

## Technical details (sketch)

- **Filter syntax — `#tag` exact.** A leading `#` is the tag selector
  (precise: `#db` matches a tuber tagged `db`, not one named `db-stage`).
  Plain queries stay fuzzy over name/type/endpoint. (Tags participating in
  the fuzzy match too is a possible later refinement; v1 keeps the two
  modes distinct so a tag never collides with a name.)
- **`a` / `x` gating.** `enableAll` / `disableAll` switch from iterating
  `m.ctrl.List()` unconditionally to gating each candidate on
  `m.matches(s)`. `matches` returns true for every tuber when no filter is
  active, so the common case is unchanged — the win is that a filtered view
  (incl. `#tag`) becomes an ad-hoc group with no new keybinding.
- **Tags flow config → State.** `controller.State` (not just
  `config.Tuber`) gains a `Tags` field so the TUI can filter on the live
  state rather than re-reading config, and so `list --json` reports tags
  uniformly. The json tag keeps it out of any non-tag-aware consumer's
  way (`omitempty`).
- **Editor carry-through.** The editor rebuilds a `Tuber` from its input
  fields on save; `tags` joins the per-field carry-through already in place
  for `jump` / `identity` (Phase 43), so editing another field never wipes
  tags.
- **`--tag` source.** Resolve tagged tubers from the loaded config (the
  daemon already holds it); completion reads `config.yaml` directly via the
  Phase-45 file-read pattern (not `config.Load`, which has TAB-unfriendly
  side effects).
- **Responsive layout.** No new TUI column in v1 — tags live in the
  selected-row detail strip (Phase 39), so the Phase-38 column shrinking
  behaviour and narrow-terminal footprints are untouched.
- **No breaking change.** `tags:` is optional (`omitempty`); existing
  configs and CLI invocations behave identically. Additive ⇒ MINOR.

### Notes from implementation

- **`tuberRaw` / `UnmarshalYAML` carry-through.** `Tuber` has a custom
  `UnmarshalYAML` that decodes into a private `tuberRaw` and copies fields
  explicitly. `Tags` had to be added to `tuberRaw` *and* the copy, or it would
  be silently dropped on load — the same pitfall Phase 43 hit with `jump:`.
  Covered by a load round-trip test (`TestLoadWithTags`).
- **IPC without a new method.** Tags ride the existing `forward.Status`
  (populated in `Tuber.Status()`), so `daemon.handleList` → `Engine.List` →
  `Tuber.Status()` carries them to `list` / `list --json` / the TUI with no
  daemon or client change. `--tag` resolution reuses `Client().List()` (the
  authoritative roster) rather than re-reading config.
- **Display invariant.** Every tag renders as `#tag` (no space after `#`),
  space-separated, everywhere — the TUI detail strip *and* the CLI `list`
  table (inline in the NAME cell). The display token is byte-identical to the
  `#tag` filter token, so the displayed value teaches the filter syntax.
- **Strip stays ≤1 line.** In the TUI, an error wins the Phase-39 detail strip
  (error is actionable, tags are identity); tags surface only when the
  selected row has tags *and* no error. The strip never toggles between one
  and two lines, so the table height is stable (no layout jitter).
- **`Validate` split.** Adding `validateTags` pushed `Config.Validate` to
  gocyclo 16 (>15 repo limit); the per-tuber body was extracted into
  `validateTuber` to bring both under the cap.
- **`tuberChanged` carry-through (fixed in v1.4.1).** `Engine.Reload`
  reconfigures a tuber only when `tuberChanged` reports a field change, and
  that comparison predated the `Tags` field — so editing *only* a tuber's tags
  (via the editor) was not detected, `Reconfigure` was skipped, and the
  running tuber's `cfg.Tags` stayed empty: `Status().Tags` was empty, so the
  `#tag` filter and `list` missed the live-edited tag (the file was correct,
  the running engine was not). `Tags` — plus `Jump` and
  `Socks5User`/`Socks5Password`, which had the same gap from their phases —
  were added to `tuberChanged` in v1.4.1. The same explicit-field-enumeration
  pitfall as the `tuberRaw` note above.
