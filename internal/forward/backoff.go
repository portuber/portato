package forward

import "time"

const (
	backoffBase = 1 * time.Second
	backoffMax  = 30 * time.Second
)

// nextBackoff returns the reconnect delay for a given (zero-based) attempt,
// doubling from 1s up to a 30s cap.
func nextBackoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	d := backoffBase << uint(attempt)
	if d <= 0 || d > backoffMax {
		return backoffMax
	}
	return d
}

// nextAttemptAfterDisconnect computes the attempt counter to use after a
// connection drops. A connection that stayed up for at least stableResetInterval
// resets the counter (so the next reconnect uses the base delay again);
// otherwise the counter advances, growing the backoff.
func nextAttemptAfterDisconnect(stable time.Duration, attempt int) int {
	if stable >= stableResetInterval {
		return 0
	}
	return attempt + 1
}
