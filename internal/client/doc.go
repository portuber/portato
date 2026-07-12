// Package client is the HTTP client side of the daemon IPC: it dials the
// daemon's unix socket and exposes typed methods (Healthz, List, Enable,
// Disable, Restart, Reload, ...) used by the CLI commands and by
// controller.Remote.
package client
