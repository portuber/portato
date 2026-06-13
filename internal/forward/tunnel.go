package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"golang.org/x/crypto/ssh"
)

const (
	keepaliveInterval   = 30 * time.Second
	keepaliveTimeout    = 5 * time.Second
	stableResetInterval = 30 * time.Second
)

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
// Dialing happens asynchronously; Start returns once the listener is bound.
func (t *Tunnel) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = t.baseCtx
	}
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return errors.New("tunnel already running")
	}
	addr := t.cfg.ListenAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.state = Error
		t.errMsg = fmt.Sprintf("listen %s: %v", addr, err)
		t.mu.Unlock()
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
		t.log.Info("tunnel connected")

		t.serveConnected(ctx, client)

		stable := time.Since(t.connectedAt)
		attempt++
		if stable >= stableResetInterval {
			attempt = 0
		}
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
		go t.handleConn(client, conn)
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
}

func (t *Tunnel) setStateErr(s State, msg string) {
	t.mu.Lock()
	t.state = s
	t.errMsg = msg
	t.mu.Unlock()
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
