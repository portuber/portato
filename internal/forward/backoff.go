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
