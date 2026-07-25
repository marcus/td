package db

import (
	"testing"

	"github.com/marcus/td/internal/models"
)

// Regression coverage for td-406d65: `td search` advertised logs and handoffs
// but the SQL only matched id, title and description, so material recorded
// against an issue was invisible with no signal that it had not been searched.

func searchScopeFixture(t *testing.T) (*DB, string) {
	t.Helper()
	database, err := Initialize(t.TempDir())
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	issue := &models.Issue{
		Title:       "Unrelated title",
		Description: "Unrelated description",
		Status:      models.StatusOpen,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if err := database.AddLog(&models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   "ratelimiter needs a token bucket",
		Type:      models.LogTypeProgress,
	}); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	if err := database.AddHandoff(&models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"wired up the backoff"},
		Remaining: []string{"decide the jitter strategy"},
		Decisions: []string{"chose exponential over linear"},
		Uncertain: []string{"unsure about the retry budget"},
	}); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	// A second issue with nothing recorded against it, so scope widening does
	// not simply return everything.
	other := &models.Issue{Title: "Second issue", Status: models.StatusOpen}
	if err := database.CreateIssue(other); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	return database, issue.ID
}

func TestSearchIssuesRankedCoversLogsAndHandoffs(t *testing.T) {
	database, issueID := searchScopeFixture(t)

	tests := []struct {
		name       string
		query      string
		wantField  string
		wantMatch  bool
		wantIssues int
	}{
		{"log message", "token bucket", "log", true, 1},
		{"handoff done", "wired up the backoff", "handoff", true, 1},
		{"handoff remaining", "jitter strategy", "handoff", true, 1},
		{"handoff decisions", "exponential over linear", "handoff", true, 1},
		{"handoff uncertain", "retry budget", "handoff", true, 1},
		{"title still wins", "Unrelated title", "title", true, 1},
		{"absent everywhere", "kubernetes operator", "", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := database.SearchIssuesRanked(tt.query, ListIssuesOptions{Limit: 50})
			if err != nil {
				t.Fatalf("SearchIssuesRanked(%q) failed: %v", tt.query, err)
			}
			if len(results) != tt.wantIssues {
				t.Fatalf("SearchIssuesRanked(%q) returned %d results, want %d", tt.query, len(results), tt.wantIssues)
			}
			if !tt.wantMatch {
				return
			}
			if results[0].Issue.ID != issueID {
				t.Errorf("matched %s, want %s", results[0].Issue.ID, issueID)
			}
			if results[0].MatchField != tt.wantField {
				t.Errorf("MatchField = %q, want %q", results[0].MatchField, tt.wantField)
			}
		})
	}
}

// TestSearchRankingPrefersIssueText: activity matches are real matches but must
// not outrank the issue's own title or description.
func TestSearchRankingPrefersIssueText(t *testing.T) {
	database, _ := searchScopeFixture(t)

	titled := &models.Issue{Title: "token bucket rewrite", Status: models.StatusOpen}
	if err := database.CreateIssue(titled); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	results, err := database.SearchIssuesRanked("token bucket", ListIssuesOptions{Limit: 50})
	if err != nil {
		t.Fatalf("SearchIssuesRanked failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Issue.ID != titled.ID {
		t.Errorf("first result is %s, want the title match %s", results[0].Issue.ID, titled.ID)
	}
	if results[0].Score <= results[1].Score {
		t.Errorf("title match scored %d, activity match scored %d; title must rank higher",
			results[0].Score, results[1].Score)
	}
}

// TestListIssuesSearchStaysNarrow: only the search path widens. `td list
// --search` and everything else keep matching the issue's own fields.
func TestListIssuesSearchStaysNarrow(t *testing.T) {
	database, _ := searchScopeFixture(t)

	narrow, err := database.ListIssues(ListIssuesOptions{Search: "token bucket"})
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}
	if len(narrow) != 0 {
		t.Errorf("ListIssues without SearchActivity returned %d issues, want 0", len(narrow))
	}

	wide, err := database.ListIssues(ListIssuesOptions{Search: "token bucket", SearchActivity: true})
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}
	if len(wide) != 1 {
		t.Errorf("ListIssues with SearchActivity returned %d issues, want 1", len(wide))
	}
}

// TestHandoffFieldsAreBlobs documents why the search SQL casts: handoff content
// is marshalled JSON written as []byte, so SQLite stores it with BLOB affinity
// and a bare LIKE never matches it.
func TestHandoffFieldsAreBlobs(t *testing.T) {
	database, issueID := searchScopeFixture(t)

	var affinity string
	if err := database.conn.QueryRow(
		`SELECT typeof(done) FROM handoffs WHERE issue_id = ?`, issueID).Scan(&affinity); err != nil {
		t.Fatalf("typeof query failed: %v", err)
	}

	var bare, cast int
	if err := database.conn.QueryRow(
		`SELECT count(*) FROM handoffs WHERE done LIKE '%backoff%'`).Scan(&bare); err != nil {
		t.Fatalf("bare LIKE query failed: %v", err)
	}
	if err := database.conn.QueryRow(
		`SELECT count(*) FROM handoffs WHERE CAST(done AS TEXT) LIKE '%backoff%'`).Scan(&cast); err != nil {
		t.Fatalf("cast LIKE query failed: %v", err)
	}

	if cast != 1 {
		t.Fatalf("CAST(done AS TEXT) LIKE matched %d rows, want 1", cast)
	}
	if affinity == "blob" && bare != 0 {
		t.Logf("bare LIKE now matches BLOB handoff content (typeof=%s); the CAST is harmless but no longer load-bearing", affinity)
	}
}
