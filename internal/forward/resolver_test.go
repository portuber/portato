package forward

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
)

type fakeResolver struct {
	ip  net.IP
	err error
}

func (f fakeResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	return ctx, f.ip, f.err
}

func TestLoggingResolver(t *testing.T) {
	t.Run("success logs debug with name and ip", func(t *testing.T) {
		var buf bytes.Buffer
		l := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		r := loggingResolver{inner: fakeResolver{ip: net.IPv4(1, 2, 3, 4)}, log: l}

		_, ip, err := r.Resolve(context.Background(), "ipinfo.io")
		if err != nil || !ip.Equal(net.IPv4(1, 2, 3, 4)) {
			t.Fatalf("Resolve = %v, %v, want ip propagated", ip, err)
		}
		out := buf.String()
		if !strings.Contains(out, "level=DEBUG") || !strings.Contains(out, `msg="socks5 resolve"`) {
			t.Errorf("expected a debug socks5 resolve line, got: %s", out)
		}
		if !strings.Contains(out, "name=ipinfo.io") || !strings.Contains(out, "ip=1.2.3.4") {
			t.Errorf("expected name+ip attrs, got: %s", out)
		}
	})

	t.Run("failure logs warn and returns the error", func(t *testing.T) {
		var buf bytes.Buffer
		l := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		boom := errors.New("lookup ipinfo.po: no such host")
		r := loggingResolver{inner: fakeResolver{err: boom}, log: l}

		_, ip, err := r.Resolve(context.Background(), "ipinfo.po")
		if !errors.Is(err, boom) {
			t.Fatalf("Resolve err = %v, want %v", err, boom)
		}
		if ip != nil {
			t.Errorf("Resolve ip = %v, want nil on failure", ip)
		}
		out := buf.String()
		if !strings.Contains(out, "level=WARN") || !strings.Contains(out, `msg="socks5 resolve failed"`) {
			t.Errorf("expected a warn socks5 resolve failed line, got: %s", out)
		}
		if !strings.Contains(out, "name=ipinfo.po") {
			t.Errorf("expected name attr, got: %s", out)
		}
	})
}
