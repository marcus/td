package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/td/internal/db"
)

func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestGetOrCreateReusesSessionWhenContextStable(t *testing.T) {
	database := setupTestDB(t)

	t.Setenv("TD_SESSION_ID", "ctx-1")

	s1, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if s1.ID == "" {
		t.Fatalf("expected session ID")
	}
	if !s1.IsNew {
		t.Fatalf("expected IsNew=true on first create")
	}

	s2, err := GetOrCreate(database)
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
	database := setupTestDB(t)

	t.Setenv("TD_SESSION_ID", "agent-1")
	s1, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	t.Setenv("TD_SESSION_ID", "agent-2")
	s2, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate (different agent): %v", err)
	}

	if s1.ID == s2.ID {
		t.Fatalf("expected DIFFERENT session IDs for different agents, both got %q", s1.ID)
	}
	if !s2.IsNew {
		t.Fatalf("expected IsNew=true for new agent session")
	}
}

func TestForceNewSessionAlwaysCreatesNew(t *testing.T) {
	database := setupTestDB(t)

	t.Setenv("TD_SESSION_ID", "ctx-1")
	s1, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	s2, err := ForceNewSession(database)
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

func TestMigrateLegacySessionCleanupOldFile(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer database.Close()

	t.Setenv("TD_SESSION_ID", "agent-1")

	// Create legacy session file
	legacyPath := filepath.Join(baseDir, ".todos", "session")
	legacyContent := `{"id":"ses_legacymig","started_at":"2025-01-01T00:00:00Z"}`
	if err := os.WriteFile(legacyPath, []byte(legacyContent), 0644); err != nil {
		t.Fatalf("write legacy session: %v", err)
	}

	// Trigger migration via GetOrCreate
	_, err = GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	// Verify old file is deleted
	if _, statErr := os.Stat(legacyPath); !os.IsNotExist(statErr) {
		t.Fatalf("legacy session file should be deleted after migration")
	}

	// Verify session data was migrated to DB
	row, err := database.GetSessionByID("ses_legacymig")
	if err != nil {
		t.Fatalf("GetSessionByID: %v", err)
	}
	if row == nil {
		t.Fatal("migrated session should exist in DB")
	}
}

func TestMigrateBranchSessionCleanupOldFile(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer database.Close()

	t.Setenv("TD_SESSION_ID", "agent-1")

	// Create legacy branch-scoped session file
	branch := getCurrentBranch()
	sessionsPath := filepath.Join(baseDir, ".todos", "sessions")
	branchDir := filepath.Join(sessionsPath, branch)
	if err := os.MkdirAll(branchDir, 0755); err != nil {
		t.Fatalf("mkdir branch dir: %v", err)
	}
	agentFile := filepath.Join(branchDir, "explicit_agent-1.json")
	content := fmt.Sprintf(`{"id":"ses_branchmig","branch":"%s","agent_type":"explicit","started_at":"2025-01-01T00:00:00Z"}`, branch)
	if err := os.WriteFile(agentFile, []byte(content), 0644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	// Trigger migration via GetOrCreate
	_, err = GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	// Verify filesystem cleaned up
	if _, statErr := os.Stat(sessionsPath); !os.IsNotExist(statErr) {
		t.Fatalf("sessions directory should be removed after migration")
	}

	// Verify migrated session exists in DB
	row, err := database.GetSessionByID("ses_branchmig")
	if err != nil {
		t.Fatalf("GetSessionByID: %v", err)
	}
	if row == nil {
		t.Fatal("migrated session should exist in DB")
	}
}

func TestMigrationStatePreservation(t *testing.T) {
	tests := []struct {
		name           string
		sessionContent string
		verifyID       string
		verifyFunc     func(*db.SessionRow) error
	}{
		{
			name:           "preserves_session_id",
			sessionContent: `{"id":"ses_preserved_123","started_at":"2025-01-01T12:00:00Z"}`,
			verifyID:       "ses_preserved_123",
			verifyFunc: func(row *db.SessionRow) error {
				if row == nil {
					return fmt.Errorf("session not found in DB")
				}
				return nil
			},
		},
		{
			name:           "preserves_name",
			sessionContent: `{"id":"ses_named_001","name":"important-session","started_at":"2025-01-01T12:00:00Z"}`,
			verifyID:       "ses_named_001",
			verifyFunc: func(row *db.SessionRow) error {
				if row == nil {
					return fmt.Errorf("session not found in DB")
				}
				if row.Name != "important-session" {
					return fmt.Errorf("name = %q, want %q", row.Name, "important-session")
				}
				return nil
			},
		},
		{
			name:           "preserves_previous_session_id",
			sessionContent: `{"id":"ses_new_001","previous_session_id":"ses_old_999","started_at":"2025-01-01T12:00:00Z"}`,
			verifyID:       "ses_new_001",
			verifyFunc: func(row *db.SessionRow) error {
				if row == nil {
					return fmt.Errorf("session not found in DB")
				}
				if row.PreviousSessionID != "ses_old_999" {
					return fmt.Errorf("previous_session_id = %q, want %q", row.PreviousSessionID, "ses_old_999")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			database, err := db.Initialize(baseDir)
			if err != nil {
				t.Fatalf("init db: %v", err)
			}
			defer database.Close()

			t.Setenv("TD_SESSION_ID", "agent-preserve-test")

			// Write legacy session file
			legacyPath := filepath.Join(baseDir, ".todos", "session")
			if err := os.WriteFile(legacyPath, []byte(tt.sessionContent), 0644); err != nil {
				t.Fatalf("write session: %v", err)
			}

			// Trigger migration via GetOrCreate
			_, err = GetOrCreate(database)
			if err != nil {
				t.Fatalf("GetOrCreate: %v", err)
			}

			// Verify in DB directly
			row, err := database.GetSessionByID(tt.verifyID)
			if err != nil {
				t.Fatalf("GetSessionByID: %v", err)
			}
			if err := tt.verifyFunc(row); err != nil {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestSessionPersistsThroughDBReopen(t *testing.T) {
	baseDir := t.TempDir()

	t.Setenv("TD_SESSION_ID", "persist-test")

	// Create session
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	sess1, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	database.Close()

	// Reopen and verify
	database2, err := db.Open(baseDir)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer database2.Close()

	sess2, err := GetOrCreate(database2)
	if err != nil {
		t.Fatalf("GetOrCreate after reopen: %v", err)
	}

	if sess1.ID != sess2.ID {
		t.Fatalf("session should persist: got %q, want %q", sess2.ID, sess1.ID)
	}
}

func TestSessionFingerprinting(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		wantAgent AgentType
	}{
		{
			name:      "explicit_session_id",
			sessionID: "my-test-session",
			wantAgent: "explicit",
		},
		{
			name:      "explicit_session_special_chars",
			sessionID: "user@example.com-session",
			wantAgent: "explicit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TD_SESSION_ID", tc.sessionID)

			fp := GetAgentFingerprint()
			if fp.Type != tc.wantAgent {
				t.Errorf("agent type = %q, want %q", fp.Type, tc.wantAgent)
			}

			fpString := fp.String()
			if fpString == "" {
				t.Error("fingerprint string should not be empty")
			}
		})
	}
}

func TestEdgeCasesSessionMigration(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(baseDir string) error
		shouldSucceed bool
	}{
		{
			name: "empty_session_file",
			setup: func(baseDir string) error {
				return os.WriteFile(filepath.Join(baseDir, ".todos", "session"), []byte(""), 0644)
			},
			shouldSucceed: true,
		},
		{
			name: "malformed_json",
			setup: func(baseDir string) error {
				return os.WriteFile(filepath.Join(baseDir, ".todos", "session"), []byte("{invalid json}"), 0644)
			},
			shouldSucceed: true,
		},
		{
			name: "legacy_line_format",
			setup: func(baseDir string) error {
				content := "ses_line_format_001\n2025-01-01T00:00:00Z\nctx-123\nold-session"
				return os.WriteFile(filepath.Join(baseDir, ".todos", "session"), []byte(content), 0644)
			},
			shouldSucceed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			database, err := db.Initialize(baseDir)
			if err != nil {
				t.Fatalf("init db: %v", err)
			}
			defer database.Close()

			t.Setenv("TD_SESSION_ID", "agent-edge-case")

			if err := tt.setup(baseDir); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			sess, err := GetOrCreate(database)
			if tt.shouldSucceed && err != nil {
				t.Fatalf("should succeed: %v", err)
			}
			if tt.shouldSucceed && (sess == nil || sess.ID == "") {
				t.Fatalf("session should be valid")
			}
		})
	}
}
