---
phase: 43
title: "ProxyJump (jump hosts)"
status: in-progress
depends_on: []
---

## Goal

Reach SSH hosts behind a bastion / jump host: a `jump:` field (single hop
or a chain) so a tuber dials its target via one or more intermediates — the
OpenSSH `-J` / `ProxyJump` equivalent. Today `ssh:` is a single hop, so a
host only reachable through a bastion cannot be forwarded to.

## Background

`dialOnce` (`internal/forward/ssh.go:64-95`) opens a TCP connection to
`cfg.Host:cfg.Port` with `net.Dialer`, then wraps it in
`ssh.NewClientConn` — there is no jump-host path (verified: no
`ProxyJump` / `JumpHosts` / `ProxyCommand` anywhere in the codebase, and
the `Tuber` struct has no `jump` field). Multi-hop is the top
user-requested gap (Reddit feedback on the launch post + the awesome-list
comparison with autossh / sshuttle / tund).

The dial already separates the TCP dial from the SSH handshake, so chaining
hops reuses the handshake primitive unchanged (see Technical details).

## Tasks

- [ ] Config: a `jump:` field on `Tuber` — a single `user@host[:port]` or a
      comma-separated chain (`user@edge,user@bastion`); validate each hop
      (reuse the existing `parseSSH` shape). Validate at load time.
- [ ] Dial: when `jump:` is set, dial hop 1 via the existing `net.Dialer`
      path, then each subsequent hop via `prevClient.Dial("tcp", addr)`
      wrapped in `ssh.NewClientConn`; the final client runs the tuber's
      forward (`Listen` / direct-tcpip `Dial`) exactly as today.
- [ ] Each hop honours its own `user` plus the tuber's identity / agent +
      host-key policy. Per-hop identity files are a later refinement — start
      with a shared identity.
- [ ] `docs/SPEC.md` + `config.example.yaml` + `README.md`: a `jump:`
      example + a short note (and a Troubleshooting pointer for bastion
      auth failures).
- [ ] Tests: a two-hop end-to-end via the `sshtest` fixture (edge → target);
      config validation of a malformed `jump:` value.

## Definition of Done

- [ ] A tuber with `jump: bastion` forwards to a target reachable only via
      that bastion (verified via an `sshtest` two-hop setup).
- [ ] A chain of 2+ hops works.
- [ ] A tuber **without** `jump:` is unchanged (zero behaviour change — the
      default single-hop path is untouched).
- [ ] `go build ./...`, `gofmt -l .`, `go vet ./...`, `golangci-lint run
      ./...` are clean; new functions sit under gocyclo 15; the phase's
      tests are green.
- [ ] SPEC + README + `config.example.yaml` updated.

## Verification

```sh
make fmt && make vet && make test && make lint

# manual:
#   two-hop dial through an sshtest bastion to a target host; confirm a
#   -L forward through the chain carries traffic end to end.
```

## Technical details (sketch)

- `dialOnce` (`ssh.go:64-95`) already separates the TCP dial
  (`net.Dialer.DialContext`) from the SSH handshake (`ssh.NewClientConn`).
  To chain hops, replace hop N+1's base dial with
  `prevClient.Dial("tcp", hopAddr)`; the handshake primitive is reused
  unchanged for every hop. Keep the existing context-aware dial +
  `connectTimeout` + host-key callback per hop.
- ssh-config-driven ProxyJump (reading `~/.ssh/config` to *populate* `jump:`
  from an alias's `ProxyJump` directive) is **out of scope** here — a
  separate follow-up phase (see the "Post-1.0 candidate features" backlog in
  `ROADMAP.md`).
- v1.0.x → this is additive (a new optional field), so it ships as a MINOR
  (`v1.1.0`), not a patch.
