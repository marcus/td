//go:build unix

package db

import (
	"os"
	"syscall"
)

// tryLock attempts to acquire an exclusive lock without blocking.
// Returns nil on success, error if lock is held by another process.
func (l *writeLocker) tryLock() error {
	return syscall.Flock(int(l.lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// unlock releases the exclusive lock.
func (l *writeLocker) unlock() {
	if l.lockFile != nil {
		syscall.Flock(int(l.lockFile.Fd()), syscall.LOCK_UN)
	}
}

// isProcessAlive checks if a process with the given PID is still running.
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; send signal 0 to check if process exists
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
