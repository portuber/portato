---
phase: 2
title: Forward engine (native SSH, local -L)
status: todo
depends_on: [1]
---

## Goal

Actually forward a local port to a remote host over native SSH (`golang.org/x/crypto/ssh`),
with auto-reconnect, keepalive and a precise state machine. Only **local (-L)** in this phase.
A thread-safe `Engine` as the tunnel manager — `localController` will be built on top of it
in Phase 3.

## Phase scope (what we do)

- `Tunnel` with a state machine: `Off → Connecting → Connected → Error(msg) → Reconnecting`.
- Auth: ssh-agent (`SSH_AUTH_SOCK`) → `identity` key (`ssh.ParsePrivateKey`).
- `HostKeyCallback` via `knownhosts.New`; with `accept_new_hosts: true` — a TOFU wrapper that appends a new key.
- Data flow for local: `net.Listen(local)` → accept-loop → `sshClient.Dial("tcp", remote)` → bidirectional `io.Copy`.
- Reconnect with exponential backoff (1s→2s→…→30s max), reset after 30s of stable `Connected`.
- Keepalive `keepalive@openssh.com` every 30s.
- `Engine`: thread-safe (sync.RWMutex), `List() []Status`, `Enable(name)`, `Disable(name)`, `Restart(name)`, `Reload(cfg)`.

## Phase scope (what we do NOT do)

- Remote (-R) and dynamic (-D) — phases 7, 8.
- IPC/daemon — Phase 4.
- TUI — Phase 3 (but the Engine is ready to be used from the TUI).

## Tasks

- [ ] `glm-complex/internal/forward/ssh.go`:
  - [ ] `func dialSSH(t config.Tunnel, defaults config.Defaults, log *slog.Logger) (*ssh.Client, error)`:
    - build `ssh.ClientConfig` (User, AuthMethod, HostKeyCallback, Timeout: 5s).
    - auth chain: `ssh.PublicKeysCallback` from the agent → on error `ssh.ParsePrivateKey` + `ssh.PublicKeys` from identity.
    - `HostKeyCallback`: `knownhosts.New(defaults.KnownHosts)`; if `defaults.AcceptNewHosts` — a wrapper that appends the key.
    - readable errors: `unknown host: <fingerprint>. Add to known_hosts or set accept_new_hosts: true` / `auth failed: ...` / `connect refused: ...`.
- [ ] `glm-complex/internal/forward/tunnel.go`:
  - [ ] `type State int` with constants `Off, Connecting, Connected, Reconnecting, Error`.
  - [ ] `type Tunnel struct { cfg config.Tunnel; defaults config.Defaults; state State; errMsg string; listener net.Listener; client *ssh.Client; log *slog.Logger; cancel context.CancelFunc; mu sync.RWMutex; connectedAt time.Time }`.
  - [ ] `func New(cfg, defaults, log) *Tunnel`.
  - [ ] `func (t *Tunnel) Start(ctx)`: open the listener, run the accept-loop in a goroutine. State `Connecting` → after the first successful `ssh.Dial`, `Connected` → on error `Error` + start the reconnect-loop.
  - [ ] `func (t *Tunnel) Stop()`: close the listener and client, state `Off`.
  - [ ] `func (t *Tunnel) Restart()`: `Stop()` + `Start()`.
  - [ ] `func (t *Tunnel) Status() Status` — under the mutex: name/state/error/uptime.
  - [ ] accept-loop: for each incoming conn — `client.Dial("tcp", remote)` + two `io.Copy` in both directions (goroutines). On a `client.Dial` error — log warning, close the incoming conn.
  - [ ] reconnect-loop: on SSH disconnect → state `Reconnecting` → backoff (1s→2s→4s→8s→16s→30s cap) → repeat `Start`. Reset backoff after 30s of stable `Connected`.
  - [ ] keepalive-loop: every 30s `client.SendRequest("keepalive@openssh.com", true, nil)` with a 5s timeout; on error — close the client, which triggers the reconnect-loop.
- [ ] `glm-complex/internal/forward/engine.go`:
  - [ ] `type Engine struct { tunnels map[string]*Tunnel; cfg *config.Config; log *slog.Logger; mu sync.RWMutex }`.
  - [ ] `func New(cfg *config.Config, log *slog.Logger) *Engine` — creates tunnels from the config (but does not start them).
  - [ ] `func (e *Engine) Enable(name string) error` — check existence + `Start`.
  - [ ] `func (e *Engine) Disable(name string) error` — `Stop`.
  - [ ] `func (e *Engine) Restart(name string) error` — `Restart`.
  - [ ] `func (e *Engine) UpAll()`, `DownAll()` (used in TUI hotkeys `a`/`x`).
  - [ ] `func (e *Engine) List() []Status` — snapshot under the mutex.
  - [ ] `func (e *Engine) Reload(cfg *config.Config)` — compare with the current set: new ones — add, disappeared ones — `Stop` + remove, changed ones — `Restart`.
  - [ ] `func (e *Engine) StartEnabled()` — start all tunnels with `cfg.Enabled == true` (used by the daemon on startup).
  - [ ] `func (e *Engine) StopAll()` — graceful shutdown of all tunnels.
- [ ] Tests:
  - [ ] `engine_test.go`: test `Enable/Disable/Restart/List` against a fake tunnel (via an interface or mock) — without a real SSH.
  - [ ] `tunnel_test.go` (optional, integration with a localhost sshd or via an `httptest`-style analog) — verify that traffic flows. If too complex — leave for Phase 11.
  - [ ] Factor the backoff calculation out into a function `nextBackoff(attempt int) time.Duration` — unit-test it.

## Definition of Done

- [ ] `go test ./internal/forward/...` is green (engine + backoff unit tests).
- [ ] An enabled tunnel to a real/test SSH server opens a local port (`nc -z 127.0.0.1 <local>` succeeds) and proxies traffic to the remote.
- [ ] `Disable` closes the port (`nc -z` fails).
- [ ] Dropping the SSH session on the server side (kill sshd) → the engine recovers `Connected` on its own via backoff.
- [ ] Backoff resets after ~30s of stable operation (verify with mocks/logs).
- [ ] `List()` reflects correct statuses and error texts (a broken ssh target → `Error` with the reason).
- [ ] `Reload(cfg)` correctly adds/removes/restarts tunnels according to the config diff.
- [ ] `go vet ./...` and `gofmt -l .` are clean.

## Verification

```sh
cd glm-complex
go test ./internal/forward/... -v

# Manual verification (needs a test SSH server, e.g. a local sshd or a container):
# 1. Bring up sshd on localhost:2222 with a test key.
# 2. Configure a tunnel in the config.
# 3. Write a temporary main or use the existing one (see Phase 3):
go run ./cmd/portato attach  # not implemented yet, but a debug command can be added temporarily
# 4. After Enable — check nc -z 127.0.0.1 <local>
# 5. kill sshd → confirm in the logs that the engine reconnects
```

Full manual verification will be available after Phase 3 (TUI). In this phase, unit tests and an integration test in code are sufficient.

## Technical details

- **State machine:**
  ```
  Off --Enable()--> Connecting --ssh.Dial ok--> Connected
                                       \
                                        --ssh.Dial err--> Error
  Connected/Connecting --disconnect--> Reconnecting --backoff--> Connecting
  any --Disable()--> Off
  ```
- **Backoff:** exponential with jitter (optional). Cap 30s. Reset on stable `Connected` ≥ 30s.
- **Keepalive:** `keepalive@openssh.com` — the standard OpenSSH request name. The server usually answers `false` (i.e. "I don't support it"), but it is a signal of channel liveness. On response timeout → consider the session dead.
- **TOFU wrapper:** a simple `HostKeyCallback` that, when `key == nil`, appends the key to known_hosts and returns nil. On mismatch — an error. Implementation: `knownhosts.New` + a custom `HostKeyCallback`.
- **io.Copy:** use `io.Copy` in two goroutines per conn pair; when either side breaks — close both.
- **Read/write mutex:** `List()` is called often from the TUI (every frame/tick); `Enable/Disable` rarely. `RWMutex` is optimal.
- **Do not introduce** abstractions for remote/dynamic — they will be added in Phase 7/8 on top of the existing code (shared SSH-client lifecycle, only the accept/dial flow is specific).

## Phase output artifact

- The `internal/forward` package with a ready-to-use `Engine` — in Phase 3 it will be wrapped by `localController`, and in Phase 4 by `daemon`.
- Proof that a local tunnel actually proxies traffic and survives disconnects.
