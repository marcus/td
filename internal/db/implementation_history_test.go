package db

import (
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestHasImplementationHistoryStartedActions(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	hasHistory, err := db.HasImplementationHistory(issue.ID)
	if err != nil {
		t.Fatalf("HasImplementationHistory failed: %v", err)
	}
	if hasHistory {
		t.Fatal("new issue should not have implementation history")
	}

	if err := db.RecordSessionAction(issue.ID, "ses_test", models.ActionSessionCreated); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}
	hasHistory, err = db.HasImplementationHistory(issue.ID)
	if err != nil {
		t.Fatalf("HasImplementationHistory failed: %v", err)
	}
	if hasHistory {
		t.Fatal("created action should not count as implementation history")
	}

	if err := db.RecordSessionAction(issue.ID, "ses_test", models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}
	hasHistory, err = db.HasImplementationHistory(issue.ID)
	if err != nil {
		t.Fatalf("HasImplementationHistory failed: %v", err)
	}
	if !hasHistory {
		t.Fatal("started action should count as implementation history")
	}
}
