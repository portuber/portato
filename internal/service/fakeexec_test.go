package service

import (
	"strings"
	"sync"
)

// fakeExec records every invocation as "name arg arg..." and returns canned
// responses keyed by the program name. Lets tests assert the exact launchd /
// systemctl command sequence without touching the system. Shared by the
// per-OS service tests (lives in a non-build-tagged file so the linux tests
// can use it too — the helper itself is OS-independent).
type fakeExec struct {
	mu    sync.Mutex
	calls []string
	resp  map[string][]byte
	errOn map[string]error
}

func newFakeExec() *fakeExec {
	return &fakeExec{resp: map[string][]byte{}, errOn: map[string]error{}}
}

func (f *fakeExec) run(name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, strings.Join(append([]string{name}, args...), " "))
	if err, ok := f.errOn[name]; ok {
		return nil, err
	}
	return f.resp[name], nil
}

func (f *fakeExec) joined() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, "\n")
}
