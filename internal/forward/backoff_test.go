package forward

import (
	"testing"
	"time"
)

func TestNextBackoff(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 30 * time.Second},
		{6, 30 * time.Second},
		{100, 30 * time.Second},
		{-1, 1 * time.Second},
	}
	for _, c := range cases {
		if got := nextBackoff(c.attempt); got != c.want {
			t.Errorf("nextBackoff(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestNextAttemptAfterDisconnect(t *testing.T) {
	cases := []struct {
		name    string
		stable  time.Duration
		attempt int
		want    int
	}{
		{"short session advances", 1 * time.Second, 0, 1},
		{"short session advances further", 2 * time.Second, 3, 4},
		{"just under threshold advances", stableResetInterval - 1, 5, 6},
		{"at threshold resets", stableResetInterval, 5, 0},
		{"long session resets", 5 * time.Minute, 9, 0},
		{"reset from zero stays zero", 5 * time.Minute, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nextAttemptAfterDisconnect(c.stable, c.attempt); got != c.want {
				t.Errorf("nextAttemptAfterDisconnect(%v, %d) = %d, want %d", c.stable, c.attempt, got, c.want)
			}
		})
	}
}
