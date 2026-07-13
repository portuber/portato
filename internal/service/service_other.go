//go:build !darwin && !linux && !windows

package service

import "fmt"

// unsupportedInstaller lets the package compile on OSes without an autostart
// implementation (any OS other than macOS, Linux and Windows). Every method
// returns a clear error.
type unsupportedInstaller struct{}

func newInstaller() Installer { return &unsupportedInstaller{} }

func (unsupportedInstaller) Install(Options) (string, error) {
	return "", fmt.Errorf("autostart is not supported on this OS (macOS, Linux and Windows only)")
}

func (unsupportedInstaller) Uninstall(Options) error {
	return fmt.Errorf("autostart is not supported on this OS (macOS, Linux and Windows only)")
}

func (unsupportedInstaller) Status(Options) (string, error) {
	return "", fmt.Errorf("autostart is not supported on this OS (macOS, Linux and Windows only)")
}
