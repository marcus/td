package dependency

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func setupTestDB(t *testing.T) (*db.DB, func()) {
	t.Helper()

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "td-dep-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Create .todos directory
	todosDir := filepath.Join(tmpDir, ".todos")
	if err := os.MkdirAll(todosDir, 0755); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create .todos dir: %v", err)
	}

	// Initialize database
	database, err := db.Initialize(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to initialize db: %v", err)
	}

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return database, cleanup
}

func createTestIssue(t *testing.T, database *db.DB, title string) *models.Issue {
	t.Helper()

	issue := &models.Issue{
		Title:    title,
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
	}

	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	return issue
}

func TestWouldCreateCycle(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Create three issues: A, B, C
	issueA := createTestIssue(t, database, "Issue A")
	issueB := createTestIssue(t, database, "Issue B")
	issueC := createTestIssue(t, database, "Issue C")

	// A -> B (A depends on B)
	if err := database.AddDependency(issueA.ID, issueB.ID, "depends_on"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	// B -> C (B depends on C)
	if err := database.AddDependency(issueB.ID, issueC.ID, "depends_on"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	// Test: C -> A would create cycle (C -> A -> B -> C)
	if !WouldCreateCycle(database, issueC.ID, issueA.ID) {
		t.Error("expected cycle detection for C -> A")
	}

	// Test: C -> B would create cycle (C -> B -> C)
	if !WouldCreateCycle(database, issueC.ID, issueB.ID) {
		t.Error("expected cycle detection for C -> B")
	}

	// Test: A -> C would NOT create cycle (A already depends on B -> C)
	if WouldCreateCycle(database, issueA.ID, issueC.ID) {
		t.Error("expected no cycle for A -> C (redundant but not circular)")
	}

	// Create isolated issue D
	issueD := createTestIssue(t, database, "Issue D")

	// Test: A -> D would NOT create cycle
	if WouldCreateCycle(database, issueA.ID, issueD.ID) {
		t.Error("expected no cycle for A -> D")
	}
}

func TestValidateAndAdd(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	issueA := createTestIssue(t, database, "Issue A")
	issueB := createTestIssue(t, database, "Issue B")

	// Test successful add
	err := ValidateAndAdd(database, issueA.ID, issueB.ID)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	// Verify dependency was added
	deps, err := database.GetDependencies(issueA.ID)
	if err != nil {
		t.Fatalf("failed to get dependencies: %v", err)
	}
	if len(deps) != 1 || deps[0] != issueB.ID {
		t.Errorf("expected dependency on %s, got: %v", issueB.ID, deps)
	}

	// Test duplicate add returns ErrDependencyExists
	err = ValidateAndAdd(database, issueA.ID, issueB.ID)
	if err != ErrDependencyExists {
		t.Errorf("expected ErrDependencyExists, got: %v", err)
	}

	// Test circular dependency prevention
	err = ValidateAndAdd(database, issueB.ID, issueA.ID)
	if err == nil {
		t.Error("expected error for circular dependency")
	}

	// Test non-existent issue
	err = ValidateAndAdd(database, "nonexistent", issueB.ID)
	if err == nil {
		t.Error("expected error for non-existent issue")
	}

	err = ValidateAndAdd(database, issueA.ID, "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent dependency target")
	}
}

func TestRemove(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	issueA := createTestIssue(t, database, "Issue A")
	issueB := createTestIssue(t, database, "Issue B")

	// Add dependency
	if err := ValidateAndAdd(database, issueA.ID, issueB.ID); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	// Verify it exists
	deps, _ := database.GetDependencies(issueA.ID)
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}

	// Remove dependency
	if err := Remove(database, issueA.ID, issueB.ID); err != nil {
		t.Errorf("failed to remove dependency: %v", err)
	}

	// Verify it's gone
	deps, _ = database.GetDependencies(issueA.ID)
	if len(deps) != 0 {
		t.Errorf("expected 0 dependencies after removal, got %d", len(deps))
	}
}

func TestGetDependencies(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	issueA := createTestIssue(t, database, "Issue A")
	issueB := createTestIssue(t, database, "Issue B")
	issueC := createTestIssue(t, database, "Issue C")

	// A depends on B and C
	if err := ValidateAndAdd(database, issueA.ID, issueB.ID); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}
	if err := ValidateAndAdd(database, issueA.ID, issueC.ID); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	deps, err := GetDependencies(database, issueA.ID)
	if err != nil {
		t.Fatalf("failed to get dependencies: %v", err)
	}

	if len(deps) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(deps))
	}

	// Verify returned issues have correct data
	foundB, foundC := false, false
	for _, dep := range deps {
		if dep.ID == issueB.ID && dep.Title == "Issue B" {
			foundB = true
		}
		if dep.ID == issueC.ID && dep.Title == "Issue C" {
			foundC = true
		}
	}
	if !foundB || !foundC {
		t.Error("expected to find both Issue B and Issue C")
	}
}

func TestGetDependents(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	issueA := createTestIssue(t, database, "Issue A")
	issueB := createTestIssue(t, database, "Issue B")
	issueC := createTestIssue(t, database, "Issue C")

	// A and B depend on C
	if err := ValidateAndAdd(database, issueA.ID, issueC.ID); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}
	if err := ValidateAndAdd(database, issueB.ID, issueC.ID); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	dependents, err := GetDependents(database, issueC.ID)
	if err != nil {
		t.Fatalf("failed to get dependents: %v", err)
	}

	if len(dependents) != 2 {
		t.Errorf("expected 2 dependents, got %d", len(dependents))
	}

	// Verify returned issues have correct data
	foundA, foundB := false, false
	for _, dep := range dependents {
		if dep.ID == issueA.ID && dep.Title == "Issue A" {
			foundA = true
		}
		if dep.ID == issueB.ID && dep.Title == "Issue B" {
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Error("expected to find both Issue A and Issue B")
	}
}

func TestGetTransitiveBlocked(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Create chain: D -> C -> B -> A (D depends on C depends on B depends on A)
	issueA := createTestIssue(t, database, "Issue A")
	issueB := createTestIssue(t, database, "Issue B")
	issueC := createTestIssue(t, database, "Issue C")
	issueD := createTestIssue(t, database, "Issue D")

	// B depends on A
	if err := database.AddDependency(issueB.ID, issueA.ID, "depends_on"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}
	// C depends on B
	if err := database.AddDependency(issueC.ID, issueB.ID, "depends_on"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}
	// D depends on C
	if err := database.AddDependency(issueD.ID, issueC.ID, "depends_on"); err != nil {
		t.Fatalf("failed to add dependency: %v", err)
	}

	// A blocks B, C, D transitively
	blocked := GetTransitiveBlocked(database, issueA.ID, make(map[string]bool))

	if len(blocked) != 3 {
		t.Errorf("expected 3 transitively blocked, got %d: %v", len(blocked), blocked)
	}

	// B blocks C, D transitively
	blocked = GetTransitiveBlocked(database, issueB.ID, make(map[string]bool))

	if len(blocked) != 2 {
		t.Errorf("expected 2 transitively blocked for B, got %d", len(blocked))
	}

	// D blocks nothing
	blocked = GetTransitiveBlocked(database, issueD.ID, make(map[string]bool))

	if len(blocked) != 0 {
		t.Errorf("expected 0 transitively blocked for D, got %d", len(blocked))
	}
}

func TestGetTransitiveBlockedDiamond(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Diamond: D depends on both B and C, both B and C depend on A
	// A -> B -> D
	// A -> C -> D
	issueA := createTestIssue(t, database, "Issue A")
	issueB := createTestIssue(t, database, "Issue B")
	issueC := createTestIssue(t, database, "Issue C")
	issueD := createTestIssue(t, database, "Issue D")

	database.AddDependency(issueB.ID, issueA.ID, "depends_on")
	database.AddDependency(issueC.ID, issueA.ID, "depends_on")
	database.AddDependency(issueD.ID, issueB.ID, "depends_on")
	database.AddDependency(issueD.ID, issueC.ID, "depends_on")

	// A blocks B, C, D - each counted exactly once
	blocked := GetTransitiveBlocked(database, issueA.ID, make(map[string]bool))
	if len(blocked) != 3 {
		t.Errorf("diamond: expected 3 unique blocked, got %d: %v", len(blocked), blocked)
	}
}

func TestGetTransitiveBlockedMultiPath(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Complex multi-path: E depends on B, C, D; all depend on A
	// A -> B -> E
	// A -> C -> E
	// A -> D -> E
	issueA := createTestIssue(t, database, "Issue A")
	issueB := createTestIssue(t, database, "Issue B")
	issueC := createTestIssue(t, database, "Issue C")
	issueD := createTestIssue(t, database, "Issue D")
	issueE := createTestIssue(t, database, "Issue E")

	database.AddDependency(issueB.ID, issueA.ID, "depends_on")
	database.AddDependency(issueC.ID, issueA.ID, "depends_on")
	database.AddDependency(issueD.ID, issueA.ID, "depends_on")
	database.AddDependency(issueE.ID, issueB.ID, "depends_on")
	database.AddDependency(issueE.ID, issueC.ID, "depends_on")
	database.AddDependency(issueE.ID, issueD.ID, "depends_on")

	// A blocks B, C, D, E - E counted exactly once despite 3 paths
	blocked := GetTransitiveBlocked(database, issueA.ID, make(map[string]bool))
	if len(blocked) != 4 {
		t.Errorf("multi-path: expected 4 unique blocked, got %d: %v", len(blocked), blocked)
	}
}

func TestGetTransitiveBlockedOpenExcludesClosed(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Chain: C -> B -> A, but B is closed
	issueA := createTestIssue(t, database, "Issue A")
	issueB := createTestIssue(t, database, "Issue B")
	issueC := createTestIssue(t, database, "Issue C")

	database.AddDependency(issueB.ID, issueA.ID, "depends_on")
	database.AddDependency(issueC.ID, issueB.ID, "depends_on")

	// Close B
	issueB.Status = models.StatusClosed
	database.UpdateIssue(issueB)

	// GetTransitiveBlocked includes closed issues
	all := GetTransitiveBlocked(database, issueA.ID, make(map[string]bool))
	if len(all) != 2 {
		t.Errorf("expected 2 total blocked (including closed), got %d", len(all))
	}

	// GetTransitiveBlockedOpen excludes closed issues and their subtrees
	open := GetTransitiveBlockedOpen(database, issueA.ID, make(map[string]bool))
	if len(open) != 0 {
		t.Errorf("expected 0 open blocked (B closed, C unreachable via open path), got %d: %v", len(open), open)
	}
}

func TestGetTransitiveBlockedOpenPartialClosed(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// A -> B (open) -> D (open)
	// A -> C (closed)
	issueA := createTestIssue(t, database, "Issue A")
	issueB := createTestIssue(t, database, "Issue B")
	issueC := createTestIssue(t, database, "Issue C")
	issueD := createTestIssue(t, database, "Issue D")

	database.AddDependency(issueB.ID, issueA.ID, "depends_on")
	database.AddDependency(issueC.ID, issueA.ID, "depends_on")
	database.AddDependency(issueD.ID, issueB.ID, "depends_on")

	// Close C
	issueC.Status = models.StatusClosed
	database.UpdateIssue(issueC)

	open := GetTransitiveBlockedOpen(database, issueA.ID, make(map[string]bool))
	if len(open) != 2 {
		t.Errorf("expected 2 open blocked (B and D), got %d: %v", len(open), open)
	}
}
