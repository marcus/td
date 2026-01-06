package cmd

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/dependency"
	"github.com/marcus/td/internal/models"
)

// TestWouldCreateCycleSimple tests simple circular dependency detection
func TestWouldCreateCycleSimple(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create two issues
	issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusOpen}
	database.CreateIssue(issue1)
	database.CreateIssue(issue2)

	// Add issue2 depends on issue1
	database.AddDependency(issue2.ID, issue1.ID, "depends_on")

	// Check if adding issue1 depends on issue2 would create cycle
	if !dependency.WouldCreateCycle(database, issue1.ID, issue2.ID) {
		t.Error("Expected cycle detection: issue1 -> issue2 -> issue1")
	}
}

// TestWouldCreateCycleTransitive tests transitive circular dependency detection
func TestWouldCreateCycleTransitive(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create chain: issue3 -> issue2 -> issue1
	issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusOpen}
	issue3 := &models.Issue{Title: "Issue 3", Status: models.StatusOpen}
	database.CreateIssue(issue1)
	database.CreateIssue(issue2)
	database.CreateIssue(issue3)

	database.AddDependency(issue2.ID, issue1.ID, "depends_on")
	database.AddDependency(issue3.ID, issue2.ID, "depends_on")

	// issue1 -> issue3 would create cycle: issue1 -> issue3 -> issue2 -> issue1
	if !dependency.WouldCreateCycle(database, issue1.ID, issue3.ID) {
		t.Error("Expected cycle detection in transitive chain")
	}

	// issue1 -> issue1 (self-reference) should be detected
	if !dependency.WouldCreateCycle(database, issue1.ID, issue1.ID) {
		t.Error("Expected self-reference cycle detection")
	}
}

// TestWouldCreateCycleNoCycle tests that valid dependencies are allowed
func TestWouldCreateCycleNoCycle(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create independent issues
	issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusOpen}
	issue3 := &models.Issue{Title: "Issue 3", Status: models.StatusOpen}
	database.CreateIssue(issue1)
	database.CreateIssue(issue2)
	database.CreateIssue(issue3)

	// issue2 depends on issue1 (no cycle yet)
	database.AddDependency(issue2.ID, issue1.ID, "depends_on")

	// issue3 -> issue1 should be fine (no cycle)
	if dependency.WouldCreateCycle(database, issue3.ID, issue1.ID) {
		t.Error("False positive: issue3 -> issue1 should not create cycle")
	}

	// issue3 -> issue2 should also be fine
	if dependency.WouldCreateCycle(database, issue3.ID, issue2.ID) {
		t.Error("False positive: issue3 -> issue2 should not create cycle")
	}
}

// TestGetTransitiveBlockedEmpty tests no transitive blocks
func TestGetTransitiveBlockedEmpty(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Standalone Issue", Status: models.StatusOpen}
	database.CreateIssue(issue)

	blocked := dependency.GetTransitiveBlocked(database, issue.ID, make(map[string]bool))
	if len(blocked) != 0 {
		t.Errorf("Expected 0 blocked, got %d", len(blocked))
	}
}

// TestGetTransitiveBlockedDirect tests direct blocks only
func TestGetTransitiveBlockedDirect(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create blocker and blockee
	blocker := &models.Issue{Title: "Blocker", Status: models.StatusOpen}
	blocked1 := &models.Issue{Title: "Blocked 1", Status: models.StatusOpen}
	blocked2 := &models.Issue{Title: "Blocked 2", Status: models.StatusOpen}
	database.CreateIssue(blocker)
	database.CreateIssue(blocked1)
	database.CreateIssue(blocked2)

	// Both blocked1 and blocked2 depend on blocker
	database.AddDependency(blocked1.ID, blocker.ID, "depends_on")
	database.AddDependency(blocked2.ID, blocker.ID, "depends_on")

	allBlocked := dependency.GetTransitiveBlocked(database, blocker.ID, make(map[string]bool))
	if len(allBlocked) != 2 {
		t.Errorf("Expected 2 blocked issues, got %d", len(allBlocked))
	}
}

// TestGetTransitiveBlockedChain tests transitive blocking through chain
func TestGetTransitiveBlockedChain(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create chain: issue4 -> issue3 -> issue2 -> issue1
	issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusOpen}
	issue3 := &models.Issue{Title: "Issue 3", Status: models.StatusOpen}
	issue4 := &models.Issue{Title: "Issue 4", Status: models.StatusOpen}
	database.CreateIssue(issue1)
	database.CreateIssue(issue2)
	database.CreateIssue(issue3)
	database.CreateIssue(issue4)

	database.AddDependency(issue2.ID, issue1.ID, "depends_on")
	database.AddDependency(issue3.ID, issue2.ID, "depends_on")
	database.AddDependency(issue4.ID, issue3.ID, "depends_on")

	// issue1 transitively blocks 3 issues
	allBlocked := dependency.GetTransitiveBlocked(database, issue1.ID, make(map[string]bool))
	if len(allBlocked) != 3 {
		t.Errorf("Expected 3 transitively blocked issues, got %d", len(allBlocked))
	}

	// issue2 transitively blocks 2 issues
	allBlocked = dependency.GetTransitiveBlocked(database, issue2.ID, make(map[string]bool))
	if len(allBlocked) != 2 {
		t.Errorf("Expected 2 transitively blocked issues from issue2, got %d", len(allBlocked))
	}
}

// TestGetTransitiveBlockedDiamond tests diamond dependency pattern
func TestGetTransitiveBlockedDiamond(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Diamond pattern:
	//     top
	//    /   \
	//  mid1  mid2
	//    \   /
	//    bottom
	top := &models.Issue{Title: "Top", Status: models.StatusOpen}
	mid1 := &models.Issue{Title: "Mid1", Status: models.StatusOpen}
	mid2 := &models.Issue{Title: "Mid2", Status: models.StatusOpen}
	bottom := &models.Issue{Title: "Bottom", Status: models.StatusOpen}
	database.CreateIssue(top)
	database.CreateIssue(mid1)
	database.CreateIssue(mid2)
	database.CreateIssue(bottom)

	database.AddDependency(mid1.ID, top.ID, "depends_on")
	database.AddDependency(mid2.ID, top.ID, "depends_on")
	database.AddDependency(bottom.ID, mid1.ID, "depends_on")
	database.AddDependency(bottom.ID, mid2.ID, "depends_on")

	// getTransitiveBlocked returns all paths, so bottom appears twice (via mid1 and mid2)
	// This is how it counts total blocking relationships, not unique issues
	allBlocked := dependency.GetTransitiveBlocked(database, top.ID, make(map[string]bool))
	// mid1, mid2, bottom (via mid1), bottom (via mid2) = 4 entries
	if len(allBlocked) < 3 {
		t.Errorf("Expected at least 3 blocked relationships in diamond pattern, got %d", len(allBlocked))
	}
}

// TestSelfReferenceDetection tests self-referencing detection via WouldCreateCycle
func TestSelfReferenceDetection(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Self Loop", Status: models.StatusOpen}
	database.CreateIssue(issue)

	// Direct self-reference via WouldCreateCycle
	if !dependency.WouldCreateCycle(database, issue.ID, issue.ID) {
		t.Error("Expected self-reference to be detected as cycle")
	}
}

// TestBuildCriticalPathSequenceEmpty tests empty path with no issues
func TestBuildCriticalPathSequenceEmpty(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issueMap := make(map[string]*models.Issue)
	blockCounts := make(map[string]int)

	sequence := buildCriticalPathSequence(database, issueMap, blockCounts)
	if len(sequence) != 0 {
		t.Errorf("Expected empty sequence, got %d items", len(sequence))
	}
}

// TestBuildCriticalPathSequenceSingle tests single issue path
func TestBuildCriticalPathSequenceSingle(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Single", Status: models.StatusOpen}
	database.CreateIssue(issue)

	issueMap := map[string]*models.Issue{issue.ID: issue}
	blockCounts := make(map[string]int)

	sequence := buildCriticalPathSequence(database, issueMap, blockCounts)
	if len(sequence) != 1 {
		t.Errorf("Expected 1 issue in sequence, got %d", len(sequence))
	}
}

// TestBuildCriticalPathSequenceChain tests ordered path through dependency chain
func TestBuildCriticalPathSequenceChain(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Chain: issue3 -> issue2 -> issue1
	issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusOpen}
	issue3 := &models.Issue{Title: "Issue 3", Status: models.StatusOpen}
	database.CreateIssue(issue1)
	database.CreateIssue(issue2)
	database.CreateIssue(issue3)

	database.AddDependency(issue2.ID, issue1.ID, "depends_on")
	database.AddDependency(issue3.ID, issue2.ID, "depends_on")

	issueMap := map[string]*models.Issue{
		issue1.ID: issue1,
		issue2.ID: issue2,
		issue3.ID: issue3,
	}
	blockCounts := map[string]int{
		issue1.ID: 2, // blocks issue2 and issue3
		issue2.ID: 1, // blocks issue3
	}

	sequence := buildCriticalPathSequence(database, issueMap, blockCounts)
	if len(sequence) != 3 {
		t.Errorf("Expected 3 issues in sequence, got %d", len(sequence))
	}

	// issue1 should come first (blocks most)
	if len(sequence) > 0 && sequence[0] != issue1.ID {
		t.Errorf("Expected issue1 first in sequence (blocks most), got %s", sequence[0])
	}
}

// TestBuildCriticalPathSkipsClosedIssues tests that closed issues are excluded
func TestBuildCriticalPathSkipsClosedIssues(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	openIssue := &models.Issue{Title: "Open", Status: models.StatusOpen}
	closedIssue := &models.Issue{Title: "Closed", Status: models.StatusClosed}
	database.CreateIssue(openIssue)
	database.CreateIssue(closedIssue)

	issueMap := map[string]*models.Issue{
		openIssue.ID:   openIssue,
		closedIssue.ID: closedIssue,
	}
	blockCounts := make(map[string]int)

	sequence := buildCriticalPathSequence(database, issueMap, blockCounts)

	// Only open issue should be in sequence
	for _, id := range sequence {
		if id == closedIssue.ID {
			t.Error("Closed issue should not be in critical path sequence")
		}
	}
}

// TestDepAddDependsOnFlag tests that --depends-on flag exists on dep add command
func TestDepAddDependsOnFlag(t *testing.T) {
	// Test that --depends-on flag exists
	if depAddCmd.Flags().Lookup("depends-on") == nil {
		t.Error("Expected --depends-on flag to be defined on dep add command")
	}

	// Test that the flag can be set
	if err := depAddCmd.Flags().Set("depends-on", "td-test123"); err != nil {
		t.Errorf("Failed to set --depends-on flag: %v", err)
	}

	dependsOnValue, err := depAddCmd.Flags().GetString("depends-on")
	if err != nil {
		t.Errorf("Failed to get --depends-on flag value: %v", err)
	}
	if dependsOnValue != "td-test123" {
		t.Errorf("Expected depends-on value 'td-test123', got %s", dependsOnValue)
	}

	// Reset
	depAddCmd.Flags().Set("depends-on", "")
}
