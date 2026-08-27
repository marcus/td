package sync

import (
	"database/sql"
	"encoding/json"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// reviewsSchema mirrors internal/db's issue_reviews table after migration 37.
// It is deliberately a copy rather than an import: this package's tests are
// schema-free by design, and the point of these cases is how the generic apply
// path treats a newly-added TEXT DEFAULT ” column.
//
// The copy means these tests alone cannot catch a migration that declares the
// real column differently. That half is pinned by
// TestMigration37_ReviewedByColumn in internal/db, which asserts the PRAGMA
// default literal is exactly "”" — the value getTextEmptyDefaultColumns keys
// on. Read the two together: internal/db pins the declaration, this file pins
// the behavior that declaration buys.
const reviewsSchema = `CREATE TABLE issue_reviews (
	id                   TEXT PRIMARY KEY,
	issue_id             TEXT NOT NULL,
	reviewer_session     TEXT NOT NULL,
	decision             TEXT NOT NULL,
	summary              TEXT,
	requested_by_session TEXT,
	created_at           DATETIME,
	superseded_at        DATETIME,
	self_review          INTEGER NOT NULL DEFAULT 0,
	reviewed_by          TEXT NOT NULL DEFAULT ''
);`

var reviewsValidator EntityValidator = func(t string) bool { return t == "issue_reviews" }

func setupReviewsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(reviewsSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func reviewedByOf(t *testing.T, tx *sql.Tx, id string) (string, bool) {
	t.Helper()
	var v sql.NullString
	if err := tx.QueryRow(`SELECT reviewed_by FROM issue_reviews WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("scan reviewed_by: %v", err)
	}
	return v.String, v.Valid
}

// TestSyncReviewedBy_NewPeerPayloadApplies covers the forward direction: a
// sender that has migration 37 emits reviewed_by, and it lands intact.
func TestSyncReviewedBy_NewPeerPayloadApplies(t *testing.T) {
	db := setupReviewsDB(t)
	tx := beginTx(t, db)
	defer func() { _ = tx.Rollback() }()

	payload, _ := json.Marshal(map[string]any{
		"issue_id":         "td-a1",
		"reviewer_session": "ses-orchestrator",
		"decision":         "approved",
		"summary":          "sub-agent reviewed the diff",
		"self_review":      true,
		"reviewed_by":      "code-reviewer sub-agent",
	})
	if _, err := ApplyEvent(tx, Event{
		ActionType: "create",
		EntityType: "issue_reviews",
		EntityID:   "rev-1",
		Payload:    payload,
	}, reviewsValidator); err != nil {
		t.Fatalf("apply: %v", err)
	}

	got, _ := reviewedByOf(t, tx, "rev-1")
	if got != "code-reviewer sub-agent" {
		t.Errorf("reviewed_by = %q, want the attribution", got)
	}
	var selfReview int
	if err := tx.QueryRow(`SELECT self_review FROM issue_reviews WHERE id = ?`, "rev-1").Scan(&selfReview); err != nil {
		t.Fatalf("scan self_review: %v", err)
	}
	if selfReview != 1 {
		t.Errorf("self_review = %d, want 1 (attributed rows are still recorded by an involved session)", selfReview)
	}
}

// TestSyncReviewedBy_OldPeerPayloadApplies covers the backward direction: a
// peer running a pre-37 build emits a payload with no reviewed_by key at all.
// It must apply cleanly and leave the column at its ” default rather than
// failing or writing NULL.
func TestSyncReviewedBy_OldPeerPayloadApplies(t *testing.T) {
	db := setupReviewsDB(t)
	tx := beginTx(t, db)
	defer func() { _ = tx.Rollback() }()

	payload, _ := json.Marshal(map[string]any{
		"issue_id":         "td-a1",
		"reviewer_session": "ses-reviewer",
		"decision":         "approved",
		"summary":          "looks good",
		"self_review":      false,
	})
	if _, err := ApplyEvent(tx, Event{
		ActionType: "create",
		EntityType: "issue_reviews",
		EntityID:   "rev-old",
		Payload:    payload,
	}, reviewsValidator); err != nil {
		t.Fatalf("apply from pre-37 peer: %v", err)
	}

	got, valid := reviewedByOf(t, tx, "rev-old")
	if !valid {
		t.Error("reviewed_by must not be NULL when the sender omits it")
	}
	if got != "" {
		t.Errorf("reviewed_by = %q, want empty", got)
	}
}

// TestSyncReviewedBy_ExplicitNullCoerced pins the getTextEmptyDefaultColumns
// behavior for the new column: a sender that serializes the empty attribution
// as JSON null must not write NULL into a NOT NULL column, or readers scanning
// into a plain string break.
func TestSyncReviewedBy_ExplicitNullCoerced(t *testing.T) {
	db := setupReviewsDB(t)
	tx := beginTx(t, db)
	defer func() { _ = tx.Rollback() }()

	payload, _ := json.Marshal(map[string]any{
		"issue_id":         "td-a1",
		"reviewer_session": "ses-reviewer",
		"decision":         "approved",
		"reviewed_by":      nil,
	})
	if _, err := ApplyEvent(tx, Event{
		ActionType: "create",
		EntityType: "issue_reviews",
		EntityID:   "rev-null",
		Payload:    payload,
	}, reviewsValidator); err != nil {
		t.Fatalf("apply with null reviewed_by: %v", err)
	}

	got, valid := reviewedByOf(t, tx, "rev-null")
	if !valid {
		t.Error("explicit null reviewed_by must be coerced to '' for a NOT NULL TEXT column")
	}
	if got != "" {
		t.Errorf("reviewed_by = %q, want empty", got)
	}
}

// TestSyncReviewedBy_UnknownColumnFiltered is the peer-is-older case seen from
// the other side: a payload naming a column this database does not have must
// be filtered rather than failing the whole event. Without that, rolling out a
// future review column would break sync for everyone on the old build.
func TestSyncReviewedBy_UnknownColumnFiltered(t *testing.T) {
	db := setupReviewsDB(t)
	tx := beginTx(t, db)
	defer func() { _ = tx.Rollback() }()

	payload, _ := json.Marshal(map[string]any{
		"issue_id":              "td-a1",
		"reviewer_session":      "ses-reviewer",
		"decision":              "approved",
		"reviewed_by":           "future sub-agent",
		"reviewed_by_context":   "ses-future",
		"some_column_from_2027": "whatever",
	})
	if _, err := ApplyEvent(tx, Event{
		ActionType: "create",
		EntityType: "issue_reviews",
		EntityID:   "rev-future",
		Payload:    payload,
	}, reviewsValidator); err != nil {
		t.Fatalf("apply with unknown columns: %v", err)
	}

	got, _ := reviewedByOf(t, tx, "rev-future")
	if got != "future sub-agent" {
		t.Errorf("known column dropped alongside unknown ones: reviewed_by = %q", got)
	}
}
