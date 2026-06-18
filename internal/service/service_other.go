//go:build !darwin && !linux

package service

import "fmt"

// unsupportedInstaller lets the package compile on OSes without an autostart
// implementation (e.g. Windows — post-MVP). Every method returns a clear error.
type unsupportedInstaller struct{}

func newInstaller() Installer { return &unsupportedInstaller{} }

func (unsupportedInstaller) Install(Options) (string, error) {
	return "", fmt.Errorf("autostart is not supported on this OS (macOS and Linux only)")
}

func (unsupportedInstaller) Uninstall(Options) error {
	return fmt.Errorf("autostart is not supported on this OS (macOS and Linux only)")
}

func (unsupportedInstaller) Status(Options) (string, error) {
	return "", fmt.Errorf("autostart is not supported on this OS (macOS and Linux only)")
}
