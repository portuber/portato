// Package daemon implements the background daemon: an HTTP server over a unix
// socket that owns a forward.Engine and exposes the REST API consumed by the
// TUI (portato attach) and the CLI commands. It also covers single-instance
// locking, socket/PID discovery, systemd socket activation, and config-file
// watching for live reload.
package daemon
