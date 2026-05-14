//go:build windows

package config

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// withConfigLock serializes access to config.json using LockFileEx.
func withConfigLock(baseDir string, fn func() error) error {
	lockPath := filepath.Join(baseDir, lockFile)

	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// LockFileEx with LOCKFILE_EXCLUSIVE_LOCK (blocking) locks the first byte
	// of the file, which is sufficient for whole-file mutual exclusion since
	// every contender uses the same offset/length.
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0, // reserved
		1, // lock 1 byte
		0, // high bits of length
		ol,
	); err != nil {
		return err
	}
	defer func() {
		ul := new(windows.Overlapped)
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ul)
	}()

	return fn()
}
