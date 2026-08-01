package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
)

// staleEnvelope is the --json shape emitted by `td unstart --stale`.
type staleEnvelope struct {
	Action string `json:"action"`
	Stale  string `json:"stale"`
	Forced bool   `json:"forced"`
	Count  int    `json:"count"`
	Claims []struct {
		ID           string `json:"id"`
		Session      string `json:"session"`
		IdleSeconds  int64  `json:"idle_seconds"`
		Idle         string `json:"idle"`
		LastActivity string `json:"last_activity"`
	} `json:"claims"`
	Unresolved []struct {
		ID      string `json:"id"`
		Session string `json:"session"`
		Reason  string `json:"reason"`
	} `json:"unresolved"`
}

// setupStaleTest builds a db with one issue claimed by a long-idle session and
// one claimed by a session that is still active, plus the caller's own session.
func setupStaleTest(t *testing.T) (*db.DB, *models.Issue, *models.Issue) {
	t.Helper()
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_stale_supervisor")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := session.GetOrCreate(database); err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	newHolder := func(id string, idle time.Duration) {
		t.Helper()
		row := &db.SessionRow{
			ID:           id,
			Branch:       "main",
			AgentType:    "claude-code",
			StartedAt:    time.Now().Add(-24 * time.Hour),
			LastActivity: time.Now().Add(-idle),
		}
		if err := database.UpsertSession(row); err != nil {
			t.Fatalf("UpsertSession failed: %v", err)
		}
	}
	newHolder("ses_dead_tick", 3*time.Hour)
	newHolder("ses_live_tick", 5*time.Minute)

	claim := func(title, holder string) *models.Issue {
		t.Helper()
		issue := &models.Issue{Title: title, Type: models.TypeTask, Status: models.StatusInProgress}
		if err := database.CreateIssue(issue); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		issue.Status = models.StatusInProgress
		issue.ImplementerSession = holder
		if err := database.UpdateIssue(issue); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}
		return issue
	}

	dead := claim("Claimed by a killed tick", "ses_dead_tick")
	live := claim("Claimed by a working tick", "ses_live_tick")

	setStaleFlags(t, "", "false")
	return database, dead, live
}

func setStaleFlags(t *testing.T, stale, force string) {
	t.Helper()
	setWorkflowExitFlag(t, unstartCmd, "stale", stale)
	setWorkflowExitFlag(t, unstartCmd, "force", force)
}

func runStale(t *testing.T, wantErr bool) string {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = unstartCmd.RunE(unstartCmd, nil)
	})
	if wantErr && err == nil {
		t.Fatalf("expected failure, got success. output: %q", out)
	}
	if !wantErr && err != nil {
		t.Fatalf("unstart --stale failed: %v (output %q)", err, out)
	}
	return out
}

// TestUnstartStalePreviewDoesNotRelease is the safety property: preview is the
// default and mutates nothing.
func TestUnstartStalePreviewDoesNotRelease(t *testing.T) {
	database, dead, live := setupStaleTest(t)
	setStaleFlags(t, "2h", "false")
	setJSONFlag(t, false)

	out := runStale(t, false)

	if !strings.Contains(out, "Will release 1 stale claim") || !strings.Contains(out, dead.ID) {
		t.Fatalf("unexpected preview output: %q", out)
	}
	if strings.Contains(out, live.ID) {
		t.Fatalf("preview must not name the live session's claim: %q", out)
	}
	for _, id := range []string{dead.ID, live.ID} {
		got, err := database.GetIssue(id)
		if err != nil {
			t.Fatalf("GetIssue: %v", err)
		}
		if got.Status != models.StatusInProgress || got.ImplementerSession == "" {
			t.Fatalf("preview released %s: status=%s implementer=%q", id, got.Status, got.ImplementerSession)
		}
	}
}

// TestUnstartStaleForceReleasesOnlyTheDeadClaim is the core behavior: a claim
// held by an idle session is released, a recently-active session's claim is
// untouched, and the reason recorded on the released issue names the cause.
func TestUnstartStaleForceReleasesOnlyTheDeadClaim(t *testing.T) {
	database, dead, live := setupStaleTest(t)
	setStaleFlags(t, "2h", "true")
	setJSONFlag(t, false)

	out := runStale(t, false)

	if !strings.Contains(out, "RELEASED "+dead.ID) || !strings.Contains(out, "Released 1 stale claim") {
		t.Fatalf("unexpected force output: %q", out)
	}

	gotDead, err := database.GetIssue(dead.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if gotDead.Status != models.StatusOpen || gotDead.ImplementerSession != "" {
		t.Fatalf("dead claim not released: status=%s implementer=%q", gotDead.Status, gotDead.ImplementerSession)
	}

	gotLive, err := database.GetIssue(live.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if gotLive.Status != models.StatusInProgress || gotLive.ImplementerSession != "ses_live_tick" {
		t.Fatalf("live claim was disturbed: status=%s implementer=%q", gotLive.Status, gotLive.ImplementerSession)
	}

	logs, err := database.GetLogs(dead.ID, 10)
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	var found bool
	for _, l := range logs {
		if strings.Contains(l.Message, "claim released: implementer session ses_dead_tick idle") {
			found = true
		}
	}
	if !found {
		t.Fatalf("release reason missing from issue history: %+v", logs)
	}
}

// TestUnstartStaleJSONShape asserts callers can log exactly what happened.
func TestUnstartStaleJSONShape(t *testing.T) {
	database, dead, _ := setupStaleTest(t)

	// An in_progress issue whose holder no longer has a session row cannot be
	// measured, so it must be reported rather than released.
	orphan := &models.Issue{Title: "Orphaned claim", Type: models.TypeTask, Status: models.StatusInProgress}
	if err := database.CreateIssue(orphan); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	orphan.Status = models.StatusInProgress
	orphan.ImplementerSession = "ses_vanished"
	if err := database.UpdateIssue(orphan); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	setStaleFlags(t, "2h", "false")
	setJSONFlag(t, true)

	var preview staleEnvelope
	previewOut := runStale(t, false)
	if err := json.Unmarshal([]byte(previewOut), &preview); err != nil {
		t.Fatalf("preview output is not valid JSON: %v\noutput: %q", err, previewOut)
	}
	if preview.Action != "would_release_stale_claims" || preview.Forced || preview.Stale != "2h" {
		t.Fatalf("unexpected preview envelope: %q", previewOut)
	}
	if preview.Count != 1 || len(preview.Claims) != 1 {
		t.Fatalf("expected exactly one previewed claim: %q", previewOut)
	}
	claim := preview.Claims[0]
	if claim.ID != dead.ID || claim.Session != "ses_dead_tick" {
		t.Fatalf("claim does not identify issue + holder: %+v", claim)
	}
	if claim.IdleSeconds < 10500 || claim.IdleSeconds > 11100 {
		t.Fatalf("idle_seconds = %d, want ~10800", claim.IdleSeconds)
	}
	if _, err := time.Parse(time.RFC3339, claim.LastActivity); err != nil {
		t.Fatalf("last_activity %q is not RFC3339: %v", claim.LastActivity, err)
	}
	if len(preview.Unresolved) != 1 || preview.Unresolved[0].ID != orphan.ID {
		t.Fatalf("orphaned claim must be reported as unresolved: %q", previewOut)
	}

	setStaleFlags(t, "2h", "true")
	var forced staleEnvelope
	forceOut := runStale(t, false)
	if err := json.Unmarshal([]byte(forceOut), &forced); err != nil {
		t.Fatalf("force output is not valid JSON: %v\noutput: %q", err, forceOut)
	}
	if forced.Action != "released_stale_claims" || !forced.Forced || forced.Count != 1 {
		t.Fatalf("unexpected force envelope: %q", forceOut)
	}
	if forced.Claims[0].ID != dead.ID {
		t.Fatalf("released claim mismatch: %q", forceOut)
	}

	gotOrphan, err := database.GetIssue(orphan.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if gotOrphan.Status != models.StatusInProgress {
		t.Fatalf("unmeasurable claim must not be released, got %s", gotOrphan.Status)
	}
}

// TestUnstartStaleNoMatchesIsSuccess: nothing to reclaim is not a failure.
func TestUnstartStaleNoMatchesIsSuccess(t *testing.T) {
	setupStaleTest(t)
	setStaleFlags(t, "30d", "true")
	setJSONFlag(t, true)

	out := runStale(t, false)

	var env staleEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Count != 0 || len(env.Claims) != 0 {
		t.Fatalf("expected no claims: %q", out)
	}
}

// TestUnstartStaleNeverReleasesTheCallersOwnClaim guards the case where the
// supervisor session itself holds an issue and has not touched td recently.
func TestUnstartStaleNeverReleasesTheCallersOwnClaim(t *testing.T) {
	database, _, _ := setupStaleTest(t)

	sess, err := session.GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	mine := &models.Issue{Title: "My own claim", Type: models.TypeTask, Status: models.StatusInProgress}
	if err := database.CreateIssue(mine); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	mine.Status = models.StatusInProgress
	mine.ImplementerSession = sess.ID
	if err := database.UpdateIssue(mine); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}
	if err := database.UpdateSessionActivity(sess.ID, time.Now().Add(-9*time.Hour)); err != nil {
		t.Fatalf("UpdateSessionActivity failed: %v", err)
	}

	setStaleFlags(t, "1h", "true")
	setJSONFlag(t, true)

	out := runStale(t, false)
	if strings.Contains(out, mine.ID) {
		t.Fatalf("the calling session's own claim must never be released: %q", out)
	}
	got, err := database.GetIssue(mine.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != models.StatusInProgress {
		t.Fatalf("own claim was released: %s", got.Status)
	}
}

// TestUnstartStaleRejectsBadInput covers the guardrails around the threshold.
func TestUnstartStaleRejectsBadInput(t *testing.T) {
	setupStaleTest(t)
	setJSONFlag(t, false)

	setStaleFlags(t, "banana", "false")
	runStale(t, true)

	setStaleFlags(t, "0s", "true")
	runStale(t, true)
}

// TestUnstartStaleArgsValidation: --stale takes no ids, and --force is
// meaningless without it.
func TestUnstartStaleArgsValidation(t *testing.T) {
	setupStaleTest(t)

	setStaleFlags(t, "2h", "false")
	if err := unstartCmd.Args(unstartCmd, []string{"td-abc1"}); err == nil {
		t.Fatal("--stale with issue ids must be rejected")
	}
	if err := unstartCmd.Args(unstartCmd, nil); err != nil {
		t.Fatalf("--stale with no ids must be accepted: %v", err)
	}

	setStaleFlags(t, "", "true")
	if err := unstartCmd.Args(unstartCmd, []string{"td-abc1"}); err == nil {
		t.Fatal("--force without --stale must be rejected")
	}

	setStaleFlags(t, "", "false")
	if err := unstartCmd.Args(unstartCmd, nil); err == nil {
		t.Fatal("plain unstart with no ids must be rejected")
	}
}
