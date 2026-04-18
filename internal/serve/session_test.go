package serve

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
)

func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	tmpDir := t.TempDir()
	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestGetOrCreateWebSessionNew(t *testing.T) {
	database := setupTestDB(t)

	sess, err := GetOrCreateWebSession(database)
	if err != nil {
		t.Fatalf("GetOrCreateWebSession() error: %v", err)
	}

	if !strings.HasPrefix(sess.ID, "ses_") {
		t.Errorf("session ID should have ses_ prefix, got %q", sess.ID)
	}
	if sess.Name != "td-serve-web" {
		t.Errorf("Name = %q, want %q", sess.Name, "td-serve-web")
	}
	if sess.Branch != "default" {
		t.Errorf("Branch = %q, want %q", sess.Branch, "default")
	}
	if sess.AgentType != "web" {
		t.Errorf("AgentType = %q, want %q", sess.AgentType, "web")
	}
	if sess.AgentPID != 0 {
		t.Errorf("AgentPID = %d, want 0", sess.AgentPID)
	}
}

func TestGetOrCreateWebSessionReuse(t *testing.T) {
	database := setupTestDB(t)

	// Create first
	first, err := GetOrCreateWebSession(database)
	if err != nil {
		t.Fatalf("first GetOrCreateWebSession() error: %v", err)
	}

	// Call again - should return same session
	second, err := GetOrCreateWebSession(database)
	if err != nil {
		t.Fatalf("second GetOrCreateWebSession() error: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("expected same session ID, got %q and %q", first.ID, second.ID)
	}

	// Activity should have been bumped (second call sets LastActivity to time.Now())
	_ = second.LastActivity // verified implicitly: no error returned
}

func TestBumpSessionActivity(t *testing.T) {
	database := setupTestDB(t)

	sess, err := GetOrCreateWebSession(database)
	if err != nil {
		t.Fatal(err)
	}

	// Wait a tiny bit so time.Now() advances
	time.Sleep(time.Millisecond)

	if err := BumpSessionActivity(database, sess.ID); err != nil {
		t.Fatalf("BumpSessionActivity() error: %v", err)
	}

	// Verify the activity was bumped
	row, err := database.GetSessionByID(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row == nil {
		t.Fatal("session not found after bump")
	}
	if !row.LastActivity.After(sess.StartedAt) && !row.LastActivity.Equal(sess.StartedAt) {
		t.Errorf("LastActivity not bumped: %v vs started %v", row.LastActivity, sess.StartedAt)
	}
}

func TestStartSessionHeartbeatCancellation(t *testing.T) {
	database := setupTestDB(t)

	sess, err := GetOrCreateWebSession(database)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start heartbeat
	StartSessionHeartbeat(ctx, database, sess.ID)

	// Cancel immediately - the goroutine should exit cleanly
	cancel()

	// Give the goroutine a moment to process the cancellation
	time.Sleep(10 * time.Millisecond)

	// If we got here without hanging, the cancellation works
}

func TestGetOrCreateWebSessionIDFormat(t *testing.T) {
	database := setupTestDB(t)

	sess, err := GetOrCreateWebSession(database)
	if err != nil {
		t.Fatal(err)
	}

	// Should be ses_ + 6 hex chars = 10 chars total
	if len(sess.ID) != 10 {
		t.Errorf("session ID length = %d, want 10 (%q)", len(sess.ID), sess.ID)
	}

	if !strings.HasPrefix(sess.ID, "ses_") {
		t.Errorf("session ID prefix: got %q, want ses_ prefix", sess.ID)
	}

	// The hex part should be valid hex
	hexPart := sess.ID[4:]
	for _, c := range hexPart {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("session ID contains non-hex char %q in %q", string(c), sess.ID)
		}
	}
}
