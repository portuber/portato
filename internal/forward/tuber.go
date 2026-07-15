package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/armon/go-socks5"
	"github.com/portuber/portato/internal/config"
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

// Tuber manages one local (-L) SSH port forward: it owns the local listener,
// the SSH client (with reconnect), and the per-connection copy loops.
type Tuber struct {
	baseCtx  context.Context
	cfg      config.Tuber
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

	// TOFU (Phase 11): the last rejected unknown host key, captured by the
	// host-key callback via recordUnknownHost. Surfaced through Status so the
	// TUI can offer to accept it (and AcceptHost appends PendingHostLine).
	pendingHost        string
	pendingFingerprint string
	pendingHostLine    string

	// Passphrase (Phase 19): the identity path currently awaiting a passphrase
	// (set by passphraseSink while the dial blocks on PassphraseProvider.Wait),
	// surfaced through Status.PendingPassphrase. Empty when none is needed.
	pendingPassphrase string
	// passphraseAttempts counts how many times a submitted identity passphrase
	// was wrong (driven by passphraseSink re-prompts). Surfaced via
	// Status.PassphraseAttempts so the TUI shows an accurate "wrong passphrase"
	// hint only on a real rejection, not on every submit. Reset on a new dial.
	passphraseAttempts int
	// provider obtains the passphrase for a protected identity (nil in tests /
	// the one-shot `portato forward` command → no passphrase support).
	provider PassphraseProvider

	// Password (Phase 35): the server account currently awaiting a password
	// (set by passwordSink while the dial blocks on PasswordProvider.Wait),
	// surfaced through Status.PendingPassword. Empty when none is needed.
	pendingPassword string
	// passwordAttempts counts how many times the server rejected a submitted
	// password for this tuber (driven by passwordSink re-prompts). Surfaced via
	// Status.PasswordAttempts so the TUI shows an accurate "wrong password"
	// hint only on a real rejection, not on every submit. Reset on a new dial.
	passwordAttempts int
	// passwordProvider obtains the password for a password-only account (nil
	// disables password support even if password_auth is on).
	passwordProvider PasswordProvider

	// onChange is wired by the Engine (Phase 9) so every state transition
	// fans out to event subscribers. Nil-safe: standalone tests / fakes
	// leave it unset. Fires after the state mutex is released.
	onChange func()
}

// notifyChange propagates a state transition to the Engine broker. Called
// only by the goroutine that just changed t.state, after releasing t.mu.
func (t *Tuber) notifyChange() {
	if t.onChange != nil {
		t.onChange()
	}
}

// NewTuber constructs a tuber. baseCtx is reused for manual Restart. provider
// (optional) enables passphrase-protected identity loading (Phase 19).
// passwordProvider (optional) enables interactive password auth (Phase 35).
func NewTuber(baseCtx context.Context, cfg config.Tuber, def config.Defaults, log *slog.Logger, provider PassphraseProvider, passwordProvider PasswordProvider) *Tuber {
	if log == nil {
		log = slog.Default()
	}
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	return &Tuber{
		baseCtx:          baseCtx,
		cfg:              cfg,
		defaults:         def,
		log:              log.With("tuber", cfg.Name),
		provider:         provider,
		passwordProvider: passwordProvider,
	}
}

// Start opens the local listener synchronously and spawns the run loop.
// For type=local it binds the local listener up front (so bind failures are
// reported by Start and the local port is reserved before Start returns).
// For type=remote there is no local listener — it is bound on the server after
// each successful SSH dial inside runRemote, so Start always succeeds and
// connect/listen errors surface via the state machine.
func (t *Tuber) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = t.baseCtx
	}
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return errors.New("tuber already running")
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

// StartWith is like Start but adopts a pre-bound listener (handed in during a
// standalone->daemon hand-off) instead of binding its own. The tuber takes
// ownership of ln and closes it on Stop. Only valid for the local-listener
// types (local/dynamic); a type=remote tuber has no local listener to adopt.
// Phase 16.
func (t *Tuber) StartWith(ctx context.Context, ln net.Listener) error {
	if ctx == nil {
		ctx = t.baseCtx
	}
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return errors.New("tuber already running")
	}
	if t.cfg.Type == "remote" {
		t.mu.Unlock()
		return fmt.Errorf("StartWith: %s is type=remote (no local listener to adopt)", t.cfg.Name)
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
func (t *Tuber) Stop() error {
	t.mu.Lock()
	if !t.running {
		// A non-running tuber may still display a terminal Error from a failed
		// bind: Start set state=Error but never flipped running, so it never
		// reached the teardown path below. Fold it back to Off so Disable (and
		// the TUI toggle) can clear the error and a subsequent Enable rebinds.
		// Also drop any stale pending prompt so no "password?"/"passphrase?"
		// hint or modal lingers over a stopped tuber.
		changed := t.state != Off || t.errMsg != "" || t.clearPendingLocked()
		t.state = Off
		t.errMsg = ""
		t.connectedAt = time.Time{}
		t.mu.Unlock()
		if changed {
			t.notifyChange()
		}
		return nil
	}
	t.running = false
	t.state = Off
	t.errMsg = ""
	t.connectedAt = time.Time{}
	t.clearPendingLocked()
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
func (t *Tuber) Restart() error {
	if err := t.Stop(); err != nil {
		return err
	}
	return t.Start(t.baseCtx)
}

// Reconfigure swaps the tuber's config and defaults in place. If the tuber is
// currently running, it is restarted so the new endpoints/auth take effect;
// otherwise it stays stopped, but Status() already reflects the new fields.
// Used by Engine.Reload so editing a tuber updates its displayed Local/Remote
// without starting a tuber that was off (and without leaving a stale cfg).
func (t *Tuber) Reconfigure(cfg config.Tuber, def config.Defaults) error {
	t.mu.Lock()
	running := t.running
	t.cfg = cfg
	t.defaults = def
	// A failed bind leaves state=Error with running=false; updating cfg (e.g.
	// a new Local port) must not keep showing the stale error referencing the
	// old address. Reset a non-running tuber to a clean Off, and drop any
	// stale pending prompt.
	changed := false
	if !running {
		pc := t.clearPendingLocked()
		if t.state != Off || t.errMsg != "" || pc {
			t.state = Off
			t.errMsg = ""
			t.connectedAt = time.Time{}
			changed = true
		}
	}
	t.mu.Unlock()
	if running {
		return t.Restart()
	}
	if changed {
		t.notifyChange()
	}
	return nil
}

func (t *Tuber) Status() Status {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return Status{
		Name:               t.cfg.Name,
		Type:               t.cfg.Type,
		Local:              t.cfg.ListenAddr(),
		Remote:             t.cfg.Remote,
		State:              t.state,
		Error:              t.errMsg,
		ConnectedAt:        t.connectedAt,
		PendingHost:        t.pendingHost,
		PendingFingerprint: t.pendingFingerprint,
		PendingHostLine:    t.pendingHostLine,
		PendingPassphrase:  t.pendingPassphrase,
		PassphraseAttempts: t.passphraseAttempts,
		PendingPassword:    t.pendingPassword,
		PasswordAttempts:   t.passwordAttempts,
	}
}

// recordUnknownHost is the hostKeySink wired into dialSSH: it remembers the
// rejected key so Status can surface it for the TUI TOFU prompt. Called from
// the dial goroutine; t.mu is taken here.
func (t *Tuber) recordUnknownHost(host, fingerprint, line string) {
	t.mu.Lock()
	t.pendingHost = host
	t.pendingFingerprint = fingerprint
	t.pendingHostLine = line
	t.mu.Unlock()
}

// clearPendingHost forgets a previously recorded unknown host key. Called at
// the start of each dial attempt so a stale entry does not outlive the
// rejection that produced it.
func (t *Tuber) clearPendingHost() {
	t.mu.Lock()
	t.pendingHost = ""
	t.pendingFingerprint = ""
	t.pendingHostLine = ""
	t.mu.Unlock()
}

// passphraseSink is wired into dialSSH: it records the identity path awaiting a
// passphrase (path != "") so Status.PendingPassphrase surfaces it for the UI to
// prompt, or clears it (path == "") once the passphrase is accepted. Called
// from the dial goroutine. Each re-prompt (a non-empty path while one was
// already pending) means the dial rejected the previous passphrase, so it bumps
// passphraseAttempts for an accurate TUI hint.
func (t *Tuber) passphraseSink(path string) {
	t.mu.Lock()
	if path == "" {
		t.pendingPassphrase = ""
	} else {
		if t.pendingPassphrase != "" {
			t.passphraseAttempts++
		}
		t.pendingPassphrase = path
	}
	t.mu.Unlock()
	t.notifyChange()
}

// clearPendingPassphrase forgets a recorded passphrase need and the rejection
// counter. Called at the start of each dial so a stale entry does not outlive
// the attempt that produced it. Always resets so a fresh dial starts at 0.
func (t *Tuber) clearPendingPassphrase() {
	t.mu.Lock()
	changed := t.pendingPassphrase != "" || t.passphraseAttempts != 0
	t.pendingPassphrase = ""
	t.passphraseAttempts = 0
	t.mu.Unlock()
	if changed {
		t.notifyChange()
	}
}

// passwordSink is wired into dialSSH: it records the server account awaiting a
// password (account != "") so Status.PendingPassword surfaces it for the UI to
// prompt, or clears it (account == "") once the password is accepted. Called
// from the dial goroutine. Each re-prompt (a non-empty account while one was
// already pending) means the server rejected the previous password, so it
// bumps passwordAttempts for an accurate TUI hint.
func (t *Tuber) passwordSink(account string) {
	t.mu.Lock()
	if account == "" {
		t.pendingPassword = ""
	} else {
		if t.pendingPassword != "" {
			t.passwordAttempts++
		}
		t.pendingPassword = account
	}
	t.mu.Unlock()
	t.notifyChange()
}

// clearPendingPassword forgets a recorded password need and the rejection
// counter. Called at the start of each dial so a stale entry does not outlive
// the attempt that produced it. Always resets (even if the pending account was
// already cleared by a success), so a fresh dial starts at 0 attempts.
func (t *Tuber) clearPendingPassword() {
	t.mu.Lock()
	changed := t.pendingPassword != "" || t.passwordAttempts != 0
	t.pendingPassword = ""
	t.passwordAttempts = 0
	t.mu.Unlock()
	if changed {
		t.notifyChange()
	}
}

// clearPendingLocked resets every pending prompt (unknown host, passphrase,
// password) and the password rejection counter. Caller must hold t.mu. It
// returns whether any field was non-empty so Stop/Reconfigure can decide
// whether to fire notifyChange. Used on stop/reconfigure/dial-error so an Off
// or errored tuber shows no stale "password?"/"passphrase?" hint and no modal
// auto-opens over a dead tuber (Phase 35 dogfooding fix).
func (t *Tuber) clearPendingLocked() bool {
	changed := t.pendingHost != "" || t.pendingFingerprint != "" ||
		t.pendingHostLine != "" || t.pendingPassphrase != "" || t.pendingPassword != "" ||
		t.passphraseAttempts != 0 || t.passwordAttempts != 0
	t.pendingHost = ""
	t.pendingFingerprint = ""
	t.pendingHostLine = ""
	t.pendingPassphrase = ""
	t.pendingPassword = ""
	t.passphraseAttempts = 0
	t.passwordAttempts = 0
	return changed
}

// PendingHostLine returns the known_hosts line for the last rejected unknown
// host (and ok=false when there is none). AcceptHost (controller) reads it.
func (t *Tuber) PendingHostLine() (line string, ok bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.pendingHostLine, t.pendingHostLine != ""
}

// ErrNoListener is returned by ListenerFile when the tuber has no local
// listener to hand off: it is stopped, or type=remote (whose listener is bound
// on the SSH server, not locally). LiveListenerFiles treats it as "skip this
// tuber" rather than a failure.
var ErrNoListener = errors.New("tuber has no local listener to pass")

// ListenerFile returns a duplicated file descriptor for the tuber's local
// listener, for passing to the spawned daemon during the standalone->daemon
// hand-off (Phase 16). File dups the fd; the tuber's own listener stays open
// and keeps accepting until Close. Returns ErrNoListener when the tuber is
// stopped or has no local listener (type=remote); any other failure is a hard
// error.
func (t *Tuber) ListenerFile() (*os.File, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.running || t.listener == nil || t.cfg.Type == "remote" {
		return nil, ErrNoListener
	}
	tcp, ok := t.listener.(*net.TCPListener)
	if !ok {
		return nil, fmt.Errorf("listener is %T, want *net.TCPListener", t.listener)
	}
	return tcp.File()
}

func (t *Tuber) run(ctx context.Context, ln net.Listener, done chan<- struct{}) {
	defer close(done)
	go t.acceptLoop(ctx, ln)

	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		t.setState(Connecting)
		t.clearPendingHost()
		t.clearPendingPassphrase()
		t.clearPendingPassword()
		client, err := dialSSH(ctx, t.cfg, t.defaults, t.log, t.recordUnknownHost, t.provider, t.passphraseSink, t.passwordProvider, t.passwordSink)
		if err != nil {
			// The dial exited and is no longer waiting on a passphrase/password
			// prompt — drop those so their modals don't linger during the
			// backoff. PendingHost is intentionally KEPT: a host-key rejection
			// (TOFU) records it inside the dial, and the TUI must surface the
			// accept prompt — clearing it here (before setStateErr's notify)
			// would suppress that prompt (Phase 35 regression fix).
			t.clearPendingPassphrase()
			t.clearPendingPassword()
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
		t.log.Info("tuber connected")

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
		t.log.Info("tuber disconnected, reconnecting")
		if !t.sleep(ctx, nextBackoff(attempt)) {
			return
		}
	}
}

func (t *Tuber) acceptLoop(ctx context.Context, ln net.Listener) {
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

func (t *Tuber) handleConn(client *ssh.Client, conn net.Conn) {
	remote, err := client.Dial("tcp", t.cfg.Remote)
	if err != nil {
		t.log.Warn("dial remote failed", "remote", t.cfg.Remote, "err", err)
		_ = conn.Close()
		return
	}
	t.log.Debug("connection forwarded", "remote", t.cfg.Remote)
	pipe(conn, remote)
}

// handleDynamicConn serves a type=dynamic (-D) connection: the inbound conn is a
// SOCKS5 client, and each requested destination is dialed through the SSH client
// on the server side. When socks5_user/socks5_password are configured (tuber
// or defaults), the proxy requires user/pass authentication; otherwise NoAuth
// (loopback bind only) — preserving the pre-Phase-20 behaviour (Phase 20).
func (t *Tuber) handleDynamicConn(client *ssh.Client, conn net.Conn) {
	srv, err := socks5.New(&socks5.Config{
		Logger:      socks5SilencedLogger,
		Resolver:    loggingResolver{inner: socks5.DNSResolver{}, log: t.log},
		Credentials: socks5Credentials(t.cfg.ResolvedSocks5User(t.defaults), t.cfg.ResolvedSocks5Password(t.defaults)),
		Dial: func(_ context.Context, network, addr string) (net.Conn, error) {
			c, derr := client.Dial(network, addr)
			if derr != nil {
				t.log.Warn("socks5 dial failed", "dest", addr, "err", derr)
				return nil, derr
			}
			t.log.Debug("socks5 dial", "dest", addr)
			return c, nil
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

// socks5Credentials returns a StaticCredentials store when BOTH user and pass
// are non-empty, otherwise nil — so armon/go-socks5 falls back to NoAuth
// (len(AuthMethods)==0 + Credentials==nil → NoAuthAuthenticator). Returning nil
// on a partial pair (user without pass or vice-versa) keeps the proxy open
// rather than silently requiring an unwinnable half-credential.
func socks5Credentials(user, pass string) socks5.CredentialStore {
	if user == "" || pass == "" {
		return nil
	}
	return socks5.StaticCredentials{user: pass}
}

// loggingResolver wraps go-socks5's name resolver so SOCKS destination
// resolution is visible in the logs screen: a resolved name logs the hostname
// + IP at Debug, and a name that fails to resolve logs a Warn (symmetric with
// socks5 dial failed) — otherwise a typo'd/non-existent host would surface only
// as an opaque ServeConn error. inner is injectable for tests.
type loggingResolver struct {
	inner socks5.NameResolver
	log   *slog.Logger
}

func (r loggingResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	c, ip, err := r.inner.Resolve(ctx, name)
	if err != nil {
		r.log.Warn("socks5 resolve failed", "name", name, "err", err)
		return c, nil, err
	}
	r.log.Debug("socks5 resolve", "name", name, "ip", ip.String())
	return c, ip, nil
}

// runRemote is the reconnect loop for a type=remote (-R) tuber. The listener
// is bound on the SSH server via client.Listen, so it is created right after
// each successful dial and torn down when the client drops. The
// dial/backoff/keepalive scaffolding is shared with run.
func (t *Tuber) runRemote(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		t.setState(Connecting)
		t.clearPendingHost()
		t.clearPendingPassphrase()
		t.clearPendingPassword()
		client, err := dialSSH(ctx, t.cfg, t.defaults, t.log, t.recordUnknownHost, t.provider, t.passphraseSink, t.passwordProvider, t.passwordSink)
		if err != nil {
			// The dial exited and is no longer waiting on a passphrase/password
			// prompt — drop those so their modals don't linger during the
			// backoff. PendingHost is intentionally KEPT: a host-key rejection
			// (TOFU) records it inside the dial, and the TUI must surface the
			// accept prompt — clearing it here (before setStateErr's notify)
			// would suppress that prompt (Phase 35 regression fix).
			t.clearPendingPassphrase()
			t.clearPendingPassword()
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
		t.log.Info("remote tuber connected", "bind", bindAddr)

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
		t.log.Info("remote tuber disconnected, reconnecting")
		if !t.sleep(ctx, nextBackoff(attempt)) {
			return
		}
	}
}

// serveRemoteConnected blocks while a remote tuber's SSH session is alive. It
// runs the keepalive loop and the server-side accept loop in parallel, and
// returns when the client drops or the context is cancelled (Stop). The remote
// listener is closed here to unblock the accept loop.
func (t *Tuber) serveRemoteConnected(ctx context.Context, client *ssh.Client, ln net.Listener) {
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
func (t *Tuber) remoteAcceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go t.handleRemoteConn(conn)
	}
}

func (t *Tuber) handleRemoteConn(conn net.Conn) {
	target := t.cfg.ListenAddr()
	local, err := net.Dial("tcp", target)
	if err != nil {
		t.log.Warn("dial local failed", "local", target, "err", err)
		_ = conn.Close()
		return
	}
	// conn.RemoteAddr() is the originator the SSH server reported for this
	// forwarded-tcpip channel (RFC 4254 §7.2): a real external IP when the
	// server-side port is reachable (GatewayPorts), else 127.0.0.1.
	t.log.Debug("connection forwarded", "local", target, "peer", conn.RemoteAddr())
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

func (t *Tuber) serveConnected(ctx context.Context, client *ssh.Client) {
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

func (t *Tuber) keepaliveLoop(ctx context.Context, client *ssh.Client, stop <-chan struct{}) {
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

func (t *Tuber) setState(s State) {
	t.mu.Lock()
	t.state = s
	t.mu.Unlock()
	t.notifyChange()
}

func (t *Tuber) setStateErr(s State, msg string) {
	t.mu.Lock()
	t.state = s
	t.errMsg = msg
	t.mu.Unlock()
	// Surface the failure in the logs so it is reachable from the TUI's `l`
	// screen and the rotated log file — otherwise the truncated Status.Error in
	// the list row is the only place the message appears, and a remote-listen
	// failure (e.g. "listen 0.0.0.0:9090 on server: …") is invisible. A nil log
	// is tolerated for tests that build a Tuber literal without a logger.
	if t.log != nil {
		t.log.Error(msg)
	}
	t.notifyChange()
}

func (t *Tuber) sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
