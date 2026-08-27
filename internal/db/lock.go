package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	lockFileName = "db.lock"
	// Matches the SQLite busy_timeout. Rollback-journal writes (journal
	// create + fsync per transaction) queue noticeably longer than WAL
	// writes did, and concurrent agent sessions routinely burst past 500ms.
	defaultTimeout = 5 * time.Second
	initialBackoff = 5 * time.Millisecond
	maxBackoff     = 50 * time.Millisecond
)

// writeLocker manages exclusive write access to the database using OS file locks.
// The lock is automatically released when the process exits (including crashes).
type writeLocker struct {
	lockPath string
	lockFile *os.File
}

// newWriteLocker creates a new write locker for the given base directory.
func newWriteLocker(baseDir string) *writeLocker {
	return &writeLocker{
		lockPath: filepath.Join(baseDir, ".todos", lockFileName),
	}
}

// WithMaintenanceLock runs fn while holding the same cross-process lock used
// by normal CLI writes. It is intended for database-file maintenance (such as
// snapshot replacement) that cannot safely overlap another td writer.
func WithMaintenanceLock(baseDir string, fn func() error) error {
	locker := newWriteLocker(ResolveBaseDir(baseDir))
	if err := locker.acquire(5 * time.Second); err != nil {
		return err
	}
	defer func() { _ = locker.release() }()
	return fn()
}

// acquire attempts to get an exclusive write lock with the given timeout.
// Returns an error with diagnostic info if the lock cannot be acquired.
func (l *writeLocker) acquire(timeout time.Duration) error {
	// Open or create lock file
	f, err := os.OpenFile(l.lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	l.lockFile = f

	deadline := time.Now().Add(timeout)
	backoff := initialBackoff

	for {
		// Try non-blocking exclusive lock (platform-specific)
		err := l.tryLock()
		if err == nil {
			// Got the lock - write holder info for debugging
			l.writeHolder()
			return nil
		}

		// Check timeout
		if time.Now().After(deadline) {
			holder := l.readHolder()
			_ = l.lockFile.Close()
			l.lockFile = nil
			return fmt.Errorf("write lock timeout after %v\n  holder: %s\n  try again or check if holder process is stuck", timeout, holder)
		}

		// Exponential backoff with cap
		time.Sleep(backoff)
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// release releases the write lock.
func (l *writeLocker) release() error {
	if l.lockFile == nil {
		return nil
	}

	// Clear holder info
	_ = l.lockFile.Truncate(0)

	// Release lock (platform-specific)
	l.unlock()

	_ = l.lockFile.Close()
	l.lockFile = nil

	return nil
}

// writeHolder writes current process info to the lock file for debugging.
func (l *writeLocker) writeHolder() {
	if l.lockFile == nil {
		return
	}
	_ = l.lockFile.Truncate(0)
	_, _ = l.lockFile.Seek(0, 0)
	_, _ = fmt.Fprintf(l.lockFile, "pid:%d\ntime:%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	_ = l.lockFile.Sync()
}

// readHolder reads the current holder info from the lock file.
func (l *writeLocker) readHolder() string {
	data, err := os.ReadFile(l.lockPath)
	if err != nil {
		return "unknown"
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return "unknown"
	}

	var pid, timestamp string
	for _, line := range lines {
		if strings.HasPrefix(line, "pid:") {
			pid = strings.TrimPrefix(line, "pid:")
		} else if strings.HasPrefix(line, "time:") {
			timestamp = strings.TrimPrefix(line, "time:")
		}
	}

	if pid == "" {
		return "unknown"
	}

	// Check if process is still alive
	pidInt, err := strconv.Atoi(pid)
	if err == nil && !isProcessAlive(pidInt) {
		return fmt.Sprintf("pid:%s since %s (STALE - process dead)", pid, timestamp)
	}

	return fmt.Sprintf("pid:%s since %s", pid, timestamp)
}

// tryLock and unlock are implemented in platform-specific files:
// - lock_unix.go for Unix systems (flock)
// - lock_windows.go for Windows (LockFileEx)
