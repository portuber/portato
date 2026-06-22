package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/forward"
	routelog "github.com/kipkaev55/portato/internal/log"
)

const shutdownTimeout = 10 * time.Second

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
	pidPath    string
	log        *slog.Logger
	logs       *routelog.Ring

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	srv      *http.Server
	listener net.Listener

	shutdownOnce sync.Once
}

// New prepares a daemon for cfg/cfgPath: it resolves the socket and PID paths
// and refuses to start if another live daemon holds them (stale files are cleaned).
func New(cfg *config.Config, cfgPath string, log *slog.Logger, ring *routelog.Ring) (*Server, error) {
	if log == nil {
		log = slog.Default()
	}
	socketPath, err := SocketPath()
	if err != nil {
		return nil, fmt.Errorf("resolve socket path: %w", err)
	}
	pidPath, err := PIDPath()
	if err != nil {
		return nil, fmt.Errorf("resolve pid path: %w", err)
	}
	if err := ensureNotRunning(pidPath, socketPath); err != nil {
		return nil, err
	}
	s := newServer(nil, cfg, cfgPath, socketPath, pidPath, log, ring)
	s.engine = forward.NewEngine(s.ctx, cfg, log)
	return s, nil
}

func newServer(engine tunneler, cfg *config.Config, cfgPath, socketPath, pidPath string, log *slog.Logger, ring *routelog.Ring) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		engine:     engine,
		cfg:        cfg,
		cfgPath:    cfgPath,
		socketPath: socketPath,
		pidPath:    pidPath,
		log:        log,
		logs:       ring,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Socket returns the unix-socket path the daemon binds (for logging/display).
func (s *Server) Socket() string { return s.socketPath }

// Start binds the socket, writes the PID file, starts the enabled tunnels and
// serves HTTP until ctx is cancelled (or serving fails). It always shuts down
// cleanly on return. SPEC §6.
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
	if err := s.writePID(); err != nil {
		_ = ln.Close()
		s.cleanup()
		return fmt.Errorf("write pid: %w", err)
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
// socket and PID file. Safe to call once.
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

func (s *Server) writePID() error {
	pid := strconv.Itoa(os.Getpid())
	return os.WriteFile(s.pidPath, []byte(pid), 0o600)
}

func (s *Server) cleanup() {
	_ = os.Remove(s.socketPath)
	_ = os.Remove(s.pidPath)
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
	return mux
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

// ensureNotRunning reads the PID file (if any) and signals the process to
// detect a live daemon. A dead PID or corrupt file means stale artefacts,
// which are removed so a fresh daemon can start.
func ensureNotRunning(pidPath, socketPath string) error {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.Remove(socketPath)
			return nil
		}
		return fmt.Errorf("read pid file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		_ = os.Remove(pidPath)
		_ = os.Remove(socketPath)
		return nil
	}
	if err := syscall.Kill(pid, 0); err == nil || errors.Is(err, syscall.EPERM) {
		return fmt.Errorf("daemon already running (pid %d)", pid)
	}
	_ = os.Remove(pidPath)
	_ = os.Remove(socketPath)
	return nil
}
