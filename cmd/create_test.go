package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestIsValidType tests type validation
func TestIsValidType(t *testing.T) {
	validTypes := []models.Type{
		models.TypeBug,
		models.TypeFeature,
		models.TypeTask,
		models.TypeEpic,
		models.TypeChore,
	}

	for _, typ := range validTypes {
		if !models.IsValidType(typ) {
			t.Errorf("Expected %q to be valid type", typ)
		}
	}

	invalidTypes := []models.Type{"invalid", "unknown", "story", ""}
	for _, typ := range invalidTypes {
		if models.IsValidType(typ) {
			t.Errorf("Expected %q to be invalid type", typ)
		}
	}
}

// TestIsValidPriority tests priority validation
func TestIsValidPriority(t *testing.T) {
	validPriorities := []models.Priority{
		models.PriorityP0,
		models.PriorityP1,
		models.PriorityP2,
		models.PriorityP3,
		models.PriorityP4,
	}

	for _, p := range validPriorities {
		if !models.IsValidPriority(p) {
			t.Errorf("Expected %q to be valid priority", p)
		}
	}

	invalidPriorities := []models.Priority{"P5", "high", "low", "critical", ""}
	for _, p := range invalidPriorities {
		if models.IsValidPriority(p) {
			t.Errorf("Expected %q to be invalid priority", p)
		}
	}
}

// TestIsValidPoints tests Fibonacci story point validation
func TestIsValidPoints(t *testing.T) {
	validPoints := []int{1, 2, 3, 5, 8, 13, 21}

	for _, pts := range validPoints {
		if !models.IsValidPoints(pts) {
			t.Errorf("Expected %d to be valid Fibonacci point", pts)
		}
	}

	invalidPoints := []int{0, 4, 6, 7, 9, 10, 11, 12, 14, 20, 22, -1}
	for _, pts := range invalidPoints {
		if models.IsValidPoints(pts) {
			t.Errorf("Expected %d to be invalid Fibonacci point", pts)
		}
	}
}

// TestCreateIssueWithValidData tests creating an issue with valid data
func TestCreateIssueWithValidData(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:       "Test Issue",
		Type:        models.TypeTask,
		Priority:    models.PriorityP1,
		Points:      5,
		Labels:      []string{"backend", "urgent"},
		Description: "A test issue",
	}

	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if issue.ID == "" {
		t.Error("Expected issue ID to be generated")
	}

	retrieved, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if retrieved.Title != "Test Issue" {
		t.Errorf("Title mismatch: got %q", retrieved.Title)
	}
	if retrieved.Type != models.TypeTask {
		t.Errorf("Type mismatch: got %q", retrieved.Type)
	}
	if retrieved.Priority != models.PriorityP1 {
		t.Errorf("Priority mismatch: got %q", retrieved.Priority)
	}
	if retrieved.Points != 5 {
		t.Errorf("Points mismatch: got %d", retrieved.Points)
	}
}

// TestCreateIssueWithDependency tests creating issue with dependency
func TestCreateIssueWithDependency(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create prerequisite issue
	prereq := &models.Issue{
		Title:  "Prerequisite",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(prereq); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create dependent issue
	dependent := &models.Issue{
		Title:  "Dependent Issue",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(dependent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add dependency
	if err := database.AddDependency(dependent.ID, prereq.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	// Verify dependency
	deps, err := database.GetDependencies(dependent.ID)
	if err != nil {
		t.Fatalf("GetDependencies failed: %v", err)
	}

	if len(deps) != 1 || deps[0] != prereq.ID {
		t.Errorf("Expected dependency on %s, got %v", prereq.ID, deps)
	}
}

// TestCreateIssueWithBlocks tests creating issue that blocks another
func TestCreateIssueWithBlocks(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create blocked issue first
	blocked := &models.Issue{
		Title:  "Blocked Issue",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(blocked); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create blocker issue
	blocker := &models.Issue{
		Title:  "Blocker Issue",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(blocker); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add blocks relationship (blocked depends on blocker)
	if err := database.AddDependency(blocked.ID, blocker.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	// Verify blocked-by relationship
	blockedBy, err := database.GetBlockedBy(blocker.ID)
	if err != nil {
		t.Fatalf("GetBlockedBy failed: %v", err)
	}

	if len(blockedBy) != 1 || blockedBy[0] != blocked.ID {
		t.Errorf("Expected %s to be blocked, got %v", blocked.ID, blockedBy)
	}
}

// TestCreateIssueWithLabels tests label parsing
func TestCreateIssueWithLabels(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Labeled Issue",
		Labels: []string{"frontend", "ui", "accessibility"},
	}

	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if len(retrieved.Labels) != 3 {
		t.Errorf("Expected 3 labels, got %d", len(retrieved.Labels))
	}
}

// TestCreateIssueWithParent tests parent relationship
func TestCreateIssueWithParent(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create parent (epic)
	parent := &models.Issue{
		Title: "Parent Epic",
		Type:  models.TypeEpic,
	}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create child
	child := &models.Issue{
		Title:    "Child Task",
		ParentID: parent.ID,
	}
	if err := database.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	retrieved, _ := database.GetIssue(child.ID)
	if retrieved.ParentID != parent.ID {
		t.Errorf("Expected parent %s, got %s", parent.ID, retrieved.ParentID)
	}
}

// TestIssueDefaultStatus tests that new issues start as open
func TestIssueDefaultStatus(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title: "New Issue",
	}
	database.CreateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusOpen {
		t.Errorf("Expected status 'open', got %q", retrieved.Status)
	}
}

// TestCreateMultipleDependencies tests adding multiple dependencies at once
func TestCreateMultipleDependencies(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create three prerequisite issues
	prereq1 := &models.Issue{Title: "Prereq 1"}
	prereq2 := &models.Issue{Title: "Prereq 2"}
	prereq3 := &models.Issue{Title: "Prereq 3"}
	database.CreateIssue(prereq1)
	database.CreateIssue(prereq2)
	database.CreateIssue(prereq3)

	// Create dependent issue
	dependent := &models.Issue{Title: "Dependent"}
	database.CreateIssue(dependent)

	// Add multiple dependencies
	database.AddDependency(dependent.ID, prereq1.ID, "depends_on")
	database.AddDependency(dependent.ID, prereq2.ID, "depends_on")
	database.AddDependency(dependent.ID, prereq3.ID, "depends_on")

	// Verify all dependencies
	deps, _ := database.GetDependencies(dependent.ID)
	if len(deps) != 3 {
		t.Errorf("Expected 3 dependencies, got %d", len(deps))
	}
}

// TestValidPointsReturnsCorrectValues tests the ValidPoints function
func TestValidPointsReturnsCorrectValues(t *testing.T) {
	expected := []int{1, 2, 3, 5, 8, 13, 21}
	actual := models.ValidPoints()

	if len(actual) != len(expected) {
		t.Fatalf("Expected %d valid points, got %d", len(expected), len(actual))
	}

	for i, v := range expected {
		if actual[i] != v {
			t.Errorf("Expected ValidPoints()[%d] = %d, got %d", i, v, actual[i])
		}
	}
}

// TestCreateIssueIDFormat tests that issue IDs follow expected format
func TestCreateIssueIDFormat(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	// ID should be "td-" + 6 hex chars = 9 total chars
	if !strings.HasPrefix(issue.ID, "td-") {
		t.Errorf("Expected ID to start with 'td-', got %q", issue.ID)
	}
	if len(issue.ID) != 9 {
		t.Errorf("Expected ID length of 9 (td- + 6 hex chars), got %d: %q", len(issue.ID), issue.ID)
	}
}

// TestCreateIssueTimestamps tests that timestamps are set correctly
func TestCreateIssueTimestamps(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	if issue.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}
	if issue.UpdatedAt.IsZero() {
		t.Error("Expected UpdatedAt to be set")
	}
	if issue.ClosedAt != nil {
		t.Error("Expected ClosedAt to be nil for new issue")
	}
}

// TestCreateNotesFlagAlias tests that --notes is an alias for --description
func TestCreateNotesFlagAlias(t *testing.T) {
	// Test that --notes flag exists
	if createCmd.Flags().Lookup("notes") == nil {
		t.Error("Expected --notes flag to be defined")
	}

	// Test that --notes flag can be set
	if err := createCmd.Flags().Set("notes", "test description via notes"); err != nil {
		t.Errorf("Failed to set --notes flag: %v", err)
	}

	notesValue, err := createCmd.Flags().GetString("notes")
	if err != nil {
		t.Errorf("Failed to get --notes flag value: %v", err)
	}
	if notesValue != "test description via notes" {
		t.Errorf("Expected notes value 'test description via notes', got %s", notesValue)
	}

	// Reset
	createCmd.Flags().Set("notes", "")
}

// TestCreateTagFlagParsing tests that --tag and --tags flags are defined and work
func TestCreateTagFlagParsing(t *testing.T) {
	// Test that --tag flag exists
	if createCmd.Flags().Lookup("tag") == nil {
		t.Error("Expected --tag flag to be defined")
	}

	// Test that --tags flag exists
	if createCmd.Flags().Lookup("tags") == nil {
		t.Error("Expected --tags flag to be defined")
	}

	// Test that --tag flag can be set
	if err := createCmd.Flags().Set("tag", "test,data"); err != nil {
		t.Errorf("Failed to set --tag flag: %v", err)
	}

	tagValue, err := createCmd.Flags().GetString("tag")
	if err != nil {
		t.Errorf("Failed to get --tag flag value: %v", err)
	}
	if tagValue != "test,data" {
		t.Errorf("Expected tag value 'test,data', got %s", tagValue)
	}

	// Reset flags
	createCmd.Flags().Set("tag", "")

	// Test that --tags flag can be set
	if err := createCmd.Flags().Set("tags", "backend,api"); err != nil {
		t.Errorf("Failed to set --tags flag: %v", err)
	}

	tagsValue, err := createCmd.Flags().GetString("tags")
	if err != nil {
		t.Errorf("Failed to get --tags flag value: %v", err)
	}
	if tagsValue != "backend,api" {
		t.Errorf("Expected tags value 'backend,api', got %s", tagsValue)
	}
}

// TestMinorFlagExists tests that --minor flag is defined
func TestMinorFlagExists(t *testing.T) {
	if createCmd.Flags().Lookup("minor") == nil {
		t.Error("Expected --minor flag to be defined")
	}
}

// TestCreateIssueWithMinorFlag tests creating an issue with --minor flag
func TestCreateIssueWithMinorFlag(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issue with Minor flag set
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

// TestMinorTaskAllowsSelfReview tests that minor tasks can be reviewed by creator
func TestMinorTaskAllowsSelfReview(t *testing.T) {
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

// TestNormalTaskDoesNotAllowSelfReview tests that normal tasks cannot be reviewed by creator/implementer
func TestNormalTaskDoesNotAllowSelfReview(t *testing.T) {
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

// TestMinorTaskBypass tests that minor flag bypasses review restrictions
func TestMinorTaskBypass(t *testing.T) {
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

// TestMinorVsNormalWorkflow tests the complete workflow difference
func TestMinorVsNormalWorkflow(t *testing.T) {
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

// TestMinorTaskDoesNotAppearToOthersAsNormalTask tests that minor status is preserved
func TestMinorTaskDoesNotAppearToOthersAsNormalTask(t *testing.T) {
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

// TestMinorFlagDefaultIsFalse tests that new issues default to non-minor
func TestMinorFlagDefaultIsFalse(t *testing.T) {
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

// TestMultipleMinorTasksByCreator tests creator can review multiple minor tasks
func TestMultipleMinorTasksByCreator(t *testing.T) {
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

// TestValidateTitleMinLength tests that titles shorter than min are rejected
func TestValidateTitleMinLength(t *testing.T) {
	tests := []struct {
		title     string
		minLen    int
		maxLen    int
		wantError bool
	}{
		{"Short", 15, 100, true},                              // 5 chars < 15
		{"This is fine!", 15, 100, true},                      // 13 chars < 15
		{"This is long enough to pass", 15, 100, false},       // 27 chars >= 15
		{"Exactly fifteen!", 15, 100, false},                  // 16 chars >= 15
		{"Fix the login bug", 15, 100, false},                 // 17 chars >= 15
		{"A", 1, 100, false},                                  // Custom min=1
		{"Unicode: 日本語テスト長さ確認", 15, 100, false},               // Unicode rune count (19 runes >= 15)
	}

	for _, tt := range tests {
		err := validateTitle(tt.title, tt.minLen, tt.maxLen)
		if tt.wantError && err == nil {
			t.Errorf("validateTitle(%q, %d, %d) expected error, got nil", tt.title, tt.minLen, tt.maxLen)
		}
		if !tt.wantError && err != nil {
			t.Errorf("validateTitle(%q, %d, %d) unexpected error: %v", tt.title, tt.minLen, tt.maxLen, err)
		}
	}
}

// TestValidateTitleMaxLength tests that titles longer than max are rejected
func TestValidateTitleMaxLength(t *testing.T) {
	tests := []struct {
		title     string
		minLen    int
		maxLen    int
		wantError bool
	}{
		{"This is a normal length title that should pass easily", 15, 100, false},
		{strings.Repeat("a", 100), 15, 100, false},  // Exactly 100 chars
		{strings.Repeat("a", 101), 15, 100, true},   // 101 chars > 100
		{strings.Repeat("a", 200), 15, 100, true},   // Way too long
		{strings.Repeat("a", 50), 15, 50, false},    // Custom max
		{strings.Repeat("a", 51), 15, 50, true},     // Custom max exceeded
	}

	for _, tt := range tests {
		err := validateTitle(tt.title, tt.minLen, tt.maxLen)
		if tt.wantError && err == nil {
			t.Errorf("validateTitle(len=%d, min=%d, max=%d) expected error, got nil", len(tt.title), tt.minLen, tt.maxLen)
		}
		if !tt.wantError && err != nil {
			t.Errorf("validateTitle(len=%d, min=%d, max=%d) unexpected error: %v", len(tt.title), tt.minLen, tt.maxLen, err)
		}
	}
}

// TestValidateTitleGenericRejection tests that generic titles are rejected
func TestValidateTitleGenericRejection(t *testing.T) {
	genericTitles := []string{
		"task", "TASK", "Task",
		"issue", "bug", "feature",
		"fix", "update", "change",
		"todo", "work", "item",
		"thing", "stuff", "test", "new", "add",
	}

	for _, title := range genericTitles {
		err := validateTitle(title, 1, 100) // Use min=1 to isolate generic check
		if err == nil {
			t.Errorf("validateTitle(%q) should reject generic title", title)
		}
		if err != nil && !strings.Contains(err.Error(), "generic") {
			t.Errorf("validateTitle(%q) error should mention 'generic': %v", title, err)
		}
	}
}

// TestValidateTitleErrorMessages tests that error messages are helpful
func TestValidateTitleErrorMessages(t *testing.T) {
	// Too short error
	err := validateTitle("Short", 15, 100)
	if err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("Expected 'too short' error, got: %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "5 chars") {
		t.Errorf("Error should include actual length, got: %v", err)
	}

	// Too long error
	err = validateTitle(strings.Repeat("a", 150), 15, 100)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("Expected 'too long' error, got: %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "max 100") {
		t.Errorf("Error should include max length, got: %v", err)
	}
}

// TestConfigDefaultTitleLengths tests that config defaults are correct
func TestConfigDefaultTitleLengths(t *testing.T) {
	if config.DefaultTitleMinLength != 15 {
		t.Errorf("DefaultTitleMinLength should be 15, got %d", config.DefaultTitleMinLength)
	}
	if config.DefaultTitleMaxLength != 100 {
		t.Errorf("DefaultTitleMaxLength should be 100, got %d", config.DefaultTitleMaxLength)
	}
}

// TestConfigGetTitleLengthLimitsDefaults tests getting limits without config file
func TestConfigGetTitleLengthLimitsDefaults(t *testing.T) {
	dir := t.TempDir()

	min, max, _ := config.GetTitleLengthLimits(dir)
	if min != config.DefaultTitleMinLength {
		t.Errorf("Expected default min %d, got %d", config.DefaultTitleMinLength, min)
	}
	if max != config.DefaultTitleMaxLength {
		t.Errorf("Expected default max %d, got %d", config.DefaultTitleMaxLength, max)
	}
}
