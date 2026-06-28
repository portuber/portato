package cmd

import (
	"log/slog"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
		err  bool
	}{
		{"", slog.LevelInfo, false},
		{"info", slog.LevelInfo, false},
		{"debug", slog.LevelDebug, false},
		{"warn", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"INFO", 0, true},  // case-sensitive: reject
		{"trace", 0, true}, // unknown level
		{"verbose", 0, true},
	}
	for _, c := range cases {
		got, err := parseLogLevel(c.in)
		if c.err {
			if err == nil {
				t.Errorf("parseLogLevel(%q): want error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseLogLevel(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseLogLevel(%q): got %v, want %v", c.in, got, c.want)
		}
	}
}
