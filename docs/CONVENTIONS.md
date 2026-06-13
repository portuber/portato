# Conventions — rules for working with phases

This document describes how planning and implementation are organized for the `portato` project. These are the "rules of the game" for the agent and the human.

## Planning structure

```
glm-complex/docs/
├── SPEC.md              # unified technical spec of the project (source of truth)
├── ROADMAP.md           # summary table of all phases with statuses
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

- **SPEC.md** — source of truth on the stack, architecture, config, TUI, IPC. Rarely changed.
- **ROADMAP.md** — a mirror of the current state of all phases (a quick glance).
- **phases/phase-N-*.md** — the detailed plan and tasks of a specific phase.

## Phase statuses

Every phase has exactly one status:

| Marker  | State       | Meaning                                            |
|---------|-------------|----------------------------------------------------|
| `[ ]`   | pending     | phase has not been started                         |
| `[~]`   | in progress | phase has been taken into work                      |
| `[x]`   | done        | all Definition of Done (DoD) criteria are checked off as fulfilled |

The status is stored **in two places at once** and must match:

1. In the YAML frontmatter of the `phases/phase-N-*.md` file (`status: todo | in-progress | done`).
2. In the phase row in `ROADMAP.md` (the markers `[ ]` / `[~]` / `[x]`).

On any status change, **both places are updated in a single pass**.

## Sequencing rules

1. You **cannot** start phase N until every phase in its `depends_on` has status `[x]`.
2. You **cannot** mark a phase `[x]` until every item in its "Definition of Done" block is checked off `[x]`.
3. A phase is taken into work (`[~]`) only on an explicit human command.
4. A phase is completed (`[x]`) only on an explicit human command — after they have verified the result.
5. At any time, **only one** phase may be in progress (`[~]`).

## Who does what

- The **human** says: "start phase N", "complete phase N", "roll back", etc.
- The **agent**:
  - on "start phase N": verifies that all `depends_on` are `[x]`; sets `[~]` in the frontmatter and in the ROADMAP; implements the tasks; checks off checklist items; along the way updates the SPEC if something new is discovered.
  - on "complete phase N": verifies that all DoD are actually fulfilled and the "Verification" block is passed; sets `[x]` in the frontmatter and in the ROADMAP.
  - if something is not fulfilled — honestly reports what is left, and does **not** set `[x]`.

## Phase file format

```markdown
---
phase: N
title: <title>
status: todo           <!-- todo | in-progress | done -->
depends_on: [<list of numbers>]
---

## Goal
<one or two sentences: what the user will get>

## Tasks
- [ ] task 1
- [ ] task 2

## Definition of Done
- [ ] measurable criterion 1 (verifiable by a command/action)
- [ ] measurable criterion 2

## Verification
<specific shell commands the human/agent uses to verify the phase is ready>

## Technical details
<files, libraries, approach, nuances>
```

## Checklists inside a phase

- The "Tasks" and "Definition of Done" items are **separate** checklists.
- "Tasks" = what to do. "Criteria" = how to tell that the phase is ready.
- Tasks may be extended as work progresses. Criteria are more stable (changing the criteria = a notable event, to be described in the SPEC or a commit).

## Commits (when the repository is under git)

- **Every phase status change** (`[ ]→[~]`, `[~]→[x]`) is a separate commit with a message of the form `phase N: start` / `phase N: complete`.
- Implementation commits within a phase are ordinary, but without mixing them with a status change.
- Changes to SPEC.md / CONVENTIONS.md are a separate commit with the `docs:` prefix.
- Message format: below is the general convention (it can be adapted):

  ```
  phase N: start        — phase start (status: todo → in-progress)
  phase N: complete     — phase completion (status: in-progress → done)
  phase N: <topic>      — implementation commit within a phase
  docs: <topic>         — SPEC/CONVENTIONS/ROADMAP edits unrelated to a status change
  chore: <topic>        — Makefile, .gitignore, etc.
  ```

## If a phase is blocked

- The status stays `[~]`; the agent does not close it.
- In `phases/phase-N-*.md`, a `## Blockers` block is appended at the end with a description and what is needed from the human.
- After unblocking — the block is removed and work continues.

## When and how to change the SPEC

The SPEC is a stable document. You may change anything in it in two cases:

1. **During the implementation of a phase it turned out** that the spec diverges from reality (for example, a library does not behave as expected) — the agent fixes the SPEC and mentions it in the implementation commit.
2. **The human explicitly asks** to change an architectural decision (add a tunnel type, change the IPC transport, etc.) — this is framed as a separate discussion + a `docs:` commit.

If unsure — ask a question rather than editing silently.
