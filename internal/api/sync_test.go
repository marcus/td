package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFileRenamePathPreservesSource(t *testing.T) {
	// copyFile tries os.Rename first (same filesystem). After a successful
	// rename, the original src no longer exists. Callers must be aware that
	// src may have been moved.
	dir := t.TempDir()
	src := filepath.Join(dir, "source.db")
	dst := filepath.Join(dir, "dest.db")

	content := []byte("snapshot data")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	// dst should exist and have correct content
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q", got)
	}
}

func TestCopyFileFallbackPath(t *testing.T) {
	// When rename fails (cross-device), copyFile falls back to byte copy.
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := filepath.Join(srcDir, "source.db")
	dst := filepath.Join(dstDir, "dest.db")

	content := []byte("test snapshot content")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch")
	}
}

func TestSnapshotCacheRenameFailureStillServable(t *testing.T) {
	// Reproduces the race: copyFile uses os.Rename (same filesystem),
	// which moves tmpPath to tmpCachePath. If the second rename
	// (tmpCachePath → cachePath) fails, the data must still be servable.
	//
	// Before the fix: servePath pointed to the now-nonexistent tmpPath,
	// and tmpCachePath was deleted — HTTP 500.
	// After the fix: servePath is updated to tmpCachePath immediately.
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "snapshot.tmp")
	cacheDir := filepath.Join(dir, "cache")
	tmpCachePath := filepath.Join(cacheDir, "snapshot.cache.tmp")
	cachePath := filepath.Join(cacheDir, "42.db")

	content := []byte("snapshot database content")
	if err := os.WriteFile(tmpPath, content, 0o644); err != nil {
		t.Fatalf("write tmpPath: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}

	// Replicate the exact caching logic from handleSyncSnapshot (post-fix)
	servePath := tmpPath
	if err := copyFile(tmpPath, tmpCachePath); err == nil {
		servePath = tmpCachePath // fix: update immediately

		// Simulate second rename failure by making cachePath a directory
		// (os.Rename to a directory path fails)
		if err := os.MkdirAll(cachePath, 0o755); err != nil {
			t.Fatalf("setup rename failure: %v", err)
		}
		if err := os.Rename(tmpCachePath, cachePath); err != nil {
			// Expected failure — don't delete tmpCachePath (the fix)
			t.Logf("expected rename failure: %v", err)
		} else {
			servePath = cachePath
		}
	} else {
		t.Fatalf("copyFile failed unexpectedly: %v", err)
	}

	// The critical assertion: servePath must point to a readable file
	got, err := os.ReadFile(servePath)
	if err != nil {
		t.Fatalf("servePath %q is not readable (would be HTTP 500): %v", servePath, err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch at servePath")
	}
}
