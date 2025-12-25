package cmd

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func TestStatusCommand(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Create test issues
	issue1 := &models.Issue{
		Title:    "Test issue 1",
		Type:     models.TypeTask,
		Priority: models.PriorityP1,
		Status:   models.StatusOpen,
	}
	err = database.CreateIssue(issue1)
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	issue2 := &models.Issue{
		Title:              "Test issue 2",
		Type:               models.TypeTask,
		Priority:           models.PriorityP2,
		Status:             models.StatusInReview,
		ImplementerSession: "ses_other",
	}
	err = database.CreateIssue(issue2)
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	issue3 := &models.Issue{
		Title:    "Test issue 3",
		Type:     models.TypeTask,
		Priority: models.PriorityP1,
		Status:   models.StatusBlocked,
	}
	err = database.CreateIssue(issue3)
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add dependency
	err = database.AddDependency(issue3.ID, issue2.ID, "depends_on")
	if err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	// Test outputStatusDashboard doesn't crash
	err = outputStatusDashboard(database, baseDir, "ses_test")
	if err != nil {
		t.Errorf("outputStatusDashboard failed: %v", err)
	}

	// Test outputStatusJSON doesn't crash
	err = outputStatusJSON(database, baseDir, "ses_test")
	if err != nil {
		t.Errorf("outputStatusJSON failed: %v", err)
	}
}

func TestStatusWithEmptyDatabase(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Test with empty database
	err = outputStatusDashboard(database, baseDir, "ses_test")
	if err != nil {
		t.Errorf("outputStatusDashboard failed with empty db: %v", err)
	}

	err = outputStatusJSON(database, baseDir, "ses_test")
	if err != nil {
		t.Errorf("outputStatusJSON failed with empty db: %v", err)
	}
}
