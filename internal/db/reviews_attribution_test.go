package db

import (
	"database/sql"
	"strings"
	"testing"
)

// TestMigration37_ReviewedByColumn asserts migration 37 lands the attribution
// column with the right shape: present, TEXT, and defaulting to the empty string rather than
// NULL so it reads into a plain Go string on every path.
func TestMigration37_ReviewedByColumn(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	has, err := database.columnExists("issue_reviews", "reviewed_by")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if !has {
		t.Fatal("issue_reviews.reviewed_by column missing after migrations")
	}

	// Pin the DECLARED shape, not just the observed INSERT behavior. An
	// independent review of this change mutated the ALTER to a bare
	// `ADD COLUMN reviewed_by TEXT` and the entire suite still passed, because
	// every test either inserted a value or scanned through sql.NullString.
	//
	// The declaration matters beyond tidiness: sync's getTextEmptyDefaultColumns
	// (internal/sync/events.go) keys on the PRAGMA default literal being
	// exactly "''" to decide whether to coerce an inbound JSON null to "". A
	// nullable column silently drops out of that set, and a peer sending
	// reviewed_by:null then writes NULL into a column the readers scan as a
	// plain string.
	var (
		gotType    string
		gotNotNull int
		gotDefault sql.NullString
		found      bool
	)
	rows, err := database.conn.Query(`PRAGMA table_info(issue_reviews)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == "reviewed_by" {
			gotType, gotNotNull, gotDefault, found = ctype, notnull, dflt, true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if !found {
		t.Fatal("reviewed_by absent from PRAGMA table_info")
	}
	if !strings.EqualFold(gotType, "TEXT") {
		t.Errorf("reviewed_by type = %q, want TEXT", gotType)
	}
	if gotNotNull != 1 {
		t.Error("reviewed_by must be declared NOT NULL")
	}
	if !gotDefault.Valid || gotDefault.String != "''" {
		t.Errorf("reviewed_by default = %q (valid=%v), want the literal '' — sync's null-coercion keys on exactly this",
			gotDefault.String, gotDefault.Valid)
	}

	// A row written without an attribution must default to '' (not NULL).
	seedIssueForReviewTests(t, database, "td-rbdef")
	id, err := database.CreateIssueReview(NewReview{IssueID: "td-rbdef", ReviewerSession: "ses-r", Decision: "approved"})
	if err != nil {
		t.Fatalf("CreateIssueReview: %v", err)
	}
	var reviewedBy string
	var isNull bool
	if err := database.conn.QueryRow(
		`SELECT reviewed_by, reviewed_by IS NULL FROM issue_reviews WHERE id = ?`, id,
	).Scan(&reviewedBy, &isNull); err != nil {
		t.Fatalf("scan reviewed_by: %v", err)
	}
	if isNull {
		t.Error("reviewed_by must default to '' rather than NULL")
	}
	if reviewedBy != "" {
		t.Errorf("default reviewed_by: want empty, got %q", reviewedBy)
	}
}

// TestMigration37_Idempotent proves the columnExists guard holds: re-running
// the migration on a database that already has the column is a no-op rather
// than a duplicate-column error. Cross-version databases re-open cleanly.
func TestMigration37_Idempotent(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	for i := 0; i < 2; i++ {
		if err := database.migrateReviewedByColumn(); err != nil {
			t.Fatalf("re-run %d of migrateReviewedByColumn: %v", i, err)
		}
	}
}

// TestCreateIssueReview_ReviewedByRoundTrips asserts the attribution survives
// both read paths, and that the three record shapes stay distinguishable —
// which is the whole point of adding a column instead of overloading
// self_review.
func TestCreateIssueReview_ReviewedByRoundTrips(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	cases := []struct {
		name           string
		issueID        string
		selfReview     bool
		reviewedBy     string
		wantSelfReview bool
		wantReviewedBy string
	}{
		{
			name:    "independent session, no attribution",
			issueID: "td-attr1",
		},
		{
			name:           "involved session reviewed its own work",
			issueID:        "td-attr2",
			selfReview:     true,
			wantSelfReview: true,
		},
		{
			name:           "involved session, review credited to a sub-agent",
			issueID:        "td-attr3",
			selfReview:     true,
			reviewedBy:     "code-reviewer sub-agent",
			wantSelfReview: true,
			wantReviewedBy: "code-reviewer sub-agent",
		},
		{
			name:           "independent session crediting someone else",
			issueID:        "td-attr4",
			reviewedBy:     "human reviewer",
			wantReviewedBy: "human reviewer",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			seedIssueForReviewTests(t, database, tc.issueID)
			if _, err := database.CreateIssueReview(NewReview{
				IssueID:         tc.issueID,
				ReviewerSession: "ses-recorder",
				Decision:        "approved",
				Summary:         "ok",
				SelfReview:      tc.selfReview,
				ReviewedBy:      tc.reviewedBy,
			}); err != nil {
				t.Fatalf("CreateIssueReview: %v", err)
			}

			active, err := database.GetActiveApprovalReview(tc.issueID)
			if err != nil || active == nil {
				t.Fatalf("GetActiveApprovalReview: %v (row=%v)", err, active)
			}
			if active.SelfReview != tc.wantSelfReview {
				t.Errorf("GetActiveApprovalReview SelfReview = %v, want %v", active.SelfReview, tc.wantSelfReview)
			}
			if active.ReviewedBy != tc.wantReviewedBy {
				t.Errorf("GetActiveApprovalReview ReviewedBy = %q, want %q", active.ReviewedBy, tc.wantReviewedBy)
			}

			list, err := database.ListIssueReviews(tc.issueID)
			if err != nil || len(list) != 1 {
				t.Fatalf("ListIssueReviews: %v (n=%d)", err, len(list))
			}
			if list[0].SelfReview != tc.wantSelfReview {
				t.Errorf("ListIssueReviews SelfReview = %v, want %v", list[0].SelfReview, tc.wantSelfReview)
			}
			if list[0].ReviewedBy != tc.wantReviewedBy {
				t.Errorf("ListIssueReviews ReviewedBy = %q, want %q", list[0].ReviewedBy, tc.wantReviewedBy)
			}
		})
	}
}

// TestReviewedBy_PreExistingRowsUnaffected simulates a database written before
// migration 37: the column is dropped back off, a row is inserted the old way,
// and the migration is re-applied. The pre-existing row must come back with an
// empty attribution and its original self_review value — the claim in the
// migration comment that no backfill is needed.
func TestReviewedBy_PreExistingRowsUnaffected(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	seedIssueForReviewTests(t, database, "td-preexist")

	if _, err := database.conn.Exec(`ALTER TABLE issue_reviews DROP COLUMN reviewed_by`); err != nil {
		t.Fatalf("drop reviewed_by to simulate a pre-37 database: %v", err)
	}
	if _, err := database.conn.Exec(`
		INSERT INTO issue_reviews (id, issue_id, reviewer_session, decision, summary, requested_by_session, created_at, self_review)
		VALUES ('rev-old', 'td-preexist', 'ses-impl', 'approved', 'legacy self-review', 'ses-impl', CURRENT_TIMESTAMP, 1)
	`); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	if err := database.migrateReviewedByColumn(); err != nil {
		t.Fatalf("migrateReviewedByColumn: %v", err)
	}

	active, err := database.GetActiveApprovalReview("td-preexist")
	if err != nil || active == nil {
		t.Fatalf("GetActiveApprovalReview: %v (row=%v)", err, active)
	}
	if active.ID != "rev-old" {
		t.Fatalf("got review %q, want the legacy row", active.ID)
	}
	if active.ReviewedBy != "" {
		t.Errorf("legacy row ReviewedBy = %q, want empty", active.ReviewedBy)
	}
	if !active.SelfReview {
		t.Error("legacy row must keep self_review=true")
	}
}
