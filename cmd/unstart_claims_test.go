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

// claimFixture is a database with a supervisor session and helpers for
// building the fleet states the sweeps have to get right.
type claimFixture struct {
	database *db.DB
	baseDir  string
	sess     *session.Session
}

func setupClaimTest(t *testing.T) *claimFixture {
	t.Helper()
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_claim_supervisor")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	sess, err := session.GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	setStaleFlags(t, "", "false")
	setWorkflowExitFlag(t, unstartCmd, "reason", "")
	return &claimFixture{database: database, baseDir: dir, sess: sess}
}

// holder inserts a session row that last spoke to td `idle` ago.
func (f *claimFixture) holder(t *testing.T, id string, idle time.Duration) {
	t.Helper()
	f.holderWith(t, db.SessionRow{
		ID:           id,
		Branch:       "main",
		AgentType:    "claude-code_1000",
		AgentPID:     1000,
		StartedAt:    time.Now().Add(-24 * time.Hour),
		LastActivity: time.Now().Add(-idle),
	})
}

func (f *claimFixture) holderWith(t *testing.T, row db.SessionRow) {
	t.Helper()
	if err := f.database.UpsertSession(&row); err != nil {
		t.Fatalf("UpsertSession failed: %v", err)
	}
}

// claim creates an in_progress issue held by holder, aged by `age`.
func (f *claimFixture) claim(t *testing.T, title, holder string, age time.Duration) *models.Issue {
	t.Helper()
	issue := &models.Issue{Title: title, Type: models.TypeTask, Status: models.StatusInProgress}
	if err := f.database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	execTestSQL(t, f.baseDir,
		`UPDATE issues SET status = 'in_progress', implementer_session = ? WHERE id = ?`, holder, issue.ID)
	backdateIssue(t, f.baseDir, issue.ID, age)
	got, err := f.database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	return got
}

func (f *claimFixture) status(t *testing.T, id string) (models.Status, string) {
	t.Helper()
	got, err := f.database.GetIssue(id)
	if err != nil {
		t.Fatalf("GetIssue %s: %v", id, err)
	}
	return got.Status, got.ImplementerSession
}

func setSessionFlag(t *testing.T, id, force string) {
	t.Helper()
	setWorkflowExitFlag(t, unstartCmd, "stale", "")
	setWorkflowExitFlag(t, unstartCmd, "session", id)
	setWorkflowExitFlag(t, unstartCmd, "force", force)
}

func runUnstart(t *testing.T, args []string, wantErr bool) string {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = unstartCmd.RunE(unstartCmd, args)
	})
	if wantErr && err == nil {
		t.Fatalf("expected failure, got success. output: %q", out)
	}
	if !wantErr && err != nil {
		t.Fatalf("unstart failed: %v (output %q)", err, out)
	}
	return out
}

func decodeEnvelope(t *testing.T, out string) staleEnvelope {
	t.Helper()
	var env staleEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	return env
}

// --- finding 1: a rotated session is not a dead session ---------------------

// TestUnstartStaleKeepsClaimOfRotatedLiveSession is the fleet-fatal case. Every
// slot runs in its own worktree and agents branch constantly; each of those
// mints a NEW session row, and the row named in implementer_session stops being
// heartbeated while the agent is alive and working. Guarding on the session row
// alone released that live agent's claim.
func TestUnstartStaleKeepsClaimOfRotatedLiveSession(t *testing.T) {
	f := setupClaimTest(t)
	f.holder(t, "ses_before_branch", 3*time.Hour)
	issue := f.claim(t, "Work in progress", "ses_before_branch", 3*time.Hour)

	// The agent rotated onto a new branch (a new session row) and kept
	// working: `td log` a minute ago, recorded against the NEW session.
	f.holderWith(t, db.SessionRow{
		ID: "ses_after_branch", Branch: "feature/work",
		AgentType: "claude-code_1000", AgentPID: 1000,
		StartedAt: time.Now().Add(-time.Hour), LastActivity: time.Now().Add(-time.Minute),
	})
	if err := f.database.AddLog(&models.Log{
		IssueID: issue.ID, SessionID: "ses_after_branch",
		Message: "still working", Type: models.LogTypeProgress,
	}); err != nil {
		t.Fatalf("AddLog: %v", err)
	}

	setStaleFlags(t, "2h", "true")
	setJSONFlag(t, true)
	env := decodeEnvelope(t, runStale(t, false))

	if env.Count != 0 {
		t.Fatalf("released a live agent's claim after its session rotated: %+v", env)
	}
	if st, holder := f.status(t, issue.ID); st != models.StatusInProgress || holder != "ses_before_branch" {
		t.Fatalf("claim disturbed: status=%s implementer=%q", st, holder)
	}
}

// TestSessionKinCoversLineageAndFingerprintSiblings pins the self-claim
// guarantee to identity rather than to one row id. Both relations must hold:
// `td session --new` lineage, and the same agent process appearing on another
// branch or worktree.
func TestSessionKinCoversLineageAndFingerprintSiblings(t *testing.T) {
	self := &session.Session{ID: "ses_me", Branch: "main", AgentType: "claude-code_77", AgentPID: 77}
	sessions := []session.Session{
		*self,
		{ID: "ses_prev", AgentType: "claude-code_77", AgentPID: 77},
		{ID: "ses_next", PreviousSessionID: "ses_me", AgentType: "other_9", AgentPID: 9},
		{ID: "ses_branch", Branch: "feature/x", AgentType: "claude-code_77", AgentPID: 77},
		{ID: "ses_other_ctx", Branch: "main", AgentType: "claude-code_77", AgentPID: 77, MatchContextID: "reviewer"},
		{ID: "ses_stranger", Branch: "main", AgentType: "claude-code_88", AgentPID: 88},
		{ID: "ses_legacy_a", AgentType: "", AgentPID: 0},
	}
	self.PreviousSessionID = "ses_prev"
	sessions[0] = *self

	kin := sessionKin(sessions, self)
	for _, want := range []string{"ses_me", "ses_prev", "ses_next", "ses_branch"} {
		if !kin[want] {
			t.Errorf("%s must count as the caller's own identity", want)
		}
	}
	for _, notKin := range []string{"ses_other_ctx", "ses_stranger", "ses_legacy_a"} {
		if kin[notKin] {
			t.Errorf("%s must NOT count as the caller's identity", notKin)
		}
	}
}

// --- finding 5: unmeasurable liveness must fail closed ----------------------

// TestUnstartStaleUnparseableSessionTimestampDoesNotRelease covers the live
// migration hazard: older td builds wrote timestamps this build cannot parse,
// which read back as the zero time. A zero time makes idle ~2562047h, which
// exceeds every threshold — so the claim was released under any --stale value.
func TestUnstartStaleUnparseableSessionTimestampDoesNotRelease(t *testing.T) {
	f := setupClaimTest(t)
	f.holder(t, "ses_legacy_build", time.Minute)
	issue := f.claim(t, "Held by a legacy session row", "ses_legacy_build", time.Minute)
	execTestSQL(t, f.baseDir,
		`UPDATE sessions SET started_at = 'garbage', last_activity = 'garbage' WHERE id = 'ses_legacy_build'`)

	// A ten-year threshold: nothing honest can exceed it.
	setStaleFlags(t, "3650d", "true")
	setJSONFlag(t, true)
	env := decodeEnvelope(t, runStale(t, false))

	if env.Count != 0 {
		t.Fatalf("unmeasurable holder released under a 10-year threshold: %+v", env)
	}
	if st, _ := f.status(t, issue.ID); st != models.StatusInProgress {
		t.Fatalf("claim released: %s", st)
	}
}

// TestClaimLivenessRoutesTheImmeasurableToUnresolved is the unit-level
// statement of the same rule: no evidence at all is "unresolved", never
// "infinitely idle".
func TestClaimLivenessRoutesTheImmeasurableToUnresolved(t *testing.T) {
	issue := &models.Issue{ID: "td-zero"}
	if _, _, ok := claimLiveness(session.Session{}, false, issue, time.Time{}); ok {
		t.Fatal("no timestamps anywhere must not resolve to a measurable liveness")
	}
	now := time.Now()
	got, source, ok := claimLiveness(session.Session{}, false, &models.Issue{UpdatedAt: now}, time.Time{})
	if !ok || source != "issue" || !got.Equal(now) {
		t.Fatalf("issue updated_at must be usable evidence: %v %q %v", got, source, ok)
	}
}

// --- finding 7: cleanup must not strand what --stale exists to reclaim ------

// TestUnstartStaleReclaimsAfterSessionCleanup: `td session cleanup` and
// `td unstart --stale` select by the same idleness predicate, so on a cron the
// cleanup deletes the row the sweep needs and the issue leaks forever. The
// sweep now measures the work on the issue when the session row is gone.
func TestUnstartStaleReclaimsAfterSessionCleanup(t *testing.T) {
	f := setupClaimTest(t)
	issue := f.claim(t, "Holder session already cleaned up", "ses_deleted", 4*time.Hour)

	setStaleFlags(t, "2h", "true")
	setJSONFlag(t, true)
	env := decodeEnvelope(t, runStale(t, false))

	if env.Count != 1 || len(env.Claims) != 1 || env.Claims[0].ID != issue.ID {
		t.Fatalf("claim of a deleted session must still be reclaimable: %+v", env)
	}
	if env.Claims[0].Source != "issue" {
		t.Fatalf("expected the issue itself to be the liveness source, got %q", env.Claims[0].Source)
	}
	if env.UnresolvedCount != 0 {
		t.Fatalf("nothing should be left unresolved: %+v", env)
	}
	if st, _ := f.status(t, issue.ID); st != models.StatusOpen {
		t.Fatalf("claim not released: %s", st)
	}
}

// TestUnstartStaleKeepsRecentWorkWhenSessionRowIsGone is the other half: a
// missing session row is not by itself evidence of death.
func TestUnstartStaleKeepsRecentWorkWhenSessionRowIsGone(t *testing.T) {
	f := setupClaimTest(t)
	issue := f.claim(t, "Recently touched, no session row", "ses_deleted", time.Minute)

	setStaleFlags(t, "2h", "true")
	setJSONFlag(t, true)
	env := decodeEnvelope(t, runStale(t, false))

	if env.Count != 0 {
		t.Fatalf("recently worked issue released: %+v", env)
	}
	if st, _ := f.status(t, issue.ID); st != models.StatusInProgress {
		t.Fatalf("claim released: %s", st)
	}
}

// TestSessionCleanupKeepsClaimHolders: cleanup refuses to delete the rows that
// make a claim reclaimable, and says which ones it kept and how to clear them.
func TestSessionCleanupKeepsClaimHolders(t *testing.T) {
	f := setupClaimTest(t)
	f.holder(t, "ses_holder", 5*time.Hour)
	f.holder(t, "ses_idle_no_claims", 5*time.Hour)
	f.claim(t, "Held by an idle session", "ses_holder", 5*time.Hour)

	setWorkflowExitFlag(t, sessionCleanupCmd, "older-than", "2h")
	setWorkflowExitFlag(t, sessionCleanupCmd, "force", "true")
	setJSONFlag(t, true)

	var err error
	out := captureStdout(t, func() { err = sessionCleanupCmd.RunE(sessionCleanupCmd, nil) })
	if err != nil {
		t.Fatalf("session cleanup failed: %v (%q)", err, out)
	}
	var env struct {
		Action    string           `json:"action"`
		Count     int              `json:"count"`
		Sessions  []map[string]any `json:"sessions"`
		Held      []map[string]any `json:"held"`
		HeldCount int              `json:"held_count"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("cleanup output is not JSON: %v (%q)", err, out)
	}
	if env.HeldCount != 1 || len(env.Held) != 1 || env.Held[0]["session"] != "ses_holder" {
		t.Fatalf("the claim holder must be kept and reported: %q", out)
	}
	if !strings.Contains(env.Action, "with_held_claims") {
		t.Fatalf("action must surface the kept holders: %q", env.Action)
	}
	row, err := f.database.GetSessionByID("ses_holder")
	if err != nil || row == nil {
		t.Fatalf("claim holder row was deleted (err=%v)", err)
	}
	gone, err := f.database.GetSessionByID("ses_idle_no_claims")
	if err != nil {
		t.Fatalf("GetSessionByID: %v", err)
	}
	if gone != nil {
		t.Fatal("an idle session holding nothing should still be cleaned up")
	}
}

// --- finding 2: release exactly one session's claims ------------------------

// TestUnstartSessionReleasesOnlyTheNamedSession is the fleet scenario: four
// slots hold claims, one is killed, and the supervisor releases that tick's
// claim without touching the other three — including a slot whose session
// rotated onto a feature branch while alive.
func TestUnstartSessionReleasesOnlyTheNamedSession(t *testing.T) {
	f := setupClaimTest(t)
	for _, s := range []string{"ses_slot1", "ses_slot2", "ses_slot3", "ses_slot4"} {
		f.holder(t, s, 90*time.Minute)
	}
	// Slot 4 rotated onto a feature branch: its claim still names the old row.
	f.holderWith(t, db.SessionRow{
		ID: "ses_slot4_branched", Branch: "feature/slot4",
		AgentType: "claude-code_4000", AgentPID: 4000,
		StartedAt: time.Now().Add(-time.Hour), LastActivity: time.Now(),
	})

	killed := f.claim(t, "slot 1 work (killed by usage limit)", "ses_slot1", 90*time.Minute)
	others := map[string]*models.Issue{
		"ses_slot2": f.claim(t, "slot 2 work", "ses_slot2", 90*time.Minute),
		"ses_slot3": f.claim(t, "slot 3 work", "ses_slot3", 90*time.Minute),
		"ses_slot4": f.claim(t, "slot 4 work", "ses_slot4", 90*time.Minute),
	}

	setSessionFlag(t, "ses_slot1", "false")
	setJSONFlag(t, true)
	preview := decodeEnvelope(t, runUnstart(t, nil, false))
	if preview.Action != "would_release_session_claims" || preview.Count != 1 || preview.Session != "ses_slot1" {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	if st, _ := f.status(t, killed.ID); st != models.StatusInProgress {
		t.Fatalf("preview mutated: %s", st)
	}

	setSessionFlag(t, "ses_slot1", "true")
	env := decodeEnvelope(t, runUnstart(t, nil, false))
	if env.Action != "released_session_claims" || env.Count != 1 || env.Claims[0].ID != killed.ID {
		t.Fatalf("unexpected release envelope: %+v", env)
	}
	if st, holder := f.status(t, killed.ID); st != models.StatusOpen || holder != "" {
		t.Fatalf("killed tick's claim not released: %s %q", st, holder)
	}
	for holder, issue := range others {
		st, got := f.status(t, issue.ID)
		if st != models.StatusInProgress || got != holder {
			t.Fatalf("slot %s was disturbed: status=%s implementer=%q", holder, st, got)
		}
	}
}

// TestUnstartSessionUnknownSessionIsAnError: a typo must not read as
// "that session held nothing".
func TestUnstartSessionUnknownSessionIsAnError(t *testing.T) {
	f := setupClaimTest(t)
	f.holder(t, "ses_real", time.Minute)
	f.claim(t, "held", "ses_real", time.Minute)

	setSessionFlag(t, "ses_typo", "true")
	setJSONFlag(t, false)
	runUnstart(t, nil, true)
}

// TestUnstartSessionArgsValidation: the selectors are mutually exclusive and
// neither takes ids.
func TestUnstartSessionArgsValidation(t *testing.T) {
	setupClaimTest(t)

	setSessionFlag(t, "ses_x", "false")
	if err := unstartCmd.Args(unstartCmd, []string{"td-abc1"}); err == nil {
		t.Fatal("--session with issue ids must be rejected")
	}
	if err := unstartCmd.Args(unstartCmd, nil); err != nil {
		t.Fatalf("--session with no ids must be accepted: %v", err)
	}
	setWorkflowExitFlag(t, unstartCmd, "stale", "2h")
	if err := unstartCmd.Args(unstartCmd, nil); err == nil {
		t.Fatal("--session together with --stale must be rejected")
	}
}

// --- finding 8: an open issue can still be claimed --------------------------

// TestReopenClearsTheImplementerClaim: reopen cleared ClosedAt and the
// reviewer but left the implementer, producing an issue that is open AND held.
func TestReopenClearsTheImplementerClaim(t *testing.T) {
	f := setupClaimTest(t)
	issue := f.claim(t, "closed then reopened", "ses_old_impl", time.Minute)
	execTestSQL(t, f.baseDir,
		`UPDATE issues SET status = 'closed', closed_at = ? WHERE id = ?`,
		time.Now().Format(db.CanonicalTimeLayout), issue.ID)

	setJSONFlag(t, false)
	setWorkflowExitFlag(t, reopenCmd, "reason", "")
	out := captureStdout(t, func() {
		if err := reopenCmd.RunE(reopenCmd, []string{issue.ID}); err != nil {
			t.Fatalf("reopen failed: %v", err)
		}
	})
	if !strings.Contains(out, "REOPENED") {
		t.Fatalf("unexpected reopen output: %q", out)
	}
	if st, holder := f.status(t, issue.ID); st != models.StatusOpen || holder != "" {
		t.Fatalf("reopen left the claim in place: status=%s implementer=%q", st, holder)
	}
}

// TestUnstartReleasesALeakedClaimOnAnOpenIssue: an open issue that still names
// an implementer is NOT "already unstarted". Reporting success there is the
// false success that got an earlier fix rejected on review — and --stale
// cannot see it either, because it lists in_progress only.
func TestUnstartReleasesALeakedClaimOnAnOpenIssue(t *testing.T) {
	f := setupClaimTest(t)
	issue := f.claim(t, "open but still claimed", "ses_ghost", time.Minute)
	execTestSQL(t, f.baseDir, `UPDATE issues SET status = 'open' WHERE id = ?`, issue.ID)

	setStaleFlags(t, "", "false")
	setJSONFlag(t, false)
	out := runUnstart(t, []string{issue.ID}, false)
	if strings.Contains(out, "already unstarted") {
		t.Fatalf("a held claim must not report success as a no-op: %q", out)
	}
	if !strings.Contains(out, "UNSTARTED "+issue.ID) {
		t.Fatalf("unexpected output: %q", out)
	}
	if st, holder := f.status(t, issue.ID); st != models.StatusOpen || holder != "" {
		t.Fatalf("claim not released: status=%s implementer=%q", st, holder)
	}

	// Now it really is a no-op.
	out = runUnstart(t, []string{issue.ID}, false)
	if !strings.Contains(out, "already unstarted") {
		t.Fatalf("a genuinely unclaimed open issue must be an idempotent no-op: %q", out)
	}
}
