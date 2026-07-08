---
phase: 28
title: config reload — portato reload + file watch
status: in-progress
depends_on: []
---

## Goal

The daemon picks up `config.yaml` changes without a restart: a `portato reload`
CLI command (manual) and automatic reload when the file changes (file watch).
Today editing the config has no effect on a running daemon (no watch, no CLI
reload even though `POST /reload` exists) — a surprise hit during the Phase 16
E2E, where an edit to `enabled:` was ignored by the live daemon.

## Background

`POST /reload` (`handleReload` → `applyReload`) exists but no CLI surfaces it,
and nothing watches the file. Phase 27 adds `stop`; this phase adds reload in
both forms.

## Tasks

- [x] `internal/cmd/reload.go`: a cobra command that resolves the socket and
      POSTs `/reload` via `client.Client`; report the result.
- [x] Register `reloadCmd` in `Execute()`; add to the root help; `make reload`
      target.
- [x] Daemon: a file watcher on the config path (fsnotify, or polling fallback)
      that debounces (~500ms) and triggers `applyReload`; tolerates parse errors
      (keep the last-good config, log).
- [x] Tests: reload command (ok / no-daemon / error); watch applies an edit;
      a syntactically bad edit keeps the last-good config and logs.

## Definition of Done

- [ ] `portato reload` makes the daemon re-read the config (an edit takes effect
      without a restart).
- [ ] Editing `config.yaml` while the daemon runs applies within ~1s (no reload
      or restart needed).
- [ ] A syntactically bad edit does NOT crash the daemon (keeps last-good,
      logs the error).
- [ ] `go vet ./...`, `gofmt -l .`, `go test ./...` clean; cross-compilation
      darwin/linux × amd64/arm64 green.

## Verification

```sh
./bin/portato daemon --config ./config.yaml &
# add a tunnel to config.yaml and save
./bin/portato reload          # or just save (watch) -> the new tunnel appears
./bin/portato list           # new tunnel present
```

## Technical details

- Dependency candidate: `fsnotify/fsnotify` (verify it is not already a dep
  first); a polling fallback keeps the watcher cross-platform and CGO-free.
- Debounce coalesces an editor's save-burst; the reload path is the existing
  `applyReload`, so manual and auto reload share one code path.
