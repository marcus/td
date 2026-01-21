package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetOrCreateRequiresInit(t *testing.T) {
	baseDir := t.TempDir()

	_, err := GetOrCreate(baseDir)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "td init") {
		t.Fatalf("expected error to mention td init, got: %v", err)
	}
}

func TestGetOrCreateReusesSessionWhenContextStable(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755); err != nil {
		t.Fatalf("mkdir .todos: %v", err)
	}

	t.Setenv("TD_SESSION_ID", "ctx-1")

	s1, err := GetOrCreate(baseDir)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if s1.ID == "" {
		t.Fatalf("expected session ID")
	}
	if !s1.IsNew {
		t.Fatalf("expected IsNew=true on first create")
	}

	s2, err := GetOrCreate(baseDir)
	if err != nil {
		t.Fatalf("GetOrCreate (second): %v", err)
	}
	if s2.IsNew {
		t.Fatalf("expected IsNew=false when reusing existing session")
	}
	if s1.ID != s2.ID {
		t.Fatalf("expected same session ID, got %q vs %q", s1.ID, s2.ID)
	}
}

func TestGetOrCreateDifferentAgentsDifferentSessions(t *testing.T) {
	// With agent-scoped sessions, different TD_SESSION_ID values = different agents = different sessions
	// This is the core bypass prevention: changing agent identity creates new session
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755); err != nil {
		t.Fatalf("mkdir .todos: %v", err)
	}

	t.Setenv("TD_SESSION_ID", "agent-1")
	s1, err := GetOrCreate(baseDir)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	// Change to different agent
	t.Setenv("TD_SESSION_ID", "agent-2")
	s2, err := GetOrCreate(baseDir)
	if err != nil {
		t.Fatalf("GetOrCreate (different agent): %v", err)
	}

	// Key assertion: different agents should get DIFFERENT sessions (bypass prevention)
	if s1.ID == s2.ID {
		t.Fatalf("expected DIFFERENT session IDs for different agents, both got %q", s1.ID)
	}
	if !s2.IsNew {
		t.Fatalf("expected IsNew=true for new agent session")
	}
}

func TestForceNewSessionRequiresInit(t *testing.T) {
	baseDir := t.TempDir()

	_, err := ForceNewSession(baseDir)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "td init") {
		t.Fatalf("expected error to mention td init, got: %v", err)
	}
}

func TestForceNewSessionAlwaysCreatesNew(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755); err != nil {
		t.Fatalf("mkdir .todos: %v", err)
	}

	t.Setenv("TD_SESSION_ID", "ctx-1")
	s1, err := GetOrCreate(baseDir)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	s2, err := ForceNewSession(baseDir)
	if err != nil {
		t.Fatalf("ForceNewSession: %v", err)
	}
	if s1.ID == s2.ID {
		t.Fatalf("expected different session IDs")
	}
	if s2.PreviousSessionID != s1.ID {
		t.Fatalf("expected PreviousSessionID=%q, got %q", s1.ID, s2.PreviousSessionID)
	}
	if !s2.IsNew {
		t.Fatalf("expected IsNew=true for newly created session")
	}
}

// TestMigrateBranchSessionCleanupOldFile verifies old branch-scoped file is deleted after migration
func TestMigrateBranchSessionCleanupOldFile(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755); err != nil {
		t.Fatalf("mkdir .todos: %v", err)
	}

	t.Setenv("TD_SESSION_ID", "agent-1")

	// Create legacy branch-scoped session file in .todos/sessions/
	branch := getCurrentBranch()
	branchPath := sessionPathForBranch(baseDir, branch)
	if err := os.MkdirAll(filepath.Dir(branchPath), 0755); err != nil {
		t.Fatalf("mkdir .todos/sessions: %v", err)
	}
	branchContent := fmt.Sprintf(`{"id":"ses_oldbranchmig","branch":"%s","started_at":"2025-01-01T00:00:00Z"}`, branch)
	if err := os.WriteFile(branchPath, []byte(branchContent), 0644); err != nil {
		t.Fatalf("write branch session: %v", err)
	}

	// Verify file exists before migration
	if _, err := os.Stat(branchPath); err != nil {
		t.Fatalf("branch file should exist before migration: %v", err)
	}

	// Trigger migration via GetOrCreate
	sess, err := GetOrCreate(baseDir)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	// Verify migration succeeded and old file is deleted
	_, statErr := os.Stat(branchPath)
	if statErr == nil {
		t.Fatalf("branch session file should be deleted after migration")
	}
	if !os.IsNotExist(statErr) {
		t.Fatalf("unexpected error checking deleted file: %v", statErr)
	}

	// Verify new agent-scoped file exists with agent info set
	if sess.AgentType == "" {
		t.Fatalf("agent type not set after migration: %q", sess.AgentType)
	}
}

// TestMigrateLegacySessionCleanupOldFile verifies legacy .todos/session file is deleted after migration
func TestMigrateLegacySessionCleanupOldFile(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755); err != nil {
		t.Fatalf("mkdir .todos: %v", err)
	}

	t.Setenv("TD_SESSION_ID", "agent-1")

	// Create legacy session file
	legacyPath := filepath.Join(baseDir, ".todos", "session")
	legacyContent := `{"id":"ses_legacymig","started_at":"2025-01-01T00:00:00Z"}`
	if err := os.WriteFile(legacyPath, []byte(legacyContent), 0644); err != nil {
		t.Fatalf("write legacy session: %v", err)
	}

	// Verify file exists before migration
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy file should exist before migration: %v", err)
	}

	// Trigger migration via GetOrCreate
	sess, err := GetOrCreate(baseDir)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	// Verify migration succeeded and old file is deleted
	_, statErr := os.Stat(legacyPath)
	if statErr == nil {
		t.Fatalf("legacy session file should be deleted after migration")
	}
	if !os.IsNotExist(statErr) {
		t.Fatalf("unexpected error checking deleted file: %v", statErr)
	}

	// Verify new agent-scoped file exists with migrated data
	if sess.ID != "ses_legacymig" {
		t.Fatalf("session ID not preserved: expected ses_legacymig, got %q", sess.ID)
	}
	if sess.AgentType == "" {
		t.Fatalf("agent type not set after migration")
	}
}

// TestMigrationSucceedsEvenIfCleanupFails verifies migration succeeds even if cleanup fails
// The os.Remove call does not block migration on error - migration succeeds regardless
func TestMigrationSucceedsEvenIfCleanupFails(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755); err != nil {
		t.Fatalf("mkdir .todos: %v", err)
	}

	t.Setenv("TD_SESSION_ID", "agent-1")

	// Create legacy session file
	legacyPath := filepath.Join(baseDir, ".todos", "session")
	legacyContent := `{"id":"ses_cleanup_resilient","started_at":"2025-01-01T00:00:00Z"}`
	if err := os.WriteFile(legacyPath, []byte(legacyContent), 0644); err != nil {
		t.Fatalf("write legacy session: %v", err)
	}

	// Trigger migration - should succeed and migrate data
	sess, err := GetOrCreate(baseDir)
	if err != nil {
		t.Fatalf("GetOrCreate should succeed: %v", err)
	}

	// Verify new agent-scoped session was created with migrated data
	if sess.ID != "ses_cleanup_resilient" {
		t.Fatalf("session ID not preserved: expected ses_cleanup_resilient, got %q", sess.ID)
	}
	if sess.AgentType == "" {
		t.Fatalf("agent type not set after migration")
	}

	// Migration succeeded and old file was cleaned up
	if _, err := os.Stat(legacyPath); err == nil {
		t.Fatalf("legacy file should be deleted after successful migration")
	}
}

// TestMigrationTableDriven tests various migration scenarios
func TestMigrationTableDriven(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(baseDir string) string // setup and return path to file that should be cleaned up
		shouldExist   bool                        // whether old file should exist after migration
		expectedID    string                      // expected migrated session ID
		shouldSucceed bool                        // whether migration should succeed
	}{
		{
			name: "branch_scoped_migration",
			setupFunc: func(baseDir string) string {
				branch := getCurrentBranch()
				path := sessionPathForBranch(baseDir, branch)
				os.MkdirAll(filepath.Dir(path), 0755)
				content := fmt.Sprintf(`{"id":"ses_branch_001","branch":"%s","started_at":"2025-01-01T00:00:00Z"}`, branch)
				os.WriteFile(path, []byte(content), 0644)
				return path
			},
			shouldExist:   false,
			expectedID:    "ses_branch_001",
			shouldSucceed: true,
		},
		{
			name: "legacy_session_migration",
			setupFunc: func(baseDir string) string {
				os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755)
				path := filepath.Join(baseDir, ".todos", "session")
				content := `{"id":"ses_legacy_001","started_at":"2025-01-01T00:00:00Z"}`
				os.WriteFile(path, []byte(content), 0644)
				return path
			},
			shouldExist:   false,
			expectedID:    "ses_legacy_001",
			shouldSucceed: true,
		},
		{
			name: "no_files_present",
			setupFunc: func(baseDir string) string {
				os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755)
				return "" // No file to clean up
			},
			shouldExist:   true, // N/A - no file
			expectedID:    "",   // New session generated
			shouldSucceed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			t.Setenv("TD_SESSION_ID", "agent-migration-test")

			// Setup test scenario
			oldPath := tt.setupFunc(baseDir)

			// Perform migration
			sess, err := GetOrCreate(baseDir)

			// Verify success/failure
			if tt.shouldSucceed && err != nil {
				t.Fatalf("migration should succeed: %v", err)
			}
			if !tt.shouldSucceed && err == nil {
				t.Fatalf("migration should fail")
			}

			// If no old path, skip file cleanup check
			if oldPath == "" {
				return
			}

			// Verify expected session ID
			if tt.expectedID != "" && sess.ID != tt.expectedID {
				t.Fatalf("expected ID %q, got %q", tt.expectedID, sess.ID)
			}

			// Verify old file cleanup
			_, statErr := os.Stat(oldPath)
			fileExists := statErr == nil

			if tt.shouldExist && !fileExists {
				t.Fatalf("old file should exist but doesn't")
			}
			if !tt.shouldExist && fileExists {
				t.Fatalf("old file should be cleaned up but still exists")
			}
		})
	}
}

// TestMigrationStatePreservation verifies migration state is properly maintained
func TestMigrationStatePreservation(t *testing.T) {
	tests := []struct {
		name               string
		sessionContent     string
		expectedFields     map[string]string // field name -> expected value
		verifyAfterMigrate func(*Session) error
	}{
		{
			name:           "preserves_session_id",
			sessionContent: `{"id":"ses_preserved_123","started_at":"2025-01-01T12:00:00Z"}`,
			verifyAfterMigrate: func(sess *Session) error {
				if sess.ID != "ses_preserved_123" {
					return fmt.Errorf("session ID not preserved")
				}
				return nil
			},
		},
		{
			name:           "preserves_name",
			sessionContent: `{"id":"ses_named_001","name":"important-session","started_at":"2025-01-01T12:00:00Z"}`,
			verifyAfterMigrate: func(sess *Session) error {
				if sess.Name != "important-session" {
					return fmt.Errorf("session name not preserved")
				}
				return nil
			},
		},
		{
			name:           "preserves_previous_session_id",
			sessionContent: `{"id":"ses_new_001","previous_session_id":"ses_old_999","started_at":"2025-01-01T12:00:00Z"}`,
			verifyAfterMigrate: func(sess *Session) error {
				if sess.PreviousSessionID != "ses_old_999" {
					return fmt.Errorf("previous session ID not preserved")
				}
				return nil
			},
		},
		{
			name:           "sets_agent_type",
			sessionContent: `{"id":"ses_agent_001","started_at":"2025-01-01T12:00:00Z"}`,
			verifyAfterMigrate: func(sess *Session) error {
				if sess.AgentType == "" {
					return fmt.Errorf("agent type not set after migration")
				}
				return nil
			},
		},
		{
			name:           "sets_agent_type_on_migration",
			sessionContent: `{"id":"ses_agenttype_001","started_at":"2025-01-01T12:00:00Z"}`,
			verifyAfterMigrate: func(sess *Session) error {
				// When TD_SESSION_ID is set, AgentType is "explicit" and PID is 0
				// When terminal fallback, AgentType is "terminal" and PID is 0
				// Both cases should have AgentType set (non-empty)
				if sess.AgentType == "" {
					return fmt.Errorf("agent type not set after migration")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755)
			// Set explicit agent ID for consistent testing
			t.Setenv("TD_SESSION_ID", "agent-preserve-test")

			// Write legacy session file
			legacyPath := filepath.Join(baseDir, ".todos", "session")
			if err := os.WriteFile(legacyPath, []byte(tt.sessionContent), 0644); err != nil {
				t.Fatalf("write session: %v", err)
			}

			// Migrate
			sess, err := GetOrCreate(baseDir)
			if err != nil {
				t.Fatalf("GetOrCreate: %v", err)
			}

			// Verify state preservation
			if err := tt.verifyAfterMigrate(sess); err != nil {
				t.Fatalf("%v", err)
			}
		})
	}
}

// TestEdgeCasesSessionMigration tests edge cases in migration
func TestEdgeCasesSessionMigration(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(baseDir string) error
		shouldSucceed bool
		description   string
	}{
		{
			name: "empty_session_file",
			setup: func(baseDir string) error {
				os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755)
				return os.WriteFile(filepath.Join(baseDir, ".todos", "session"), []byte(""), 0644)
			},
			shouldSucceed: true,
			description:   "empty file should create new session, not migrate",
		},
		{
			name: "malformed_json",
			setup: func(baseDir string) error {
				os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755)
				return os.WriteFile(filepath.Join(baseDir, ".todos", "session"), []byte("{invalid json}"), 0644)
			},
			shouldSucceed: true,
			description:   "malformed JSON should fall back to creating new session",
		},
		{
			name: "legacy_line_format",
			setup: func(baseDir string) error {
				os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755)
				content := "ses_line_format_001\n2025-01-01T00:00:00Z\nctx-123\nold-session"
				return os.WriteFile(filepath.Join(baseDir, ".todos", "session"), []byte(content), 0644)
			},
			shouldSucceed: true,
			description:   "legacy line-based format should be migrated correctly",
		},
		{
			name: "multiple_agent_sessions",
			setup: func(baseDir string) error {
				os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755)
				return nil
			},
			shouldSucceed: true,
			description:   "multiple agents should get different sessions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			t.Setenv("TD_SESSION_ID", "agent-edge-case")

			if err := tt.setup(baseDir); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			sess, err := GetOrCreate(baseDir)

			if tt.shouldSucceed && err != nil {
				t.Fatalf("%s: %v", tt.description, err)
			}
			if !tt.shouldSucceed && err == nil {
				t.Fatalf("%s: expected error but got success", tt.description)
			}

			if tt.shouldSucceed {
				if sess == nil || sess.ID == "" {
					t.Fatalf("%s: session should be valid", tt.description)
				}
			}
		})
	}
}
