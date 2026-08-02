package serve

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// heldIssue creates an issue in the given status still carrying an implementer
// claim, using the unlogged writer so the fixture is not itself normalized.
func heldIssue(t *testing.T, database *db.DB, title string, status models.Status, holder string) *models.Issue {
	t.Helper()
	issue := &models.Issue{Title: title, Type: models.TypeTask}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	issue.Status = status
	issue.ImplementerSession = holder
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}
	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	return got
}

func assertOpenAndUnclaimed(t *testing.T, database *db.DB, id string) {
	t.Helper()
	got, err := database.GetIssue(id)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if got.Status != models.StatusOpen {
		t.Fatalf("status = %s, want open", got.Status)
	}
	if got.ImplementerSession != "" {
		t.Fatalf("implementer = %q, want empty: open issue still holds a claim", got.ImplementerSession)
	}
}

// TestUnblockReleasesClaim is the API half of td-cdbbbe. The CLI's `td unblock`
// was fixed under td-d2e612 but HandleUnblock had no clearing side effect at
// all, so the same transition left different state depending on the surface
// that performed it. Both now go through the shared logged-write release.
func TestUnblockReleasesClaim(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	issue := heldIssue(t, srv.db, "Blocked but still held", models.StatusBlocked, "ses_holder")

	resp, env := doJSON(t, ts, "POST", "/v1/issues/"+issue.ID+"/unblock", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (error: %+v)", resp.StatusCode, env.Error)
	}
	if !env.OK {
		t.Fatalf("ok = false, error = %+v", env.Error)
	}

	assertOpenAndUnclaimed(t, srv.db, issue.ID)
}

// TestReopenReleasesClaim is the API mirror of the `td reopen` fix from
// td-c45f99: HandleReopen cleared reviewer_session and closed_at but left the
// implementer claim on the reopened issue.
func TestReopenReleasesClaim(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	issue := heldIssue(t, srv.db, "Closed but still held", models.StatusClosed, "ses_holder")

	resp, env := doJSON(t, ts, "POST", "/v1/issues/"+issue.ID+"/reopen", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (error: %+v)", resp.StatusCode, env.Error)
	}
	if !env.OK {
		t.Fatalf("ok = false, error = %+v", env.Error)
	}

	assertOpenAndUnclaimed(t, srv.db, issue.ID)
}
