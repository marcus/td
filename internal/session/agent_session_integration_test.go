package session

import (
	"testing"

	"github.com/marcus/td/internal/db"
)

// TestAgentScopedSessionIsolation verifies that different agents get different sessions
func TestAgentScopedSessionIsolation(t *testing.T) {
	database := setupTestDB(t)

	// Simulate Agent A (explicit override)
	t.Setenv("TD_SESSION_ID", "agent-a")
	sessA, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate for agent A: %v", err)
	}

	// Simulate Agent B (different explicit override)
	t.Setenv("TD_SESSION_ID", "agent-b")
	sessB, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate for agent B: %v", err)
	}

	// Key assertion: different agents should get different sessions
	if sessA.ID == sessB.ID {
		t.Errorf("Agents A and B should have different session IDs, both got %s", sessA.ID)
	}

	// Verify both sessions exist in DB
	allSessions, err := database.ListAllSessions()
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(allSessions) < 2 {
		t.Errorf("Expected at least 2 sessions in DB, got %d", len(allSessions))
	}
}

// TestSameAgentSameSession verifies stability within same agent
func TestSameAgentSameSession(t *testing.T) {
	database := setupTestDB(t)

	t.Setenv("TD_SESSION_ID", "stable-agent")

	sess1, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate (1): %v", err)
	}

	sess2, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate (2): %v", err)
	}

	sess3, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate (3): %v", err)
	}

	if sess1.ID != sess2.ID || sess2.ID != sess3.ID {
		t.Errorf("Same agent should get same session ID across calls: %s, %s, %s",
			sess1.ID, sess2.ID, sess3.ID)
	}
}

// TestAgentSessionPersistence verifies session survives DB reopen
func TestAgentSessionPersistence(t *testing.T) {
	baseDir := t.TempDir()

	t.Setenv("TD_SESSION_ID", "persistent-agent")

	// Create initial session
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	sess1, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate (initial): %v", err)
	}
	initialID := sess1.ID
	database.Close()

	// Reopen - should load from DB
	database2, err := db.Open(baseDir)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer database2.Close()

	sess2, err := GetOrCreate(database2)
	if err != nil {
		t.Fatalf("GetOrCreate (after restart): %v", err)
	}

	if sess2.ID != initialID {
		t.Errorf("Session should persist across restarts: got %s, want %s", sess2.ID, initialID)
	}
}

// TestAgentSessionDBStructure verifies session data is correctly stored in DB
func TestAgentSessionDBStructure(t *testing.T) {
	database := setupTestDB(t)

	t.Setenv("TD_SESSION_ID", "test-agent")

	sess, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	// Check that session has expected agent info (stored as fingerprint string)
	if sess.AgentType != "explicit_test-agent" {
		t.Errorf("AgentType = %q, want %q", sess.AgentType, "explicit_test-agent")
	}

	// Verify in DB directly
	row, err := database.GetSessionByID(sess.ID)
	if err != nil {
		t.Fatalf("GetSessionByID: %v", err)
	}
	if row == nil {
		t.Fatal("session not found in DB")
	}
	if row.AgentType != "explicit_test-agent" {
		t.Errorf("DB AgentType = %q, want %q", row.AgentType, "explicit_test-agent")
	}
	if row.Branch == "" {
		t.Error("DB Branch should be set")
	}
}

// TestForceNewSessionCreatesNewAgentSession verifies --new-session behavior
func TestForceNewSessionCreatesNewAgentSession(t *testing.T) {
	database := setupTestDB(t)

	t.Setenv("TD_SESSION_ID", "force-new-agent")

	sess1, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	sess2, err := ForceNewSession(database)
	if err != nil {
		t.Fatalf("ForceNewSession: %v", err)
	}

	if sess1.ID == sess2.ID {
		t.Errorf("ForceNewSession should create new ID, both got %s", sess1.ID)
	}

	if sess2.PreviousSessionID != sess1.ID {
		t.Errorf("PreviousSessionID = %q, want %q", sess2.PreviousSessionID, sess1.ID)
	}

	// Subsequent GetOrCreate should return the new session
	sess3, err := GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate after force: %v", err)
	}

	if sess3.ID != sess2.ID {
		t.Errorf("GetOrCreate after force should return new session: got %s, want %s",
			sess3.ID, sess2.ID)
	}
}
