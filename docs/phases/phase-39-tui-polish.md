---
phase: 39
title: TUI polish (modals, footer pin, microcopy, error display)
status: in-progress
depends_on: []
---

> TUI polish phase. A bundle of MEDIUM/MINOR behavioral and microcopy defects
> found in the TUI: modals erase the list, the footer hugs the content, the
> empty state points at config-editing instead of `n`, the quit gate has a hole
> around the Error state and divergent microcopy, the attach header dumps a
> socket path, and error messages are truncated before the useful part.

## Goal

Modals stop destroying context, the footer pins to the bottom edge, the empty
state routes users to the in-app editor, quitting is consistent across the
Error/Reconnecting cycle and the three texts agree, the attach header drops its
socket-path noise, and error messages keep the actionable tail.

## Background / why

- **Modals erase context (F7).** `render()` (`view.go:105-151`) returns *only*
  the centered block for confirm-delete / TOFU / passphrase / password / quit —
  the list disappears. Confirming a delete, the user cannot see neighboring
  tubers. Genre convention (lazygit, k9s) is an overlay above a dimmed list.
- **Footer hugs content (F8).** `render()` (`view.go:136-150`) emits
  header → table → footer with no bottom padding, so on a 35-row terminal the
  footer sits right under the ~14 content rows and the bottom ~60% is empty.
  Users look for key hints at the bottom edge; pinning also removes the footer
  "jump" when the filter line appears/disappears (`view.go:141-145`).
- **Empty-state CTA (F9).** The empty state says "add one to config and press R
  to reload" (`view.go:175`) although the app has a full editor on `n`; the
  footer shows keys irrelevant to an empty list (`space toggle`, `p passphrase`).
- **Quit gate hole (F10).** `hasLiveTubers()` (`update.go:688-696`) counts
  Connecting/Connected/Reconnecting but **not Error**. An enabled tuber cycles
  Connecting → Error → (backoff) → Connecting; pressing `q` during the Error
  window exits silently where one second earlier (Reconnecting) it would raise
  the confirm dialog. Microcopy also diverges across three places: footer says
  `q quit`, help says `q quit (stops all tubers)`, reality is a "leave them
  running in the background?" modal.
- **Attach header noise (F12).** The header's right side reads
  `mode: attach @ /var/folders/.../portato-501.sock` (the mode string is built
  as `"attach @ " + socket` in `cmd/root.go:70` and `cmd/attach.go:35`). The
  60+ char temp path is permanent header noise that crowds the title and carries
  no day-to-day value.
- **Error truncation (F13).** `truncate(s.Error, 18)` (`view.go:267-269`) chops
  error text at 18 cells — exactly before the conflicting port in a listen
  conflict, which is the one fact the user needs.

## Design decisions (locked at plan time)

| Aspect | Decision |
|---|---|
| Modals | Overlay the modal above a `Faint`-dimmed list (lipgloss v2 layer/canvas composition), or the cheaper variant: keep header + table rendered and replace only the footer zone with the prompt. |
| Footer pin | Pad content to `m.height - footerHeight` before appending the footer. Optional stats line (`3 connected · 1 error · 2 off`). |
| Empty state | Message → `no tubers — press n to create one (or edit the config and press R)`; filter footer entries by applicability. |
| Quit gate | Treat enabled-but-erroring tubers as live in `hasLiveTubers`; unify footer / help / modal microcopy to match reality (a background-handoff modal in standalone). |
| Attach header | Show `mode: attach` only; expose the socket path in `--version`/help/logs or behind a debug flag. |
| Error display | Tail-preserving truncation (ports/addresses live at the end); surface the full error of the selected row somewhere persistent (e.g. a one-line detail strip). |

## Tasks

### A — modals keep context
- [ ] `view.go` `render()` — for the modal states, render the dimmed list behind
      the centered prompt (lipgloss v2 layers), or keep header+table and replace
      only the footer zone. No modal may leave the rest of the screen blank.

### B — footer pinned to the bottom
- [ ] `view.go` `render()` — pad content to `m.height - footerHeight` so the
      footer sits at the bottom edge across all row counts.
- [ ] (Optional) Add a one-line aggregate stats line
      (`<n> connected · <n> error · <n> off`).

### C — empty state + footer applicability
- [ ] `view.go` `table()` empty path (`view.go:175`) — change the CTA to point
      at `n`; keep the config+R alternative.
- [ ] `view.go` `footer()` — filter entries by applicability (e.g. hide
      `space toggle` / `p passphrase` when the list is empty or none selected).

### D — quit gate + microcopy
- [ ] `update.go` `hasLiveTubers()` (`update.go:688-696`) — count enabled tubers
      in the Error state as live (so `q` during the retry cycle still confirms).
- [ ] Unify microcopy: footer, help (`view.go:398`), and the modal
      (`view.go:428`) must describe the same behavior.

### E — attach header
- [ ] `cmd/root.go:70`, `cmd/attach.go:35` — set the TUI mode to `attach`
      (drop the `@ <socket>` suffix); expose the socket path elsewhere
      (`--version`, help text, or logs).

### F — error display
- [ ] `view.go` `row()` (`view.go:267-269`) — replace `truncate(s.Error, 18)`
      with a tail-preserving truncation (keep the port/address tail).
- [ ] (Optional) Surface the selected row's full error in a persistent one-line
      strip; (bonus) `C` duplicate could auto-bump the local port to avoid
      guaranteed listen conflicts.

### G — bookkeeping
- [ ] `docs/ROADMAP.md` — phase-39 row added at plan time; flip status on
      start/complete.
- [ ] This file — flip status on start/complete.

## Definition of Done

- [ ] Every modal (delete / TOFU / passphrase / password / quit) renders above a
      visible (dimmed) list — the screen is never blank around the prompt.
- [ ] The footer sits at the bottom edge regardless of how many rows the content
      occupies (no footer "jump" when the filter line appears/disappears).
- [ ] The empty state routes the user to `n`; the footer hides keys that do not
      apply to an empty list.
- [ ] Pressing `q` while an enabled tuber is in the Error phase still raises the
      confirm modal (treats it as live); footer / help / modal microcopy agree.
- [ ] In attach mode the header reads `mode: attach` without the socket path.
- [ ] A listen-conflict error keeps the conflicting port visible (not truncated
      before it).
- [ ] No regression at the reference size (120×35).
- [ ] `go build ./...`, `gofmt -l .`, `go vet ./...`, `go test ./...` are clean;
      `make lint` is clean.

## Verification

```sh
make fmt && make vet && make test && make lint

# modal keeps context + footer pinned:
tmux new-session -d -s t -x 120 -y 35
tmux send-keys -t t './bin/portato --force-standalone --config <isolated>' Enter
tmux send-keys -t t 'd'      # delete-confirm: list still visible behind the prompt
tmux capture-pane -ep -t t
tmux kill-session -t t

# empty state CTA + footer:
./bin/portato --force-standalone --config /tmp/empty.yaml   # "press n"; footer filtered

# quit gate during Error: enable a tuber pointing at 127.0.0.1:9, wait for Error, press q → confirm modal
# attach header:
portato daemon &  portato attach    # header reads "mode: attach", no socket path
```

Safety: use an isolated config (`--config`) with `ssh:` hosts set to
`127.0.0.1:9` or a local test sshd; never enable the user's real tubers.

## Technical details / risks

- **lipgloss v2 layers** enable compositing a modal over a dimmed list; the
  cheaper "keep header+table, replace footer zone" variant is lower-risk and may
  be enough — pick based on how clean the layering is.
- **hasLiveTubers change** is behavioral: an Error-state tuber is mid-retry, so
  treating it as live is the correct (safer) choice; it only adds one confirm
  step, never removes one.
- **Attach microcopy** is purely presentational; the socket path stays available
  via other channels, so dropping it from the header loses nothing actionable.
- This phase is independent of phases 37 (theme) and 38 (layout); any order.

## Commit plan (per CONVENTIONS)

1. `docs(phase-39): plan` — this file + ROADMAP row `[ ]`.
2. `docs(phase-39): start` — flip frontmatter + ROADMAP `[ ] -> [~]`.
3. `feat(tui): modals overlay the dimmed list` — task A.
4. `feat(tui): pin footer to bottom edge` — task B.
5. `fix(tui): empty-state CTA, quit gate, attach header, error tail` — tasks C, D, E, F.
6. `docs(phase-39): complete` — `[~] -> [x]` after the DoD passes.

## Start guard

This phase is `status: todo`. It may start only on an explicit "start phase 39"
command (its `depends_on` is empty), and while no other phase is `[~]`.
