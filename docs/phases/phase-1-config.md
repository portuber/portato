---
phase: 1
title: Config
status: done
depends_on: [0]
---

## Goal

Loading and saving the YAML config with tunnels. Persisting the `enabled` state
(written back to YAML on every toggle) — this is the foundation of the "leave in
the background" hand-off in Phase 5. Strict validation and defaults. Full unit
coverage.

## Phase scope (what we do)

- `Config`, `Defaults`, `Tunnel` structs with yaml tags.
- Loading from an XDG path with `~` expansion (`identity`, `known_hosts`).
- Defaults: `local` without a host → `127.0.0.1:<port>`; `known_hosts` → `~/.ssh/known_hosts`; `enabled` → `false`; ssh port → `22`; ssh `user` → `$USER`.
- `Save()` — writing the config back while preserving structure (without saving comments — acceptable for MVP, post-MVP via `yaml.Node` AST).
- If the config is missing — create a default one with an example tunnel (`enabled: false`).
- Validation: uniqueness of `name`, non-empty `remote`/`ssh`, valid port (1–65535), `type == "local"` in MVP.
- Parsing the `ssh: user@host:port` field → separate `User`, `Host`, `Port`.

## Phase scope (what we do NOT do)

- Real SSH logic — Phase 2.
- External hot-reload (watch) — in MVP only on demand (`Reload` via `POST /reload`, Phase 4).

## Tasks

- [x] `glm-complex/internal/config/config.go`:
  - [x] `type Tunnel struct` with fields: `Name, Type, Local, Remote, SSH, Identity, Enabled`, plus parsed `User, Host, Port` (not serialized, populated from `SSH`).
  - [x] `type Defaults struct { Identity, KnownHosts string; AcceptNewHosts bool }`.
  - [x] `type Config struct { Defaults Defaults; Tunnels []Tunnel }`.
  - [x] All fields with yaml tags (e.g. `yaml:"name"`, `yaml:"local"`).
- [x] `func DefaultPath() string` — via `xdg.ConfigHome` (see SPEC §7).
- [x] `func Load(path string) (*Config, error)`:
  - [x] `~` expansion via `os.UserHomeDir`.
  - [x] Applying defaults (Defaults.Identity → to tunnels without their own identity, Defaults.KnownHosts → when empty).
  - [x] Parsing the `SSH` string `user@host:port` → `User/Host/Port` fields; defaults `$USER` and `22`.
  - [x] Normalizing `Local`: if only a port → `127.0.0.1:<port>`.
- [x] `func (c *Config) Validate() error`:
  - [x] Uniqueness of `Name` (map by name).
  - [x] `Name` non-empty, alphanumeric+dash.
  - [x] `Type == "local"` (MVP); otherwise an explicit error "type X not supported yet, supported: local".
  - [x] `Remote` and `Host` non-empty.
  - [x] `Port` in the range 1–65535.
- [x] `func (c *Config) Save(path string) error`:
  - [x] `yaml.Marshal` + `os.WriteFile` with mode `0600`.
  - [x] Do not fail on an empty config.
- [x] `func EnsureExample(path string) (created bool, err error)`:
  - [x] If the file is missing — create it with one example tunnel (`enabled: false`), return `created=true`.
- [x] `glm-complex/config.example.yaml` — reference example.
- [x] Unit tests (`config_test.go`):
  - [x] Parsing of valid YAML.
  - [x] Applying defaults (local host, known_hosts, ssh port/user).
  - [x] Round-trip: `Load` → change `Enabled` → `Save` → `Load` again yields the same state.
  - [x] Validation error: duplicate `name`, broken port, empty `ssh`, unsupported `type`.
  - [x] `~` expansion in `identity` and `known_hosts`.

## Definition of Done

- [x] `go test ./internal/config/...` is green, all the tests above exist.
- [x] `Load(DefaultPath())` on an empty system creates the example (`EnsureExample` ran).
- [x] Round-trip works: after `Save` the file remains a valid YAML and is read by the same `Load`.
- [x] The config file is created with `0600` permissions.
- [x] The field `ssh: user@host:port` is correctly split into three values; the variants `host:port`, `user@host`, `host` work (with defaults).
- [x] `Validate` produces readable errors for each invalid case.
- [x] `go vet ./...` and `gofmt -l .` are clean.

## Verification

```sh
cd glm-complex
go test ./internal/config/... -v
go run ./cmd/portato list    # not implemented yet, but the config must load/be created in the background
ls -la "$(go run ./cmd/portato config-path 2>/dev/null || true)"  # optional: add a temporary subcommand for debugging
```

(The `config-path` subcommand for debugging is optional — you can verify the path via `EnsureExample` in a test.)

## Technical details

- **yaml.v3 and comments:** `yaml.Marshal` from a struct drops comments. This is acceptable for MVP (tunnels are edited via the TUI in Phase 10). Post-MVP, switch to `yaml.Node` AST if needed.
- **Persistance invariant:** every `enable/disable` in the daemon/TUI must call `Save()` (see Phase 4, 5). The config = the source of truth about the desired state.
- **`0600` permissions** on the config — it may contain paths to private keys.
- **The `SSH` string** — the only input field for the server address, parsed into `User/Host/Port` (not serialized back, to avoid spawning multiple sources of truth).
- **XDG Runtime/State dirs** are NOT needed in this phase — only `ConfigHome`.

## Phase output artifact

- The `internal/config` package, through which all subsequent phases read/write the config.
- Guarantee: the config is created with defaults, validated, and persisted — the foundation for the Engine (Phase 2) and hand-off (Phase 5).
