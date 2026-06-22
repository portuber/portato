package log

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestRingHandler_CapturesTunnelAttrAndFilters(t *testing.T) {
	var buf bytes.Buffer
	ring := NewRing()
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := ringHandler{base: base, ring: ring}
	logger := slog.New(h)

	// WithAttrs persists the tunnel attr across records (this is how the
	// engine's per-tunnel sub-logger is built: log.With("tunnel", name)).
	tn := logger.With("tunnel", "db")
	tn.Info("connected")
	tn.Debug("keepalive ok")
	logger.Info("daemon started") // no tunnel attr

	all := ring.Lines("")
	if len(all) != 3 {
		t.Fatalf("Lines(\"\") = %d entries, want 3: %+v", len(all), all)
	}

	db := ring.Lines("db")
	if len(db) != 2 {
		t.Fatalf("Lines(db) = %d entries, want 2", len(db))
	}
	for _, e := range db {
		if e.Tunnel != "db" {
			t.Errorf("filtered entry tunnel = %q, want db", e.Tunnel)
		}
	}
	if db[0].Msg != "connected" || db[1].Msg != "keepalive ok" {
		t.Errorf("unexpected order: %+v", db)
	}

	// The file handler still received the formatted lines.
	out := buf.String()
	for _, want := range []string{"connected", "keepalive ok", "daemon started"} {
		if !strings.Contains(out, want) {
			t.Errorf("base output missing %q: %s", want, out)
		}
	}
}

func TestRingHandler_PerCallAttr(t *testing.T) {
	ring := NewRing()
	h := ringHandler{base: slog.NewTextHandler(io.Discard, nil), ring: ring}
	logger := slog.New(h)
	logger.Info("msg", "tunnel", "alpha")
	if e := ring.Lines("alpha"); len(e) != 1 || e[0].Msg != "msg" {
		t.Fatalf("per-call attr not captured: %+v", ring.Lines("alpha"))
	}
}

// TestRingHandler_RendersAttrs proves the non-tunnel record attributes are
// captured into Entry.Attrs as "k=v" (e.g. dest / err), so the logs screen
// can show connection context instead of a bare message.
func TestRingHandler_RendersAttrs(t *testing.T) {
	ring := NewRing()
	h := ringHandler{base: slog.NewTextHandler(io.Discard, nil), ring: ring}
	logger := slog.New(h)

	// Per-tunnel sub-logger: the tunnel attr is routed to Entry.Tunnel and
	// must NOT appear in Attrs.
	tn := logger.With("tunnel", "db")
	tn.Warn("socks5 dial failed", "dest", "ipinfo.po:443", "err", errors.New("no such host"))

	got := ring.Lines("db")
	if len(got) != 1 {
		t.Fatalf("Lines(db) = %d entries, want 1", len(got))
	}
	e := got[0]
	if e.Tunnel != "db" {
		t.Errorf("Tunnel = %q, want db", e.Tunnel)
	}
	if !strings.Contains(e.Attrs, "dest=ipinfo.po:443") {
		t.Errorf("Attrs %q missing dest=ipinfo.po:443", e.Attrs)
	}
	if !strings.Contains(e.Attrs, "err=no such host") {
		t.Errorf("Attrs %q missing err=no such host", e.Attrs)
	}
	if strings.Contains(e.Attrs, "tunnel=") {
		t.Errorf("tunnel must not appear in Attrs: %q", e.Attrs)
	}
}

func TestRing_DropsOldestOnOverflow(t *testing.T) {
	ring := NewRing()
	for i := 0; i < ringCap+50; i++ {
		ring.Append(Entry{Msg: "m", Tunnel: "x"})
	}
	if got := len(ring.Lines("")); got != ringCap {
		t.Fatalf("ring size = %d, want %d", got, ringCap)
	}
}

func TestRing_NilSafe(t *testing.T) {
	var r *Ring
	r.Append(Entry{})
	if got := r.Lines(""); got != nil {
		t.Fatalf("nil ring Lines = %v, want nil", got)
	}
}

func TestRingHandler_RingCapturesBelowFileLevel(t *testing.T) {
	var buf bytes.Buffer
	ring := NewRing()
	// The file handler is at Warn; the ring must still capture Info/Debug.
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := ringHandler{base: base, ring: ring}
	logger := slog.New(h)

	logger.Info("info-msg")   // below Warn → not written, but captured by the ring
	logger.Debug("debug-msg") // below Warn → not written, but captured by the ring

	if got := len(ring.Lines("")); got != 2 {
		t.Fatalf("ring = %d entries, want 2 (ring captures below the file level)", got)
	}
	if buf.Len() != 0 {
		t.Errorf("file should not receive below-threshold records: %q", buf.String())
	}
	// A record below even the ring level (Debug) must not be captured.
	logger.Log(context.Background(), slog.LevelDebug-8, "trace-msg")
	if got := len(ring.Lines("")); got != 2 {
		t.Errorf("trace-level record should not enter the ring: got %d", got)
	}
	// An Error meets both thresholds: captured and written.
	logger.Error("boom")
	if got := len(ring.Lines("")); got != 3 {
		t.Errorf("error record missing from ring: %d", got)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("error should reach the file: %q", buf.String())
	}
}
