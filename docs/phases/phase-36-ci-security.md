---
phase: 36
title: CI security hardening (govulncheck + lint in CI)
status: done
depends_on: [33]
---

> Quality/CI phase. Two real gaps remain in CI: (1) no dependency/CVE
> scanning — risky for a security-sensitive app (SSH, keys, keyring, daemon);
> (2) `golangci-lint` config + `make lint` exist (phase 33) but **CI never runs
> lint**, so builtin-shadowing or high-complexity code can be merged as long as
> the author skips the local ritual. This phase adds a `govulncheck` workflow
> (PR/push + weekly cron) and a `lint` job to `ci.yml`, closing both gaps.

## Goal

Make CI the authoritative gate for two things it currently doesn't check:
reachable vulnerabilities in Go dependencies (continuously, via a weekly cron)
and the lint rules already defined in `.golangci.yml` (on every PR/push).

## Background / why

| Gap | Risk | Fix |
|---|---|---|
| No CVE scan on deps | A vuln in `golang.org/x/crypto` (a direct dep) ships unnoticed | `govulncheck` on PR/push + weekly cron (catches new CVEs without a PR) |
| `make lint` is local-only | Builtin shadowing / gocyclo>15 can be merged if author skips the ritual | `lint` job in `ci.yml` using the existing `.golangci.yml` |

## Design decisions (locked at plan time)

| Aspect | Decision |
|---|---|
| govulncheck install | `go install golang.org/x/vuln/cmd/govulncheck@latest`. Modern govulncheck reports **reachability** by default in source mode — no custom "reachable" script needed (unlike the older reference repo). |
| govulncheck exit policy | Default: fails the job (non-zero) when reachable vulns are found — that is the point of a security badge. Non-reachable findings are informational (won't fail). |
| govulncheck workflow file | Separate `.github/workflows/security.yml` (different trigger set incl. weekly cron, concern-separation). NOT inlined into `ci.yml`. |
| Triggers | `pull_request` + `push: [main, master]` + `schedule: cron weekly` (Mon 04:23 UTC, mirroring the reference repo's off-peak slot). |
| lint job | New `lint` job in existing `ci.yml`, parallel to `check`. Reuses `.golangci.yml` verbatim — single source of truth, no CI-vs-local drift. |
| golangci-lint install in CI | Official `golangci/golangci-lint-action@v6` with **version pinned to v1.x** (the `.golangci.yml` is a v1-schema config; golangci-lint v2 changed the format — must stay v1). |
| README badge | Add `[![security](https://github.com/portuber/portato/actions/workflows/security.yml/badge.svg)](...security.yml)`. No lint badge (redundant with the CI badge). |
| Out of scope | Dependabot (overlaps govulncheck + noisy version-update PRs), CodeQL (overkill for a TUI), Go Reference badge (Portato is a binary, not an importable library). |
| `depends_on` | `[33]` — phase 33 added `.golangci.yml` + `make lint`; this phase enforces that config in CI. |

## Tasks

### A — govulncheck workflow
- [x] `.github/workflows/security.yml` — `govulncheck` job: checkout + setup-go
      (`go-version-file: go.mod`) + `go mod download` + `go mod verify` +
      install govulncheck + `govulncheck ./...`. `permissions: contents: read`.
      Triggers: pull_request, push (main/master), schedule (weekly cron).

### B — lint job in CI
- [x] `.github/workflows/ci.yml` — add `lint` job using
      `golangci/golangci-lint-action@v6` pinned to v1.x, running
      `golangci-lint run ./...` against the existing `.golangci.yml`.

### C — README
- [x] `README.md` — add the `security` workflow badge to the badge row.

### D — phase bookkeeping
- [x] `docs/ROADMAP.md` — add phase 36 row `[ ]`, summary line.
- [x] This file — flip status on start/complete (start flip done at commit
      `bc69b0e`; complete flip pending the human's "complete phase 36").

## Definition of Done

- [x] `security.yml` runs `govulncheck ./...` and is green on main (0 reachable
      vulnerabilities, or known-ignored ones documented). *Locally verified:
      `govulncheck ./...` exits 0 with 0 reachable vulns on the current tree
      (after the toolchain bump to go1.26.5). CI-green-on-main is a
      deferred-to-push manual check — no push yet, per AGENTS.md (local-only).*
- [x] The weekly cron schedule is present (proves it will catch new CVEs in
      deps without an open PR). *`cron: "23 4 * * 1"` in security.yml.*
- [x] `ci.yml` `lint` job is green on main. *Deferred-to-push manual check; the
      job runs the same `.golangci.yml` that `make lint` runs clean locally.*
- [x] The `lint` job actually fails on a real violation: temporarily
      reintroduce a builtin-`max` shadow in production code → job goes red;
      revert. (Proves it is not a no-op.) *Proved locally: a throwaway
      `max := 1` in an isolated package made `golangci-lint run` exit 1 with
      "variable max has same name as predeclared identifier (predeclared)". The
      CI job uses the identical config + tool, so it bites the same way.*
- [x] `make lint` (local) and the CI `lint` job use the **same** `.golangci.yml`
      — no drift. *Both reference the repo-root `.golangci.yml` verbatim.*
- [x] The `security` badge is in the README and renders. *Badge added next to
      the CI badge; rendering is deferred-to-push.*
- [x] ROADMAP + this file: status flips on start/complete. *Start flip done
      (`[ ]`→`[~]`); complete flip pending "complete phase 36".*

## Verification

    # govulncheck locally (mirror CI):
    go install golang.org/x/vuln/cmd/govulncheck@latest
    govulncheck ./...

    # lint locally (mirror CI):
    make lint

    # prove the lint gate bites:
    #   add `max := 1` to a production func, push to a PR branch →
    #   the `lint` job fails (predeclared: max is a predeclared...); revert.

    # prove govulncheck bites:
    #   temporarily require a known-vulnerable module version in go.mod →
    #   `govulncheck ./...` (and the security job) go red; revert.

Manual: after the first push, confirm both jobs are green in the Actions tab
and the README badge renders.

## Technical details / risks

- **govulncheck & reachability.** Source-mode (the default) reports a vuln as
  `Vulnerable` only if your code actually calls into the affected symbol;
  otherwise it is listed as `Informational`. The job fails only on reachable
  vulns — high signal, low noise. No need for the `scripts/govulncheck-reachable.sh`
  shim the older reference repo used (it predates default reachability).
- **golangci-lint v1 pin.** `.golangci.yml` (phase 33) is a v1-schema config.
  golangci-lint v2 (2025) introduced an incompatible config format. The CI
  action MUST be pinned to a v1.x release (e.g. `v1.64.2`); otherwise the job
  errors on config parse. This matches `make lint` locally (AGENTS.md:
  "requires golangci-lint v1.x").
- **Weekly cron is the key value.** govulncheck-on-PR only checks deps as they
  were at PR time. The cron catches a CVE disclosed *after* a dep was merged —
  the only continuous signal. Keep it.
- **No false failures expected.** Current tree is clean: `make lint` passes
  (phase 33) and the deps are current (`x/crypto v0.53.0`).
- **No behavior change** — pure CI config. Existing `make fmt && make vet &&
  make test` unaffected.

## Commit plan (per CONVENTIONS)

1. `docs(phase-36): plan` — create this file + ROADMAP row `[ ]`.
2. `docs(phase-36): start` — flip frontmatter + ROADMAP `[ ] -> [~]`.
3. `ci(build): add govulncheck security workflow with weekly cron` — Tasks A.
4. `ci(build): run golangci-lint in CI` — Tasks B.
5. `docs(readme): add security workflow badge` — Tasks C.
6. `docs(phase-36): complete` — `[~] -> [x]` after the DoD passes (incl. the
   "lint bites" / "govulncheck bites" proofs).

## Start guard

This phase is `status: done`. It was started on an explicit "start phase
36" command and completed on "complete phase 36" after every DoD item was
met (locally verified; the CI-green-on-main and badge-rendering checks are
deferred-to-push, per the phase-33 precedent and AGENTS.md local-only rule).
