---
phase: 25
title: Easter egg — "pórtate bien" footer in --help
status: done
depends_on: [24]
---

## Goal

Print a small bilingual easter egg at the end of `portato --help` (and
`portato help`): "And please, pórtate bien" — the Spanish imperative
*¡pórtate bien!* ("behave yourself!"), a near-homophone of the product
name *portato*. The potato emoji 🥔 is appended only when the terminal is
emoji-capable, reusing the Phase 24 gate.

## Background

- *¡Pórtate bien!* is the affirmative tú imperative of the Spanish reflexive
  verb *portarse* ("to behave"): "behave well / be good". The line uses the
  correct Spanish; *pórtate* is a near-homophone of the brand *portato*
  (port + potato), so it doubles as "portato, behave (well in the network)".
- Bonus layer: *portato* is also an Italian musical term (a bowing between
  legato and staccato) and the past participle of *portare* ("carried") — a
  quiet nod to port-forwarding.
- `--help` is rendered by cobra's default help template (no custom template
  in the codebase). The output ends with
  `Use "portato [command] --help" for more information about a command.`
- Phase 24 added `logo.EmojiEnabled()` (darwin default + `PORTATO_LOGO_EMOJI`
  override) — the project's proxy for "this terminal renders emoji cleanly".
  There is no reliable cross-platform terminfo flag for emoji, so reusing it
  keeps the gate consistent with the header.

## Design decisions (locked at phase start)

| Aspect | Decision |
|---|---|
| Placement | End of `portato --help` and `portato help` only. NOT on subcommand `--help` (each renders its own template). |
| Wording | `And please, pórtate bien` (correct Spanish imperative; near-homophone of the brand portato). |
| Emoji | Append ` 🥔` only when `logo.EmojiEnabled()` is true; plain text otherwise. |
| Mechanism | `rootCmd.SetHelpTemplate(rootCmd.HelpTemplate() + "\n\n" + easterEggFooter() + "\n")` in `init()`. Root-only by nature (subcommands have their own template), so no runtime gate is needed. |
| Testability | Extract `easterEggFooter() string` (reads `logo.EmojiEnabled()` at call time) so the emoji logic is unit-testable independently of the init-time template build. |
| Override | Inherits `PORTATO_LOGO_EMOJI=on\|off` (and `PORTATO_LOGO=off` → no emoji) via `logo.EmojiEnabled()`. |
| Phase | #25, `depends_on: [24]` (reuses the Phase 24 emoji gate). |

## Tasks

- [x] `internal/cmd/root.go`: add `easterEggFooter()`; in `init()` call
      `rootCmd.SetHelpTemplate(...)` appending the footer to the default
      template.
- [x] Test `internal/cmd/help_easter_egg_test.go`:
      - `easterEggFooter()` contains "pórtate bien"; with
        `PORTATO_LOGO_EMOJI=on` it contains 🥔, with `=off` it does not;
      - `portato --help` output contains "pórtate bien";
      - `portato list --help` does NOT contain the footer.
- [x] Docs: SPEC §3 note + ROADMAP post-MVP table — add phase 25 row.

## Definition of Done

- [x] `portato --help` and `portato help` end with "And please, pórtate bien".
- [x] 🥔 appears on darwin (and via `PORTATO_LOGO_EMOJI=on`); absent with
      `=off` and on non-darwin.
- [x] Subcommand `--help` (e.g. `portato list --help`) is unchanged.
- [x] `go vet ./...`, `gofmt -l .`, `go test ./...` clean.

## Verification

```sh
./bin/portato --help                            # ends with "pórtate bien" (+ 🥔 on darwin)
./bin/portato help                              # same
PORTATO_LOGO_EMOJI=off ./bin/portato --help     # no emoji
PORTATO_LOGO_EMOJI=on ./bin/portato --help      # emoji forced on (linux)
./bin/portato list --help                       # unchanged — no footer
```

## Technical details

- cobra's default help template is
  `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}\n\n{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`.
  Appending the footer after it places it below the trailing
  `Use "portato [command] --help" ...` line.
- `SetHelpTemplate` on `rootCmd` affects only root's help rendering;
  subcommands keep their own template (no footer) — the desired scope, no
  `c == rootCmd` gate needed (unlike the `SetHelpFunc` alternative).
- The footer is baked into the template once at `init()`, so the emoji
  decision reflects the launch-time env. The emoji *logic* itself is tested
  via the extracted `easterEggFooter()` (call-time env read); the help-output
  test asserts only the text line, avoiding init-time freeze.
- Language policy: CONVENTIONS.md mandates English for commits/CLI; this is
  a deliberate, hidden bilingual pun — a documented exception.

## Commit plan

1. `docs(phase-25): plan` — create this phase file + ROADMAP row (`[ ]`).
2. `feat(cmd): "pórtate bien" easter egg in --help` — root.go wiring + test.
3. `docs(spec): note the --help easter egg` (optional; can fold into 2).
4. `docs(phase-25): complete` — DoD checklist + `[ ] -> [x]`.

## Start guard

This phase is `status: todo`. It starts only on an explicit "start phase 25"
command (per docs/CONVENTIONS.md). The first action then is commit 1
(planning) — creating this file + the ROADMAP row.

## Out of scope

- Localizing the whole CLI (Spanish etc.) — this is a one-line easter egg.
- Emoji detection beyond the darwin + override heuristic.

## Deviation from plan (during implementation)

The design table ("Mechanism") and the Technical-details bullet claimed that
`SetHelpTemplate` on `rootCmd` affects only the root and that subcommands keep
their own template, so "no runtime gate is needed". That is **incorrect**:
cobra's `Command.HelpTemplate()` walks up to the parent when a command has no
template of its own, so every subcommand inherited the root's footer-augmented
template (verified: `portato list --help` showed the footer pre-fix).

Actual mechanism implemented in `internal/cmd/root.go`:

- The default help template is captured into `defaultHelpTemplate` *before*
  the root is augmented (in `init()`).
- `rootCmd.SetHelpTemplate(defaultHelpTemplate + "\n\n" + easterEggFooter() + "\n")`.
- In `Execute()`, after `AddCommand`, every `rootCmd.Commands()` subcommand is
  pinned to `defaultHelpTemplate`, breaking the inheritance. Grandchildren
  inherit the (now-default) parent template, so they stay clean too.

`TestSubcommandHelp_NoFooter` mirrors this (attach + pin) and
`TestSubcommandHelp_NeedsPin` guards the inheritance so the pin is not
silently dropped. All DoD items are met, including "Subcommand `--help` is
unchanged" (0 occurrences of the footer across all subcommand helps).
