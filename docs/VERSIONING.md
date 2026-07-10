# Versioning

Portato follows **Semantic Versioning 2.0.0** ([semver.org](https://semver.org)).
Release tags use the `v` prefix: `v0.1.0`, `v1.2.3`. Pre-releases (when used):
`v1.0.0-rc.1`.

## The stability surface

The version number is a contract over the **user-facing** surfaces:

1. **`config.yaml`** — its keys and fields (`defaults`, `tubers`, …) and their
   semantics.
2. **The CLI** — commands and flags (`enable`, `--config`, `--log-level`, …)
   and their semantics.

Explicitly **not** part of the contract (free to change in any release):

- The daemon **IPC** (`/tubers…` routes, JSON shapes) — internal to the binary;
  both sides ship together.
- The **importable Go API** — everything lives under `internal/` (Go enforces
  non-importability); portato is a CLI, not a library.
- **TUI internals** (keybindings are a feature, see below).

## When the version bumps

- **PATCH** `x.y.Z` — bug fixes, refactors, dependency updates; no new
  user-facing behaviour, no config/CLI change.
- **MINOR** `x.Y.0` — new, backward-compatible user-facing behaviour: a new
  config field, CLI flag/command, tunnel type, or TUI feature. Additive only.
- **MAJOR** `X.0.0` — a breaking change to the stability surface: a removed or
  renamed config key/field, command or flag, or an incompatible semantic change.

## 0.x (pre-1.0)

While `MAJOR=0`, SemVer permits breaking changes between any two releases.
Portato's convention during 0.x:

- A breaking change bumps **MINOR** (`0.1.0 → 0.2.0`), never PATCH; each MINOR
  is a "may break" boundary.
- Prefer additive changes. When a break is unavoidable: flag it in the release
  notes with **BREAKING**, and provide a migration path where feasible.

## TUI keybindings

Changing a keybinding (`space`, `e`/`n`/`d`, `l`, `Shift+C`, `/`) is a **MINOR**
change, recorded in the changelog. It is not a breaking/MAJOR change (TUI
internals are not part of the stability surface).

## Pre-releases

Format `vX.Y.Z-rc.N` (release candidate); `-beta.N` / `-alpha.N` earlier.
Pre-releases are marked on GitHub, so `brew upgrade`, `scoop`, and
`go install …@latest` skip them automatically. Portato currently cuts stable
releases only; RCs will be used around risky releases (e.g. the eventual
`v1.0.0`).

## Deprecation (post-1.0)

After `v1.0.0`, before removing or renaming a config key or CLI flag:

1. Keep it working but emit a deprecation warning for one MINOR cycle.
2. Remove it in the next MAJOR (or after a stated grace period).

## The `v1.0.0` milestone

Cut `v1.0.0` when `config.yaml` and the CLI surface are considered stable (no
planned breaking changes), the roadmap's core is complete, and this policy is
in place. `v1.0.0` is the commitment that subsequent breaks go through the
deprecation cycle.

## Mechanics

- The maintainer cuts a tag; [goreleaser](https://goreleaser.com) builds the
  release and changelog (from [Conventional Commits](./CONVENTIONS.md)) on tag
  push.
- `BREAKING CHANGE:` footers and `feat!:` / `refactor!:` commits flag breaks in
  the changelog and justify a MAJOR (or, in 0.x, MINOR) bump.
