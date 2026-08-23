//go:build windows

package main

import (
	"os"

	"github.com/portuber/portato/internal/cmd"
	"github.com/portuber/portato/internal/service"
	"github.com/portuber/portato/internal/update"
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
//
// Before either branch, a staged self-update (portato.new next to this
// binary, written by `portato update apply`) is completed: at this point the
// previous process has exited, so the old .exe is no longer held and the
// rename dance succeeds. This early — before SCM detection — so a service
// relaunch picks the new version up too.
func run() error {
	if exe, err := os.Executable(); err == nil {
		if update.StagedSwapPending(exe) {
			update.CompleteStagedSwap(exe)
		}
	}
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
