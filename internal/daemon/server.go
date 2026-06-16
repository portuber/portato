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

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	srv      *http.Server
	listener net.Listener

	shutdownOnce sync.Once
}

// New prepares a daemon for cfg/cfgPath: it resolves the socket and PID paths
// and refuses to start if another live daemon holds them (stale files are cleaned).
func New(cfg *config.Config, cfgPath string, log *slog.Logger) (*Server, error) {
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
	s := newServer(nil, cfg, cfgPath, socketPath, pidPath, log)
	s.engine = forward.NewEngine(s.ctx, cfg, log)
	return s, nil
}

func newServer(engine tunneler, cfg *config.Config, cfgPath, socketPath, pidPath string, log *slog.Logger) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		engine:     engine,
		cfg:        cfg,
		cfgPath:    cfgPath,
		socketPath: socketPath,
		pidPath:    pidPath,
		log:        log,
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
	mux.HandleFunc("POST /tunnels/{name}/enable", s.handleEnable)
	mux.HandleFunc("POST /tunnels/{name}/disable", s.handleDisable)
	mux.HandleFunc("POST /tunnels/{name}/restart", s.handleRestart)
	mux.HandleFunc("POST /reload", s.handleReload)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.List())
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

func (s *Server) handleReload(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reload config: %v", err)
		return
	}
	s.engine.Reload(cfg)
	s.cfg = cfg
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
