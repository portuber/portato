package controller

import (
	"errors"
	"testing"
	"time"

	"github.com/kipkaev55/portato/internal/forward"
)

type fakeClient struct {
	list []forward.Status
	err  error

	enables, disables, restarts, reloads int
}

func (f *fakeClient) List() ([]forward.Status, error) { return f.list, f.err }
func (f *fakeClient) Enable(string) error             { f.enables++; return nil }
func (f *fakeClient) Disable(string) error            { f.disables++; return nil }
func (f *fakeClient) Restart(string) error            { f.restarts++; return nil }
func (f *fakeClient) Reload() error                   { f.reloads++; return nil }

func TestRemote_ListDelegates(t *testing.T) {
	want := []forward.Status{{Name: "a", State: forward.Connected}}
	r := newRemote(&fakeClient{list: want}, time.Hour)
	got := r.List()
	if len(got) != 1 || got[0].Name != "a" || got[0].State != forward.Connected {
		t.Fatalf("got %+v", got)
	}
}

func TestRemote_ListOnErrorEmpty(t *testing.T) {
	r := newRemote(&fakeClient{err: errors.New("boom")}, time.Hour)
	if got := r.List(); len(got) != 0 {
		t.Fatalf("expected empty list on error, got %+v", got)
	}
}

func TestRemote_MutationsDelegate(t *testing.T) {
	fc := &fakeClient{}
	r := newRemote(fc, time.Hour)
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

func TestRemote_ChangesEmitsAndCloses(t *testing.T) {
	r := newRemote(&fakeClient{}, 5*time.Millisecond)
	ch := r.Changes()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Changes() did not emit within 1s")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after Close")
	}
}

func TestRemote_CloseIdempotent(t *testing.T) {
	r := newRemote(&fakeClient{}, time.Hour)
	r.Changes()
	for i := 0; i < 3; i++ {
		if err := r.Close(); err != nil {
			t.Fatalf("close #%d: %v", i, err)
		}
	}
}
