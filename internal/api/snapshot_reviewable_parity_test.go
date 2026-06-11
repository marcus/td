package api

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestSnapshotReviewableParity_TrustedMode asserts that the snapshot-backed
// read path (SnapshotQuerySource.ListIssues) returns the exact same set of
// "reviewable by you" issues as the live DB path (db.DB.ListIssues) under
// trusted mode. Both surfaces route ReviewableBy through
// db.ReviewableByFilterForMode, so once the composer learns trusted mode this
// parity must hold automatically — this test guards against drift.
//
// Trusted mode is the interesting case because its reviewable set is broader
// than delegated: it includes self-implemented in_review issues (the
// implementer-independence requirement is enforced at action time via
// --self-review, not at query time).
func TestSnapshotReviewableParity_TrustedMode(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}

	// Seed a fixture set exercising the trusted reviewable filter:
	//   A — self-implemented in_review (trusted INCLUDES; delegated excludes)
	//   B — other-implemented in_review (included in all modes)
	//   C — open issue (never reviewable)
	//   D — closed issue (never reviewable)
	seed := func(title string, status models.Status, impl string) string {
		iss := &models.Issue{Title: title, Type: models.TypeTask, Status: models.StatusOpen}
		if err := database.CreateIssue(iss); err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		iss.Status = status
		iss.ImplementerSession = impl
		if err := database.UpdateIssue(iss); err != nil {
			t.Fatalf("update %s: %v", title, err)
		}
		return iss.ID
	}

	const session = "ses-self"
	aID := seed("A self-implemented in_review", models.StatusInReview, session)
	bID := seed("B other-implemented in_review", models.StatusInReview, "ses-other")
	_ = seed("C open", models.StatusOpen, session)
	_ = seed("D closed", models.StatusClosed, session)

	opts := db.ListIssuesOptions{
		ReviewableBy:     session,
		ReviewPolicyMode: "trusted",
	}

	// Live path.
	liveIssues, err := database.ListIssues(opts)
	if err != nil {
		t.Fatalf("live ListIssues: %v", err)
	}
	liveIDs := idSet(liveIssues)

	// Sanity: trusted reviewable includes the self-implemented in_review issue.
	if !liveIDs[aID] {
		t.Fatalf("live trusted reviewable missing self-implemented issue %s; got %v", aID, sortedKeys(liveIDs))
	}
	if !liveIDs[bID] {
		t.Fatalf("live trusted reviewable missing other-implemented issue %s; got %v", bID, sortedKeys(liveIDs))
	}

	// Flush WAL so the snapshot connection sees the seeded rows.
	if err := database.Close(); err != nil {
		t.Fatalf("close live db: %v", err)
	}

	// Snapshot path: open the same file as a raw *sql.DB and query through the
	// SnapshotQuerySource, which inherits the same mode-aware composer.
	dbPath := filepath.Join(dir, ".todos", "issues.db")
	sqlDB, err := db.OpenSQLite(dbPath, db.OpenOptions{})
	if err != nil {
		t.Fatalf("open snapshot sql.DB: %v", err)
	}
	defer sqlDB.Close()

	snapIssues, err := NewSnapshotQuerySource(sqlDB).ListIssues(opts)
	if err != nil {
		t.Fatalf("snapshot ListIssues: %v", err)
	}
	snapIDs := idSet(snapIssues)

	if !equalSets(liveIDs, snapIDs) {
		t.Fatalf("snapshot/live trusted reviewable mismatch:\n  live=%v\n  snap=%v",
			sortedKeys(liveIDs), sortedKeys(snapIDs))
	}
}

func idSet(issues []models.Issue) map[string]bool {
	m := make(map[string]bool, len(issues))
	for _, iss := range issues {
		m[iss.ID] = true
	}
	return m
}

func equalSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
