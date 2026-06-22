package controller

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kipkaev55/portato/internal/config"
	"github.com/kipkaev55/portato/internal/forward"
	routelog "github.com/kipkaev55/portato/internal/log"
)

type fakeClient struct {
	list []forward.Status
	err  error

	enables, disables, restarts, reloads int

	cfg       *config.Config
	adds      []config.Tunnel
	updates   []config.Tunnel
	deletes   []string
	muTunnels sync.Mutex

	mu      sync.Mutex
	streams []io.ReadCloser
	calls   int
}

func (f *fakeClient) List() ([]forward.Status, error) { return f.list, f.err }
func (f *fakeClient) Enable(string) error             { f.enables++; return nil }
func (f *fakeClient) Disable(string) error            { f.disables++; return nil }
func (f *fakeClient) Restart(string) error            { f.restarts++; return nil }
func (f *fakeClient) Reload() error                   { f.reloads++; return nil }

func (f *fakeClient) Config() (*config.Config, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.cfg == nil {
		return &config.Config{}, nil
	}
	return f.cfg.Clone(), nil
}
func (f *fakeClient) AddTunnel(t config.Tunnel) error {
	f.muTunnels.Lock()
	defer f.muTunnels.Unlock()
	f.adds = append(f.adds, t)
	return f.err
}
func (f *fakeClient) UpdateTunnel(name string, t config.Tunnel) error {
	f.muTunnels.Lock()
	defer f.muTunnels.Unlock()
	f.updates = append(f.updates, t)
	return f.err
}
func (f *fakeClient) DeleteTunnel(name string) error {
	f.muTunnels.Lock()
	defer f.muTunnels.Unlock()
	f.deletes = append(f.deletes, name)
	return f.err
}

func (f *fakeClient) Logs(string) ([]routelog.Entry, error) { return nil, f.err }

func (f *fakeClient) AcceptHost(string) error { return f.err }

// Events pops the next queued stream; when none remain it blocks on ctx,
// modelling a daemon that is up but produces no further events. This lets the
// reconnect loop be exercised deterministically without a real server.
func (f *fakeClient) Events(ctx context.Context) (io.ReadCloser, error) {
	f.mu.Lock()
	f.calls++
	var s io.ReadCloser
	if len(f.streams) > 0 {
		s = f.streams[0]
		f.streams = f.streams[1:]
	}
	f.mu.Unlock()
	if s != nil {
		return s, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeClient) eventsCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// dataFrames builds a ReadCloser that emits n signal-only SSE data frames.
func dataFrames(n int) io.ReadCloser {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("data: {}\n\n")
	}
	return io.NopCloser(strings.NewReader(b.String()))
}

func TestRemote_ListDelegates(t *testing.T) {
	want := []forward.Status{{Name: "a", State: forward.Connected}}
	r := newRemote(&fakeClient{list: want})
	got := r.List()
	if len(got) != 1 || got[0].Name != "a" || got[0].State != forward.Connected {
		t.Fatalf("got %+v", got)
	}
}

func TestRemote_ListOnErrorEmpty(t *testing.T) {
	r := newRemote(&fakeClient{err: errors.New("boom")})
	if got := r.List(); len(got) != 0 {
		t.Fatalf("expected empty list on error, got %+v", got)
	}
}

func TestRemote_MutationsDelegate(t *testing.T) {
	fc := &fakeClient{}
	r := newRemote(fc)
	if err := r.Enable("a"); err != nil {
		t.Fatal(err)
	}
	if err := r.Disable("a"); err != nil {
		t.Fatal(err)
	}
	if err := r.Restart("a"); err != nil {
		t.Fatal(err)
	}
	if err := r.Reload(); err != nil {
		t.Fatal(err)
	}
	if fc.enables != 1 || fc.disables != 1 || fc.restarts != 1 || fc.reloads != 1 {
		t.Fatalf("counts = enables:%d disables:%d restarts:%d reloads:%d", fc.enables, fc.disables, fc.restarts, fc.reloads)
	}
}

func TestRemote_ChangesEmitsOnStreamFrame(t *testing.T) {
	fc := &fakeClient{streams: []io.ReadCloser{dataFrames(1)}}
	r := newRemote(fc)
	ch := r.Changes()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no push signal from stream frame")
	}

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after Close")
	}
}

// TestRemote_ChangesReconnectsAcrossStreamBreak proves the stream loop
// re-establishes the subscription after a stream ends: two single-frame
// streams yield two signals, and Events is called at least twice.
func TestRemote_ChangesReconnectsAcrossStreamBreak(t *testing.T) {
	fc := &fakeClient{streams: []io.ReadCloser{dataFrames(1), dataFrames(1)}}
	r := newRemote(fc)
	ch := r.Changes()

	for i := 0; i < 2; i++ {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("signal %d not received (reconnect failed); calls=%d", i, fc.eventsCalls())
		}
	}
	if calls := fc.eventsCalls(); calls < 2 {
		t.Fatalf("Events called %d times, want >= 2 (reconnect)", calls)
	}

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRemote_CloseIdempotent(t *testing.T) {
	r := newRemote(&fakeClient{})
	r.Changes()
	for i := 0; i < 3; i++ {
		if err := r.Close(); err != nil {
			t.Fatalf("close #%d: %v", i, err)
		}
	}
}
