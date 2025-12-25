//go:build windows

package db

import (
	"golang.org/x/sys/windows"
)

// tryLock attempts to acquire an exclusive lock without blocking.
// Returns nil on success, error if lock is held by another process.
func (l *writeLocker) tryLock() error {
	// LockFileEx with LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY
	// locks the entire file (offset 0, length 1)
	ol := new(windows.Overlapped)
	return windows.LockFileEx(
		windows.Handle(l.lockFile.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, // reserved
		1, // lock 1 byte
		0, // high bits of length
		ol,
	)
}

// unlock releases the exclusive lock.
func (l *writeLocker) unlock() {
	if l.lockFile != nil {
		ol := new(windows.Overlapped)
		windows.UnlockFileEx(
			windows.Handle(l.lockFile.Fd()),
			0, // reserved
			1, // unlock 1 byte
			0, // high bits of length
			ol,
		)
	}
}

// isProcessAlive checks if a process with the given PID is still running.
func isProcessAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	var exitCode uint32
	err = windows.GetExitCodeProcess(handle, &exitCode)
	if err != nil {
		return false
	}

	// STILL_ACTIVE (259) means process is running
	return exitCode == 259
}
