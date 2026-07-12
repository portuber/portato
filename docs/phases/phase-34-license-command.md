---
phase: 34
title: "`portato license` command + `--license` flag"
status: in-progress
depends_on: []
---

## Goal

Let the binary self-report its licensing: a `portato license` subcommand and a
`--license` root flag (parallel to the existing `portato version` / `--version`
pair) that print the project's MIT license, the source URL, and a pointer to
the bundled third-party notices. A `--full` flag on the subcommand dumps the
full MIT License text (embedded). License info thus reaches the user without
the repo; the terse `--version` output is left untouched (Go-CLI convention).

## Background

After Phase 32, release archives and deb/rpm carry a bundled
`THIRD_PARTY_LICENSES.txt`. License info otherwise lives in the README /
`LICENSE` / that bundled file. A `portato license` command makes the binary
itself a source of license information — useful for packaged / offline /
audited environments. We deliberately keep this **out of `--version`** (which
stays version+commit+date, the Go-CLI norm) and put it behind its own command
and flag.

## Design decisions (locked)

| Aspect | Decision |
|---|---|
| Forms | **Both**: `portato license` subcommand **and** `--license` root flag (parallel to `version` / `--version`). |
| Output | A shared helper `printLicense(w io.Writer, full bool)` renders the short summary; `full=true` appends the embedded MIT LICENSE text. |
| Short summary | Project name + version, `License: MIT` + source URL, a note that the binary embeds MIT/Apache-2.0/BSD-3-Clause software, and a pointer to `THIRD_PARTY_LICENSES.txt` (release archive; `/usr/share/doc/portato/THIRD_PARTY_LICENSES.txt` for deb/rpm). Ends with a `portato license --full` hint. |
| `--full` | Local flag on the `license` subcommand only (no `--license --full` flag combination). |
| LICENSE text | Embedded via `//go:embed LICENSE` in the `cmd` package; printed verbatim under a separator on `--full`. |
| Third-party notices | **Not** embedded in the binary (avoids binary bloat and drift); the command only points to the external bundled file. |
| `--version` | Unchanged — license stays out of the version banner. |
| Versioning | A new CLI command **and** flag is additive user-facing behaviour = **MINOR** per `docs/VERSIONING.md`; the next release after this lands is **v0.2.0**, not a patch. |

## Tasks

### `internal/cmd/license.go` (new)
- [ ] `licenseCmd` (`Use: "license"`, `Short: "Print license information"`) with
      a local `--full` bool flag.
- [ ] `printLicense(w io.Writer, full bool)` — renders the short summary
      (project name + version from the embedded vars, MIT + source URL, the
      bundled-notices pointer, the `--full` hint); when `full`, append a
      separator and the embedded MIT LICENSE text.
- [ ] `//go:embed LICENSE` → `licenseBytes []byte` for the `--full` output.

### `internal/cmd/root.go`
- [ ] Register `licenseCmd` in `Execute()` alongside the other subcommands.
- [ ] Add `rootCmd.Flags().BoolVar(&showLicense, "license", false, "print license information and exit")`.
- [ ] In `rootRunE`, handle `showLicense` before the standalone/attach logic
      (next to the existing `showVersion` check): `printLicense(cmd.OutOrStdout(), false); return nil`.
      Pin `licenseCmd` to the default help template (like every other subcommand)
      so the easter-egg footer does not leak.

### Tests — `internal/cmd/license_test.go` (new)
- [ ] `TestLicenseShort`: `printLicense(&b, false)` output contains `MIT`, the
      source URL, `THIRD_PARTY_LICENSES.txt`, and the `--full` hint; does not
      contain the full MIT body ("PERMISSION IS HEREBY GRANTED" absent).
- [ ] `TestLicenseFull`: `printLicense(&b, true)` contains the short summary
      **and** the full MIT body (e.g. "OUT OF OR IN CONNECTION WITH THE SOFTWARE").
- [ ] `TestLicenseCmdRun`: `portato license` via the cobra command writes the
      short summary to stdout; `--full` writes the long form.
- [ ] `TestRootLicenseFlag`: `portato --license` (rootRunE path) prints the
      short summary and returns nil (does not start the daemon/TUI).

### Docs
- [ ] `README.md`: add a `portato license` row to the mode/command table.
- [ ] `docs/SPEC.md`: add `license` to the CLI command list.
- [ ] `docs/VERSIONING.md`: no change needed — the general "new CLI command /
      flag = MINOR" rule already covers this; the phase records the resulting
      v0.2.0 bump.

## Definition of Done

- [ ] `portato license` prints the short summary (version, MIT + URL, bundled
      pointer, `--full` hint); `portato license --full` appends the full MIT
      License text.
- [ ] `portato --license` prints the same short summary and exits cleanly
      (does not start the daemon or TUI).
- [ ] `portato --help` lists `license`; `portato license --help` documents
      `--full`; neither inherits the easter-egg footer.
- [ ] `--version` output is unchanged (no license line added there).
- [ ] `make fmt && make vet && make test && make lint` clean; `gofmt -l .` empty.
- [ ] README command table + SPEC updated.

## Verification

```sh
make fmt && make vet && make test && make lint
./bin/portato license            # short summary
./bin/portato license --full     # + full MIT text
./bin/portato --license          # short summary, then exit (no TUI/daemon)
./bin/portato --help | grep license
./bin/portato --version          # unchanged — no license line
```

## Technical details / risks

- **Shared renderer.** Both the subcommand and the root flag call the same
  `printLicense(w, full)`, so their short output is byte-identical (consistency,
  one place to edit). The subcommand's `--full` is the only divergence.
- **Version string.** `printLicense` reads the same `version`/`commit`/`date`
  vars `version.go` uses; for `--full` the embedded `LICENSE` is canonical
  (single source, no drift).
- **Embed path.** `//go:embed LICENSE` embeds the repo-root `LICENSE`; the
  `cmd` package is at `internal/cmd/`, so the embed directive needs the file
  reachable from there — use `//go:embed LICENSE` with the file at the module
  root by placing the embed in a package that can reach it, or embed at the
  module root and reference. Resolve the exact embed path at implementation.
- **Versioning.** This is a MINOR: the release after this phase is `v0.2.0`
  (new CLI command + flag), not a patch — flag it in the release commit.

## Commit plan (per CONVENTIONS)

1. `docs(phase-34): plan` — create this file + the ROADMAP row (`[ ]`). ✅
2. On "start phase 34": `docs(phase-34): start` — flip `[ ] -> [~]`.
3. `feat(cmd): add portato license command and --license flag` —
   `internal/cmd/license.go` + tests + `root.go` wiring + README/SPEC.
4. `docs(phase-34): complete` — `[~] -> [x]` after the DoD passes.

## Start guard

This phase is `status: todo`. It starts only on an explicit **"start phase 34"**
command (per `docs/CONVENTIONS.md`). The first action then is to flip the
frontmatter + ROADMAP row `[ ] -> [~]` (commit `docs(phase-34): start`) — not
before.
