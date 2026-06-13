// Package forward implements the SSH tunnel engine: per-tunnel lifecycle
// (state machine, reconnect with exponential backoff, keepalive) and a
// thread-safe Engine managing a set of tunnels built from a config.Config.
package forward
