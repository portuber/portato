// Package forward implements the SSH tuber engine: per-tuber lifecycle
// (state machine, reconnect with exponential backoff, keepalive) and a
// thread-safe Engine managing a set of tubers built from a config.Config.
package forward
