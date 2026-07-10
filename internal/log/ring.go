package log

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Entry is one captured log record, for display in the TUI logs screen.
type Entry struct {
	Time  time.Time  `json:"time"`
	Level slog.Level `json:"level"`
	Tuber string     `json:"tuber,omitempty"`
	Msg   string     `json:"msg"`
	// Attrs is the record's non-tuber attributes rendered as "k=v k2=v2"
	// (e.g. dest=host:port, err=…). Pre-rendered so it is JSON-safe across the
	// daemon IPC and trivial to display.
	Attrs string `json:"attrs,omitempty"`
}

// ringCap bounds how many entries the ring keeps in memory. Old entries are
// dropped once the cap is reached. Sized for a reasonable scrollback window
// without unbounded growth in a long-running daemon.
const ringCap = 2000

// Ring is an in-memory ring buffer of recent log entries, keyed by the
// `tuber` slog attribute. It is the source the TUI logs screen reads from
// (directly in standalone mode, via the daemon's GET /logs in attach mode).
// Safe for concurrent use.
type Ring struct {
	mu  sync.Mutex
	buf []Entry
}

func NewRing() *Ring { return &Ring{} }

func (r *Ring) Append(e Entry) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, e)
	if len(r.buf) > ringCap {
		// Drop the oldest overflow in one trim; called rarely relative to the
		// steady-state append rate, so the copy cost is negligible.
		r.buf = append([]Entry(nil), r.buf[len(r.buf)-ringCap:]...)
	}
}

// Lines returns a copy of the entries for the given tuber. An empty tuber
// returns every entry. The slice is safe to hold; later appends do not mutate
// it. A nil ring yields nil.
func (r *Ring) Lines(tuber string) []Entry {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if tuber == "" {
		out := make([]Entry, len(r.buf))
		copy(out, r.buf)
		return out
	}
	out := make([]Entry, 0, len(r.buf))
	for _, e := range r.buf {
		if e.Tuber == tuber {
			out = append(out, e)
		}
	}
	return out
}

// ringHandler is a slog.Handler that fans each record out to a base handler
// (the file text handler) and into a Ring (for the TUI logs screen). It also
// remembers attrs added via WithAttrs so the per-tuber attribute — set by
// `logger.With("tuber", name)` — can be attached to the captured entry.
type ringHandler struct {
	base  slog.Handler
	ring  *Ring
	attrs []slog.Attr
}

// ringLevel is the minimum level captured into the ring. The TUI logs screen
// surfaces per-connection activity (logged at Debug), so the ring captures at
// Debug regardless of the file handler's threshold. The file write is still
// gated by the base handler (Info) in Handle — the ring simply sees more.
const ringLevel = slog.LevelDebug

// Enabled admits a record if either the ring wants it (>= ringLevel) or the
// base/file handler would write it. This is what lets Debug records reach
// Handle so they can be captured into the ring even when the file is at Info.
func (h ringHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= ringLevel || h.base.Enabled(ctx, level)
}

func (h ringHandler) Handle(ctx context.Context, r slog.Record) error {
	// Capture into the ring first (cheap); the ring keeps Debug+ independent
	// of the file level. The base handler writes the file line only when the
	// record meets the base (Info) threshold.
	tuber := attrString(h.attrs, "tuber")
	if tuber == "" {
		tuber = recordAttrString(r, "tuber")
	}
	h.ring.Append(Entry{
		Time:  r.Time,
		Level: r.Level,
		Tuber: tuber,
		Msg:   r.Message,
		Attrs: renderAttrs(h.attrs, r),
	})
	if h.base.Enabled(ctx, r.Level) {
		return h.base.Handle(ctx, r)
	}
	return nil
}

// renderAttrs renders the record's attributes (persistent WithAttrs ones plus
// the per-call ones) as "k=v k2=v2", skipping the tuber attribute (it is
// already carried by Entry.Tuber). Used so the logs screen shows context like
// dest=host:port or err=… instead of a bare message.
func renderAttrs(persistent []slog.Attr, r slog.Record) string {
	var b strings.Builder
	add := func(a slog.Attr) {
		if a.Equal(slog.Attr{}) || a.Key == "tuber" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%s=%v", a.Key, a.Value.Any())
	}
	for _, a := range persistent {
		add(a)
	}
	r.Attrs(func(a slog.Attr) bool { add(a); return true })
	return b.String()
}

func (h ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return ringHandler{
		base:  h.base.WithAttrs(attrs),
		ring:  h.ring,
		attrs: merged,
	}
}

func (h ringHandler) WithGroup(name string) slog.Handler {
	return ringHandler{
		base:  h.base.WithGroup(name),
		ring:  h.ring,
		attrs: h.attrs,
	}
}

func attrString(attrs []slog.Attr, key string) string {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value.String()
		}
	}
	return ""
}

func recordAttrString(r slog.Record, key string) string {
	var out string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			out = a.Value.String()
			return false
		}
		return true
	})
	return out
}
