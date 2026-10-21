---
phase: 60
title: "ssh-config leniency + CLI failure visibility"
status: done
depends_on: []
---

## Goal

A `~/.ssh/config` containing directives portato does not use (a `Match exec`
block, and similar) no longer breaks config loading for every command, and no
CLI command exits non-zero without printing an actionable error.

## Background

Issue #4 reported two acute bugs (fixed in the issue #4 hotfix: the
agent/identity auth-fallback merge in `authMethods`, and the CRUD commands'
silent exits routed through `crudFail`). Three follow-ups remain:

1. `loadUserSSHConfig` (`internal/config/config.go`) runs the whole file
   through `ssh_config.Decode`, which hard-fails on `Match exec` ("Match Exec
   is not supported"). One such block anywhere in `~/.ssh/config` makes
   `config.Load` — and therefore *every* portato command — fail. The doc
   comment claims the parser "does not reject odd but salvageable input",
   which does not hold for `Match` blocks.
2. The hotfix audited only `add`/`set`/`rm`. The other `SilenceErrors`
   commands (doctor, logs, update, identity, install, uninstall, import, …)
   may still have error returns that never print.
3. The issue reports tunnels stuck showing `connecting` in the TUI with
   `portato logs --tuber` silent at info level during auth failures. The
   state machine does call `setStateErr` (which reaches the log ring), so
   this needs verification on the fixed build — and a fix if it reproduces.

## Tasks

- [x] Tolerate unsupported-but-harmless `~/.ssh/config` directives (at
      minimum `Match exec`): skip-with-warn instead of failing
      `config.Load`; alias resolution ignores what it cannot use.
- [x] Audit every `SilenceErrors` command's error returns: no path exits
      non-zero without printing (extend the `crudFail` pattern or
      centralize the printing).
- [x] Verify the auth-failure visibility claim from issue #4 on the fixed
      build (TUI state + `portato logs` at info); fix if it reproduces,
      and answer in the issue either way.
- [x] SPEC: document the `~/.ssh/config` leniency contract (what is
      tolerated vs a load error).

## Definition of Done

- [x] A `~/.ssh/config` containing a `Match exec …` block loads cleanly,
      `portato doctor` is green, and a warning names the skipped block.
- [x] The `SilenceErrors` audit is complete; every fixed silent path has a
      regression test (clean paths are noted as audited).
- [x] The visibility question from issue #4 is answered in the issue
      (verified-fixed or reproduced-and-fixed).
- [x] `make fmt && make vet && make test && make lint` clean.

## Verification

```sh
go test ./internal/config/... ./internal/cmd/... -v
```

## Technical details

- The leniency fix belongs in `loadUserSSHConfig` / the Phase-44 reader:
      preprocess or wrap `ssh_config.Decode` so `Match` criteria portato
      cannot evaluate degrade to "block skipped, warn once" — mirroring
      OpenSSH, where an unevaluable `Match exec` on a *client* that does
      not support it simply never matches.
- The audit may conclude that cobra-level printing (dropping
      `SilenceErrors` or printing once in `main`) is cleaner than per-path
      prints; whatever lands, the invariant is "returned error ⇒ printed
      exactly once", and it must not double-print for commands cobra
      already prints for.
- Phase 54 covers the machine-readable exit-code surface; this phase is
      the human-readable half. Keep the scopes disjoint.
