//go:build windows

package cmd

import "github.com/portuber/portato/internal/service"

// stopViaSCM stops the running Portato SCM service (if any). Returns
// (true, nil) when a service was found and stopped, so stopRunE returns without
// falling through to the PID-based kill path. Overridable in tests.
var stopViaSCM = service.StopService
