// Package controller defines the Controller interface — the abstraction the
// TUI and CLI use to drive tubers — together with its two implementations:
// Local, which runs a forward.Engine in-process (the standalone portato
// launcher), and Remote, which proxies to a running daemon via the client
// package (portato attach and the CLI commands).
package controller
