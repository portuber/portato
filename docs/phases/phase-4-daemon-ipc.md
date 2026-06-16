---
phase: 4
title: Daemon and HTTP-over-unix-socket IPC
status: in-progress
depends_on: [3]
---

## Goal

Run `portato` as a background daemon: it holds tunnels + an HTTP server on a unix socket.
The TUI (`portato attach`) and the CLI (`portato list/enable/...` in Phase 5) talk to the daemon over
this socket. The TUI can be closed — the tunnels keep running. This is the mode for autostart
(Phase 6 will install exactly `portato daemon`).

## Phase scope (what we do)

- `portato daemon` (cobra command) — a background process: Engine + HTTP server on a unix socket.
- HTTP endpoints: `GET /tunnels`, `POST /tunnels/{name}/{enable|disable|restart}`, `POST /reload`, `GET /healthz`.
- Every `enable/disable` persists `enabled` to YAML (via `config.Save`).
- A PID file to guard against a double launch; socket permissions `0600`.
- Graceful shutdown on SIGTERM/SIGINT: close tunnels, remove the socket and the PID file.
- `remoteController` — a `Controller` implementation over an HTTP client with a unix-socket dialer.
- `portato attach` — the TUI that uses `remoteController`.
- The TUI header shows `attach @ <socket>`.

## Phase scope (what we do NOT do)

- Smart-launcher (auto-detection of the daemon in `portato` with no arguments) — Phase 5.
- The "leave in background?" modal — Phase 5 (for now `portato` with no arguments = standalone, no questions asked).
- CLI commands `list/enable/disable/restart` as daemon clients — Phase 5 (for now only the HTTP server + `attach`).
- Push events (`GET /events`) — Phase 9 (for now 1s polling).

## Tasks

- [x] `glm-complex/internal/daemon/server.go`:
  - [x] `type Server struct { engine *forward.Engine; cfg *config.Config; cfgPath, socketPath, pidPath string; log *slog.Logger; srv *http.Server; }`.
  - [x] `func New(cfg, cfgPath, log) (*Server, error)` — compute the socket/PID paths (see SPEC §6), check that no daemon is already running (PID file + live process).
  - [x] HTTP routes (via `http.ServeMux` or a chi analogue — stdlib is fine):
    - `GET /healthz` → `{"ok":true}`.
    - `GET /tunnels` → `[]Status` (JSON, conversion from `engine.List()`).
    - `POST /tunnels/{name}/enable` → `engine.Enable(name)` + update `cfg` (`Enabled=true` for this tunnel) + `config.Save(cfgPath)`.
    - `POST /tunnels/{name}/disable` → `engine.Disable(name)` + `Enabled=false` + `config.Save`.
    - `POST /tunnels/{name}/restart` → `engine.Restart(name)` (no persistence, state does not change).
    - `POST /reload` → `config.Load(cfgPath)` + `engine.Reload(newCfg)` + update `cfg`.
  - [x] `func (s *Server) Start(ctx) error`:
    - create `net.Listen("unix", socketPath)`.
    - `os.Chmod(socketPath, 0600)`.
    - write the PID to `pidPath`.
    - start `engine.StartEnabled()` (tunnels with `Enabled=true`).
    - start `srv.Serve(listener)` in a goroutine.
    - wait for `ctx.Done()` or a signal.
  - [x] `func (s *Server) Shutdown()` — `srv.Shutdown(ctx)`, `engine.StopAll()`, remove the socket and the PID file.
  - [x] Signal handling: `signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)` → triggers `Shutdown()`.
- [x] `glm-complex/internal/client/client.go`:
  - [x] `type Client struct { http *http.Client; socketPath string }`.
  - [x] `http.Client.Transport = &http.Transport{ DialContext: func(...) { net.Dial("unix", socketPath) } }`.
  - [x] `func New(socketPath string) *Client`.
  - [x] `func (c *Client) List() ([]controller.Status, error)`.
  - [x] `func (c *Client) Enable(name) error`, `Disable(name)`, `Restart(name)`.
  - [x] `func (c *Client) Reload() error`.
  - [x] `func (c *Client) Healthz() error` — `GET /healthz`.
- [x] `glm-complex/internal/controller/remote.go`:
  - [x] `type Remote struct { client *client.Client; changes chan struct{}; }`.
  - [x] Implementation of all `Controller` methods via `client.*`.
  - [x] `Changes()` — a goroutine with a `tea.Tick` of 1s, sending into the channel (polling).
  - [x] `Close()` — close the channel (do NOT close the client, it is stateless).
- [x] `glm-complex/internal/cmd/daemon.go` (replace the stub):
  - [x] `RunE`: load the config (`config.Load`), create a logger (file + stderr only), create `daemon.New(...)`, start it.
  - [x] Logs to `xdg.StateHome/portato/daemon.log`.
- [x] `glm-complex/internal/cmd/attach.go` (replace the stub):
  - [x] `RunE`: determine the socket path (same as in the daemon), create `client.New(socketPath)`, call `Healthz()` — on error, a clear message `«daemon not running, try 'portato daemon' or 'portato install'»`.
  - [x] Create `controller.Remote(client, ...)`, call `tui.Run(ctrl)`.
  - [x] The TUI header shows `mode: attach @ <socket>` — this string must be threaded into the Model (via a `Run` option or a field).
- [x] Verification: when the socket is taken (the daemon is already running) — a clear error, not a panic.

## Definition of Done

- [x] `portato daemon` starts up, opens the socket, writes the PID file, handles SIGTERM/SIGINT, and shuts down cleanly (the socket and PID removed).
- [x] The socket has mode `0600`; another user cannot connect (check `curl --unix-socket` under another user — refused).
- [x] On a second `portato daemon` (the daemon already running) — a clear error, not a crash.
- [x] `curl --unix-socket <sock> http://x/healthz` → `{"ok":true}`.
- [x] `curl --unix-socket <sock> http://x/tunnels` → JSON with the list of statuses.
- [ ] `curl -X POST --unix-socket <sock> http://x/tunnels/<name>/enable` enables the tunnel, and in the next `GET /tunnels` it is `Connected`; `enabled: true` has appeared in the YAML.
- [x] `curl -X POST --unix-socket <sock> http://x/tunnels/<name>/disable` → `Off`; `enabled: false` in the YAML.
- [ ] `portato attach` in another terminal opens a TUI that actually drives the daemon's tunnels.
- [ ] Closing `portato attach` (`q`) does **not** bring down the daemon's active tunnels (raise a tunnel → close the TUI → traffic still flows).
- [ ] The TUI header shows `attach @ <socket>`.
- [x] `go vet ./...` and `gofmt -l .` are clean.

> Implementation is complete. Clearing the remaining three items (enable→`Connected`, `attach` TUI, header, closing attach without losing tunnels) requires interactive verification against a live sshd — as in Phase 3. Persisting `enabled` to YAML, the transition to `Off`, and a clean lifecycle are confirmed by a smoke test (`curl --unix-socket`).

## Verification

```sh
cd glm-complex
make build

# Terminal A:
./bin/portato daemon &            # background
SOCK="${XDG_RUNTIME_DIR:-$HOME/.config/portato}/portato.sock"
curl --unix-socket "$SOCK" http://x/healthz
curl --unix-socket "$SOCK" http://x/tunnels
curl -X POST --unix-socket "$SOCK" http://x/tunnels/<name>/enable
curl --unix-socket "$SOCK" http://x/tunnels                # status changed
nc -z 127.0.0.1 <local_port>                                # success — the tunnel works

# Terminal B:
./bin/portato attach             # the TUI connects to the daemon
# space — toggle (visible in both terminals)
# q — quit; the daemon's tunnels keep running

# Terminal A (continued):
kill -TERM $(cat $HOME/.config/portato/portato.pid)              # graceful shutdown of the daemon
ls "$SOCK" 2>/dev/null && echo "FAIL: socket not removed" || echo "OK"
```

## Technical details

- **Unix-socket path:**
  - Linux: `$XDG_RUNTIME_DIR/portato.sock` (usually `/run/user/<uid>/portato.sock`, created by systemd).
  - macOS: `$(xdg.RuntimeDir)/portato.sock` — `xdg.RuntimeDir` on macOS points to `/var/folders/.../T/`; if empty — fall back to `$HOME/.config/portato/portato.sock`.
  - Both the daemon and the clients compute the same path — factor it into a shared function `internal/daemon/paths.go` (or into `config`).
- **HTTP framework:** stdlib `net/http` + `http.ServeMux`. Routes with `{name}` — parse manually (path segments) or use chi/gorilla. To avoid a new dependency, stdlib + simple path parsing is fine for the MVP.
- **PID-file liveness check:** at daemon startup, read the PID, send `signal 0` (`syscall.Kill(pid, 0)`) — on success the process is alive, abort startup. On error (ESRCH) — the process is dead, remove the stale PID file and start.
- **Persist invariant:** every `enable/disable` must: (1) change the Engine state, (2) update `cfg` in memory, (3) call `config.Save(cfgPath)`. Do this under a single mutex to avoid races.
- **Reload vs Save race:** if an `enable` (writes the file) and a `reload` (reads the file) arrive at the same time — synchronization is needed. Solution: all operations on `cfg` and the file go through a single point (e.g. a method `Server.mutate(fn func(*config.Config))`).
- **Graceful shutdown:** `signal.NotifyContext` gives a ctx that is cancelled on a signal; in `Start` wait for `<-ctx.Done()`, then `Shutdown`. Shutdown timeout — 10s.
- **HTTP client unix-dialer:** via a custom `http.Transport.DialContext`. The `http.Client` can be stateless, reused through `*Client`.
- **Healthz without auth:** used by the smart-launcher (Phase 5) to check the daemon's liveness. Access is limited by the socket permissions (0600).

## Phase output artifact

- A full-fledged `portato daemon` + `portato attach`. You can keep tunnels in the background, attach a TUI to them, close the TUI without losing the tunnels.
- A ready `Controller` (local + remote) — the foundation for the smart-launcher in Phase 5.
