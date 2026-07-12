# AGENTS.md

Briefing for AI agents working on **Portato**. Read this first.

## Project

Portato — SSH port-forwarding manager with a TUI. One Go binary, several
modes: smart launcher (`portato`), background daemon (`portato daemon`),
TUI client (`portato attach`), CLI commands (`list/enable/disable/restart`),
and autostart (`install/uninstall`). Native SSH via `golang.org/x/crypto/ssh`
(no system `ssh` dependency).

Source of truth:

- `docs/SPEC.md` — stack, architecture, config, IPC, TUI.
- `docs/ROADMAP.md` — phase status (the live view).
- `docs/CONVENTIONS.md` — how phases are planned and implemented.
- `docs/phases/phase-N-*.md` — per-phase plans and Definition of Done.

Module: `github.com/portuber/portato` (Go 1.26+).

## Layout

```
cmd/portato/      binary entrypoint (cobra root)
internal/
  cmd/                cobra commands (root + subcommands)
  config/             YAML config load/save/validation (phase 1)
  forward/            tunnel engine, native SSH (phase 2)
  fdpass/             SCM_RIGHTS listener transfer for the hand-off (phase 16)
  controller/         Controller interface + local/remote (phase 3+)
  secret/             identity passphrase cache + OS keyring (phase 19)
  daemon/             HTTP server over unix socket (phase 4)
  client/             HTTP client over unix socket (phase 4)
  ipctoken/           daemon IPC bearer token (phase 18)
  tui/                bubbletea UI (phase 3)
  logo/               version/help logo banner (phase 24)
  service/            autostart: launchd/systemd (phase 6)
  log/                slog setup + rotating writer (phase 13)
  sshtest/            in-process SSH server test fixture (phase 16 E2E)
```

## Build & verify

```
make build            # -> bin/portato
make run              # go run ./cmd/portato
make test             # go test ./...
make vet              # go vet ./...
make lint             # golangci-lint run ./... (predeclared + gocyclo@15)
make fmt              # gofmt -w .
```

Run after every change: `make fmt && make vet && make test`. Run `make lint`
before closing a phase (it guards against builtin shadowing and high
cyclomatic complexity — the codefactor.io issue classes; requires
golangci-lint v1.x). Before closing a phase, ensure all of these are clean:

- `go build ./...` succeeds;
- `gofmt -l .` is empty;
- `go vet ./...` is clean;
- `golangci-lint run ./...` is clean (i.e. `make lint`);
- the phase's tests are green, e.g. `go test ./internal/config/... -v`.

## Phases (GSD)

- Phase statuses are `[ ]` (pending) / `[~]` (in progress) / `[x]` (done), kept
  **in sync in two places**: the YAML frontmatter of `docs/phases/phase-N-*.md`
  (`status: todo|in-progress|done`) and the table in `docs/ROADMAP.md`. Update
  both in a single pass.
- A phase starts only on an explicit human command ("start phase N"), and only
  after every phase in its `depends_on` is `[x]`. Only **one** phase may be
  `[~]` at a time.
- On "complete phase N": verify every DoD item is actually met, then flip
  `[~]→[x]`. If something is missing, report it and do **not** mark `[x]`.
- "Tasks" and "Definition of Done" checklists inside a phase file are
  independent lists; check them off as the work lands.

## Commits

Follow Conventional Commits — see `docs/CONVENTIONS.md` §Commits for the full
spec. Quick reference:

- `docs(phase-N): start` / `docs(phase-N): complete` — phase lifecycle.
- `feat(<scope>): …` — implementation within a phase.
- `docs(<scope>): …` — SPEC / CONVENTIONS / ROADMAP edits.
- `chore(<scope>): …` — tooling, deps, Makefile.

Non-trivial commits include a body: **what changed and why**.

## Operating rules

- **Do not push** to any remote unless explicitly asked. Local commits only.
- Do not rewrite history that has been pushed. Rewriting local history is OK
  only when explicitly requested.
- Do not edit `docs/SPEC.md` silently; if reality diverges from the spec, fix
  the spec and mention it in the commit. If unsure — ask.
- Do not change global git config. Local repo config is fine when a task needs it.
- Do not add comments to code unless asked.

## Current focus

See `docs/ROADMAP.md` — "Current focus" and the phase status table.
