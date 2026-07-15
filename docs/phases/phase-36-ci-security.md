---
phase: 36
title: CI security hardening (govulncheck + lint in CI)
status: in-progress
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
- [ ] `.github/workflows/security.yml` — `govulncheck` job: checkout + setup-go
      (`go-version-file: go.mod`) + `go mod download` + `go mod verify` +
      install govulncheck + `govulncheck ./...`. `permissions: contents: read`.
      Triggers: pull_request, push (main/master), schedule (weekly cron).

### B — lint job in CI
- [ ] `.github/workflows/ci.yml` — add `lint` job using
      `golangci/golangci-lint-action@v6` pinned to v1.x, running
      `golangci-lint run ./...` against the existing `.golangci.yml`.

### C — README
- [ ] `README.md` — add the `security` workflow badge to the badge row.

### D — phase bookkeeping
- [ ] `docs/ROADMAP.md` — add phase 36 row `[ ]`, summary line.
- [ ] This file — flip status on start/complete.

## Definition of Done

- [ ] `security.yml` runs `govulncheck ./...` and is green on main (0 reachable
      vulnerabilities, or known-ignored ones documented).
- [ ] The weekly cron schedule is present (proves it will catch new CVEs in
      deps without an open PR).
- [ ] `ci.yml` `lint` job is green on main.
- [ ] The `lint` job actually fails on a real violation: temporarily
      reintroduce a builtin-`max` shadow in production code → job goes red;
      revert. (Proves it is not a no-op.)
- [ ] `make lint` (local) and the CI `lint` job use the **same** `.golangci.yml`
      — no drift.
- [ ] The `security` badge is in the README and renders.
- [ ] ROADMAP + this file: status flips on start/complete.

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

This phase is `status: in-progress`. It was started on an explicit
"start phase 36" command (per docs/CONVENTIONS.md). The `[~]` flip and the
implementation commits are landing now; it returns to `status: done` on
"complete phase 36" once the DoD is met.
