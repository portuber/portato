package secret

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func newTestStore(persist bool) (*Store, *MemBackend) {
	b := NewMemBackend()
	return NewStore(b, func() bool { return persist }), b
}

// TestSetGetRoundTrip covers the basic cache path: Set then Get returns the
// value; Delete removes it.
func TestSetGetRoundTrip(t *testing.T) {
	s, _ := newTestStore(false)
	if _, ok := s.Get("/x/id"); ok {
		t.Fatal("Get before Set should miss")
	}
	if err := s.Set("/x/id", "hunter2"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, ok := s.Get("/x/id"); !ok || v != "hunter2" {
		t.Fatalf("Get after Set = %q,%v; want hunter2,true", v, ok)
	}
	if err := s.Delete("/x/id"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get("/x/id"); ok {
		t.Fatal("Get after Delete should miss")
	}
}

// TestPersistWritesKeyring asserts Set writes the keyring only when persist()
// is true, and that Delete always clears the keyring regardless of persist.
func TestPersistWritesKeyring(t *testing.T) {
	s, b := newTestStore(true)
	if err := s.Set("/x/id", "pw"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, err := b.Get(Service, "/x/id"); err != nil || v != "pw" {
		t.Fatalf("persist on: keyring should hold the value; got %q,%v", v, err)
	}

	sNo, bNo := newTestStore(false)
	if err := sNo.Set("/x/id", "pw"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := bNo.Get(Service, "/x/id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("persist off: keyring must NOT be written; got err=%v", err)
	}

	// Delete always clears the keyring, even with persist off. Seed the
	// keyring directly, then Delete via a non-persisting store.
	if err := bNo.Set(Service, "/x/id", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := sNo.Delete("/x/id"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := bNo.Get(Service, "/x/id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete should clear keyring; got err=%v", err)
	}
}

// TestGetFallsBackToKeyring seeds the keyring directly (as portato add-identity
// would with no daemon) and checks Get picks it up and caches it.
func TestGetFallsBackToKeyring(t *testing.T) {
	s, b := newTestStore(false)
	if err := b.Set(Service, "/x/id", "from-keyring"); err != nil {
		t.Fatal(err)
	}
	v, ok := s.Get("/x/id")
	if !ok || v != "from-keyring" {
		t.Fatalf("Get should fall back to keyring; got %q,%v", v, ok)
	}
	// Now cached: dropping the keyring value must not change Get.
	_ = b.Delete(Service, "/x/id")
	if v, ok := s.Get("/x/id"); !ok || v != "from-keyring" {
		t.Fatalf("Get should read the cache after a keyring hit; got %q,%v", v, ok)
	}
}

// TestWaitUnblocksOnSet starts a blocked Wait, then Set from another goroutine
// and asserts Wait returns the value promptly.
func TestWaitUnblocksOnSet(t *testing.T) {
	s, _ := newTestStore(false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got := make(chan string, 1)
	go func() {
		v, ok := s.Wait(ctx, "/x/id")
		if !ok {
			got <- ""
			return
		}
		got <- v
	}()

	// Give the waiter a moment to register.
	time.Sleep(20 * time.Millisecond)
	if err := s.Set("/x/id", "late"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	select {
	case v := <-got:
		if v != "late" {
			t.Fatalf("Wait returned %q; want late", v)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not unblock on Set")
	}
}

// TestWaitReturnsOnContextDone asserts a cancelled Wait returns promptly with
// ok=false (the dial's disable/shutdown path).
func TestWaitReturnsOnContextDone(t *testing.T) {
	s, _ := newTestStore(false)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		if _, ok := s.Wait(ctx, "/x/id"); ok {
			t.Error("Wait should return ok=false on ctx cancel")
		}
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after ctx cancel")
	}
}

// TestWaitPreSeeded asserts that if Set arrives BEFORE Wait registers, Wait
// still returns the value (no lost-wakeup).
func TestWaitPreSeeded(t *testing.T) {
	s, _ := newTestStore(false)
	if err := s.Set("/x/id", "early"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	v, ok := s.Wait(ctx, "/x/id")
	if !ok || v != "early" {
		t.Fatalf("Wait on a pre-seeded path = %q,%v; want early,true", v, ok)
	}
}

// TestConcurrentWaiters sets many waiters on the same path and asserts Set
// wakes all of them exactly once.
func TestConcurrentWaiters(t *testing.T) {
	s, _ := newTestStore(false)
	const n = 5
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	results := make([]string, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			v, ok := s.Wait(ctx, "/x/id")
			if ok {
				results[i] = v
			}
		}(i)
	}
	close(start)
	time.Sleep(30 * time.Millisecond) // let all register
	if err := s.Set("/x/id", "all"); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	for i, v := range results {
		if v != "all" {
			t.Errorf("waiter %d got %q; want all", i, v)
		}
	}
}

// TestDeleteDoesNotWakeWaiters asserts Delete leaves a blocked Wait in place
// (a waiting dial has no value to forget; waking would only force a re-Wait).
func TestDeleteDoesNotWakeWaiters(t *testing.T) {
	s, _ := newTestStore(false)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	returned := make(chan struct{})
	go func() {
		_, _ = s.Wait(ctx, "/x/id")
		close(returned)
	}()
	time.Sleep(20 * time.Millisecond)
	_ = s.Delete("/x/id")
	// Delete must not unblock the waiter; only the ctx timeout should end it.
	select {
	case <-returned:
		t.Fatal("Delete woke the waiter; it should stay blocked until ctx done")
	case <-time.After(80 * time.Millisecond):
	}
	<-returned // ctx deadline ends Wait
}
