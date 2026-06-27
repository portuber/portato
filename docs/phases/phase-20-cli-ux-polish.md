---
phase: 20
title: CLI/UX polish (--log-level, list --json, SOCKS5 auth, fuzzy filter)
status: todo
depends_on: [8, 11, 13]
---

## Goal

Four small, additive improvements: a `--log-level` flag, `portato list --json`
output, SOCKS5 user/pass authentication for `type=dynamic` tunnels, and a
fuzzy `fzf`-style `/` list filter (subsuming the current substring match).

## Tasks

- [ ] `--log-level` root persistent flag (`debug|info|warn|error`); parse to a
      `slog.Level` and propagate to `log.Setup`/the handler. Default `info`.
- [ ] `portato list --json` flag: marshal `[]forward.Status` to stdout (the
      same JSON shape the IPC already returns), one JSON document.
- [ ] SOCKS5 user/pass: `armon/go-socks5` with `Config.AuthMethods =
      []socks5.Authenticator{UserPass}`; config
      `tunnels[].socks5_user` / `socks5_password` (and `defaults.socks5_*`);
      wire a `Credentials` callback; leave `NoAuth` as the fallback when no
      creds are configured (preserves current behavior).
- [ ] Fuzzy `/` filter: replace `strings.Contains` in the TUI filter with a
      fuzzy matcher (`github.com/lithammer/fuzzysearch/fuzzy` or
      `github.com/sahilm/fuzzy`); fall back to substring when the query yields
      no fuzzy matches (so exact-but-unfuzzy tokens still hit).
- [ ] Tests for each feature.

## Definition of Done

- [ ] `portato --log-level debug` surfaces debug log lines (visible in the `l`
      screen / the log file); `error` silences info.
- [ ] `portato list --json | jq '.[0].name'` returns the first tunnel's name;
      the output is valid JSON for 0, 1, and N tunnels.
- [ ] A SOCKS5 client authenticates with the configured user/pass; wrong creds
      are rejected; with no creds configured, NoAuth still works.
- [ ] Typing `dbst` in the `/` filter selects `db-stage`; the filter still
      matches an exact substring when fuzzy fails.
- [ ] `go vet ./...`, `gofmt -l .`, `go test ./...` clean.

## Verification

```sh
./bin/portato --log-level debug daemon &
./bin/portato list --json | jq '.[].name'
./bin/portato                      # in the TUI: press / , type "dbst" -> db-stage highlighted
```

## Technical details

- All four are additive and independent; they can land in any order.
- For SOCKS5 auth, keep the existing loopback-only bind posture; user/pass does
  not change the bind, it gates SOCKS connection setup.
- Pick ONE fuzzy library to avoid dependency sprawl; `lithammer/fuzzysearch`
  is pure-Go and has no transitive deps.
