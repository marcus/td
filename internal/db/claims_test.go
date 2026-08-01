package db

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func claimTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := Initialize(t.TempDir())
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func newClaimedIssue(t *testing.T, database *DB, title, holder string) *models.Issue {
	t.Helper()
	issue := &models.Issue{Title: title, Type: models.TypeTask, Status: models.StatusInProgress}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	issue.Status = models.StatusInProgress
	issue.ImplementerSession = holder
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	return issue
}

// TestReleaseClaimsSkipsAClaimThatMoved is the concurrent-`td start` case. A
// sweep picks its targets from a snapshot taken before it holds the write
// lock; between the snapshot and the write, an agent can legitimately start
// the issue. A blind write revokes that brand-new claim and reports success.
func TestReleaseClaimsSkipsAClaimThatMoved(t *testing.T) {
	database := claimTestDB(t)
	issue := newClaimedIssue(t, database, "swept while restarting", "ses_dead")

	// The concurrent `td start`: a different session now holds the claim.
	fresh, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	fresh.ImplementerSession = "ses_live"
	if err := database.UpdateIssue(fresh); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	outcomes, err := database.ReleaseClaims([]ClaimRelease{{
		IssueID: issue.ID, ExpectedHolder: "ses_dead", LogMessage: "stale sweep",
	}}, "ses_supervisor")
	if err != nil {
		t.Fatalf("ReleaseClaims: %v", err)
	}
	if outcomes[0].Released {
		t.Fatal("a claim that moved to another session must not be reported as released")
	}
	if !outcomes[0].Skipped {
		t.Fatalf("expected a skip, got %+v", outcomes[0])
	}
	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != models.StatusInProgress || got.ImplementerSession != "ses_live" {
		t.Fatalf("the live claim was revoked: status=%s implementer=%q", got.Status, got.ImplementerSession)
	}
}

// TestReleaseClaimsDoesNotRevertConcurrentEdits: the release must write only
// the fields it owns. Writing a full row from a pre-lock snapshot silently
// reverted an unrelated `td update --title` that landed in between — and that
// command had already reported success.
func TestReleaseClaimsDoesNotRevertConcurrentEdits(t *testing.T) {
	database := claimTestDB(t)
	issue := newClaimedIssue(t, database, "original title", "ses_dead")
	snapshot := *issue // what a sweep captured before taking the lock

	edited, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	edited.Title = "retitled by another agent"
	edited.Description = "and described"
	if err := database.UpdateIssueLogged(edited, "ses_editor", models.ActionUpdate); err != nil {
		t.Fatalf("UpdateIssueLogged: %v", err)
	}

	outcomes, err := database.ReleaseClaims([]ClaimRelease{{
		IssueID: snapshot.ID, ExpectedHolder: "ses_dead", LogMessage: "stale sweep",
	}}, "ses_supervisor")
	if err != nil {
		t.Fatalf("ReleaseClaims: %v", err)
	}
	if !outcomes[0].Released {
		t.Fatalf("expected release, got %+v", outcomes[0])
	}

	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Title != "retitled by another agent" || got.Description != "and described" {
		t.Fatalf("concurrent edit was reverted: title=%q description=%q", got.Title, got.Description)
	}
	if got.Status != models.StatusOpen || got.ImplementerSession != "" {
		t.Fatalf("claim not released: status=%s implementer=%q", got.Status, got.ImplementerSession)
	}
}

// TestReleaseClaimsReleasesAnOpenButHeldIssue: `td reopen` used to leave an
// issue open with its implementer still set. That claim is releasable.
func TestReleaseClaimsReleasesAnOpenButHeldIssue(t *testing.T) {
	database := claimTestDB(t)
	issue := newClaimedIssue(t, database, "open but held", "ses_ghost")
	issue.Status = models.StatusOpen
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	outcomes, err := database.ReleaseClaims([]ClaimRelease{{IssueID: issue.ID, LogMessage: "released"}}, "ses_supervisor")
	if err != nil {
		t.Fatalf("ReleaseClaims: %v", err)
	}
	if !outcomes[0].Released {
		t.Fatalf("expected release, got %+v", outcomes[0])
	}
	got, _ := database.GetIssue(issue.ID)
	if got.ImplementerSession != "" {
		t.Fatalf("claim not cleared: %q", got.ImplementerSession)
	}
}

// TestReleaseClaimsBatchDoesNotStarveAConcurrentStart is finding 4. The
// per-issue path took and released the cross-process write lock about four
// times per issue; against a 500ms lock timeout, a large sweep made a real
// `td start` fail outright, which kills a harness tick. One batch = one
// acquisition, so a writer racing the sweep waits once and succeeds.
func TestReleaseClaimsBatchDoesNotStarveAConcurrentStart(t *testing.T) {
	database := claimTestDB(t)

	const claims = 300
	reqs := make([]ClaimRelease, 0, claims)
	for i := range claims {
		issue := newClaimedIssue(t, database, fmt.Sprintf("swept %d", i), "ses_dead")
		reqs = append(reqs, ClaimRelease{IssueID: issue.ID, ExpectedHolder: "ses_dead", LogMessage: "stale sweep"})
	}
	// The issue the live agent is about to start.
	target := &models.Issue{Title: "picked up mid-sweep", Type: models.TypeTask}
	if err := database.CreateIssue(target); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	var wg sync.WaitGroup
	var sweepErr, startErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, sweepErr = database.ReleaseClaims(reqs, "ses_supervisor")
	}()
	go func() {
		defer wg.Done()
		time.Sleep(2 * time.Millisecond) // let the sweep take the lock first
		started, err := database.GetIssue(target.ID)
		if err != nil {
			startErr = err
			return
		}
		started.Status = models.StatusInProgress
		started.ImplementerSession = "ses_live"
		startErr = database.UpdateIssueLoggedIfStatus(started, models.StatusOpen, "ses_live", models.ActionStart)
	}()
	wg.Wait()

	if sweepErr != nil {
		t.Fatalf("sweep failed: %v", sweepErr)
	}
	if startErr != nil {
		t.Fatalf("a live agent's start failed during the sweep: %v", startErr)
	}
	got, err := database.GetIssue(target.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != models.StatusInProgress || got.ImplementerSession != "ses_live" {
		t.Fatalf("the concurrent start did not stick: status=%s implementer=%q", got.Status, got.ImplementerSession)
	}
}

// TestLatestIssueActivitySeesWorkByAnySession: the evidence that an agent is
// alive is the writes it makes, whichever session row they are recorded
// against — that is the whole point when a rotated agent's claim still names
// its previous session.
func TestLatestIssueActivitySeesWorkByAnySession(t *testing.T) {
	database := claimTestDB(t)
	issue := newClaimedIssue(t, database, "worked on", "ses_old")

	if err := database.AddLog(&models.Log{
		IssueID: issue.ID, SessionID: "ses_new", Message: "still here", Type: models.LogTypeProgress,
	}); err != nil {
		t.Fatalf("AddLog: %v", err)
	}

	got, err := database.LatestIssueActivity([]string{issue.ID})
	if err != nil {
		t.Fatalf("LatestIssueActivity: %v", err)
	}
	ts, ok := got[issue.ID]
	if !ok {
		t.Fatal("no activity recorded for an issue that was just logged against")
	}
	if time.Since(ts) > time.Minute {
		t.Fatalf("activity timestamp looks stale: %v", ts)
	}
	if _, ok := got["td-nosuch"]; ok {
		t.Fatal("unknown ids must not appear in the result")
	}
}

// TestSessionsHoldingClaimsCountsOnlyLiveClaims backs the cleanup guard.
func TestSessionsHoldingClaimsCountsOnlyLiveClaims(t *testing.T) {
	database := claimTestDB(t)
	newClaimedIssue(t, database, "held a", "ses_holder")
	newClaimedIssue(t, database, "held b", "ses_holder")
	open := newClaimedIssue(t, database, "released", "ses_holder")
	if _, err := database.ReleaseClaims([]ClaimRelease{{IssueID: open.ID}}, "ses_supervisor"); err != nil {
		t.Fatalf("ReleaseClaims: %v", err)
	}

	held, err := database.SessionsHoldingClaims()
	if err != nil {
		t.Fatalf("SessionsHoldingClaims: %v", err)
	}
	if held["ses_holder"] != 2 {
		t.Fatalf("expected 2 held claims, got %d (%v)", held["ses_holder"], held)
	}
}
