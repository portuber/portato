package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/armon/go-socks5"
	"github.com/kipkaev55/portato/internal/config"
	"golang.org/x/crypto/ssh"
)

const (
	keepaliveInterval   = 30 * time.Second
	keepaliveTimeout    = 5 * time.Second
	stableResetInterval = 30 * time.Second
)

// socks5SilencedLogger discards the armon/go-socks5 library's own log output.
// By default it writes [ERR] socks: ... to os.Stdout, which corrupts the TUI
// (and desyncs the bubbletea renderer). Per-connection failures are still
// surfaced via the ServeConn return value through slog (see handleDynamicConn).
var socks5SilencedLogger = log.New(io.Discard, "", 0)

// Tunnel manages one local (-L) SSH port forward: it owns the local listener,
// the SSH client (with reconnect), and the per-connection copy loops.
type Tunnel struct {
	baseCtx  context.Context
	cfg      config.Tunnel
	defaults config.Defaults
	log      *slog.Logger

	mu          sync.RWMutex
	state       State
	errMsg      string
	connectedAt time.Time
	running     bool
	listener    net.Listener
	client      *ssh.Client
	cancel      context.CancelFunc
	done        chan struct{}

	// onChange is wired by the Engine (Phase 9) so every state transition
	// fans out to event subscribers. Nil-safe: standalone tests / fakes
	// leave it unset. Fires after the state mutex is released.
	onChange func()
}

// notifyChange propagates a state transition to the Engine broker. Called
// only by the goroutine that just changed t.state, after releasing t.mu.
func (t *Tunnel) notifyChange() {
	if t.onChange != nil {
		t.onChange()
	}
}

// NewTunnel constructs a tunnel. baseCtx is reused for manual Restart.
func NewTunnel(baseCtx context.Context, cfg config.Tunnel, def config.Defaults, log *slog.Logger) *Tunnel {
	if log == nil {
		log = slog.Default()
	}
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	return &Tunnel{
		baseCtx:  baseCtx,
		cfg:      cfg,
		defaults: def,
		log:      log.With("tunnel", cfg.Name),
	}
}

// Start opens the local listener synchronously and spawns the run loop.
// For type=local it binds the local listener up front (so bind failures are
// reported by Start and the local port is reserved before Start returns).
// For type=remote there is no local listener — it is bound on the server after
// each successful SSH dial inside runRemote, so Start always succeeds and
// connect/listen errors surface via the state machine.
func (t *Tunnel) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = t.baseCtx
	}
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return errors.New("tunnel already running")
	}

	if t.cfg.Type == "remote" {
		cctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		t.cancel = cancel
		t.done = done
		t.running = true
		t.state = Connecting
		t.errMsg = ""
		t.mu.Unlock()
		t.notifyChange()
		go t.runRemote(cctx, done)
		return nil
	}

	addr := t.cfg.ListenAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.state = Error
		t.errMsg = fmt.Sprintf("listen %s: %v", addr, err)
		t.mu.Unlock()
		t.notifyChange()
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	t.listener = ln
	t.cancel = cancel
	t.done = done
	t.running = true
	t.state = Connecting
	t.errMsg = ""
	t.mu.Unlock()
	t.notifyChange()

	go t.run(cctx, ln, done)
	return nil
}

// Stop tears down the listener and SSH client and blocks until the run loop exits.
func (t *Tunnel) Stop() error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}
	t.running = false
	t.state = Off
	t.errMsg = ""
	t.connectedAt = time.Time{}
	ln := t.listener
	cancel := t.cancel
	done := t.done
	cl := t.client
	t.listener = nil
	t.client = nil
	t.cancel = nil
	t.done = nil
	t.mu.Unlock()
	t.notifyChange()

	if cancel != nil {
		cancel()
	}
	if ln != nil {
		_ = ln.Close()
	}
	if cl != nil {
		_ = cl.Close()
	}
	if done != nil {
		<-done
	}
	return nil
}

// Restart performs a synchronous Stop followed by Start.
func (t *Tunnel) Restart() error {
	if err := t.Stop(); err != nil {
		return err
	}
	return t.Start(t.baseCtx)
}

func (t *Tunnel) Status() Status {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return Status{
		Name:        t.cfg.Name,
		Type:        t.cfg.Type,
		Local:       t.cfg.ListenAddr(),
		Remote:      t.cfg.Remote,
		State:       t.state,
		Error:       t.errMsg,
		ConnectedAt: t.connectedAt,
	}
}

func (t *Tunnel) run(ctx context.Context, ln net.Listener, done chan<- struct{}) {
	defer close(done)
	go t.acceptLoop(ctx, ln)

	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		t.setState(Connecting)
		client, err := dialSSH(ctx, t.cfg, t.defaults, t.log)
		if err != nil {
			t.setStateErr(Error, err.Error())
			attempt++
			if !t.sleep(ctx, nextBackoff(attempt)) {
				return
			}
			t.setState(Reconnecting)
			continue
		}

		t.mu.Lock()
		t.client = client
		t.errMsg = ""
		t.connectedAt = time.Now()
		t.state = Connected
		t.mu.Unlock()
		t.notifyChange()
		t.log.Info("tunnel connected")

		t.serveConnected(ctx, client)

		stable := time.Since(t.connectedAt)
		attempt = nextAttemptAfterDisconnect(stable, attempt)
		t.mu.Lock()
		t.client = nil
		t.mu.Unlock()
		_ = client.Close()

		if err := ctx.Err(); err != nil {
			return
		}
		t.setState(Reconnecting)
		t.log.Info("tunnel disconnected, reconnecting")
		if !t.sleep(ctx, nextBackoff(attempt)) {
			return
		}
	}
}

func (t *Tunnel) acceptLoop(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		t.mu.RLock()
		client := t.client
		state := t.state
		t.mu.RUnlock()
		if state != Connected || client == nil {
			_ = conn.Close()
			continue
		}
		if t.cfg.Type == "dynamic" {
			go t.handleDynamicConn(client, conn)
		} else {
			go t.handleConn(client, conn)
		}
	}
}

func (t *Tunnel) handleConn(client *ssh.Client, conn net.Conn) {
	remote, err := client.Dial("tcp", t.cfg.Remote)
	if err != nil {
		t.log.Warn("dial remote failed", "remote", t.cfg.Remote, "err", err)
		_ = conn.Close()
		return
	}
	pipe(conn, remote)
}

// handleDynamicConn serves a type=dynamic (-D) connection: the inbound conn is a
// SOCKS5 client, and each requested destination is dialed through the SSH client
// on the server side. No auth (loopback bind only).
func (t *Tunnel) handleDynamicConn(client *ssh.Client, conn net.Conn) {
	srv, err := socks5.New(&socks5.Config{
		Logger: socks5SilencedLogger,
		Dial: func(_ context.Context, network, addr string) (net.Conn, error) {
			return client.Dial(network, addr)
		},
	})
	if err != nil {
		t.log.Warn("socks5 server init failed", "err", err)
		_ = conn.Close()
		return
	}
	if err := srv.ServeConn(conn); err != nil {
		t.log.Debug("socks5 connection ended", "err", err)
	}
}

// runRemote is the reconnect loop for a type=remote (-R) tunnel. The listener
// is bound on the SSH server via client.Listen, so it is created right after
// each successful dial and torn down when the client drops. The
// dial/backoff/keepalive scaffolding is shared with run.
func (t *Tunnel) runRemote(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		t.setState(Connecting)
		client, err := dialSSH(ctx, t.cfg, t.defaults, t.log)
		if err != nil {
			t.setStateErr(Error, err.Error())
			attempt++
			if !t.sleep(ctx, nextBackoff(attempt)) {
				return
			}
			t.setState(Reconnecting)
			continue
		}

		bindAddr := t.cfg.RemoteListenAddr()
		ln, lerr := client.Listen("tcp", bindAddr)
		if lerr != nil {
			t.setStateErr(Error, fmt.Sprintf("listen %s on server: %v (check GatewayPorts in sshd_config)", bindAddr, lerr))
			_ = client.Close()
			attempt++
			if !t.sleep(ctx, nextBackoff(attempt)) {
				return
			}
			t.setState(Reconnecting)
			continue
		}

		t.mu.Lock()
		t.client = client
		t.errMsg = ""
		t.connectedAt = time.Now()
		t.state = Connected
		t.mu.Unlock()
		t.notifyChange()
		t.log.Info("remote tunnel connected", "bind", bindAddr)

		t.serveRemoteConnected(ctx, client, ln)

		stable := time.Since(t.connectedAt)
		attempt = nextAttemptAfterDisconnect(stable, attempt)
		t.mu.Lock()
		t.client = nil
		t.mu.Unlock()
		_ = client.Close()

		if err := ctx.Err(); err != nil {
			return
		}
		t.setState(Reconnecting)
		t.log.Info("remote tunnel disconnected, reconnecting")
		if !t.sleep(ctx, nextBackoff(attempt)) {
			return
		}
	}
}

// serveRemoteConnected blocks while a remote tunnel's SSH session is alive. It
// runs the keepalive loop and the server-side accept loop in parallel, and
// returns when the client drops or the context is cancelled (Stop). The remote
// listener is closed here to unblock the accept loop.
func (t *Tunnel) serveRemoteConnected(ctx context.Context, client *ssh.Client, ln net.Listener) {
	stopKA := make(chan struct{})
	kaExited := make(chan struct{})
	go func() {
		defer close(kaExited)
		t.keepaliveLoop(ctx, client, stopKA)
	}()

	lnExited := make(chan struct{})
	go func() {
		defer close(lnExited)
		t.remoteAcceptLoop(ln)
	}()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		_ = client.Wait()
	}()

	select {
	case <-waitDone:
	case <-ctx.Done():
	}
	close(stopKA)
	<-kaExited
	_ = ln.Close()
	<-lnExited
}

// remoteAcceptLoop accepts connections arriving on the server-side listener
// (opened via client.Listen) and forwards each to the local address.
func (t *Tunnel) remoteAcceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go t.handleRemoteConn(conn)
	}
}

func (t *Tunnel) handleRemoteConn(conn net.Conn) {
	target := t.cfg.ListenAddr()
	local, err := net.Dial("tcp", target)
	if err != nil {
		t.log.Warn("dial local failed", "local", target, "err", err)
		_ = conn.Close()
		return
	}
	pipe(conn, local)
}

func pipe(a, b io.ReadWriteCloser) {
	done := make(chan struct{}, 2)
	cp := func(dst io.Writer, src io.Reader) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(b, a)
	go cp(a, b)
	<-done
	_ = a.Close()
	_ = b.Close()
	<-done
}

func (t *Tunnel) serveConnected(ctx context.Context, client *ssh.Client) {
	stopKA := make(chan struct{})
	kaExited := make(chan struct{})
	go func() {
		defer close(kaExited)
		t.keepaliveLoop(ctx, client, stopKA)
	}()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		_ = client.Wait()
	}()

	select {
	case <-waitDone:
	case <-ctx.Done():
	}
	close(stopKA)
	<-kaExited
}

func (t *Tunnel) keepaliveLoop(ctx context.Context, client *ssh.Client, stop <-chan struct{}) {
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := keepaliveOnce(client); err != nil {
				t.log.Debug("keepalive failed, forcing reconnect", "err", err)
				_ = client.Close()
				return
			}
		}
	}
}

func keepaliveOnce(client *ssh.Client) error {
	done := make(chan error, 1)
	go func() {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(keepaliveTimeout):
		return errors.New("keepalive reply timeout")
	}
}

func (t *Tunnel) setState(s State) {
	t.mu.Lock()
	t.state = s
	t.mu.Unlock()
	t.notifyChange()
}

func (t *Tunnel) setStateErr(s State, msg string) {
	t.mu.Lock()
	t.state = s
	t.errMsg = msg
	t.mu.Unlock()
	t.notifyChange()
}

func (t *Tunnel) sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
