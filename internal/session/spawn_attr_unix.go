//go:build !windows

package session

import (
	"os"
	"syscall"
)

// detachedProcAttr returns the SysProcAttr that puts the spawned worker into
// its own session (issue #54). With Setsid the worker is no longer in the
// conductor's process group, so neither macOS's SIGHUP-on-parent-exit nor a
// terminal-driven SIGINT walks down to the child. Combined with stdout being
// a regular file (not a pipe owned by the conductor goroutine), the worker
// keeps running after the conductor exits.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// detachedSupported reports whether detached spawning is implemented on this
// platform. POSIX builds always return true.
func detachedSupported() bool { return true }

// gracefulSignal returns the OS signal used by KillGraceful to request a
// polite shutdown before escalating to SIGKILL.
func gracefulSignal() os.Signal { return syscall.SIGTERM }
