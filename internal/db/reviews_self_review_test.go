package db

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcus/td/internal/models"
)

// TestMigration_SelfReviewColumnExistsWithDefaultZero asserts that running
// migrations on a fresh DB produces an issue_reviews.self_review column that
// defaults to 0 (Stage 2 migration v32).
func TestMigration_SelfReviewColumnExistsWithDefaultZero(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	has, err := database.columnExists("issue_reviews", "self_review")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if !has {
		t.Fatal("issue_reviews.self_review column missing after migrations")
	}

	// A review created without acknowledging self-review must default to 0.
	seedIssueForReviewTests(t, database, "td-srdef")
	id, err := database.CreateIssueReview("td-srdef", "ses-r", "approved", "", "", false)
	if err != nil {
		t.Fatalf("CreateIssueReview: %v", err)
	}
	var selfReview int
	if err := database.conn.QueryRow(`SELECT self_review FROM issue_reviews WHERE id = ?`, id).Scan(&selfReview); err != nil {
		t.Fatalf("scan self_review: %v", err)
	}
	if selfReview != 0 {
		t.Errorf("default self_review: want 0, got %d", selfReview)
	}
}

// TestCreateIssueReview_SelfReviewRoundTrips asserts that a self-review
// approval is persisted and surfaced through both read paths.
func TestCreateIssueReview_SelfReviewRoundTrips(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	seedIssueForReviewTests(t, database, "td-srtrue")
	if _, err := database.CreateIssueReview("td-srtrue", "ses-impl", "approved", "self-reviewed", "ses-impl", true); err != nil {
		t.Fatalf("CreateIssueReview: %v", err)
	}

	active, err := database.GetActiveApprovalReview("td-srtrue")
	if err != nil {
		t.Fatalf("GetActiveApprovalReview: %v", err)
	}
	if active == nil {
		t.Fatal("expected active approval review, got nil")
	}
	if !active.SelfReview {
		t.Error("GetActiveApprovalReview: SelfReview = false, want true")
	}

	list, err := database.ListIssueReviews("td-srtrue")
	if err != nil {
		t.Fatalf("ListIssueReviews: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListIssueReviews: want 1 row, got %d", len(list))
	}
	if !list[0].SelfReview {
		t.Error("ListIssueReviews: SelfReview = false, want true")
	}
}

// TestIssueReview_SelfReviewJSONRoundTrip asserts the snake_case json tag so
// the field survives any IssueReview serialization (export/import, action_log
// audit views).
func TestIssueReview_SelfReviewJSONRoundTrip(t *testing.T) {
	in := models.IssueReview{ID: "rv-1", IssueID: "td-1", Decision: "approved", SelfReview: true}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"self_review":true`) {
		t.Errorf("marshaled JSON missing self_review:true: %s", b)
	}
	var out models.IssueReview
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.SelfReview {
		t.Error("SelfReview did not survive JSON round-trip")
	}
}

// TestMigration_SelfReviewIdempotent asserts re-opening an already-migrated DB
// does not error (the columnExists guard skips the ALTER).
func TestMigration_SelfReviewIdempotent(t *testing.T) {
	dir := t.TempDir()
	d1, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize (1): %v", err)
	}
	d1.Close()

	d2, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize (2, re-open): %v", err)
	}
	defer d2.Close()

	has, err := d2.columnExists("issue_reviews", "self_review")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if !has {
		t.Fatal("self_review column missing after re-open")
	}
}
