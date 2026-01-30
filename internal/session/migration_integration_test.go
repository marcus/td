package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/marcus/td/internal/db"
)

// TestEndToEndMigration verifies the full filesystem-to-DB migration flow:
// create legacy files, call GetOrCreate, verify DB state and cleanup.
func TestEndToEndMigration(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer database.Close()

	branch := getCurrentBranch()
	t.Setenv("TD_SESSION_ID", "migtest-agent")

	// Create branch-scoped session file
	branchDir := filepath.Join(baseDir, ".todos", "sessions", branch)
	if err := os.MkdirAll(branchDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	branchContent := fmt.Sprintf(`{"id":"ses_branch_e2e","branch":"%s","agent_type":"claude-code","started_at":"2025-06-01T10:00:00Z"}`, branch)
	if err := os.WriteFile(filepath.Join(branchDir, "claude-code_123.json"), []byte(branchContent), 0644); err != nil {
		t.Fatalf("write branch session: %v", err)
	}

	// Create legacy session file
	legacyContent := `{"id":"ses_legacy_e2e","started_at":"2025-05-01T08:00:00Z"}`
	if err := os.WriteFile(filepath.Join(baseDir, ".todos", "session"), []byte(legacyContent), 0644); err != nil {
		t.Fatalf("write legacy session: %v", err)
	}

	// Trigger migration
	sess1, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if sess1.ID == "" || !sess1.IsNew {
		t.Fatalf("expected new session, got ID=%q IsNew=%v", sess1.ID, sess1.IsNew)
	}

	// Verify both migrated sessions exist in DB
	row1, err := database.GetSessionByID("ses_branch_e2e")
	if err != nil {
		t.Fatalf("GetSessionByID(branch): %v", err)
	}
	if row1 == nil {
		t.Fatal("branch session should be migrated to DB")
	}

	row2, err := database.GetSessionByID("ses_legacy_e2e")
	if err != nil {
		t.Fatalf("GetSessionByID(legacy): %v", err)
	}
	if row2 == nil {
		t.Fatal("legacy session should be migrated to DB")
	}

	// Verify filesystem cleanup
	sessionsDir := filepath.Join(baseDir, ".todos", "sessions")
	if _, err := os.Stat(sessionsDir); !os.IsNotExist(err) {
		t.Fatal("sessions directory should be removed after migration")
	}
	legacyPath := filepath.Join(baseDir, ".todos", "session")
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatal("legacy session file should be removed after migration")
	}

	// Subsequent GetOrCreate should reuse (not create new)
	sess2, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate (second): %v", err)
	}
	if sess2.ID != sess1.ID {
		t.Fatalf("should reuse session: got %q, want %q", sess2.ID, sess1.ID)
	}
	if sess2.IsNew {
		t.Fatal("should not be IsNew on reuse")
	}

	// ForceNewSession should link to current session
	sess3, err := ForceNewSession(database)
	if err != nil {
		t.Fatalf("ForceNewSession: %v", err)
	}
	if sess3.ID == sess1.ID {
		t.Fatal("ForceNewSession should create new ID")
	}
	if sess3.PreviousSessionID != sess1.ID {
		t.Fatalf("PreviousSessionID = %q, want %q", sess3.PreviousSessionID, sess1.ID)
	}
}

// TestMigrationEdgeCases covers empty dirs, malformed files, and multiple branches.
func TestMigrationEdgeCases(t *testing.T) {
	t.Run("empty_sessions_dir", func(t *testing.T) {
		baseDir := t.TempDir()
		database, err := db.Initialize(baseDir)
		if err != nil {
			t.Fatalf("init db: %v", err)
		}
		defer database.Close()

		t.Setenv("TD_SESSION_ID", "edge-empty")

		// Create empty sessions directory tree
		if err := os.MkdirAll(filepath.Join(baseDir, ".todos", "sessions", "main"), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		sess, err := GetOrCreate(database)
		if err != nil {
			t.Fatalf("should succeed with empty dir: %v", err)
		}
		if sess.ID == "" {
			t.Fatal("should create valid session")
		}
	})

	t.Run("malformed_json_skipped", func(t *testing.T) {
		baseDir := t.TempDir()
		database, err := db.Initialize(baseDir)
		if err != nil {
			t.Fatalf("init db: %v", err)
		}
		defer database.Close()

		t.Setenv("TD_SESSION_ID", "edge-malformed")

		branchDir := filepath.Join(baseDir, ".todos", "sessions", "main")
		if err := os.MkdirAll(branchDir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		// Write malformed JSON
		if err := os.WriteFile(filepath.Join(branchDir, "bad.json"), []byte("{not json!!}"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		// Write valid JSON alongside
		if err := os.WriteFile(filepath.Join(branchDir, "good.json"),
			[]byte(`{"id":"ses_good","started_at":"2025-01-01T00:00:00Z"}`), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}

		sess, err := GetOrCreate(database)
		if err != nil {
			t.Fatalf("should succeed despite malformed file: %v", err)
		}
		if sess.ID == "" {
			t.Fatal("should create valid session")
		}

		// Good file should be migrated
		row, err := database.GetSessionByID("ses_good")
		if err != nil {
			t.Fatalf("GetSessionByID: %v", err)
		}
		if row == nil {
			t.Fatal("valid session should be migrated")
		}
	})

	t.Run("multiple_branches", func(t *testing.T) {
		baseDir := t.TempDir()
		database, err := db.Initialize(baseDir)
		if err != nil {
			t.Fatalf("init db: %v", err)
		}
		defer database.Close()

		t.Setenv("TD_SESSION_ID", "edge-multi")

		sessionsBase := filepath.Join(baseDir, ".todos", "sessions")
		for _, branch := range []string{"main", "feature-x", "bugfix-y"} {
			dir := filepath.Join(sessionsBase, branch)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			content := fmt.Sprintf(`{"id":"ses_%s","branch":"%s","started_at":"2025-01-01T00:00:00Z"}`, branch, branch)
			if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(content), 0644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}

		_, err = GetOrCreate(database)
		if err != nil {
			t.Fatalf("GetOrCreate: %v", err)
		}

		// All three branch sessions should be in DB
		for _, branch := range []string{"main", "feature-x", "bugfix-y"} {
			row, err := database.GetSessionByID("ses_" + branch)
			if err != nil {
				t.Fatalf("GetSessionByID(%s): %v", branch, err)
			}
			if row == nil {
				t.Fatalf("session for branch %s should be migrated", branch)
			}
		}
	})
}

// TestConcurrentMigration verifies two goroutines calling GetOrCreate simultaneously
// don't cause errors or data corruption.
func TestConcurrentMigration(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer database.Close()

	t.Setenv("TD_SESSION_ID", "concurrent-agent")

	// Create legacy file
	legacyPath := filepath.Join(baseDir, ".todos", "session")
	if err := os.WriteFile(legacyPath, []byte(`{"id":"ses_concurrent","started_at":"2025-01-01T00:00:00Z"}`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	sessions := make([]*Session, 2)

	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s, e := GetOrCreate(database)
			sessions[idx] = s
			errs[idx] = e
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Fatalf("goroutine %d error: %v", i, e)
		}
	}

	// Both should get the same session
	if sessions[0].ID != sessions[1].ID {
		t.Errorf("concurrent GetOrCreate should return same session: %q vs %q",
			sessions[0].ID, sessions[1].ID)
	}
}
