//go:build windows

package service

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// scmService abstracts the SCM service-handle operations windowsInstaller uses,
// so Install/Uninstall/Status are unit-testable without a real Service Control
// Manager. Mirrors the execFunc seam used by the linux/darwin installers.
type scmService interface {
	updateConfig(mgr.Config) error
	setRecoveryActions([]mgr.RecoveryAction, uint32) error
	start(args ...string) error
	control(svc.Cmd) (svc.Status, error)
	query() (svc.Status, error)
	delete() error
	close() error
}

// scmAPI abstracts the connected-SCM operations: creating/opening a named
// service. The real implementation connects per call; tests inject a fake that
// records the sequence.
type scmAPI interface {
	create(name, exepath string, cfg mgr.Config, args []string) (scmService, error)
	open(name string) (scmService, error)
	installed(name string) bool
}

// realSCM is the production scmAPI: a thin adapter over golang.org/x/sys/
// windows/svc/mgr. A fresh SCM connection is opened per operation (install /
// uninstall / status are one-shot CLI commands, not hot paths); the returned
// service handle stays valid after the manager handle closes.
type realSCM struct{}

// defaultSCM is the scmAPI used by the free functions (StopService / SCMInstalled)
// that are not part of an Installer instance. Tests that need to drive the
// install path construct a windowsInstaller with their own fake scm instead.
var defaultSCM scmAPI = realSCM{}

func (realSCM) create(name, exepath string, cfg mgr.Config, args []string) (scmService, error) {
	m, err := mgr.Connect()
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, fmt.Errorf("connect SCM: %w (creating a Windows service requires administrator privileges; run from an elevated terminal, or use `portato install --legacy-runkey` for the per-user Run-key autostart)", err)
		}
		return nil, fmt.Errorf("connect SCM: %w", err)
	}
	defer m.Disconnect()
	s, err := m.CreateService(name, exepath, cfg, args...)
	if err != nil {
		// Idempotent install: if the service already exists, update its config
		// in place instead of failing (mirrors launchd bootout+bootstrap and
		// systemd daemon-reload+restart).
		if !errors.Is(err, windows.ERROR_SERVICE_EXISTS) {
			return nil, fmt.Errorf("create service %s: %w", name, err)
		}
		s, err = m.OpenService(name)
		if err != nil {
			return nil, fmt.Errorf("open existing service %s: %w", name, err)
		}
		if err := s.UpdateConfig(cfg); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("update service %s config: %w", name, err)
		}
	}
	return realSvc{s}, nil
}

func (realSCM) open(name string) (scmService, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("connect SCM: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return nil, err
	}
	return realSvc{s}, nil
}

// realSvc wraps *mgr.Service so it satisfies scmService without exposing the
// concrete mgr type outside this file.
type realSvc struct{ s *mgr.Service }

func (r realSvc) updateConfig(c mgr.Config) error { return r.s.UpdateConfig(c) }
func (r realSvc) setRecoveryActions(a []mgr.RecoveryAction, reset uint32) error {
	return r.s.SetRecoveryActions(a, reset)
}
func (r realSvc) start(args ...string) error            { return r.s.Start(args...) }
func (r realSvc) control(c svc.Cmd) (svc.Status, error) { return r.s.Control(c) }
func (r realSvc) query() (svc.Status, error)            { return r.s.Query() }
func (r realSvc) delete() error                         { return r.s.Delete() }
func (r realSvc) close() error                          { return r.s.Close() }

// installed reports whether the named service is registered. It deliberately
// avoids mgr.Connect / OpenService through the ALL_ACCESS-defaulting helpers:
// SC_MANAGER_CONNECT + SERVICE_QUERY_STATUS are granted to ordinary users, so
// an unelevated `portato doctor` sees a real service instead of an
// access-denied misread as "not installed" (the Phase-47 verification fix).
func (realSCM) installed(name string) bool {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(m)
	s, err := windows.OpenService(m, windows.StringToUTF16Ptr(name), windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false
	}
	_ = windows.CloseServiceHandle(s)
	return true
}
