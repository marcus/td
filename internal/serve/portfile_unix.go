//go:build unix

package serve

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// acquireFileLockTimeout tries to acquire an exclusive flock with exponential
// backoff up to the given timeout.
func acquireFileLockTimeout(f *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	backoff := 5 * time.Millisecond
	maxBackoff := 50 * time.Millisecond

	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %v waiting for port file lock", timeout)
		}
		time.Sleep(backoff)
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// releaseFileLock releases the exclusive flock.
func releaseFileLock(f *os.File) {
	if f != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}
}

// isProcessAlive checks if a process with the given PID is still running.
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; send signal 0 to check liveness
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
