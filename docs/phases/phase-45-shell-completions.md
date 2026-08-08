---
phase: 45
title: "Shell completions"
status: todo
depends_on: []
---

## Goal

Dynamic shell (TAB) completion for the `portato` CLI — above all tuber-name
completion for `enable` / `disable` / `restart` / `forward` and
`logs --tuber`, so users don't have to remember or type tuber names. Cobra's
`completion bash/zsh/fish/powershell` command already generates the shell
script; this phase adds the dynamic-name logic and documents the per-shell
sourcing.

## Background

Cobra's default `completion` subcommand is already enabled (no
`DisableDefaultCmd`) — `portato completion bash` works out of the box and
completes commands + flags. What's missing is *dynamic* completion of tuber
names: `enable` / `disable` / `restart` / `forward` take a positional
`<name>` (`cobra.ExactArgs(1)`), and `logs --tuber <name>` takes a flag — none
of them tab-complete the user's tubers today. Standard cobra-app pattern
(kubectl / helm / gh): `source <(portato completion bash)`.

## Tasks

- [ ] A `tuberNameCompletion(cmd, args, toComplete)` helper that loads
      `config.yaml` via the same path resolution the other commands use and
      returns the tuber names whose prefix matches `toComplete`, with
      `cobra.ShellCompDirectiveNoFileComp`. Config missing/unreadable ⇒ no
      completions (silent, not an error).
- [ ] Register `ValidArgsFunction` on `enable` / `disable` / `restart` /
      `forward`.
- [ ] Register `RegisterFlagCompletionFunc` for `logs --tuber`.
- [ ] README install section: per-shell sourcing snippets — bash
      (`eval "$(portato completion bash)"`), zsh (`source <(portato completion
      zsh)` + a `compinit` note), fish (`portato completion fish >
      ~/.config/fish/completions/portato.fish`), powershell.
- [ ] Tests: the helper returns prefix-filtered names; empty/no-config ⇒ empty
      list (no error, `ShellCompDirectiveNoFileComp`).

## Definition of Done

- [ ] `portato enable` / `disable` / `restart` / `forward <TAB>` complete tuber
      names from `config.yaml`, with no daemon running.
- [ ] `portato logs --tuber <TAB>` completes tuber names.
- [ ] `portato completion bash|zsh|fish|powershell` emits a valid script
      (cobra default — verify it is present and not disabled).
- [ ] README documents the per-shell sourcing one-liners.
- [ ] `go build ./...`, `gofmt -l .`, `go vet ./...`, `golangci-lint run ./...`
      clean; new functions under gocyclo 15; the phase's tests green.
- [ ] No packaging changes (Approach A — document only); no new dependencies.

## Verification

```sh
make fmt && make vet && make test && make lint

# manual (in a shell with the script sourced):
#   portato enable <TAB>        # → tuber names
#   portato logs --tuber <TAB>  # → tuber names
#   portato <TAB>               # → subcommands
```

## Technical details (sketch)

- Completion source = **config-file load** (not the daemon) — works with no
  daemon running, fast, deterministic. A live-daemon source is a possible later
  refinement, deliberately out of scope.
- Cobra's `completion` command and the hidden `__complete` driver are already
  wired (cobra default); this phase only adds `ValidArgsFunction` /
  `RegisterFlagCompletionFunc`.
- Delivery = **document only** (`eval` / `source`); packaging (deb/rpm
  auto-load into `/etc/bash_completion.d/`, Homebrew cask completion) is out of
  scope — a possible refinement if Linux-package users ask.
- Additive, no behaviour change for existing commands ⇒ MINOR.
