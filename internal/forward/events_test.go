package forward

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kipkaev55/portato/internal/config"
)

func TestEngineSubscribeNotify(t *testing.T) {
	e, _ := newTestEngine(&config.Config{Tunnels: []config.Tunnel{tunnelCfg("a")}})
	ch, unsub := e.Subscribe()

	e.notify()
	select {
	case <-ch:
	default:
		t.Fatal("expected an event after notify")
	}

	unsub()
	e.notify()
	select {
	case <-ch:
		t.Fatal("should not receive after unsubscribe")
	default:
	}
}

func TestEngineSubscribeMultiple(t *testing.T) {
	e, _ := newTestEngine(&config.Config{Tunnels: []config.Tunnel{tunnelCfg("a")}})
	ch1, unsub1 := e.Subscribe()
	defer unsub1()
	ch2, unsub2 := e.Subscribe()
	defer unsub2()

	e.notify()
	for i, ch := range []<-chan struct{}{ch1, ch2} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d did not receive", i)
		}
	}
}

func TestEngineNotifyDropOld(t *testing.T) {
	e, _ := newTestEngine(&config.Config{Tunnels: []config.Tunnel{tunnelCfg("a")}})
	ch, unsub := e.Subscribe()
	defer unsub()

	// Overwhelm the buffer: notify must never block, and at least one
	// signal must be observable afterwards.
	for i := 0; i < subscriberBuffer*4; i++ {
		e.notify()
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("expected at least one event after burst")
	}
}

func TestEngineSubscribeUnsubscribeIdempotent(t *testing.T) {
	e, _ := newTestEngine(&config.Config{Tunnels: []config.Tunnel{tunnelCfg("a")}})
	_, unsub := e.Subscribe()
	unsub()
	unsub() // must not panic
}

// TestEngineFactoryWiresOnChange proves the full push chain end to end without
// SSH: a real Engine builds a real *Tunnel (its onChange is wired to e.notify),
// and a state transition on that tunnel reaches a subscriber.
func TestEngineFactoryWiresOnChange(t *testing.T) {
	e := NewEngine(context.Background(), &config.Config{Tunnels: []config.Tunnel{tunnelCfg("a")}}, slog.Default())
	ch, unsub := e.Subscribe()
	defer unsub()

	tn := e.tunnels["a"].(*Tunnel)
	tn.setState(Connected)

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive a tunnel state-change event")
	}
}

func TestTunnelEmitsOnChange(t *testing.T) {
	var fired atomic.Int64
	tn := &Tunnel{
		baseCtx:  context.Background(),
		cfg:      tunnelCfg("x"),
		onChange: func() { fired.Add(1) },
	}
	tn.setState(Connecting)
	tn.setState(Connected)
	tn.setStateErr(Error, "boom")
	tn.setState(Reconnecting)
	if got := fired.Load(); got != 4 {
		t.Fatalf("fired = %d, want 4", got)
	}
}

func TestTunnelNoChangeNoNotify(t *testing.T) {
	var fired atomic.Int64
	tn := &Tunnel{
		baseCtx:  context.Background(),
		cfg:      tunnelCfg("x"),
		onChange: func() { fired.Add(1) },
	}
	// Stop on a never-started tunnel returns early and must not notify.
	if err := tn.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := fired.Load(); got != 0 {
		t.Fatalf("fired = %d, want 0 (no state transition on idle Stop)", got)
	}
}
