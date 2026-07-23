package daemon

// PidAlive is the exported form of the per-OS pidAlive, for cross-package
// reuse (the `cmd` stop/doctor recovery path). Windows and unix each provide
// their own pidAlive implementation behind this wrapper.
func PidAlive(pid int) bool { return pidAlive(pid) }
