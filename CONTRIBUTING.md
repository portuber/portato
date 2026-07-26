# Contributing to Portato

Thanks for considering a contribution! Portato is a small, MIT-licensed project
run on a phase-based workflow. The short version:

- **Small fixes** (bugs, typos, docs, tests) — open a pull request directly.
- **Anything larger** (new features, behaviour changes) — **open an issue first**
  to discuss scope and fit. Larger work lands as a planned
  [phase](./docs/CONVENTIONS.md), so syncing up front avoids wasted effort.

The canonical technical briefing is **[AGENTS.md](./AGENTS.md)** — read it for
layout, build, and conventions. This file covers the process layer.

## Development setup

- Go **1.26+**.
- Build & verify: `make build`, `make run`, `make test`, `make vet`,
  `make lint` (golangci-lint), `make fmt`.
- Local release snapshot (no publish): `make snapshot` (needs goreleaser).
- Running locally: start the daemon (`./bin/portato daemon`) and attach
  (`./bin/portato attach`), or run standalone (`./bin/portato`).

## Code conventions

- Follow the style of surrounding code; run `make fmt` and `make lint` before
  sending a PR. The lint guard catches builtin shadowing (`predeclared`) and
  caps cyclomatic complexity (`gocyclo@15`).
- **No comments** unless genuinely needed (the codebase is comment-light by
  convention).
- **Tests required** for new behaviour; run `make test`.
- Keep changes focused — one logical change per PR.

## Commit messages

Follow [Conventional Commits](./docs/CONVENTIONS.md) — e.g. `fix(forward): …`,
`feat(tui): …`, `docs(readme): …`.

## Licensing

Portato is MIT-licensed (see [LICENSE](./LICENSE)). By submitting a pull request
you agree your contribution is licensed under the same terms. There is no CLA.

## Conduct

Be respectful and constructive. We're all here for the potatoes.
