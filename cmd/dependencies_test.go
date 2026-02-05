package cmd

import (
	"fmt"
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

// TestAddDependencySingle tests adding a single dependency
func TestAddDependencySingle(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create two issues
	issue1 := &models.Issue{Title: "Setup Database", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Implement API", Status: models.StatusOpen}
	database.CreateIssue(issue1)
	database.CreateIssue(issue2)

	// Add dependency: issue2 depends on issue1
	err = addDependency(database, issue2.ID, issue1.ID, "ses_test")
	if err != nil {
		t.Errorf("Failed to add dependency: %v", err)
	}

	// Verify dependency was added
	deps, _ := database.GetDependencies(issue2.ID)
	if len(deps) != 1 || deps[0] != issue1.ID {
		t.Errorf("Expected issue2 to depend on issue1, got deps: %v", deps)
	}
}

// TestAddDependencyMultiple tests adding multiple dependencies to same issue
func TestAddDependencyMultiple(t *testing.T) {
	tests := []struct {
		name        string
		numDeps     int
		wantError   bool
		description string
	}{
		{
			name:        "two dependencies",
			numDeps:     2,
			wantError:   false,
			description: "issue depends on two separate issues",
		},
		{
			name:        "three dependencies",
			numDeps:     3,
			wantError:   false,
			description: "issue depends on three separate issues",
		},
		{
			name:        "five dependencies",
			numDeps:     5,
			wantError:   false,
			description: "issue depends on five separate issues (parallel work)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			database, err := db.Initialize(dir)
			if err != nil {
				t.Fatalf("Initialize failed: %v", err)
			}
			defer database.Close()

			// Create main issue and dependency issues
			mainIssue := &models.Issue{Title: "Integrations", Status: models.StatusOpen}
			database.CreateIssue(mainIssue)

			depIssueIDs := make([]string, tt.numDeps)
			for i := 0; i < tt.numDeps; i++ {
				issue := &models.Issue{
					Title:  fmt.Sprintf("Dependency %d", i+1),
					Status: models.StatusOpen,
				}
				database.CreateIssue(issue)
				depIssueIDs[i] = issue.ID
			}

			// Add all dependencies
			for _, depID := range depIssueIDs {
				err := addDependency(database, mainIssue.ID, depID, "ses_test")
				if (err != nil) != tt.wantError {
					t.Errorf("addDependency() error = %v, wantError %v", err, tt.wantError)
				}
			}

			// Verify all dependencies were added
			deps, _ := database.GetDependencies(mainIssue.ID)
			if len(deps) != tt.numDeps {
				t.Errorf("Expected %d dependencies, got %d", tt.numDeps, len(deps))
			}

			// Verify each dependency is present
			for _, expectedDepID := range depIssueIDs {
				found := false
				for _, actualDepID := range deps {
					if actualDepID == expectedDepID {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected dependency %s not found in %v", expectedDepID, deps)
				}
			}
		})
	}
}

// TestAddDependencyCircularDetection tests circular dependency detection
func TestAddDependencyCircularDetection(t *testing.T) {
	tests := []struct {
		name        string
		setupChain  func(db *db.DB, issues []*models.Issue)
		cycleFrom   int
		cycleTo     int
		shouldError bool
		description string
	}{
		{
			name: "simple cycle",
			setupChain: func(database *db.DB, issues []*models.Issue) {
				// issue1 -> issue2
				database.AddDependency(issues[1].ID, issues[0].ID, "depends_on")
			},
			cycleFrom:   0, // Try to add: issue1 depends on issue2
			cycleTo:     1,
			shouldError: true,
			description: "issue1 -> issue2 -> issue1",
		},
		{
			name: "transitive cycle",
			setupChain: func(database *db.DB, issues []*models.Issue) {
				// issue2 -> issue1, issue3 -> issue2
				database.AddDependency(issues[1].ID, issues[0].ID, "depends_on")
				database.AddDependency(issues[2].ID, issues[1].ID, "depends_on")
			},
			cycleFrom:   0, // Try to add: issue1 depends on issue3
			cycleTo:     2,
			shouldError: true,
			description: "issue1 -> issue3 -> issue2 -> issue1",
		},
		{
			name: "self reference",
			setupChain: func(database *db.DB, issues []*models.Issue) {
				// No setup needed
			},
			cycleFrom:   0, // Try to add: issue1 depends on issue1
			cycleTo:     0,
			shouldError: true,
			description: "issue1 -> issue1",
		},
		{
			name: "no cycle valid dep",
			setupChain: func(database *db.DB, issues []*models.Issue) {
				// issue2 -> issue1
				database.AddDependency(issues[1].ID, issues[0].ID, "depends_on")
			},
			cycleFrom:   2, // Try to add: issue3 depends on issue1 (valid)
			cycleTo:     0,
			shouldError: false,
			description: "valid: issue3 -> issue1 (no cycle)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			database, err := db.Initialize(dir)
			if err != nil {
				t.Fatalf("Initialize failed: %v", err)
			}
			defer database.Close()

			// Create 4 issues
			issues := make([]*models.Issue, 4)
			for i := 0; i < 4; i++ {
				issues[i] = &models.Issue{
					Title:  fmt.Sprintf("Issue %d", i+1),
					Status: models.StatusOpen,
				}
				database.CreateIssue(issues[i])
			}

			// Setup the dependency chain
			tt.setupChain(database, issues)

			// Try to create the cycle
			err = addDependency(database, issues[tt.cycleFrom].ID, issues[tt.cycleTo].ID, "ses_test")

			if (err != nil) != tt.shouldError {
				t.Errorf("addDependency() error = %v, wantError %v. Description: %s", err, tt.shouldError, tt.description)
			}
		})
	}
}

// TestAddDependencyValidation tests dependency validation rules
func TestAddDependencyValidation(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(db *db.DB) (string, string)
		wantError   bool
		description string
	}{
		{
			name: "issue not found source",
			setup: func(database *db.DB) (string, string) {
				issue := &models.Issue{Title: "Exists", Status: models.StatusOpen}
				database.CreateIssue(issue)
				return "nonexistent", issue.ID
			},
			wantError:   true,
			description: "source issue does not exist",
		},
		{
			name: "issue not found target",
			setup: func(database *db.DB) (string, string) {
				issue := &models.Issue{Title: "Exists", Status: models.StatusOpen}
				database.CreateIssue(issue)
				return issue.ID, "nonexistent"
			},
			wantError:   true,
			description: "target issue does not exist",
		},
		{
			name: "duplicate dependency",
			setup: func(database *db.DB) (string, string) {
				issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusOpen}
				issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusOpen}
				database.CreateIssue(issue1)
				database.CreateIssue(issue2)
				// Add dependency first time
				addDependency(database, issue1.ID, issue2.ID, "ses_test")
				return issue1.ID, issue2.ID
			},
			wantError:   false, // addDependency returns nil for duplicates (with warning)
			description: "adding same dependency twice",
		},
		{
			name: "valid dependency open issues",
			setup: func(database *db.DB) (string, string) {
				issue1 := &models.Issue{Title: "Backend", Status: models.StatusOpen}
				issue2 := &models.Issue{Title: "Database", Status: models.StatusOpen}
				database.CreateIssue(issue1)
				database.CreateIssue(issue2)
				return issue1.ID, issue2.ID
			},
			wantError:   false,
			description: "valid dependency between two open issues",
		},
		{
			name: "depends on closed issue allowed",
			setup: func(database *db.DB) (string, string) {
				issue1 := &models.Issue{Title: "Resolved API", Status: models.StatusClosed}
				issue2 := &models.Issue{Title: "New Feature", Status: models.StatusOpen}
				database.CreateIssue(issue1)
				database.CreateIssue(issue2)
				return issue2.ID, issue1.ID
			},
			wantError:   false,
			description: "issue can depend on closed issue (already resolved)",
		},
		{
			name: "mixed statuses allowed",
			setup: func(database *db.DB) (string, string) {
				issue1 := &models.Issue{Title: "In Progress", Status: models.StatusInProgress}
				issue2 := &models.Issue{Title: "Blocked", Status: models.StatusBlocked}
				database.CreateIssue(issue1)
				database.CreateIssue(issue2)
				return issue2.ID, issue1.ID
			},
			wantError:   false,
			description: "dependencies allowed for any status combination",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			database, err := db.Initialize(dir)
			if err != nil {
				t.Fatalf("Initialize failed: %v", err)
			}
			defer database.Close()

			issueID, depID := tt.setup(database)
			err = addDependency(database, issueID, depID, "ses_test")

			if (err != nil) != tt.wantError {
				t.Errorf("addDependency() error = %v, wantError %v. Description: %s", err, tt.wantError, tt.description)
			}
		})
	}
}

// TestAddDependencyPersistence tests that dependencies persist in database
func TestAddDependencyPersistence(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	issue1 := &models.Issue{Title: "Step 1", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Step 2", Status: models.StatusOpen}
	issue3 := &models.Issue{Title: "Step 3", Status: models.StatusOpen}
	database.CreateIssue(issue1)
	database.CreateIssue(issue2)
	database.CreateIssue(issue3)

	// Add dependencies
	addDependency(database, issue2.ID, issue1.ID, "ses_test")
	addDependency(database, issue3.ID, issue2.ID, "ses_test")

	database.Close()

	// Reopen database and verify dependencies persist
	database, err = db.Open(dir)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer database.Close()

	// Check issue2 depends on issue1
	deps2, _ := database.GetDependencies(issue2.ID)
	if len(deps2) != 1 || deps2[0] != issue1.ID {
		t.Errorf("Expected issue2 to depend on issue1, got: %v", deps2)
	}

	// Check issue3 depends on issue2
	deps3, _ := database.GetDependencies(issue3.ID)
	if len(deps3) != 1 || deps3[0] != issue2.ID {
		t.Errorf("Expected issue3 to depend on issue2, got: %v", deps3)
	}
}

// TestAddDependencyComplexGraph tests complex dependency graphs
func TestAddDependencyComplexGraph(t *testing.T) {
	tests := []struct {
		name        string
		buildGraph  func(db *db.DB, issues map[string]*models.Issue)
		checkFunc   func(*testing.T, *db.DB, map[string]*models.Issue)
		description string
	}{
		{
			name: "diamond pattern",
			buildGraph: func(database *db.DB, issues map[string]*models.Issue) {
				// A -> B, A -> C, B -> D, C -> D
				database.AddDependency(issues["B"].ID, issues["A"].ID, "depends_on")
				database.AddDependency(issues["C"].ID, issues["A"].ID, "depends_on")
				database.AddDependency(issues["D"].ID, issues["B"].ID, "depends_on")
				database.AddDependency(issues["D"].ID, issues["C"].ID, "depends_on")
			},
			checkFunc: func(t *testing.T, database *db.DB, issues map[string]*models.Issue) {
				// D should have 2 dependencies
				depsD, _ := database.GetDependencies(issues["D"].ID)
				if len(depsD) != 2 {
					t.Errorf("Expected D to have 2 dependencies, got %d", len(depsD))
				}
				// B and C should each have 1 dependency
				depsB, _ := database.GetDependencies(issues["B"].ID)
				depsC, _ := database.GetDependencies(issues["C"].ID)
				if len(depsB) != 1 || len(depsC) != 1 {
					t.Errorf("Expected B and C to have 1 dependency each, got %d and %d", len(depsB), len(depsC))
				}
			},
			description: "diamond dependency pattern (A blocks B and C, both block D)",
		},
		{
			name: "multi-level chain",
			buildGraph: func(database *db.DB, issues map[string]*models.Issue) {
				// A -> B -> C -> D -> E
				database.AddDependency(issues["B"].ID, issues["A"].ID, "depends_on")
				database.AddDependency(issues["C"].ID, issues["B"].ID, "depends_on")
				database.AddDependency(issues["D"].ID, issues["C"].ID, "depends_on")
				database.AddDependency(issues["E"].ID, issues["D"].ID, "depends_on")
			},
			checkFunc: func(t *testing.T, database *db.DB, issues map[string]*models.Issue) {
				// Each should have exactly 1 direct dependency
				for _, key := range []string{"B", "C", "D", "E"} {
					deps, _ := database.GetDependencies(issues[key].ID)
					if len(deps) != 1 {
						t.Errorf("Expected %s to have 1 dependency, got %d", key, len(deps))
					}
				}
				// A should have no dependencies
				depsA, _ := database.GetDependencies(issues["A"].ID)
				if len(depsA) != 0 {
					t.Errorf("Expected A to have 0 dependencies, got %d", len(depsA))
				}
			},
			description: "linear chain of 5 issues",
		},
		{
			name: "fan-out pattern",
			buildGraph: func(database *db.DB, issues map[string]*models.Issue) {
				// A blocks B, C, D, E, F
				database.AddDependency(issues["B"].ID, issues["A"].ID, "depends_on")
				database.AddDependency(issues["C"].ID, issues["A"].ID, "depends_on")
				database.AddDependency(issues["D"].ID, issues["A"].ID, "depends_on")
				database.AddDependency(issues["E"].ID, issues["A"].ID, "depends_on")
				database.AddDependency(issues["F"].ID, issues["A"].ID, "depends_on")
			},
			checkFunc: func(t *testing.T, database *db.DB, issues map[string]*models.Issue) {
				// Each of B-F should have exactly 1 dependency on A
				for _, key := range []string{"B", "C", "D", "E", "F"} {
					deps, _ := database.GetDependencies(issues[key].ID)
					if len(deps) != 1 || deps[0] != issues["A"].ID {
						t.Errorf("Expected %s to depend only on A, got deps: %v", key, deps)
					}
				}
				// A should have no dependencies
				depsA, _ := database.GetDependencies(issues["A"].ID)
				if len(depsA) != 0 {
					t.Errorf("Expected A to have 0 dependencies, got %d", len(depsA))
				}
			},
			description: "one issue blocks many (fan-out)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			database, err := db.Initialize(dir)
			if err != nil {
				t.Fatalf("Initialize failed: %v", err)
			}
			defer database.Close()

			// Create 6 issues labeled A-F
			issues := make(map[string]*models.Issue)
			for _, label := range []string{"A", "B", "C", "D", "E", "F"} {
				issue := &models.Issue{
					Title:  fmt.Sprintf("Issue %s", label),
					Status: models.StatusOpen,
				}
				database.CreateIssue(issue)
				issues[label] = issue
			}

			// Build the dependency graph
			tt.buildGraph(database, issues)

			// Verify the graph structure
			tt.checkFunc(t, database, issues)
		})
	}
}
