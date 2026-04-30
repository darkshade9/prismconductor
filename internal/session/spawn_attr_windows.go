//go:build windows

package session

import "syscall"

// detachedProcAttr is a stub on Windows. Surviving conductor exit on Windows
// requires CREATE_NEW_PROCESS_GROUP wiring, which is explicitly out of scope
// for issue #54 (Non-goals). Until that ships, conductor exit on Windows
// still terminates spawned workers.
func detachedProcAttr() *syscall.SysProcAttr { return nil }

// detachedSupported reports whether detached spawning is implemented on this
// platform.
func detachedSupported() bool { return false }
