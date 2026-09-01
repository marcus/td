package db

import (
	"testing"

	"github.com/marcus/td/internal/models"
)

// Relation getters must accept a bare id the same way GetIssue does. Before
// they normalized, `td show abc123` resolved the issue but returned no
// dependencies, logs, handoff, or git snapshot, because those queries ran
// against the raw `abc123` while the rows are keyed `td-abc123`. Each caller
// discards the error, so the omission was silent. See marcus/td#199 for the
// same defect on the write path.
func TestRelationGettersAcceptBareIDs(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	blocker := &models.Issue{Title: "Blocker", Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(blocker); err != nil {
		t.Fatalf("CreateIssue(blocker) failed: %v", err)
	}
	dependent := &models.Issue{Title: "Dependent", Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(dependent); err != nil {
		t.Fatalf("CreateIssue(dependent) failed: %v", err)
	}

	if err := database.AddDependency(dependent.ID, blocker.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}
	if err := database.AddLog(&models.Log{IssueID: dependent.ID, Message: "entry"}); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	bareDependent := stripPrefix(t, dependent.ID)
	bareBlocker := stripPrefix(t, blocker.ID)

	t.Run("GetDependencies", func(t *testing.T) {
		prefixed, err := database.GetDependencies(dependent.ID)
		if err != nil {
			t.Fatalf("GetDependencies(prefixed) failed: %v", err)
		}
		if len(prefixed) != 1 {
			t.Fatalf("GetDependencies(prefixed) returned %d deps, want 1", len(prefixed))
		}
		bare, err := database.GetDependencies(bareDependent)
		if err != nil {
			t.Fatalf("GetDependencies(bare) failed: %v", err)
		}
		if len(bare) != len(prefixed) {
			t.Errorf("bare id returned %d deps, prefixed returned %d; the two must agree",
				len(bare), len(prefixed))
		}
	})

	t.Run("GetBlockedBy", func(t *testing.T) {
		prefixed, err := database.GetBlockedBy(blocker.ID)
		if err != nil {
			t.Fatalf("GetBlockedBy(prefixed) failed: %v", err)
		}
		if len(prefixed) != 1 {
			t.Fatalf("GetBlockedBy(prefixed) returned %d, want 1", len(prefixed))
		}
		bare, err := database.GetBlockedBy(bareBlocker)
		if err != nil {
			t.Fatalf("GetBlockedBy(bare) failed: %v", err)
		}
		if len(bare) != len(prefixed) {
			t.Errorf("bare id returned %d, prefixed returned %d; the two must agree",
				len(bare), len(prefixed))
		}
	})

	t.Run("GetLogs", func(t *testing.T) {
		prefixed, err := database.GetLogs(dependent.ID, 0)
		if err != nil {
			t.Fatalf("GetLogs(prefixed) failed: %v", err)
		}
		if len(prefixed) == 0 {
			t.Fatal("GetLogs(prefixed) returned no logs, want at least 1")
		}
		bare, err := database.GetLogs(bareDependent, 0)
		if err != nil {
			t.Fatalf("GetLogs(bare) failed: %v", err)
		}
		if len(bare) != len(prefixed) {
			t.Errorf("bare id returned %d logs, prefixed returned %d; the two must agree",
				len(bare), len(prefixed))
		}
	})
}

// stripPrefix returns the id without its td- prefix, which is what a user types
// when they copy an id out of a listing and drop the prefix.
func stripPrefix(t *testing.T, id string) string {
	t.Helper()
	if len(id) <= len(idPrefix) || id[:len(idPrefix)] != idPrefix {
		t.Fatalf("expected %q to carry the %q prefix", id, idPrefix)
	}
	return id[len(idPrefix):]
}
