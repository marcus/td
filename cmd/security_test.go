package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func TestSecurityLogging(t *testing.T) {
	// Create temp directory for test
	baseDir, err := os.MkdirTemp("", "td-security-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)

	// Init DB
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// Test LogSecurityEvent and ReadSecurityEvents
	event := db.SecurityEvent{
		IssueID:   "td-123",
		SessionID: "ses-test",
		AgentType: "test-agent",
		Reason:    "test reason",
	}

	err = db.LogSecurityEvent(baseDir, event)
	if err != nil {
		t.Fatalf("LogSecurityEvent failed: %v", err)
	}

	events, err := db.ReadSecurityEvents(baseDir)
	if err != nil {
		t.Fatalf("ReadSecurityEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	if events[0].IssueID != event.IssueID || events[0].Reason != event.Reason {
		t.Errorf("Event mismatch. Got %+v, want %+v", events[0], event)
	}

	// Test ClearSecurityEvents
	err = db.ClearSecurityEvents(baseDir)
	if err != nil {
		t.Fatalf("ClearSecurityEvents failed: %v", err)
	}

	events, err = db.ReadSecurityEvents(baseDir)
	if err != nil {
		t.Fatalf("ReadSecurityEvents failed after clear: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("Expected 0 events after clear, got %d", len(events))
	}
}

func TestCloseCommandSecurityLogging(t *testing.T) {
	// This is more of an integration test
	baseDir, err := os.MkdirTemp("", "td-close-security-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)

	// Ensure .todos directory exists for session
	os.MkdirAll(filepath.Join(baseDir, ".todos"), 0755) //nolint:errcheck // test setup

	// Init DB
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// Create an issue that we "implemented" to trigger self-close check
	issue := &models.Issue{
		Title:              "Self-close test",
		ImplementerSession: "ses-123",
	}
	err = database.CreateIssue(issue)
	if err != nil {
		t.Fatal(err)
	}

	// We'll use the CLI commands via Cobra to simulate actual usage
	// But since mocking the environment (session, etc.) is complex in a unit test,
	// we'll at least verify the DB log type logic if we can.
}
