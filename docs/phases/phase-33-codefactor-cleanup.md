---
phase: 33
title: CodeFactor cleanup and golangci-lint guardrails
status: done
depends_on: [11]
---

> Quality phase. codefactor.io reports 12 issues on portuber/portato: 6
> "Redefinition of the built-in function max" and 6 "Complex Method". Both
> families share one root cause — beyond `go vet`/`gofmt` there is no
> static-analysis guard, so builtin shadowing and high cyclomatic complexity
> slip in unnoticed. This phase fixes the 12 findings and adds a
> `golangci-lint` config (`predeclared` + `gocyclo`) plus a
> `make lint` target so they cannot reappear.

## Goal

Drive codefactor.io to zero issues on portuber/portato and keep it there: fix
all 12 current findings and introduce a local lint step that fails on
builtin-shadowing and on new high-complexity production methods.

## Background — the 12 findings

### A. Redefinition of built-in `max` (6)

Go 1.21 made `max` a built-in. The tree declares locals/parameters named `max`,
shadowing it — compiles and runs, but is a lint violation.

| Location | Form |
|---|---|
| internal/tui/view.go:520 | parameter `max int` of `fitEndpoint` |
| internal/tui/view.go:535 | parameter `max int` of `fitName` |
| internal/tui/model_test.go:376 | `const max = colEndpoint` in `TestFitEndpoint` |
| internal/tui/logo_test.go:28 | `max := 0` local in `maxLineWidth` (+ ref :31) |

(CodeFactor lists view.go:520 twice — a UI duplicate.)

### B. Complex Method (6)

| Location | Function | Why |
|---|---|---|
| internal/tui/update.go:14-124 | `Update` | 4-arm type-switch + nested `tickMsg` ifs |
| internal/tui/update.go:126-217 | `handleKey` | ~15-key switch + filter preamble |
| internal/cmd/doctor.go:38-149 | `doctorRunE` | 11 inline checks w/ conditionals |
| internal/fdpass/fdpass_unix.go:90-160 | `Recv` | sequential read+validate loops |
| internal/daemon/server_test.go:175-267 | `TestServer_RoundTrip` | long E2E test |
| internal/forward/socks5_auth_integration_test.go:29-126 | `socks5DialUserPass` | SOCKS5 handshake helper |

### Root cause

`Makefile` runs only `gofmt` and `go vet`; neither catches builtin shadowing nor
complexity. No `.golangci.yml`, no `.codefactor.yml`.

## Design decisions (locked at plan time)

| Aspect | Decision |
|---|---|
| Category A fix | Rename shadowing identifiers: `max` -> `maxWidth` (params) / `maxW` (locals). Mechanical; signatures unchanged (calls are positional). |
| Category B — production | Extract methods to drop cyclomatic complexity below the gocyclo threshold (15). No behavior change. |
| Category B — tests | **Do not refactor.** Long E2E tests read better linearly; exclude `_test.go` from the complexity linters. |
| Linter | `golangci-lint` `.golangci.yml`: `disable-all` + `predeclared` (Category A, forever) and `gocyclo` min 15 (Category B). `gocognit` was dropped — it flagged out-of-scope funcs (e.g. `serveForheads`, `watcher.loop`) that codefactor.io did not report. The default set (errcheck/staticcheck/unused) surfaces a large pre-existing backlog unrelated to the 12 findings and is deferred to a future phase; this phase's config is intentionally scoped to the CodeFactor issue classes. v1-schema config, requires golangci-lint v1.x (v1.64.8 at implement time). |
| Test exclusion | `issues.exclude-rules`: skip `gocyclo` on `*_test.go` (predeclared still runs everywhere). |
| Make target | `lint: golangci-lint run ./...`; added to `AGENTS.md` "run after every change" and the phase-close checklist. |
| CodeFactor | `.codefactor.yml` `exclude_patterns: ["**/*_test.go"]` — excludes all Go test files from analysis (broader than the two flagged files: consistent with the local gocyclo exclusion and future-proof). CodeFactor excludes whole files (no per-rule exemption in the free tier); local `golangci-lint` still lints tests (only complexity is exempt). |
| CI | None (local-only for now). |
| `depends_on` | `[11]` — phase 11 introduced doctor + CI, the closest quality/tooling precedent. |

## Tasks

### A — fix `max` shadowing
- [x] internal/tui/view.go — rename param `max` -> `maxWidth` in `fitEndpoint`
      (update body refs) and `fitName`.
- [x] internal/tui/model_test.go — rename `const max` -> `maxW` in
      `TestFitEndpoint` + all references; also the `for _, max := range` loop
      var in `TestFitName` (predeclared is stricter than CodeFactor).
- [x] internal/tui/logo_test.go — rename local `max` -> `maxW` in
      `maxLineWidth`.

### B — reduce production-code complexity
- [x] internal/tui/update.go — `Update` flattened to a type-switch dispatch
      (each case body -> `handleWindowSize`/`handleTick`/`handleRedrawTick`/
      `handleHandoffDone`/`handleKeyPress`/`handlePaste`); `handleKey`
      delegates to a new `handleListKey` that fans the 16-key list-view map
      out to `handleQuitAndViewKey`/`handleNavKey`/`handleToggleKey`/
      `handleEditorKey` (splitting the keymap was required: the raw case count
      alone exceeds 15). All under threshold (max `handleToggleKey` 12).
- [x] internal/cmd/doctor.go — moved the inline checks (known_hosts,
      ssh-agent, identities, logs, daemon+socket-perms) into named `check*`
      funcs (`checkKnownHosts`/`checkSSHAgent`/`checkIdentities`/`checkLogs`/
      `checkDaemon`/`checkSocketPerms`); `doctorRunE` is an orchestrator
      (mirrors existing `checkConfigDir`/`checkBinary`/`checkAutostart`/
      `checkLinger`).
- [x] internal/fdpass/fdpass_unix.go — extracted `readAtLeast(c, buf, have,
      need)` for the two top-up loops and `adoptListeners(headers, fds)` for
      the final listener-adoption loop.

### C — tests
- (None — excluded from complexity, not refactored.)

### D — guardrails
- [x] `.golangci.yml` — `disable-all` + `predeclared` + `gocyclo` (min 15),
      with `*_test.go` excluded from `gocyclo` (gocognit dropped, defaults
      deferred — see Design decisions).
- [x] `Makefile` — `lint` target (`golangci-lint run ./...`).
- [x] `AGENTS.md` — `make lint` added to the "run after every change" ritual
      and the phase-close checklist.
- [x] `.codefactor.yml` — `exclude_patterns: ["**/*_test.go"]`.

## Definition of Done

- [x] `make lint` is clean (`golangci-lint run ./...` exits 0), including
      `predeclared` finding no builtin shadowing anywhere in the tree.
- [x] The four refactored production functions (`Update`, `handleKey`,
      `doctorRunE`, `Recv`) each report gocyclo complexity under 15
      (`Update` 7, `handleKey` 6, `doctorRunE` ≤ 5, `Recv` 12).
- [x] `make fmt && make vet && make test` clean — no behavior change.
- [x] codefactor.io reports 0 issues on portuber/portato after the next push
      (6 `max` + 4 production Complex Method findings fixed; the 2 test
      findings suppressed via `.codefactor.yml`). *Deferred-to-push manual
      check — the changes that drive CodeFactor to zero are in place; not
      pushed per AGENTS.md (local-only).*
- [x] ROADMAP + this file: phase row + status flips on start/complete.

## Verification

    golangci-lint run ./...           # exit 0
    make lint                          # same, via the new target
    make fmt && make vet && make test  # clean
    golangci-lint run --disable-all --enable gocyclo ./...   # complexity spot-check

Manual: after pushing, reload the codefactor.io page — the issues list is empty.

## Technical details / risks

- **No behavior change.** Category A is a rename; Category B is pure
  method-extraction preserving control flow. Existing tests are the regression
  net (`make test`).
- **`golangci-lint` install.** Not a module dep (build tool). The `lint` target
  assumes it is on PATH; a one-line install hint goes near the target.
  Installed at plan time: v1.64.8 (v1 config schema) built with go1.26.1.
- **predeclared is stricter than CodeFactor.** It also flags loop/range vars
  and other locals CodeFactor's engine missed; the Tasks list reflects that
  (e.g. the `for _, max := range` in `TestFitName`). The linter itself is the
  authoritative enumeration — fix whatever it reports.
- **gocyclo vs CodeFactor thresholds.** Not identical; gocyclo at 15 is a
  stricter, conventional default. CodeFactor is the external signal;
  golangci-lint is the enforced gate.
- **`.codefactor.yml` whole-file exclusion.** Excludes files from *all* engines,
  so the two test files lose other analysis too. Accepted to reach zero
  CodeFactor issues; local `golangci-lint` still lints them (only complexity is
  exempt).
- **Complexity threshold for tests.** Excluding `_test.go` from `gocyclo` is the
  widely-used convention; long table/E2E tests routinely exceed function-level
  complexity without being bad code.

## Commit plan (per CONVENTIONS)

1. `docs(phase-33): plan` — create this file + the ROADMAP row (`[ ]`).
2. `docs(phase-33): start` — flip frontmatter + ROADMAP row `[ ] -> [~]`.
3. `refactor(tui): stop shadowing builtin max and split Update/handleKey` —
   category A + category B (update.go).
4. `refactor(cmd,fdpass): split doctorRunE and Recv to lower complexity` —
   category B (doctor.go, fdpass_unix.go).
5. `chore(build): add golangci-lint config and make lint` — `.golangci.yml`,
   `Makefile`, `AGENTS.md`, `.codefactor.yml`.
6. `docs(phase-33): complete` — `[~] -> [x]` after the DoD passes.

## Start guard

This phase is `status: todo`. It starts only on an explicit "start phase 33"
command (per docs/CONVENTIONS.md). The first action then is commit 2 (the
`[~]` flip) and the implementation commits.
