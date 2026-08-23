//go:build windows

package cmd

import "github.com/portuber/portato/internal/service"

// scmServiceInstalled reports whether the Portato SCM service is registered;
// the update apply refuses to swap a service-held binary (the SCM service
// would keep running the old bytes until reboot and an in-place swap can
// fail on a held file). Mirrors the stopViaSCM seam.
var scmServiceInstalled = service.SCMInstalled
