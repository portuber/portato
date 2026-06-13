---
phase: 1
title: Config
status: todo
depends_on: [0]
---

## Goal

Loading and saving the YAML config with tunnels. Persisting the `enabled` state
(written back to YAML on every toggle) — this is the foundation of the «leave in
background» hand-off in Phase 5. Clear validation and defaults. Full unit coverage.

## Phase scope (what we do)

- The `Config`, `Defaults`, `Tunnel` structs with yaml tags.
- Loading from an XDG path with `~` expansion (`identity`, `known_hosts`).
- Defaults: `local` without host → `127.0.0.1:<port>`; `known_hosts` → `~/.ssh/known_hosts`; `enabled` → `false`; ssh port → `22`; ssh `user` → `$USER`.
- `Save()` — writing the config back while preserving the structure (without preserving comments — acceptable for MVP, post-MVP via `yaml.Node` AST).
- If the config is missing — create a default one with an example tunnel (`enabled: false`).
- Validation: unique `name`, non-empty `remote`/`ssh`, correct port (1–65535), `type == "local"` in MVP.
- Parsing the `ssh: user@host:port` field → separate `User`, `Host`, `Port`.

## Phase scope (what we do NOT do)

- Real SSH logic — Phase 2.
- Hot-reload from the outside (watch) — in MVP only on demand (`Reload` via `POST /reload`, Phase 4).

## Tasks

- [ ] `glm-complex/internal/config/config.go`:
  - [ ] `type Tunnel struct` with fields: `Name, Type, Local, Remote, SSH, Identity, Enabled`, plus parsed `User, Host, Port` (not serialized, populated from `SSH`).
  - [ ] `type Defaults struct { Identity, KnownHosts string; AcceptNewHosts bool }`.
  - [ ] `type Config struct { Defaults Defaults; Tunnels []Tunnel }`.
  - [ ] All fields with yaml tags (for example `yaml:"name"`, `yaml:"local"`).
- [ ] `func DefaultPath() string` — via `xdg.ConfigHome` (see SPEC §7).
- [ ] `func Load(path string) (*Config, error)`:
  - [ ] `~` expansion via `os.UserHomeDir`.
  - [ ] Applying defaults (Defaults.Identity → to tunnels without their own identity, Defaults.KnownHosts → when empty).
  - [ ] Parsing the `SSH` string `user@host:port` → `User/Host/Port` fields; defaults `$USER` and `22`.
  - [ ] Normalizing `Local`: if only a port → `127.0.0.1:<port>`.
- [ ] `func (c *Config) Validate() error`:
  - [ ] `Name` uniqueness (map by name).
  - [ ] `Name` non-empty, alphanumeric+dash.
  - [ ] `Type == "local"` (MVP); otherwise an explicit error «type X not supported yet, supported: local».
  - [ ] `Remote` and `Host` non-empty.
  - [ ] `Port` in the range 1–65535.
- [ ] `func (c *Config) Save(path string) error`:
  - [ ] `yaml.Marshal` + `os.WriteFile` with mode `0600`.
  - [ ] Do not fail on an empty config.
- [ ] `func EnsureExample(path string) (created bool, err error)`:
  - [ ] If the file does not exist — create one with a single example tunnel (`enabled: false`), return `created=true`.
- [ ] `glm-complex/config.example.yaml` — reference example.
- [ ] Unit tests (`config_test.go`):
  - [ ] Parsing valid YAML.
  - [ ] Applying defaults (local host, known_hosts, ssh port/user).
  - [ ] Round-trip: `Load` → change `Enabled` → `Save` → `Load` again yields the same state.
  - [ ] Validation error: duplicate `name`, broken port, empty `ssh`, unsupported `type`.
  - [ ] `~` expansion in `identity` and `known_hosts`.

## Definition of Done

- [ ] `go test ./internal/config/...` is green, all the tests above are present.
- [ ] `Load(DefaultPath())` on an empty system creates an example (`EnsureExample` ran).
- [ ] Round-trip works: after `Save` the file remains valid YAML and is read by the same `Load`.
- [ ] The config file is created with `0600` permissions.
- [ ] The `ssh: user@host:port` field is correctly parsed into three values; the variants `host:port`, `user@host`, `host` work (with defaults).
- [ ] `Validate` produces readable errors for every invalid case.
- [ ] `go vet ./...` and `gofmt -l .` are clean.

## Verification

```sh
cd glm-complex
go test ./internal/config/... -v
go run ./cmd/portato list    # not implemented yet, but config should load/be created in the background
ls -la "$(go run ./cmd/portato config-path 2>/dev/null || true)"  # optional: add a temporary subcommand for debugging
```

(The `config-path` subcommand for debugging is optional; the path can be checked via `EnsureExample` in the test.)

## Technical details

- **yaml.v3 and comments:** `yaml.Marshal` from a struct loses comments. This is acceptable for MVP (tunnels are edited via the TUI in Phase 10). Post-MVP, if necessary, switch to a `yaml.Node` AST.
- **Persistence invariant:** every `enable/disable` in the daemon/TUI must call `Save()` (see Phase 4, 5). The config = the source of truth about the desired state.
- **`0600` permissions** on the config — it may contain paths to private keys.
- **`SSH` string** — the single input field for the server address, parsed into `User/Host/Port` (not serialized back, so as not to proliferate sources of truth).
- **XDG Runtime/State dirs** are NOT needed in this phase — only `ConfigHome`.

## Phase output artifact

- The `internal/config` package, through which all subsequent phases read/write the config.
- Guarantee: the config is created with defaults, validated, and persisted — the foundation for the Engine (Phase 2) and hand-off (Phase 5).
