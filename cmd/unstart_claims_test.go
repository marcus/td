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

// TestSessionKinIsLineageOnly pins the self-claim guarantee to recorded
// lineage — `td session --new`, followed in both directions — and to nothing
// else.
//
// In particular a session carrying the caller's agent_pid/agent_type on another
// branch is NOT kin. td's stated policy (docs/multi-agent-sessions.md) is that
// pids are not identity: sessions sync between machines, so a pid from another
// host means nothing locally and can collide with an unrelated live process.
// Matching on it made a ghost row wearing the sweeper's fingerprint
// permanently unreclaimable at every threshold. Live rotated agents are
// protected by liveness instead — see
// TestUnstartStaleKeepsClaimOfRotatedLiveSession.
func TestSessionKinIsLineageOnly(t *testing.T) {
	self := &session.Session{ID: "ses_me", Branch: "main", AgentType: "claude-code_77", AgentPID: 77}
	sessions := []session.Session{
		*self,
		{ID: "ses_prev", AgentType: "claude-code_77", AgentPID: 77},
		{ID: "ses_prev_prev", PreviousSessionID: "", AgentType: "claude-code_77", AgentPID: 77},
		{ID: "ses_next", PreviousSessionID: "ses_me", AgentType: "other_9", AgentPID: 9},
		{ID: "ses_next_next", PreviousSessionID: "ses_next", AgentType: "other_9", AgentPID: 9},
		{ID: "ses_branch", Branch: "feature/x", AgentType: "claude-code_77", AgentPID: 77},
		{ID: "ses_other_ctx", Branch: "main", AgentType: "claude-code_77", AgentPID: 77, MatchContextID: "reviewer"},
		{ID: "ses_stranger", Branch: "main", AgentType: "claude-code_88", AgentPID: 88},
		{ID: "ses_legacy_a", AgentType: "", AgentPID: 0},
	}
	self.PreviousSessionID = "ses_prev"
	sessions[0] = *self
	sessions[1].PreviousSessionID = "ses_prev_prev"

	kin := sessionKin(sessions, self)
	for _, want := range []string{"ses_me", "ses_prev", "ses_prev_prev", "ses_next", "ses_next_next"} {
		if !kin[want] {
			t.Errorf("%s must count as the caller's own identity", want)
		}
	}
	for _, notKin := range []string{"ses_branch", "ses_other_ctx", "ses_stranger", "ses_legacy_a"} {
		if kin[notKin] {
			t.Errorf("%s must NOT count as the caller's identity (pid is not identity)", notKin)
		}
	}
}

// TestUnstartStaleReportsLineageHeldClaimsAsUnresolved is the other half of the
// self-claim guarantee: never releasing a lineage-held claim must not mean
// never mentioning it. `td session --new` is a routine move, so a supervisor
// that rotated its identity and then swept would be told "no claims idle
// longer than 2h" while its ancestor's claims sat 72 hours dead — a sweep that
// silently drops what it will not act on is a sweep that lies.
//
// It also pins the guard to identity rather than to one row id: with
// `holder == sess.ID` in place of the kin check these claims are released.
func TestUnstartStaleReportsLineageHeldClaimsAsUnresolved(t *testing.T) {
	f := setupClaimTest(t)

	// `td session --new`: the caller is the new row, the claims still name its
	// ancestor, and nothing has heartbeated that ancestor for three days.
	f.holder(t, "ses_lineage_old", 72*time.Hour)
	execTestSQL(t, f.baseDir,
		`UPDATE sessions SET previous_session_id = 'ses_lineage_old' WHERE id = ?`, f.sess.ID)

	ids := make(map[string]bool, 3)
	for _, name := range []string{"a", "b", "c"} {
		ids[f.claim(t, "lineage claim "+name, "ses_lineage_old", 72*time.Hour).ID] = true
	}

	setStaleFlags(t, "2h", "true")
	setJSONFlag(t, true)
	env := decodeEnvelope(t, runStale(t, false))

	if env.Count != 0 || len(env.Claims) != 0 {
		t.Fatalf("a claim held by the caller's own lineage must never be released: %+v", env)
	}
	if env.UnresolvedCount != len(ids) || len(env.Unresolved) != len(ids) {
		t.Fatalf("every lineage-held stale claim must be reported: %+v", env)
	}
	if env.Action != "released_stale_claims_with_unresolved" {
		t.Fatalf("action must surface what was left behind, got %q", env.Action)
	}
	for _, u := range env.Unresolved {
		if !ids[u.ID] {
			t.Fatalf("unexpected unresolved id %q", u.ID)
		}
		if u.Session != "ses_lineage_old" {
			t.Fatalf("unresolved entry must name the holder, got %q", u.Session)
		}
		if !strings.Contains(u.Reason, "lineage") ||
			!strings.Contains(u.Reason, "td unstart --session ses_lineage_old --force") {
			t.Fatalf("unresolved reason must name the cause and the remedy: %q", u.Reason)
		}
		if st, holder := f.status(t, u.ID); st != models.StatusInProgress || holder != "ses_lineage_old" {
			t.Fatalf("claim %s was disturbed: status=%s implementer=%q", u.ID, st, holder)
		}
	}

	// The human path must warn rather than print a bare "no claims".
	setJSONFlag(t, false)
	out := runStale(t, false)
	if !strings.Contains(out, "not evaluated:") ||
		!strings.Contains(out, "3 claim(s) left unresolved") {
		t.Fatalf("human output must not drop the lineage-held claims: %q", out)
	}
}

// TestUnstartStaleReleasesAGhostWearingTheSweepersFingerprint: a session row
// carrying the sweeping process's own pid and agent type, on another branch, is
// NOT the caller. td's policy is that a pid is not an identity — sessions sync
// between machines, so a pid from another host means nothing locally and can
// collide with an unrelated live process. Treating it as kinship made this
// ghost's claim unreclaimable at every threshold, up to ten days and beyond.
func TestUnstartStaleReleasesAGhostWearingTheSweepersFingerprint(t *testing.T) {
	f := setupClaimTest(t)

	// The caller needs a real, non-zero pid fingerprint for this to be the
	// case it names: with TD_SESSION_ID set the fingerprint pid is 0, which
	// the removed clause excluded anyway. CURSOR_AGENT gives a deterministic
	// non-zero one (os.Getppid).
	t.Setenv("TD_SESSION_ID", "")
	t.Setenv("CURSOR_AGENT", "1")
	caller, err := session.GetOrCreate(f.database)
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	if caller.AgentPID == 0 {
		t.Fatalf("fixture needs a non-zero agent pid, got session %+v", caller)
	}

	f.holderWith(t, db.SessionRow{
		ID: "ses_ghost_pid", Branch: "feature/ghost",
		AgentType: caller.AgentType, AgentPID: caller.AgentPID,
		MatchContextID: caller.MatchContextID,
		StartedAt:      time.Now().Add(-30 * 24 * time.Hour),
		LastActivity:   time.Now().Add(-10 * 24 * time.Hour),
	})
	issue := f.claim(t, "held by a ghost wearing the sweeper's pid", "ses_ghost_pid", 10*24*time.Hour)

	setStaleFlags(t, "2h", "true")
	setJSONFlag(t, true)
	env := decodeEnvelope(t, runStale(t, false))

	if env.Count != 1 || len(env.Claims) != 1 || env.Claims[0].ID != issue.ID {
		t.Fatalf("a ghost session sharing the sweeper's pid must still be reclaimable: %+v", env)
	}
	if env.UnresolvedCount != 0 {
		t.Fatalf("nothing should be left unresolved: %+v", env)
	}
	if st, holder := f.status(t, issue.ID); st != models.StatusOpen || holder != "" {
		t.Fatalf("claim not released: status=%s implementer=%q", st, holder)
	}
}

// --- finding 5: unmeasurable liveness must fail closed ----------------------

// TestUnstartStaleUnmeasurableClaimIsNotInfinitelyIdle covers the live
// migration hazard: timestamps this build cannot parse, or a zero time written
// by a bad import, read back as the zero time. A zero time makes idle
// ~2562047h, which exceeds every threshold — so the claim was released under
// any --stale value.
//
// EVERY signal has to be unmeasurable for that to be what the sweep is
// deciding on: liveness is the most recent of the holder session, the issue's
// updated_at, and the issue's history, and any one of them being fresh
// protects the claim by freshness instead — which would make this test pass
// for the wrong reason. Note the issue's own timestamp has to be a *parseable*
// zero rather than garbage: `issues.updated_at` is scanned into a time.Time,
// so an unparseable value fails ListIssues before the sweep ever runs. That
// parseable zero is what keeps this branch reachable, and it is why the guard
// cannot be deleted as dead code.
func TestUnstartStaleUnmeasurableClaimIsNotInfinitelyIdle(t *testing.T) {
	f := setupClaimTest(t)
	f.holder(t, "ses_legacy_build", time.Minute)
	issue := f.claim(t, "Held by a legacy session row", "ses_legacy_build", time.Minute)
	execTestSQL(t, f.baseDir,
		`UPDATE sessions SET started_at = 'garbage', last_activity = 'garbage' WHERE id = 'ses_legacy_build'`)
	execTestSQL(t, f.baseDir, `UPDATE issues SET updated_at = ? WHERE id = ?`,
		db.FormatCanonicalTime(time.Time{}), issue.ID)
	execTestSQL(t, f.baseDir, `DELETE FROM logs WHERE issue_id = ?`, issue.ID)
	execTestSQL(t, f.baseDir, `DELETE FROM action_log WHERE entity_id = ?`, issue.ID)

	// A short threshold: nothing here is fresh, so only "we cannot measure
	// this" can keep the claim.
	setStaleFlags(t, "2h", "true")
	setJSONFlag(t, true)
	env := decodeEnvelope(t, runStale(t, false))

	if env.Count != 0 {
		t.Fatalf("unmeasurable holder released: %+v", env)
	}
	if env.UnresolvedCount != 1 || len(env.Unresolved) != 1 || env.Unresolved[0].ID != issue.ID {
		t.Fatalf("an unmeasurable claim must be reported, not dropped: %+v", env)
	}
	if !strings.Contains(env.Action, "_with_unresolved") {
		t.Fatalf("action must surface the unresolved claim, got %q", env.Action)
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

// TestUnblockClearsTheImplementerClaim: unblock returns an issue to open, and
// an open issue is unclaimed work. `td block` keeps the implementer on purpose,
// but blocked is reachable from open as well as in_progress, so the way back
// releases rather than guessing at restoring in_progress.
func TestUnblockClearsTheImplementerClaim(t *testing.T) {
	f := setupClaimTest(t)
	issue := f.claim(t, "started then blocked", "ses_old_impl", time.Minute)
	execTestSQL(t, f.baseDir, `UPDATE issues SET status = 'blocked' WHERE id = ?`, issue.ID)

	setJSONFlag(t, false)
	setWorkflowExitFlag(t, unblockCmd, "reason", "")
	out := captureStdout(t, func() {
		if err := unblockCmd.RunE(unblockCmd, []string{issue.ID}); err != nil {
			t.Fatalf("unblock failed: %v", err)
		}
	})
	if !strings.Contains(out, "UNBLOCKED") {
		t.Fatalf("unexpected unblock output: %q", out)
	}
	if st, holder := f.status(t, issue.ID); st != models.StatusOpen || holder != "" {
		t.Fatalf("unblock left the claim in place: status=%s implementer=%q", st, holder)
	}
}

// TestSweepsSeeALeakedClaimOnAnOpenIssue: rows leaked by older builds of
// reopen/unblock are open AND held. Both sweeps used to select in_progress
// only, so nothing listed them and only a by-name `td unstart <id>` could
// clear them — which requires already knowing the id.
func TestSweepsSeeALeakedClaimOnAnOpenIssue(t *testing.T) {
	f := setupClaimTest(t)
	f.holder(t, "ses_ghost_holder", 3*time.Hour)
	issue := f.claim(t, "leaked by an older build", "ses_ghost_holder", 3*time.Hour)
	execTestSQL(t, f.baseDir, `UPDATE issues SET status = 'open' WHERE id = ?`, issue.ID)
	// An ordinary unclaimed open issue must not be swept along with it.
	quiet := &models.Issue{Title: "plain backlog item", Type: models.TypeTask}
	if err := f.database.CreateIssue(quiet); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	setJSONFlag(t, false)
	setSessionFlag(t, "ses_ghost_holder", "false")
	out := runUnstart(t, nil, false)
	if !strings.Contains(out, issue.ID) {
		t.Fatalf("--session did not list the leaked claim: %q", out)
	}
	if strings.Contains(out, quiet.ID) {
		t.Fatalf("--session swept an unclaimed open issue: %q", out)
	}

	setSessionFlag(t, "", "false")
	setStaleFlags(t, "1h", "true")
	out = runUnstart(t, nil, false)
	if !strings.Contains(out, issue.ID) {
		t.Fatalf("--stale did not release the leaked claim: %q", out)
	}
	if strings.Contains(out, quiet.ID) {
		t.Fatalf("--stale swept an unclaimed open issue: %q", out)
	}
	if st, holder := f.status(t, issue.ID); st != models.StatusOpen || holder != "" {
		t.Fatalf("claim not released: status=%s implementer=%q", st, holder)
	}
}

// TestSessionsHoldingClaimsSeesOpenHeldIssues: `td session cleanup` must not
// delete the holder row of a leaked open+held claim — that row is what names
// the claim in every report that could lead an operator to it.
func TestSessionsHoldingClaimsSeesOpenHeldIssues(t *testing.T) {
	f := setupClaimTest(t)
	issue := f.claim(t, "open but still claimed", "ses_ghost_holder", time.Minute)
	execTestSQL(t, f.baseDir, `UPDATE issues SET status = 'open' WHERE id = ?`, issue.ID)

	held, err := f.database.SessionsHoldingClaims()
	if err != nil {
		t.Fatalf("SessionsHoldingClaims failed: %v", err)
	}
	if held["ses_ghost_holder"] != 1 {
		t.Fatalf("open+held claim invisible to cleanup protection: %v", held)
	}

	// A closed issue keeps its implementer as the record of who did the work;
	// protecting that holder forever would make cleanup a no-op for every
	// session that ever finished anything.
	execTestSQL(t, f.baseDir, `UPDATE issues SET status = 'closed' WHERE id = ?`, issue.ID)
	held, err = f.database.SessionsHoldingClaims()
	if err != nil {
		t.Fatalf("SessionsHoldingClaims failed: %v", err)
	}
	if held["ses_ghost_holder"] != 0 {
		t.Fatalf("a closed issue must not count as a held claim: %v", held)
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
