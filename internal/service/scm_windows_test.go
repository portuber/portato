//go:build windows

package service

import (
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// fakeSCM is a test scmAPI that records the create/open sequence and owns a set
// of named fake services. It mirrors fakeExec (service_linux/darwin_test.go):
// the install/uninstall/status logic is exercised without a real Service
// Control Manager.
type fakeSCM struct {
	mu                sync.Mutex
	createCalls       []fakeCreateCall
	openCalls         []string
	openErr           error // returned from open (e.g. ERROR_SERVICE_DOES_NOT_EXIST)
	createErrOnExists error // if set, create returns it (simulating an existing service)
	services          map[string]*fakeSvc
}

type fakeCreateCall struct {
	name    string
	exepath string
	cfg     mgr.Config
	args    []string
}

func newFakeSCM() *fakeSCM {
	return &fakeSCM{services: map[string]*fakeSvc{}}
}

func (f *fakeSCM) create(name, exepath string, cfg mgr.Config, args []string) (scmService, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls = append(f.createCalls, fakeCreateCall{name, exepath, cfg, args})
	if f.createErrOnExists != nil {
		if existing, ok := f.services[name]; ok {
			return existing, nil // caller will UpdateConfig
		}
		return nil, f.createErrOnExists
	}
	s := &fakeSvc{name: name, queryState: svc.Stopped}
	f.services[name] = s
	return s, nil
}

func (f *fakeSCM) open(name string) (scmService, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openCalls = append(f.openCalls, name)
	if f.openErr != nil {
		return nil, f.openErr
	}
	if s, ok := f.services[name]; ok {
		return s, nil
	}
	return nil, windows.ERROR_SERVICE_DOES_NOT_EXIST
}

// fakeSvc records every operation windowsInstaller/StopService performs.
type fakeSvc struct {
	name            string
	mu              sync.Mutex
	recoveryActions []mgr.RecoveryAction
	recoveryReset   uint32
	updatedCfg      *mgr.Config
	startArgs       [][]string
	ctrl            []svc.Cmd
	queryState      svc.State
	deleted         bool
	closed          bool
}

func (s *fakeSvc) updateConfig(c mgr.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updatedCfg = &c
	return nil
}

func (s *fakeSvc) setRecoveryActions(a []mgr.RecoveryAction, reset uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoveryActions = a
	s.recoveryReset = reset
	return nil
}

func (s *fakeSvc) start(args ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startArgs = append(s.startArgs, args)
	s.queryState = svc.Running
	return nil
}

func (s *fakeSvc) control(c svc.Cmd) (svc.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctrl = append(s.ctrl, c)
	if c == svc.Stop {
		s.queryState = svc.Stopped
	}
	return svc.Status{State: s.queryState}, nil
}

func (s *fakeSvc) query() (svc.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return svc.Status{State: s.queryState}, nil
}

func (s *fakeSvc) delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = true
	return nil
}

func (s *fakeSvc) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func TestWindows_SCMInstall_ConfigAndSequence(t *testing.T) {
	fx := newFakeSCM()
	w := &windowsInstaller{scm: fx}

	path, err := w.Install(Options{
		BinaryPath: `C:\Program Files\portato\portato.exe`,
		ConfigPath: `C:\Users\me\.config\portato\config.yaml`,
		Account:    `DOMAIN\me`,
		Password:   "p4ss",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if path != scmServicePath {
		t.Errorf("Install returned %q, want %q", path, scmServicePath)
	}
	if len(fx.createCalls) != 1 {
		t.Fatalf("want 1 create call, got %d", len(fx.createCalls))
	}
	c := fx.createCalls[0]
	if c.name != ServiceName {
		t.Errorf("create name = %q, want %q", c.name, ServiceName)
	}
	wantArgs := []string{"daemon", "--config", `C:\Users\me\.config\portato\config.yaml`}
	if len(c.args) != len(wantArgs) {
		t.Errorf("create args = %v, want %v", c.args, wantArgs)
	}
	// mgr.CreateService appends args after the escaped exepath; the daemon must
	// be told to run as a daemon with the config path.
	for i, want := range wantArgs {
		if i >= len(c.args) || c.args[i] != want {
			t.Errorf("create args[%d] = %q, want %q (full: %v)", i, argOr(c.args, i), want, c.args)
		}
	}
	if c.cfg.ServiceType != windows.SERVICE_WIN32_OWN_PROCESS {
		t.Errorf("ServiceType = %d, want SERVICE_WIN32_OWN_PROCESS", c.cfg.ServiceType)
	}
	if c.cfg.StartType != mgr.StartAutomatic {
		t.Errorf("StartType = %d, want StartAutomatic", c.cfg.StartType)
	}
	if !c.cfg.DelayedAutoStart {
		t.Errorf("DelayedAutoStart = false, want true")
	}
	if len(c.cfg.Dependencies) != 1 || c.cfg.Dependencies[0] != "Tcpip" {
		t.Errorf("Dependencies = %v, want [Tcpip]", c.cfg.Dependencies)
	}
	if c.cfg.ServiceStartName != `DOMAIN\me` {
		t.Errorf("ServiceStartName = %q, want DOMAIN\\me", c.cfg.ServiceStartName)
	}
	if c.cfg.Password != "p4ss" {
		t.Errorf("Password = %q, want p4ss", c.cfg.Password)
	}

	// Recovery actions set + the service started immediately.
	svc1 := fx.services[ServiceName]
	if svc1 == nil {
		t.Fatal("service not registered in fake")
	}
	if len(svc1.recoveryActions) != 1 || svc1.recoveryActions[0].Type != mgr.ServiceRestart {
		t.Errorf("recovery = %v, want one ServiceRestart", svc1.recoveryActions)
	}
	if svc1.recoveryReset != 60 {
		t.Errorf("recovery reset = %d, want 60s", svc1.recoveryReset)
	}
	if len(svc1.startArgs) != 1 {
		t.Errorf("service was not started (startArgs=%v)", svc1.startArgs)
	}
	if svc1.deleted {
		t.Error("service was deleted on install")
	}
}

func TestWindows_SCMInstall_IdempotentUpdate(t *testing.T) {
	fx := newFakeSCM()
	fx.createErrOnExists = windows.ERROR_SERVICE_EXISTS
	existing := &fakeSvc{name: ServiceName, queryState: svc.Running}
	fx.services[ServiceName] = existing
	w := &windowsInstaller{scm: fx}

	if _, err := w.Install(Options{BinaryPath: `C:\p.exe`, ConfigPath: `C:\c.yaml`, Account: `DOMAIN\me`, Password: "x"}); err != nil {
		t.Fatalf("idempotent Install: %v", err)
	}
	// create was attempted, then open returned the existing service, whose
	// config was updated.
	if len(fx.openCalls) == 0 || fx.openCalls[len(fx.openCalls)-1] != ServiceName {
		t.Errorf("expected an open(%s) after create-on-exists; openCalls=%v", ServiceName, fx.openCalls)
	}
	if existing.updatedCfg == nil {
		t.Errorf("existing service config was not updated on re-install")
	}
	if existing.updatedCfg.StartType != mgr.StartAutomatic {
		t.Errorf("updated StartType = %d, want StartAutomatic", existing.updatedCfg.StartType)
	}
}

func TestWindows_SCMInstall_LocalSystem_NoPassword(t *testing.T) {
	// A LocalSystem / NT AUTHORITY\SYSTEM account must collapse to an empty
	// ServiceStartName (SCM's marker for LocalSystem) and carry no password.
	fx := newFakeSCM()
	w := &windowsInstaller{scm: fx}
	for _, account := range []string{"LocalSystem", `NT AUTHORITY\SYSTEM`, "localsystem"} {
		fx.services = map[string]*fakeSvc{}
		fx.createCalls = nil
		if _, err := w.Install(Options{BinaryPath: `C:\p.exe`, ConfigPath: `C:\c.yaml`, Account: account}); err != nil {
			t.Fatalf("Install account=%q: %v", account, err)
		}
		c := fx.createCalls[len(fx.createCalls)-1]
		if c.cfg.ServiceStartName != "" {
			t.Errorf("account %q: ServiceStartName = %q, want empty (LocalSystem)", account, c.cfg.ServiceStartName)
		}
		if c.cfg.Password != "" {
			t.Errorf("account %q: Password = %q, want empty", account, c.cfg.Password)
		}
	}
}

func TestWindows_SCMUninstall_StopsAndDeletes(t *testing.T) {
	fx := newFakeSCM()
	running := &fakeSvc{name: ServiceName, queryState: svc.Running}
	fx.services[ServiceName] = running
	w := &windowsInstaller{scm: fx}

	if err := w.Uninstall(Options{}); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(running.ctrl) == 0 || running.ctrl[0] != svc.Stop {
		t.Errorf("expected svc.Stop before delete; ctrl=%v", running.ctrl)
	}
	if !running.deleted {
		t.Error("service was not deleted")
	}
}

func TestWindows_SCMUninstall_MissingIsNoop(t *testing.T) {
	fx := newFakeSCM()
	fx.openErr = windows.ERROR_SERVICE_DOES_NOT_EXIST
	w := &windowsInstaller{scm: fx}
	if err := w.Uninstall(Options{}); err != nil {
		t.Errorf("Uninstall on missing service should be a no-op, got %v", err)
	}
}

func TestWindows_LegacyRunKey_RoutesToRegistry(t *testing.T) {
	fx := newFakeSCM()
	w := &windowsInstaller{scm: fx}

	path, err := w.Install(Options{Legacy: true, BinaryPath: `C:\p.exe`, ConfigPath: `C:\c.yaml`})
	if err != nil {
		t.Fatalf("legacy Install: %v", err)
	}
	if len(fx.createCalls) != 0 {
		t.Errorf("legacy install must not touch SCM; createCalls=%v", fx.createCalls)
	}
	if path != `HKCU\`+runKeyPath {
		t.Errorf("legacy path = %q, want HKCU Run key", path)
	}
	// Clean up the value the legacy path wrote to the real HKCU.
	t.Cleanup(func() { _ = windowsInstaller{}.runKeyUninstall() })
}

func TestWindows_Status_PrefersSCM(t *testing.T) {
	// SCMInstalled uses the package defaultSCM; swap it so Status routes to SCM
	// without a real service, then verify the label.
	old := defaultSCM
	t.Cleanup(func() { defaultSCM = old })
	fx := newFakeSCM()
	fx.services[ServiceName] = &fakeSvc{name: ServiceName, queryState: svc.Running}
	defaultSCM = fx

	w := &windowsInstaller{scm: fx}
	got, err := w.Status(Options{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != "running" {
		t.Errorf("Status = %q, want running", got)
	}
}

func TestWindows_StopService_SendsStopControl(t *testing.T) {
	old := defaultSCM
	t.Cleanup(func() { defaultSCM = old })
	oldTimeout, oldPoll := stopServiceTimeout, stopServicePollInterval
	stopServiceTimeout = time.Second
	stopServicePollInterval = 10 * time.Millisecond
	t.Cleanup(func() { stopServiceTimeout, stopServicePollInterval = oldTimeout, oldPoll })

	fx := newFakeSCM()
	running := &fakeSvc{name: ServiceName, queryState: svc.Running}
	fx.services[ServiceName] = running
	defaultSCM = fx

	stopped, err := StopService()
	if err != nil {
		t.Fatalf("StopService: %v", err)
	}
	if !stopped {
		t.Error("StopService returned stopped=false for a running service")
	}
	if len(running.ctrl) == 0 || running.ctrl[0] != svc.Stop {
		t.Errorf("StopService did not send svc.Stop; ctrl=%v", running.ctrl)
	}
}

func TestWindows_StopService_NoServiceIsNoop(t *testing.T) {
	old := defaultSCM
	t.Cleanup(func() { defaultSCM = old })
	fx := newFakeSCM() // no services
	defaultSCM = fx
	stopped, err := StopService()
	if err != nil {
		t.Fatalf("StopService: %v", err)
	}
	if stopped {
		t.Error("StopService returned stopped=true with no service installed")
	}
}

func argOr(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}
