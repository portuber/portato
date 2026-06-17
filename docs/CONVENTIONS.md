# Conventions — the rules for working with phases

This document describes how planning and implementation are organized in the `portato` project. These are the "rules of the game" for the agent and the human.

## Planning structure

```
glm-complex/docs/
├── SPEC.md              # the project's single technical spec (source of truth)
├── ROADMAP.md           # the summary table of all phases with statuses
├── CONVENTIONS.md       # this file — the rules of work
└── phases/
    ├── phase-0-skeleton.md
    ├── phase-1-config.md
    ├── phase-2-forward-engine.md
    ├── phase-3-standalone-tui.md
    ├── phase-4-daemon-ipc.md
    ├── phase-5-cli-smart-launcher.md
    ├── phase-6-autostart-e2e.md
    ├── phase-7-remote-R.md          # outline
    ├── phase-8-dynamic-D.md         # outline
    ├── phase-9-push-events.md       # outline
    ├── phase-10-tui-editor.md       # outline
    └── phase-11-polish.md           # outline
```

- **SPEC.md** — the source of truth for the stack, architecture, config, TUI, and IPC. Changes rarely.
- **ROADMAP.md** — a mirror of the current state of all phases (a quick glance).
- **phases/phase-N-*.md** — the detailed plan and tasks of a specific phase.

## Phase statuses

Each phase has exactly one status:

| Marker  | State        | Meaning                                             |
|---------|--------------|-----------------------------------------------------|
| `[ ]`   | pending      | the phase has not been started                       |
| `[~]`   | in progress  | the phase has been taken into work                   |
| `[x]`   | done         | all Definition-of-Done items are checked off         |

The status is stored **in two places at once** and must match:

1. In the YAML frontmatter of `phases/phase-N-*.md` (`status: todo | in-progress | done`).
2. In the phase's row in `ROADMAP.md` (the `[ ]` / `[~]` / `[x]` markers).

Whenever the status changes, **both places are updated in a single pass**.

## Sequencing rules

1. You **cannot** start phase N until every phase in its `depends_on` has status `[x]`.
2. You **cannot** mark a phase `[x]` until every item in its "Definition of Done" block is checked `[x]`.
3. A phase is taken into work (`[~]`) only on an explicit command from the human.
4. A phase is completed (`[x]`) only on an explicit command from the human — after they have verified the result.
5. At most **one** phase may be in work (`[~]`) at a time.

## Who does what

- The **human** says: "start phase N", "complete phase N", "roll back", etc.
- The **agent**:
  - on "start phase N": verifies that every `depends_on` is `[x]`; sets `[~]` in the frontmatter and in ROADMAP; implements the tasks; checks off checklist items; updates SPEC along the way if something new comes up.
  - on "complete phase N": verifies that all DoD items are actually met and the "Verification" block is passed; sets `[x]` in the frontmatter and in ROADMAP.
  - if something is not met — honestly reports what is left and does **not** set `[x]`.

## Phase file format

```markdown
---
phase: N
title: <title>
status: todo           <!-- todo | in-progress | done -->
depends_on: [<list of numbers>]
---

## Goal
<one or two sentences: what the user gets>

## Tasks
- [ ] task 1
- [ ] task 2

## Definition of Done
- [ ] measurable criterion 1 (verifiable with a command or action)
- [ ] measurable criterion 2

## Verification
<specific shell commands the human/agent uses to check that the phase is ready>

## Technical details
<files, libraries, approach, nuances>
```

## Checklists inside a phase

- The "Tasks" and "Definition of Done" items are **separate** checklists.
- "Tasks" = what to do. "Definition of Done" = how to tell the phase is ready.
- Tasks can be extended as work progresses. Criteria are more stable (changing the criteria is a notable event — describe it in the SPEC or a commit).

## Commits (when the repository is under git)

The project follows **Conventional Commits** (conventionalcommits.org). The commit
header is `<type>[scope]: <subject>` (up to 72 characters). Non-trivial changes
must have a **body**: what changed and **why**.

Commit messages are written in English only (subject and body), since the
project targets an open-source audience.

- **Every phase status change** (`[ ]->[~]`, `[~]->[x]`) is its own commit
  of the form `docs(phase-N): start` / `docs(phase-N): complete`.
- Implementation commits inside a phase are `feat(<scope>): ...`, not mixed with
  a status change.
- Changes to SPEC.md / CONVENTIONS.md / ROADMAP.md are `docs(<scope>): ...`.
- Makefile, .gitignore, dependencies, tooling are `chore(<scope>): ...`.

### Types

`feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`,
`chore`, `revert`.

### Scope (optional)

A package or area: `config`, `forward`, `controller`, `daemon`, `client`,
`tui`, `service`, `cmd`, `agents`, `conventions`, `build`. For the phase
lifecycle — `phase-N`.

### Breaking changes

`feat!: ...` (or `feat(scope)!: ...`) + a `BREAKING CHANGE: <description>` footer.

### Examples

```
docs(phase-1): start
feat(config): add YAML config load, validation and persistence
docs(phase-1): complete
fix(forward): reset backoff after stable connection
docs(conventions): switch to conventional commits
chore(build): bump go toolchain to 1.22
```

## If a phase is blocked

- The status stays `[~]`; the agent does not close it.
- A `## Blockers` block is appended to the end of `phases/phase-N-*.md` describing the situation and what is needed from the human.
- After unblocking — the block is removed and work continues.

## When and how to change the SPEC

The SPEC is a stable document. Anything in it may be changed only in two cases:

1. **During a phase's implementation it turns out** that the spec diverges from reality (for example, a library behaves differently than expected) — the agent fixes the SPEC and mentions it in the implementation commit.
2. **The human explicitly asks** to change an architectural decision (add a tunnel type, change the IPC transport, etc.) — handled as a separate discussion + a `docs:` commit.

When unsure — ask, do not edit silently.
