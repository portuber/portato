---
phase: 0
title: Project skeleton + GSD
status: in-progress
depends_on: []
---

## Goal

Initialize the Go module, dependencies, and package skeleton so that subsequent phases
can fill it with code. Set up cobra with all subcommands (as stubs for now), so that
`portato --help` immediately shows the full picture. The GSD files are already in place —
here we bring them together with a working skeleton.

## Phase scope (what we do)

- `go.mod` with module `github.com/kipkaev55/portato`, Go 1.22+.
- Directory tree `cmd/portato/`, `internal/{config,forward,controller,daemon,client,tui,service,cmd,log}/`.
- Dependencies: cobra, bubbletea, lipgloss, bubbles, `golang.org/x/crypto`, `gopkg.in/yaml.v3`, `adrg/xdg`.
- cobra root + all subcommands as stubs (`not implemented yet`).
- Makefile (`make build`, `make run`, `make test`).
- `.gitignore`.
- A minimal `README.md` (what it is, how to run it, a link to `docs/`).

## Phase scope (what we do NOT do)

- Any real logic (config/SSH/TUI) — phases 1+.
- Documentation beyond README — it is already in `docs/`.

## Tasks

- [ ] `glm-complex/go.mod` via `go mod init github.com/kipkaev55/portato`.
- [ ] Add dependencies (`go get …`).
- [ ] Create the directory tree:
  - `glm-complex/cmd/portato/main.go`
  - `glm-complex/internal/{config,forward,controller,daemon,client,tui,service,cmd,log}/`
- [ ] `cmd/portato/main.go`: calls `internal/cmd.Execute()`.
- [ ] `internal/cmd/root.go`: cobra root command `portato` with a `--config` flag and a `RunE` handler that for now prints "TUI not implemented yet" (this will be the smart-launcher in Phase 5).
- [ ] `internal/cmd/daemon.go`, `attach.go`, `list.go`, `enable.go`, `disable.go`, `restart.go`, `install.go`, `uninstall.go` — each subcommand as a stub: `RunE: func(...) { return fmt.Errorf("not implemented yet") }`.
- [ ] `.gitignore`: `bin/`, `*.log`, `*.sock`, `*.pid`, `.idea/`, `dist/`.
- [ ] `Makefile`:
  ```make
  .PHONY: build run test fmt vet
  build:
  	go build -o bin/portato ./cmd/portato
  run:
  	go run ./cmd/portato
  test:
  	go test ./...
  fmt:
  	gofmt -w .
  vet:
  	go vet ./...
  ```
- [ ] `glm-complex/README.md`: a brief project description, a link to `docs/SPEC.md`, the `make build` / `make run` commands.

## Definition of Done

- [ ] `go build ./...` completes without errors.
- [ ] `./bin/portato --help` shows the root help and the list of all subcommands (daemon, attach, list, enable, disable, restart, install, uninstall).
- [ ] Each subcommand responds "not implemented yet" when called, without panicking.
- [ ] The `--config <path>` flag is available on the root command.
- [ ] `make build`, `make run`, `make test`, `make vet`, `make fmt` work.
- [ ] `go vet ./...` is clean, `gofmt -l .` is empty.
- [ ] The directory structure matches SPEC §4.

## Verification

```sh
cd glm-complex
go build ./...
./bin/portato --help
./bin/portato daemon            # expected: not implemented yet
make fmt && make vet         # should be clean
```

## Technical details

- **Module path:** `github.com/kipkaev55/portato`. Use this prefix everywhere in `import`.
- **Go version:** set `go 1.22` in `go.mod` (the environment may contain 1.26 — that's fine, the lower bound is 1.22).
- **Cobra layout:** root + subcommands in separate files under `internal/cmd/`; each `cobra.Command` is registered via `rootCmd.AddCommand(...)` in `Execute()`.
- **Do not introduce** own config/Tunnel/Engine types yet — only empty packages, with `doc.go` if necessary (optional).
- **README** — short (for now); the primary source of knowledge lives in `docs/`.

## Phase deliverable

- A working `bin/portato` stub that responds to `--help` and the subcommands, ready to be filled with logic in Phase 1.
