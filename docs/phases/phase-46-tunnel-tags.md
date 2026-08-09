---
phase: 46
title: "Tunnel tags / groups"
status: in-progress
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

- [ ] **Config** (`internal/config`): add `Tags []string
      \`yaml:"tags,omitempty" json:"tags,omitempty"\`` to `Tuber`.
      `Validate()` — every tag must pass `validName`, be non-empty, and
      unique within the tuber (dedup, case-sensitive); cap at ≤16 tags per
      tuber and ≤32 chars per tag. Add a tagged tuber to `exampleConfig()`
      and `config.example.yaml`.
- [ ] **State carries tags** (`internal/forward/state.go` + controller):
      add `Tags []string \`json:"tags,omitempty"\`` to the State struct,
      populated from `config.Tuber.Tags`, so the TUI, `list`, and
      `list --json` can show and filter on tags over IPC.
- [ ] **CLI `--tag`** on `enable` / `disable` / `restart`
      (`internal/cmd`): change `Args: cobra.ExactArgs(1)` →
      `cobra.MaximumNArgs(1)` and add a `--tag string` flag. `--tag X`
      resolves every tuber whose `Tags` contain `X` and calls
      Enable/Disable/Restart on each, printing one line per tuber; a name
      positional keeps the current single-tuber behaviour. Exactly one of
      `--tag` / `<name>` is required (both or neither ⇒ error). `forward`
      is intentionally excluded (it is ad-hoc, not daemon state).
- [ ] **`--tag` completion**: `RegisterFlagCompletionFunc("tag", ...)`
      returning the distinct tag values from `config.yaml` (reusing the
      Phase-45 file-read helper pattern — not `config.Load`).
- [ ] **TUI `#tag` filter** (`internal/tui`): extend `matches()` so a
      filter query with a leading `#` matches tubers whose `Tags` contain
      the value after `#` (exact, case-insensitive). Non-`#` queries keep
      the current fuzzy-over-name/type/endpoint behaviour. Update
      `bindings.go` help + footer hint to mention `#tag`.
- [ ] **`a` / `x` respect the filter** (`internal/tui/update.go`:
      `enableAll` / `disableAll`): gate each candidate on `m.matches(s)`
      instead of iterating all tubers unconditionally. With no active
      filter every tuber matches (behaviour unchanged); with a filter only
      the visible tubers toggle — turning any filtered view (incl. `#tag`)
      into a group operation.
- [ ] **Editor** (`internal/tui/editor.go`): add a comma-separated `tags`
      input, parse to `[]string` (trim, validate each via the same rules,
      dedup), and carry it through the `Tuber` rebuild on save — the same
      per-field carry-through added for `jump` / `identity` in Phase 43 (no
      silent wipe of advanced fields).
- [ ] **Display**: `list` shows tags compactly; `list --json` includes the
      `tags` array. In the TUI, v1 shows tags in the selected row's detail
      strip (Phase 39) rather than a new column — keeps the Phase-38
      responsive layout intact; a width-aware TAGS column is a possible
      later refinement.
- [ ] **Docs**: README (a tags example + `--tag` + the `#tag` filter note),
      SPEC (the `tags:` field + semantics + precedence/limits),
      `config.example.yaml`, and command help text.
- [ ] **Tests**: config validation (valid/invalid tags, dedup, length +
      count caps); `--tag` CLI (multi-tuber enable/disable/restart, both
      and neither ⇒ error, one line per tuber); `--tag` completion returns
      distinct values; TUI `#tag` filter narrows correctly (`#db` matches a
      tagged tuber, not one merely named `db-stage`); `a` / `x` with and
      without an active filter (regression: no-filter byte-identical to
      today); editor tags input + carry-through (no silent wipe); State
      carries tags over the IPC round-trip; `list --json` `tags` field.

## Definition of Done

- [ ] `tags:` parses and validates (`validName` per tag, dedup, ≤16/≤32
      caps); the example config demonstrates it.
- [ ] `portato enable|disable|restart --tag X` operates on every matching
      tuber; `<name>` still works; both/neither is rejected. `forward` is
      unchanged.
- [ ] `portato enable|disable|restart --tag <TAB>` completes distinct tag
      values from `config.yaml`.
- [ ] The TUI `#tag` filter narrows to tagged tubers; `a` / `x` respect the
      active `/` filter (no filter ⇒ unchanged behaviour).
- [ ] The editor edits/creates tags and carries them through (no silent
      wipe of tags on save).
- [ ] `list` and `list --json` show tags; `controller.State` carries tags
      over IPC.
- [ ] README + SPEC + `config.example.yaml` document tags, `--tag`, and
      `#tag`.
- [ ] `go build ./...`, `gofmt -l .`, `go vet ./...`, `golangci-lint run
      ./...` clean; new functions under gocyclo 15; the phase's tests green.
- [ ] ROADMAP row + status mirrored (phase file frontmatter + table).

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
