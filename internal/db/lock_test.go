//go:build unix

package db

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWriteLocker_AcquireRelease(t *testing.T) {
	dir := t.TempDir()
	todosDir := filepath.Join(dir, ".todos")
	if err := os.MkdirAll(todosDir, 0755); err != nil {
		t.Fatalf("create .todos dir: %v", err)
	}

	locker := newWriteLocker(dir)

	// Should acquire lock successfully
	if err := locker.acquire(500 * time.Millisecond); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Lock file should exist with holder info
	lockPath := filepath.Join(todosDir, lockFileName)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	if len(data) == 0 {
		t.Error("lock file should contain holder info")
	}

	// Release should succeed
	if err := locker.release(); err != nil {
		t.Fatalf("release failed: %v", err)
	}
}

func TestWriteLocker_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	todosDir := filepath.Join(dir, ".todos")
	if err := os.MkdirAll(todosDir, 0755); err != nil {
		t.Fatalf("create .todos dir: %v", err)
	}

	const numGoroutines = 5
	const numIterations = 10

	var counter int64
	var wg sync.WaitGroup

	// Each goroutine tries to increment counter while holding lock
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				locker := newWriteLocker(dir)
				if err := locker.acquire(5 * time.Second); err != nil {
					t.Errorf("acquire failed: %v", err)
					return
				}

				// Critical section - read, increment, write
				val := atomic.LoadInt64(&counter)
				time.Sleep(1 * time.Millisecond) // Simulate work
				atomic.StoreInt64(&counter, val+1)

				if err := locker.release(); err != nil {
					t.Errorf("release failed: %v", err)
				}
			}
		}()
	}

	wg.Wait()

	expected := int64(numGoroutines * numIterations)
	if counter != expected {
		t.Errorf("counter = %d, want %d (race condition detected)", counter, expected)
	}
}

func TestWriteLocker_Timeout(t *testing.T) {
	dir := t.TempDir()
	todosDir := filepath.Join(dir, ".todos")
	if err := os.MkdirAll(todosDir, 0755); err != nil {
		t.Fatalf("create .todos dir: %v", err)
	}

	// First locker acquires
	locker1 := newWriteLocker(dir)
	if err := locker1.acquire(500 * time.Millisecond); err != nil {
		t.Fatalf("locker1 acquire failed: %v", err)
	}
	defer locker1.release()

	// Second locker should timeout
	locker2 := newWriteLocker(dir)
	start := time.Now()
	err := locker2.acquire(100 * time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		locker2.release()
		t.Fatal("expected timeout error, got nil")
	}

	// Should have waited approximately the timeout duration
	if elapsed < 80*time.Millisecond || elapsed > 200*time.Millisecond {
		t.Errorf("timeout duration = %v, want ~100ms", elapsed)
	}

	// Error message should be diagnostic
	errStr := err.Error()
	if !contains(errStr, "timeout") {
		t.Errorf("error should mention timeout: %v", err)
	}
	if !contains(errStr, "pid:") {
		t.Errorf("error should contain holder pid: %v", err)
	}
}

func TestWriteLocker_ReleaseUnlocksForOthers(t *testing.T) {
	dir := t.TempDir()
	todosDir := filepath.Join(dir, ".todos")
	if err := os.MkdirAll(todosDir, 0755); err != nil {
		t.Fatalf("create .todos dir: %v", err)
	}

	locker1 := newWriteLocker(dir)
	if err := locker1.acquire(500 * time.Millisecond); err != nil {
		t.Fatalf("locker1 acquire failed: %v", err)
	}

	// Release first lock
	locker1.release()

	// Second locker should now acquire immediately
	locker2 := newWriteLocker(dir)
	start := time.Now()
	if err := locker2.acquire(500 * time.Millisecond); err != nil {
		t.Fatalf("locker2 acquire failed after release: %v", err)
	}
	elapsed := time.Since(start)
	locker2.release()

	// Should acquire very quickly (not waiting for timeout)
	if elapsed > 50*time.Millisecond {
		t.Errorf("acquire after release took %v, should be near-instant", elapsed)
	}
}

func TestWriteLocker_HolderInfo(t *testing.T) {
	dir := t.TempDir()
	todosDir := filepath.Join(dir, ".todos")
	if err := os.MkdirAll(todosDir, 0755); err != nil {
		t.Fatalf("create .todos dir: %v", err)
	}

	locker := newWriteLocker(dir)
	if err := locker.acquire(500 * time.Millisecond); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	holder := locker.readHolder()
	if !contains(holder, "pid:") {
		t.Errorf("holder should contain pid: %s", holder)
	}
	if !contains(holder, "since") {
		t.Errorf("holder should contain timestamp: %s", holder)
	}

	locker.release()
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
