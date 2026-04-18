package monitor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
)

func TestSharedDB_SingleConnection(t *testing.T) {
	// Create a temp directory with a test database
	tmpDir, err := os.MkdirTemp("", "td-dbpool-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize the database
	todosDir := filepath.Join(tmpDir, ".todos")
	if err := os.MkdirAll(todosDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create initial database
	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()

	// Clear the pool before testing
	clearDBPool()

	// Get shared DB twice
	db1, err := getSharedDB(tmpDir)
	if err != nil {
		t.Fatalf("first getSharedDB failed: %v", err)
	}

	db2, err := getSharedDB(tmpDir)
	if err != nil {
		t.Fatalf("second getSharedDB failed: %v", err)
	}

	// Both should return the same pointer
	if db1 != db2 {
		t.Error("getSharedDB returned different pointers for same path")
	}

	// Check reference count
	resolvedDir := db.ResolveBaseDir(tmpDir)
	dbPool.mu.RLock()
	entry := dbPool.conns[resolvedDir]
	refs := entry.refs
	dbPool.mu.RUnlock()

	if refs != 2 {
		t.Errorf("expected refs=2, got refs=%d", refs)
	}

	// Release once
	if err := releaseSharedDB(tmpDir); err != nil {
		t.Fatalf("first releaseSharedDB failed: %v", err)
	}

	dbPool.mu.RLock()
	entry = dbPool.conns[resolvedDir]
	refs = entry.refs
	dbPool.mu.RUnlock()

	if refs != 1 {
		t.Errorf("expected refs=1 after first release, got refs=%d", refs)
	}

	// Release again - should close and remove
	if err := releaseSharedDB(tmpDir); err != nil {
		t.Fatalf("second releaseSharedDB failed: %v", err)
	}

	dbPool.mu.RLock()
	_, exists := dbPool.conns[resolvedDir]
	dbPool.mu.RUnlock()

	if exists {
		t.Error("expected connection to be removed from pool after all releases")
	}
}

func TestSharedDB_DifferentPaths(t *testing.T) {
	// Create two temp directories with test databases
	tmpDir1, err := os.MkdirTemp("", "td-dbpool-test1")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir1)

	tmpDir2, err := os.MkdirTemp("", "td-dbpool-test2")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir2)

	// Initialize both databases
	for _, dir := range []string{tmpDir1, tmpDir2} {
		todosDir := filepath.Join(dir, ".todos")
		if err := os.MkdirAll(todosDir, 0755); err != nil {
			t.Fatal(err)
		}
		database, err := db.Initialize(dir)
		if err != nil {
			t.Fatal(err)
		}
		database.Close()
	}

	// Clear the pool before testing
	clearDBPool()

	// Get shared DBs for different paths
	db1, err := getSharedDB(tmpDir1)
	if err != nil {
		t.Fatalf("getSharedDB for dir1 failed: %v", err)
	}

	db2, err := getSharedDB(tmpDir2)
	if err != nil {
		t.Fatalf("getSharedDB for dir2 failed: %v", err)
	}

	// Should be different pointers
	if db1 == db2 {
		t.Error("getSharedDB returned same pointer for different paths")
	}

	// Clean up
	releaseSharedDB(tmpDir1)
	releaseSharedDB(tmpDir2)
}

// TestDebugLog_EnvGated verifies that TD_MONITOR_DBPOOL_DEBUG=1 emits
// diagnostic lines on get/release, and that unsetting the var silences them.
func TestDebugLog_EnvGated(t *testing.T) {
	// Prep a temp DB so get/release succeed.
	tmpDir, err := os.MkdirTemp("", "td-dbpool-debug-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	todosDir := filepath.Join(tmpDir, ".todos")
	if err := os.MkdirAll(todosDir, 0755); err != nil {
		t.Fatal(err)
	}
	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()

	// Redirect debug output to a buffer for assertions.
	var buf bytes.Buffer
	origWriter := debugWriter
	debugWriter = &buf
	t.Cleanup(func() { debugWriter = origWriter })

	resolvedDir := db.ResolveBaseDir(tmpDir)

	// --- Case 1: env enabled, expect log lines ---
	t.Setenv(dbpoolDebugEnv, "1")
	clearDBPool()
	buf.Reset()

	if _, err := getSharedDB(tmpDir); err != nil {
		t.Fatalf("getSharedDB failed: %v", err)
	}
	if err := releaseSharedDB(tmpDir); err != nil {
		t.Fatalf("releaseSharedDB failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "op=get") {
		t.Errorf("expected op=get in output, got: %q", out)
	}
	if !strings.Contains(out, "op=release") {
		t.Errorf("expected op=release in output, got: %q", out)
	}
	if !strings.Contains(out, "path="+resolvedDir) {
		t.Errorf("expected path=%s in output, got: %q", resolvedDir, out)
	}
	if !strings.Contains(out, "refs=1") {
		t.Errorf("expected refs=1 in output, got: %q", out)
	}
	if !strings.Contains(out, "caller=") {
		t.Errorf("expected caller= in output, got: %q", out)
	}

	// --- Case 2: env unset, expect silence ---
	t.Setenv(dbpoolDebugEnv, "")
	clearDBPool()
	buf.Reset()

	if _, err := getSharedDB(tmpDir); err != nil {
		t.Fatalf("getSharedDB (silent) failed: %v", err)
	}
	if err := releaseSharedDB(tmpDir); err != nil {
		t.Fatalf("releaseSharedDB (silent) failed: %v", err)
	}

	if got := buf.String(); got != "" {
		t.Errorf("expected no output when env unset, got: %q", got)
	}
}
