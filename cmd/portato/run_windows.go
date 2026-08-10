//go:build windows

package main

import (
	"os"

	"github.com/portuber/portato/internal/cmd"
	"github.com/portuber/portato/internal/service"
	"golang.org/x/sys/windows/svc"
)

// run distinguishes an SCM launch from an interactive one. The Service Control
// Manager starts services with no stdin/stdout/stderr and an argv of just the
// image path; cobra must not run in that mode (any stdio touch would
// misbehave). When launched by SCM, portato runs the daemon as a service via
// service.RunAsService; otherwise the normal cobra tree handles every CLI
// subcommand. The daemon's recorded command line (`<exe> daemon --config
// <abs>`) is parsed by the SCM handler before RunDaemon so the config path is
// honoured under SCM the same way it is under the Run key.
func run() error {
	isSvc, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if isSvc {
		cmd.ParseDaemonArgs(os.Args[1:])
		return service.RunAsService(cmd.RunDaemon)
	}
	return cmd.Execute()
}
