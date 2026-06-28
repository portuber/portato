package daemon

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/forward"
	"github.com/kipkaev55/portato/internal/ipctoken"
	routelog "github.com/kipkaev55/portato/internal/log"
)

const shutdownTimeout = 10 * time.Second

// ipcTokenDisabled, when true, makes New build a server that does not generate
// or enforce the IPC bearer token — the --ipc-token off escape hatch
// (PORTATO_NO_IPC_TOKEN=1). Default false: production authenticates IPC. Set
// once from the root command before New runs.
var ipcTokenDisabled bool

// SetIpcTokenDisabled toggles the escape hatch. Intended to be called once
// from the root command's daemon run when --ipc-token off / the env var is set.
func SetIpcTokenDisabled(disabled bool) { ipcTokenDisabled = disabled }

// tunneler is the subset of *forward.Engine the server depends on. Kept
// unexported so the concrete Engine stays the production type while tests
// can substitute a fake (the SSH path itself is covered in the forward pkg).
type tunneler interface {
	List() []forward.Status
	Enable(name string) error
	Disable(name string) error
	Restart(name string) error
	Reload(*config.Config)
	StartEnabled()
	StopAll()
	// Subscribe returns a channel that fires on every tunnel state change
	// plus an unsubscribe func. Drives the GET /events SSE stream (Phase 9).
	Subscribe() (<-chan struct{}, func())
}

// Server is the background daemon: it owns the tunnel Engine and exposes it
// over an HTTP server bound to a unix-domain socket (SPEC §6).
type Server struct {
	engine     tunneler
	cfg        *config.Config
	cfgPath    string
	socketPath string
	markerPath string
	log        *slog.Logger
	logs       *routelog.Ring

	// ipcToken controls whether Start generates and enforces a bearer token.
	// Set on the production server from the escape-hatch flag; the test helper
	// leaves it false so existing handler tests run without auth.
	ipcToken bool
	// token is the bearer token once Start has generated it; "" means no auth
	// (routes wraps in authmw only when non-empty).
	token string
	// tokenPath is where the token file is written/removed, next to the socket.
	tokenPath string

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	srv      *http.Server
	listener net.Listener

	shutdownOnce sync.Once
}

// New prepares a daemon for cfg/cfgPath: it resolves the socket path (a
// runtime location, or the --socket override) and the stable discovery marker
// path, and refuses to start if another live daemon holds them (stale markers
// are cleaned).
func New(cfg *config.Config, cfgPath string, log *slog.Logger, ring *routelog.Ring) (*Server, error) {
	if log == nil {
		log = slog.Default()
	}
	socketPath, err := resolveListenSocket()
	if err != nil {
		return nil, fmt.Errorf("resolve socket path: %w", err)
	}
	markerPath, err := DiscoveryPath()
	if err != nil {
		return nil, fmt.Errorf("resolve discovery path: %w", err)
	}
	if err := ensureNotRunning(markerPath, socketPath); err != nil {
		return nil, err
	}
	s := newServer(nil, cfg, cfgPath, socketPath, markerPath, log, ring)
	s.engine = forward.NewEngine(s.ctx, cfg, log)
	s.ipcToken = !ipcTokenDisabled
	return s, nil
}

// resolveListenSocket picks the socket the daemon binds: the --socket /
// PORTATO_SOCKET override if set, otherwise the runtime-dir default.
func resolveListenSocket() (string, error) {
	if ov := SocketOverride(); ov != "" {
		return ov, nil
	}
	return RuntimeSocketPath()
}

func newServer(engine tunneler, cfg *config.Config, cfgPath, socketPath, markerPath string, log *slog.Logger, ring *routelog.Ring) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		engine:     engine,
		cfg:        cfg,
		cfgPath:    cfgPath,
		socketPath: socketPath,
		markerPath: markerPath,
		log:        log,
		logs:       ring,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Socket returns the unix-socket path the daemon binds (for logging/display).
func (s *Server) Socket() string { return s.socketPath }

// Start binds the socket, writes the discovery marker (advertising the socket
// path + PID), starts the enabled tunnels and serves HTTP until ctx is
// cancelled (or serving fails). It always shuts down cleanly on return. SPEC §6.
func (s *Server) Start(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.socketPath, err)
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = ln.Close()
		s.cleanup()
		return fmt.Errorf("chmod socket: %w", err)
	}
	if err := WriteMarker(s.markerPath, s.socketPath, os.Getpid()); err != nil {
		_ = ln.Close()
		s.cleanup()
		return fmt.Errorf("write discovery marker: %w", err)
	}
	if s.ipcToken {
		s.tokenPath = ipctoken.TokenPath(s.socketPath)
		tok, err := ipctoken.GenerateToken()
		if err != nil {
			_ = ln.Close()
			s.cleanup()
			return fmt.Errorf("generate ipc token: %w", err)
		}
		if err := ipctoken.WriteToken(s.tokenPath, tok); err != nil {
			_ = ln.Close()
			s.cleanup()
			return fmt.Errorf("write ipc token: %w", err)
		}
		s.token = tok
		s.log.Info("ipc token written", "path", s.tokenPath)
	}
	s.listener = ln
	s.srv = &http.Server{Handler: s.routes()}
	s.engine.StartEnabled()

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.srv.Serve(ln) }()
	s.log.Info("daemon listening", "socket", s.socketPath, "pid", os.Getpid())

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("serve failed", "err", err)
		}
	}
	return s.Shutdown()
}

// Shutdown stops the HTTP server, tears down all tunnels and removes the
// socket and the discovery marker. Safe to call once.
func (s *Server) Shutdown() error {
	s.shutdownOnce.Do(func() {
		s.cancel()
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if s.srv != nil {
			_ = s.srv.Shutdown(shutCtx)
		}
		s.engine.StopAll()
		s.cleanup()
		s.log.Info("daemon stopped")
	})
	return nil
}

func (s *Server) cleanup() {
	_ = os.Remove(s.socketPath)
	_ = RemoveMarker(s.markerPath)
	if s.tokenPath != "" {
		_ = os.Remove(s.tokenPath)
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /tunnels", s.handleList)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /logs", s.handleLogs)
	mux.HandleFunc("POST /tunnels/{name}/enable", s.handleEnable)
	mux.HandleFunc("POST /tunnels/{name}/disable", s.handleDisable)
	mux.HandleFunc("POST /tunnels/{name}/restart", s.handleRestart)
	mux.HandleFunc("POST /tunnels/{name}/accept-host", s.handleAcceptHost)
	mux.HandleFunc("POST /reload", s.handleReload)
	mux.HandleFunc("GET /config", s.handleGetConfig)
	mux.HandleFunc("POST /tunnels", s.handleAddTunnel)
	mux.HandleFunc("PUT /tunnels/{name}", s.handleUpdateTunnel)
	mux.HandleFunc("DELETE /tunnels/{name}", s.handleDeleteTunnel)
	if s.token == "" {
		return mux
	}
	return s.authmw(mux)
}

// authmw rejects any request whose Authorization header is not the bearer token
// the daemon generated at startup. Constant-time comparison avoids leaking the
// token via timing. Only installed when s.token != "" (a daemon started with
// --ipc-token off, or a test server, serves unauthenticated).
func (s *Server) authmw(next http.Handler) http.Handler {
	want := "Bearer " + s.token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.List())
}

// handleLogs returns the recent in-memory log entries for a tunnel from the
// daemon's ring buffer. Optional ?name= filters by tunnel; without it every
// tunnel's entries are returned. Nil-safe: a daemon with no ring returns an
// empty list. Phase 11 (TUI logs screen).
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	writeJSON(w, http.StatusOK, s.logs.Lines(name))
}

// eventHeartbeat is how often the SSE stream emits a comment line to keep the
// connection alive through proxies/timeouts. Over a unix socket this is mostly
// hygiene, but it lets a stalled client detect a dead stream promptly.
const eventHeartbeat = 15 * time.Second

// handleEvents streams tunnel state-change signals to the client as SSE
// (Server-Sent Events). Each signal is a signal-only `data: {}` frame: the
// client reacts by re-fetching GET /tunnels. The stream stays open until the
// client disconnects (context done) or serving ends. SPEC §6 (Phase 9).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Flush one frame immediately so a freshly attached client knows the
	// stream is live and can do an initial List() + redraw without waiting.
	writeEvent := func(frame string) bool {
		if _, err := fmt.Fprintf(w, "%s\n\n", frame); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !writeEvent("data: {}") {
		return
	}

	ch, unsub := s.engine.Subscribe()
	defer unsub()

	heartbeat := time.NewTicker(eventHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			if !writeEvent("data: {}") {
				return
			}
		case <-heartbeat.C:
			if !writeEvent(": heartbeat") {
				return
			}
		}
	}
}

func (s *Server) handleEnable(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasTunnel(name) {
		writeError(w, http.StatusNotFound, "unknown tunnel %q", name)
		return
	}
	if !s.isUp(name) {
		if err := s.engine.Enable(name); err != nil {
			writeError(w, http.StatusInternalServerError, "enable: %v", err)
			return
		}
	}
	s.setEnabled(name, true)
	if err := s.cfg.Save(s.cfgPath); err != nil {
		writeError(w, http.StatusInternalServerError, "persist config: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled", "tunnel": name})
}

func (s *Server) handleDisable(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasTunnel(name) {
		writeError(w, http.StatusNotFound, "unknown tunnel %q", name)
		return
	}
	if err := s.engine.Disable(name); err != nil {
		writeError(w, http.StatusInternalServerError, "disable: %v", err)
		return
	}
	s.setEnabled(name, false)
	if err := s.cfg.Save(s.cfgPath); err != nil {
		writeError(w, http.StatusInternalServerError, "persist config: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled", "tunnel": name})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasTunnel(name) {
		writeError(w, http.StatusNotFound, "unknown tunnel %q", name)
		return
	}
	if err := s.engine.Restart(name); err != nil {
		writeError(w, http.StatusInternalServerError, "restart: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restarted", "tunnel": name})
}

// handleAcceptHost appends the tunnel's pending unknown-host key (captured by
// the SSH host-key callback) to known_hosts and restarts it, so the tunnel
// connects on the next dial. Phase 11 TUI TOFU prompt.
func (s *Server) handleAcceptHost(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasTunnel(name) {
		writeError(w, http.StatusNotFound, "unknown tunnel %q", name)
		return
	}
	line := ""
	for _, st := range s.engine.List() {
		if st.Name == name {
			line = st.PendingHostLine
			break
		}
	}
	if line == "" {
		writeError(w, http.StatusConflict, "no pending host key for %q", name)
		return
	}
	hosts := s.cfg.Defaults.ResolvedKnownHosts()
	if err := forward.AppendKnownHostLine(hosts, line); err != nil {
		writeError(w, http.StatusInternalServerError, "append known_hosts: %v", err)
		return
	}
	if err := s.engine.Restart(name); err != nil {
		writeError(w, http.StatusInternalServerError, "restart: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted", "tunnel": name})
}

func (s *Server) handleReload(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.applyReload(); err != nil {
		writeError(w, http.StatusInternalServerError, "reload config: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// applyReload re-reads the config from disk, applies it to the engine and
// swaps the server's in-memory copy. Shared by POST /reload and the tunnel
// mutation handlers (which patch the file first, then reload). Phase 10.
func (s *Server) applyReload() error {
	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		return err
	}
	s.engine.Reload(cfg)
	s.cfg = cfg
	return nil
}

// handleGetConfig returns the daemon's current configuration as JSON. The
// daemon owns the real config path (it may have been started with --config),
// so attached clients read it through the API rather than touching disk.
// Phase 10.
func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.cfg)
}

// handleAddTunnel creates a tunnel: validate the prospective config, then
// apply a comment-preserving append to the YAML file and reload.
func (s *Server) handleAddTunnel(w http.ResponseWriter, r *http.Request) {
	t, err := decodeTunnel(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasTunnel(t.Name) {
		writeError(w, http.StatusConflict, "tunnel %q already exists", t.Name)
		return
	}
	if _, err := s.cfg.WithTunnelAdded(t); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if err := config.AddTunnelNode(s.cfgPath, t); err != nil {
		writeError(w, http.StatusInternalServerError, "persist: %v", err)
		return
	}
	if err := s.applyReload(); err != nil {
		writeError(w, http.StatusInternalServerError, "reload: %v", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "tunnel": t.Name})
}

// handleUpdateTunnel replaces the tunnel named {name} with the body (rename
// allowed): validate the prospective config, patch the file, reload.
func (s *Server) handleUpdateTunnel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	t, err := decodeTunnel(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasTunnel(name) {
		writeError(w, http.StatusNotFound, "unknown tunnel %q", name)
		return
	}
	if _, err := s.cfg.WithTunnelReplaced(name, t); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if err := config.ReplaceTunnelNode(s.cfgPath, name, t); err != nil {
		writeError(w, http.StatusInternalServerError, "persist: %v", err)
		return
	}
	if err := s.applyReload(); err != nil {
		writeError(w, http.StatusInternalServerError, "reload: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "tunnel": t.Name})
}

// handleDeleteTunnel removes the tunnel named {name}: validate, patch, reload.
// If the tunnel is active, the engine reload stops and drops it.
func (s *Server) handleDeleteTunnel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasTunnel(name) {
		writeError(w, http.StatusNotFound, "unknown tunnel %q", name)
		return
	}
	if _, err := s.cfg.WithTunnelRemoved(name); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	if err := config.DeleteTunnelNode(s.cfgPath, name); err != nil {
		writeError(w, http.StatusInternalServerError, "persist: %v", err)
		return
	}
	if err := s.applyReload(); err != nil {
		writeError(w, http.StatusInternalServerError, "reload: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "tunnel": name})
}

func decodeTunnel(r *http.Request) (config.Tunnel, error) {
	var t config.Tunnel
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		return config.Tunnel{}, fmt.Errorf("decode tunnel: %w", err)
	}
	return t, nil
}

func (s *Server) hasTunnel(name string) bool {
	for _, t := range s.cfg.Tunnels {
		if t.Name == name {
			return true
		}
	}
	return false
}

func (s *Server) isUp(name string) bool {
	for _, st := range s.engine.List() {
		if st.Name == name {
			return st.State != forward.Off
		}
	}
	return false
}

func (s *Server) setEnabled(name string, enabled bool) {
	for i := range s.cfg.Tunnels {
		if s.cfg.Tunnels[i].Name == name {
			s.cfg.Tunnels[i].Enabled = enabled
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, map[string]string{"error": fmt.Sprintf(format, args...)})
}

// ensureNotRunning reads the discovery marker (if any) and signals its PID to
// detect a live daemon. A dead/corrupt marker or a stray socket file are stale
// artefacts and are removed so a fresh daemon can start.
func ensureNotRunning(markerPath, socketPath string) error {
	m, err := ReadMarker(markerPath)
	if err != nil {
		if !os.IsNotExist(err) {
			// Corrupt marker — clean it up and fall through.
			_ = RemoveMarker(markerPath)
		}
		_ = os.Remove(socketPath)
		return nil
	}
	if pidAlive(m.PID) {
		return fmt.Errorf("daemon already running (pid %d) at %s", m.PID, m.Socket)
	}
	// The marker's PID looks dead, but the socket may still answer (a reused
	// PID, or a kill -0 hiccup). Probe before deleting so a live daemon is
	// never clobbered by a second start.
	if probeSocket(m.Socket) {
		return fmt.Errorf("daemon already running at %s (marker pid %d not alive but the socket still answers)", m.Socket, m.PID)
	}
	// Stale marker (dead PID, silent socket): remove it and any leftover
	// socket it pointed at, then let a fresh daemon start.
	_ = RemoveMarker(markerPath)
	_ = os.Remove(m.Socket)
	_ = os.Remove(socketPath)
	return nil
}
