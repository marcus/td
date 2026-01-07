package cmd

import (
	"fmt"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestMinorIntegration tests the --minor flag behavior in the issue workflow
func TestMinorIntegration(t *testing.T) {
	t.Run("minor flag exists", testMinorFlagExists)
	t.Run("create issue with minor flag", testCreateIssueWithMinor)
	t.Run("minor task allows self review", testMinorTaskAllowsSelfReview)
	t.Run("normal task blocks self review", testNormalTaskBlocksSelfReview)
	t.Run("minor task bypass restrictions", testMinorTaskBypass)
	t.Run("minor vs normal workflow", testMinorVsNormalWorkflow)
	t.Run("minor task persistence", testMinorTaskPersistence)
	t.Run("minor flag default false", testMinorFlagDefault)
	t.Run("multiple minor tasks", testMultipleMinorTasks)
}

func testMinorFlagExists(t *testing.T) {
	if createCmd.Flags().Lookup("minor") == nil {
		t.Fatal("Expected --minor flag to be defined")
	}
}

func testCreateIssueWithMinor(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title: "Minor Task",
		Minor: true,
	}

	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if !retrieved.Minor {
		t.Errorf("Expected Minor=true, got Minor=%v", retrieved.Minor)
	}
}

func testMinorTaskAllowsSelfReview(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	sessionID := "ses_creator"

	// Create a minor issue by creator
	issue := &models.Issue{
		Title:              "Minor: Fix typo",
		Status:             models.StatusInReview,
		Minor:              true,
		ImplementerSession: sessionID,
		CreatorSession:     sessionID,
	}

	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// List issues reviewable by creator - should include minor task even though creator == implementer
	reviewable, err := database.ListIssues(db.ListIssuesOptions{ReviewableBy: sessionID})
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}

	found := false
	for _, r := range reviewable {
		if r.ID == issue.ID {
			found = true
			break
		}
	}

	if !found {
		t.Error("Creator should be able to review minor task even when they implemented it")
	}
}

func testNormalTaskBlocksSelfReview(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	sessionID := "ses_implementer"

	// Create a normal (non-minor) issue where session is both implementer and creator
	issue := &models.Issue{
		Title:              "Normal Task",
		Status:             models.StatusInReview,
		Minor:              false, // Normal task
		ImplementerSession: sessionID,
		CreatorSession:     sessionID,
	}

	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// List issues reviewable by same session - should NOT include normal task
	reviewable, err := database.ListIssues(db.ListIssuesOptions{ReviewableBy: sessionID})
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}

	found := false
	for _, r := range reviewable {
		if r.ID == issue.ID {
			found = true
			break
		}
	}

	if found {
		t.Error("Implementer/creator should NOT be able to review normal task (not minor)")
	}
}

func testMinorTaskBypass(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	sessionA := "ses_aaaa"
	sessionB := "ses_bbbb"

	// Create a minor task implemented by A, created by A
	minorIssue := &models.Issue{
		Title:              "Minor fix",
		Status:             models.StatusInReview,
		Minor:              true,
		ImplementerSession: sessionA,
		CreatorSession:     sessionA,
	}

	if err := database.CreateIssue(minorIssue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if err := database.UpdateIssue(minorIssue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Session A can review (even though they created and implemented)
	reviewableA, err := database.ListIssues(db.ListIssuesOptions{ReviewableBy: sessionA})
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}

	foundA := false
	for _, r := range reviewableA {
		if r.ID == minorIssue.ID {
			foundA = true
			break
		}
	}

	if !foundA {
		t.Error("Minor task creator/implementer (A) should be able to review")
	}

	// Session B can also review
	reviewableB, err := database.ListIssues(db.ListIssuesOptions{ReviewableBy: sessionB})
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}

	foundB := false
	for _, r := range reviewableB {
		if r.ID == minorIssue.ID {
			foundB = true
			break
		}
	}

	if !foundB {
		t.Error("Any other session (B) should also be able to review minor task")
	}
}

func testMinorVsNormalWorkflow(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	sessionA := "ses_implementer"
	sessionB := "ses_reviewer"

	// Scenario 1: Minor task (self-reviewable)
	minorIssue := &models.Issue{
		Title:              "Minor: Typo fix",
		Status:             models.StatusInReview,
		Minor:              true,
		ImplementerSession: sessionA,
		CreatorSession:     sessionA,
	}

	if err := database.CreateIssue(minorIssue); err != nil {
		t.Fatalf("CreateIssue minor failed: %v", err)
	}

	if err := database.UpdateIssue(minorIssue); err != nil {
		t.Fatalf("UpdateIssue minor failed: %v", err)
	}

	// Scenario 2: Normal task (requires external review)
	normalIssue := &models.Issue{
		Title:              "Normal: Feature implementation",
		Status:             models.StatusInReview,
		Minor:              false,
		ImplementerSession: sessionA,
		CreatorSession:     sessionA,
	}

	if err := database.CreateIssue(normalIssue); err != nil {
		t.Fatalf("CreateIssue normal failed: %v", err)
	}

	if err := database.UpdateIssue(normalIssue); err != nil {
		t.Fatalf("UpdateIssue normal failed: %v", err)
	}

	// Check what Session A can review
	reviewableA, err := database.ListIssues(db.ListIssuesOptions{ReviewableBy: sessionA})
	if err != nil {
		t.Fatalf("ListIssues A failed: %v", err)
	}

	reviewableAMap := make(map[string]bool)
	for _, issue := range reviewableA {
		reviewableAMap[issue.ID] = true
	}

	// Session A SHOULD be able to review minor (self-reviewable)
	if !reviewableAMap[minorIssue.ID] {
		t.Error("Session A should be able to review minor task (self-reviewable)")
	}

	// Session A SHOULD NOT be able to review normal task
	if reviewableAMap[normalIssue.ID] {
		t.Error("Session A should NOT be able to review normal task (requires external review)")
	}

	// Check what Session B can review
	reviewableB, err := database.ListIssues(db.ListIssuesOptions{ReviewableBy: sessionB})
	if err != nil {
		t.Fatalf("ListIssues B failed: %v", err)
	}

	reviewableBMap := make(map[string]bool)
	for _, issue := range reviewableB {
		reviewableBMap[issue.ID] = true
	}

	// Session B SHOULD be able to review both
	if !reviewableBMap[minorIssue.ID] {
		t.Error("Session B should be able to review minor task")
	}

	if !reviewableBMap[normalIssue.ID] {
		t.Error("Session B should be able to review normal task")
	}
}

func testMinorTaskPersistence(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create a minor issue
	issue := &models.Issue{
		Title: "Minor task",
		Minor: true,
	}

	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Retrieve and verify Minor flag is preserved
	retrieved, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if !retrieved.Minor {
		t.Error("Minor flag should be preserved in database")
	}

	// Verify it stays true through updates
	retrieved.Status = models.StatusInProgress
	retrieved.ImplementerSession = "ses_test"

	if err := database.UpdateIssue(retrieved); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue after update failed: %v", err)
	}

	if !updated.Minor {
		t.Error("Minor flag should persist through updates")
	}
}

func testMinorFlagDefault(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issue without explicitly setting Minor
	issue := &models.Issue{
		Title: "Regular issue",
		// Minor not specified
	}

	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Minor {
		t.Error("Default Minor should be false, got true")
	}
}

func testMultipleMinorTasks(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	sessionA := "ses_creator"

	// Create 3 minor tasks all by same session
	for i := 1; i <= 3; i++ {
		issue := &models.Issue{
			Title:              fmt.Sprintf("Minor task %d", i),
			Status:             models.StatusInReview,
			Minor:              true,
			ImplementerSession: sessionA,
			CreatorSession:     sessionA,
		}

		if err := database.CreateIssue(issue); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}

		if err := database.UpdateIssue(issue); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}
	}

	// List issues reviewable by creator
	reviewable, err := database.ListIssues(db.ListIssuesOptions{ReviewableBy: sessionA})
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}

	if len(reviewable) != 3 {
		t.Errorf("Expected 3 reviewable minor tasks, got %d", len(reviewable))
	}
}
