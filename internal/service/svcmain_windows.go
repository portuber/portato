//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

// stopServiceTimeout / stopServicePollInterval bound how long StopService waits
// for the SCM service to reach Stopped after sending the Stop control. Overridable
// so tests can shrink the wait.
var (
	stopServiceTimeout      = 30 * time.Second
	stopServicePollInterval = 200 * time.Millisecond
)

// RunAsService runs the daemon as a Windows service: it blocks in svc.Run until
// the SCM stops the service (or the daemon exits on its own). The run callback
// is the daemon entry point (cmd.RunDaemon); it is invoked with a context that
// is cancelled on svc.Stop / svc.Shutdown so the daemon drains gracefully.
func RunAsService(run DaemonFunc) error {
	return svc.Run(ServiceName, &scmHandler{run: run})
}

// scmHandler implements svc.Handler. Execute is called once at service start
// and blocks for the service's whole lifetime.
type scmHandler struct {
	run DaemonFunc
}

func (h *scmHandler) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	// SCM launches services with the working directory set to System32; relative
	// identity paths in config.yaml (~/.ssh/...) resolve against the home dir,
	// so chdir to the service account's profile before running the daemon.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		_ = os.Chdir(home)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- h.run(ctx) }()

	// Report Running as soon as the daemon goroutine is started; the daemon
	// binds its IPC pipe quickly, and SCM's default 30s start hint covers it.
	changes <- svc.Status{State: svc.Running, Accepts: accepts}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				<-errCh // wait for the daemon's graceful Shutdown to finish
				changes <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		case err := <-errCh:
			// Daemon exited on its own (not via Stop). Report Stopped; a non-nil
			// error yields a nonzero exit so SCM applies the recovery action
			// (restart), mirroring launchd KeepAlive / systemd Restart=on-failure.
			changes <- svc.Status{State: svc.Stopped}
			if err != nil {
				return false, 1
			}
			return false, 0
		}
	}
}

// StopService sends a graceful Stop control to the installed Portato SCM
// service and waits for it to reach Stopped. It returns:
//   - (true, nil) when a running service was found and brought to Stopped;
//   - (false, nil) when no service is installed or it was already stopped, so
//     the caller falls back to other stop mechanisms (the Run-key PID path);
//   - (false, err) when an SCM operation failed unexpectedly.
func StopService() (bool, error) {
	s, err := defaultSCM.open(ServiceName)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return false, nil
		}
		return false, err
	}
	defer s.close()
	st, err := s.query()
	if err != nil {
		return false, err
	}
	if !stoppable(st.State) {
		return false, nil
	}
	if _, err := s.control(svc.Stop); err != nil {
		return false, fmt.Errorf("send stop control: %w", err)
	}
	deadline := time.Now().Add(stopServiceTimeout)
	for time.Now().Before(deadline) {
		st, qerr := s.query()
		if qerr != nil {
			return false, qerr
		}
		if st.State == svc.Stopped {
			return true, nil
		}
		time.Sleep(stopServicePollInterval)
	}
	return false, fmt.Errorf("service did not stop within %s", stopServiceTimeout)
}

// stoppable reports whether an SCM state can be asked to Stop (Running or a
// pending start/continue). A Stopped/Paused service has nothing to stop.
func stoppable(s svc.State) bool {
	switch s {
	case svc.Running, svc.StartPending, svc.ContinuePending:
		return true
	}
	return false
}

// SCMInstalled reports whether the Portato SCM service is registered (regardless
// of state). Used by `portato doctor` to report the SCM autostart mechanism.
func SCMInstalled() bool {
	s, err := defaultSCM.open(ServiceName)
	if err != nil {
		return false
	}
	_ = s.close()
	return true
}
