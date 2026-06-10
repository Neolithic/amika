//go:build linux || darwin

package eventlog

import "syscall"

// flockExclusive takes a blocking exclusive advisory lock on fd using flock(2).
// flock locks are tied to the open file description, so two separate opens of
// the same lock file contend even within one process — and the kernel releases
// the lock automatically when the descriptor is closed or the process exits.
func flockExclusive(fd uintptr) error {
	return syscall.Flock(int(fd), syscall.LOCK_EX)
}
