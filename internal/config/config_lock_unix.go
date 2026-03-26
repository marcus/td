//go:build unix

package config

import (
	"os"
	"path/filepath"
	"syscall"
)

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

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}
