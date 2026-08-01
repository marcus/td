package cmd

import (
	"fmt"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/workflow"
	"github.com/spf13/cobra"
)

// errUnstartAllFailed signals that every non-idempotent named unstart failed.
// Each failure is emitted in the per-issue path; the sentinel only sets the
// process exit status without adding Cobra usage or a second JSON envelope.
var errUnstartAllFailed = fmt.Errorf("no issues unstarted: %w", errSilentExit)

var unstartCmd = &cobra.Command{
	Use:     "unstart [issue-id...]",
	Aliases: []string{"stop"},
	Short:   "Revert issue(s) from in_progress to open",
	Long: `Reverts issue(s) back to open status. Clears implementer session.
Useful for undoing accidental starts or when you need to release an issue.

Two sweeps reclaim claims whose holder can no longer hand off:

  --session <id>  releases every claim held by ONE named session. Exact, with
                  no liveness guess. This is what a supervisor wants when it
                  kills a tick: it knows which session died, and releasing
                  only that session's claims cannot disturb the other slots.

  --stale <dur>   releases claims whose holder has shown no activity for
                  longer than <dur>. Use it as a backstop for holders nobody
                  is tracking, not as the primary reaper of your own fleet.

Both preview by default and act only with --force.

--stale has no default threshold on purpose. Liveness is measured as the most
recent of: the holder session's last activity, the issue's updated_at, and the
newest history entry on the issue by any session — so an agent whose session
rotated (a new branch or worktree mints a new session row) still reads as live
while it works. It is still a heuristic: an agent can work for a long time
without touching td at all, so the threshold must exceed the longest a healthy
tick can run without a td command. Too short a value releases live agents'
claims and two agents then work the same issue. td never releases a claim held
by the calling session or by another session in its identity lineage, and it
never releases a claim it cannot measure at all; those are reported under
unresolved so you can act on them explicitly.

Examples:
  td unstart td-abc1                     # Unstart single issue
  td unstart td-abc1 td-abc2 td-abc3     # Unstart multiple issues
  td unstart --session ses_abc123        # Preview one session's claims
  td unstart --session ses_abc123 --force --json
  td unstart --stale 2h                  # Preview claims idle longer than 2h
  td unstart --stale 2h --force --json   # Release them, report what was released`,
	GroupID: "workflow",
	Args: func(cmd *cobra.Command, args []string) error {
		stale, _ := cmd.Flags().GetString("stale")
		holder, _ := cmd.Flags().GetString("session")

		// The two sweeps select their targets differently and combining them
		// would make "which rule released this claim" unanswerable.
		if stale != "" && holder != "" {
			return fmt.Errorf("--session and --stale select claims by different rules; use one")
		}
		// Both sweeps choose their own targets, so neither takes ids.
		if stale != "" || holder != "" {
			if len(args) > 0 {
				which := "--stale"
				if holder != "" {
					which = "--session"
				}
				return fmt.Errorf("%s selects the issues to release; do not also name issue ids", which)
			}
			return nil
		}
		// --force only gates the sweeps. Accepting it silently on the named
		// path would advertise a safety flag that does nothing.
		if force, _ := cmd.Flags().GetBool("force"); force {
			return fmt.Errorf("--force applies to --session/--stale; naming issue ids already unstarts them")
		}
		return cobra.MinimumNArgs(1)(cmd, args)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Arguments and flags have parsed and validated by the time RunE
		// runs, so every error from here on is an operational one whose
		// message stands on its own. Usage output is still produced for the
		// genuine usage errors (bad arg count, unknown flag) that Cobra
		// reports before this point.
		cmd.SilenceUsage = true

		baseDir := getBaseDir()
		isJSON := jsonMode(cmd)

		emitErr := func(format string, args ...interface{}) {
			if !isJSON {
				output.Error(format, args...)
			}
		}

		database, err := db.Open(baseDir)
		if err != nil {
			emitErr("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			emitErr("%v", err)
			return err
		}
		scope := currentStateScope(baseDir, sess)

		reason, _ := cmd.Flags().GetString("reason")
		force, _ := cmd.Flags().GetBool("force")

		if holder, _ := cmd.Flags().GetString("session"); holder != "" {
			return unstartBySession(database, sess, scope, holder, reason, force, isJSON)
		}
		if stale, _ := cmd.Flags().GetString("stale"); stale != "" {
			return unstartStale(database, sess, scope, stale, reason, force, isJSON)
		}

		unstarted := 0
		failed := 0
		noop := 0

		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				failTransition(isJSON, output.ErrCodeNotFound, "issue not found: %s", issueID)
				failed++
				continue
			}

			// An open issue with no claim on it is the requested end state, so
			// a repeated unstart is an idempotent success. An open issue that
			// STILL names an implementer is not: `td reopen` used to leave that
			// behind, and reporting "already unstarted" for a held claim is the
			// exact false success this command exists to avoid.
			if issue.Status == models.StatusOpen && issue.ImplementerSession == "" {
				noopTransition(isJSON, issueID, issue.Status, "already unstarted")
				noop++
				continue
			}

			if issue.Status != models.StatusOpen {
				// Validate transition with state machine
				sm := workflow.DefaultMachine()
				if !sm.IsValidTransition(issue.Status, models.StatusOpen) {
					failTransition(isJSON, output.ErrCodeInvalidInput, "cannot unstart %s: invalid transition from %s", issueID, issue.Status)
					failed++
					continue
				}

				// Only unstart in_progress issues (preserving existing behavior)
				if issue.Status != models.StatusInProgress {
					failTransition(isJSON, output.ErrCodeInvalidInput, "issue not in_progress: %s (status: %s)", issueID, issue.Status)
					failed++
					continue
				}
			}

			logMsg := "Reverted to open"
			if reason != "" {
				logMsg = reason
			}

			outcome, err := releaseClaims(database, sess, scope,
				[]db.ClaimRelease{{IssueID: issue.ID, LogMessage: logMsg}})
			if err != nil {
				failTransition(isJSON, output.ErrCodeDatabaseError, "failed to update %s: %v", issueID, err)
				failed++
				continue
			}
			switch {
			case outcome[0].Err != nil:
				failTransition(isJSON, output.ErrCodeDatabaseError, "failed to update %s: %v", issueID, outcome[0].Err)
				failed++
				continue
			case outcome[0].Skipped:
				// Another writer got there first and the issue is already in
				// the requested end state.
				noopTransition(isJSON, issueID, models.StatusOpen, "already unstarted")
				noop++
				continue
			}

			if isJSON {
				// Re-fetch the now-open issue and emit one JSON object per id
				// (NDJSON in the bulk case).
				unstarted2, ferr := database.GetIssue(issueID)
				if ferr != nil {
					unstarted2 = outcome[0].Issue
				}
				if err := output.EmitIssue("unstarted", unstarted2, nil); err != nil {
					return err
				}
			} else {
				fmt.Printf("UNSTARTED %s → open\n", issueID)
			}
			unstarted++
		}

		if len(args) > 1 && !isJSON {
			// unchanged and failed are different outcomes — one is an
			// idempotent retry, the other is work that did not happen — and
			// summing them into "skipped" throws away the distinction the
			// loop above already computed.
			fmt.Printf("\nUnstarted %d, unchanged %d, failed %d\n", unstarted, noop, failed)
		}

		// A named batch succeeds when at least one issue unstarted, and an
		// already-open retry succeeds as an idempotent no-op. If nothing
		// unstarted and any true failure occurred, report failure even when the
		// same batch also contained no-ops.
		if unstarted == 0 && failed > 0 {
			return errUnstartAllFailed
		}

		return nil
	},
}

// releaseClaims performs the guarded release for a batch of claims and clears
// focus for any released issue that was focused. Both the named path and the
// sweeps go through it, so a reclaimed claim is indistinguishable from a
// hand-released one except for the recorded reason.
func releaseClaims(database *db.DB, sess *session.Session, scope db.SessionStateScope, reqs []db.ClaimRelease) ([]db.ClaimReleaseOutcome, error) {
	outcomes, err := database.ReleaseClaims(reqs, sess.ID)
	if err != nil {
		return outcomes, err
	}

	// Focus is per-scope state outside the issue row; clearing it needs its
	// own write, so it happens once after the batch rather than per issue.
	focusedID, _ := database.GetFocus(scope)
	if focusedID == "" {
		return outcomes, nil
	}
	for _, out := range outcomes {
		if out.Released && out.IssueID == focusedID {
			_ = database.ClearFocus(scope)
			break
		}
	}
	return outcomes, nil
}

// staleClaim is one claim selected for release, with the evidence that
// selected it.
type staleClaim struct {
	issue        *models.Issue
	holder       string
	idle         time.Duration
	lastActivity time.Time
	// source names which signal produced lastActivity, so an operator can see
	// whether td measured the session or the work on the issue.
	source string
}

// listClaimedIssues returns every issue the sweeps may act on: the in_progress
// ones, plus open issues that still name an implementer.
//
// The second group exists because a claim can outlive in_progress. `td reopen`
// and `td unblock` both used to land an issue on open without clearing the
// implementer, and both sweeps selected in_progress only — so those claims were
// held, releasable by db.ReleaseClaims, and yet listed by nothing. Selecting
// them here is what makes rows leaked by older builds visible to
// `td unstart --session` and `--stale`.
//
// Open issues with NO implementer are dropped rather than reported unresolved:
// an unclaimed open issue is the normal resting state of most of the backlog
// and is not a claim at all.
func listClaimedIssues(database *db.DB) ([]models.Issue, error) {
	issues, err := database.ListIssues(db.ListIssuesOptions{
		Status: []models.Status{models.StatusInProgress, models.StatusOpen},
	})
	if err != nil {
		return nil, err
	}
	claimed := issues[:0]
	for _, issue := range issues {
		if issue.Status == models.StatusOpen && issue.ImplementerSession == "" {
			continue
		}
		claimed = append(claimed, issue)
	}
	return claimed, nil
}

// unresolvedClaim is a claimed issue whose holder's liveness cannot be
// measured at all, so the sweep deliberately leaves it alone.
type unresolvedClaim struct {
	id     string
	holder string
	reason string
}

// sweepReport is the shared result shape of both sweeps.
type sweepReport struct {
	selectorKey   string // "stale" or "session"
	selectorValue string
	claims        []staleClaim
	unresolved    []unresolvedClaim
}

// sessionKin returns the set of session ids that are, for claim purposes, the
// caller itself.
//
// A session row is not a stable identity for a running agent. The identity key
// is branch + agent fingerprint + match context + worktree, so `git checkout -b`
// or moving to another worktree mints a NEW row and stops heartbeating the old
// one — while the same agent process keeps working. Guarding only on
// `holder == sess.ID` therefore lets a sweep release the caller's own claim,
// which falsifies the guarantee the command advertises.
//
// Kinship is exactly one relation: lineage. previous_session_id, followed
// transitively in both directions — which is what `td session --new` records.
//
// It deliberately does NOT include "fingerprint siblings" matched on agent_pid.
// A pid is not an identity here: sessions sync between machines, so a pid from
// another host means nothing locally and can collide with an unrelated live
// process (docs/multi-agent-sessions.md). Matching on it made any session row
// carrying the sweeper's pid — including a ghost left on another branch —
// permanently unreclaimable at every threshold. The case that clause was
// reaching for, an agent whose session rotated onto a new branch or worktree
// while it kept working, is already covered by the liveness signals: the work
// it does lands on the issue as history (`issue_activity`) or as updated_at.
func sessionKin(sessions []session.Session, self *session.Session) map[string]bool {
	kin := map[string]bool{self.ID: true}

	for changed := true; changed; {
		changed = false
		for _, s := range sessions {
			if kin[s.ID] {
				// Ancestors of a kin session are kin.
				if p := s.PreviousSessionID; p != "" && !kin[p] {
					kin[p] = true
					changed = true
				}
				continue
			}
			// Descendants of a kin session are kin.
			if s.PreviousSessionID != "" && kin[s.PreviousSessionID] {
				kin[s.ID] = true
				changed = true
			}
		}
	}
	return kin
}

// claimLiveness returns the most recent evidence that work is happening on an
// issue, and names the signal it came from. ok is false when there is no
// usable evidence at all — which must route the claim to unresolved rather
// than to release: an unparseable timestamp reads as the zero time, whose idle
// duration exceeds every threshold, and "we cannot measure this" must never
// mean "release it".
func claimLiveness(holder session.Session, holderKnown bool, issue *models.Issue, activity time.Time) (time.Time, string, bool) {
	var best time.Time
	var source string
	consider := func(t time.Time, name string) {
		if t.IsZero() {
			return
		}
		if best.IsZero() || t.After(best) {
			best = t
			source = name
		}
	}
	if holderKnown {
		consider(holder.LastActive(), "session")
	}
	consider(issue.UpdatedAt, "issue")
	consider(activity, "issue_activity")
	return best, source, !best.IsZero()
}

// unstartBySession releases every claim held by one named session.
//
// This is the sweep a supervisor actually needs. When a tick is killed by a
// usage limit or a wall-clock timeout, the supervisor knows exactly which
// session died; releasing by idle time instead would sweep every other quiet
// slot in the fleet, which is the two-agents-one-issue failure the whole
// design exists to prevent. There is no liveness heuristic here at all.
func unstartBySession(database *db.DB, sess *session.Session, scope db.SessionStateScope, holder, reason string, force, isJSON bool) error {
	fail := sweepFailer(isJSON)

	issues, err := listClaimedIssues(database)
	if err != nil {
		return fail(fmt.Errorf("failed to list issues: %w", err))
	}

	now := time.Now()
	row, err := database.GetSessionByID(holder)
	if err != nil {
		return fail(fmt.Errorf("failed to look up session %s: %w", holder, err))
	}

	report := sweepReport{selectorKey: "session", selectorValue: holder}
	for i := range issues {
		issue := &issues[i]
		if issue.ImplementerSession != holder {
			continue
		}
		last := issue.UpdatedAt
		if row != nil && row.LastActivity.After(last) {
			last = row.LastActivity
		}
		claim := staleClaim{issue: issue, holder: holder, lastActivity: last, source: "session"}
		if !last.IsZero() {
			claim.idle = now.Sub(last)
		}
		report.claims = append(report.claims, claim)
	}

	// A typo in a session id must not read as "that session held nothing".
	// With no row and no claims there is nothing this command could have been
	// asked to do, so it is an error rather than an empty success.
	if row == nil && len(report.claims) == 0 {
		if isJSON {
			output.JSONError(output.ErrCodeNotFound,
				fmt.Sprintf("no session %s in this database, and no claim names it", holder))
			return fmt.Errorf("%w", errSilentExit)
		}
		return fail(fmt.Errorf("no session %s in this database, and no claim names it", holder))
	}

	return emitSweep(database, sess, scope, report, reason, force, isJSON,
		func(claim staleClaim) string {
			msg := fmt.Sprintf("claim released: session %s (released by %s)", claim.holder, sess.ID)
			if reason != "" {
				msg += "; " + reason
			}
			return msg
		})
}

// unstartStale releases claims whose holder shows no recent activity. It
// previews by default and mutates only under force, matching
// `td session cleanup`.
func unstartStale(database *db.DB, sess *session.Session, scope db.SessionStateScope, stale, reason string, force, isJSON bool) error {
	fail := sweepFailer(isJSON)

	maxIdle, err := session.ParseDuration(stale)
	if err != nil {
		return fail(fmt.Errorf("invalid duration: %s (%v)", stale, err))
	}
	if maxIdle <= 0 {
		return fail(fmt.Errorf("--stale must be greater than zero, got %s", stale))
	}

	sessions, err := session.ListSessions(database)
	if err != nil {
		return fail(fmt.Errorf("failed to list sessions: %w", err))
	}
	byID := make(map[string]session.Session, len(sessions))
	for _, s := range sessions {
		byID[s.ID] = s
	}
	kin := sessionKin(sessions, sess)

	issues, err := listClaimedIssues(database)
	if err != nil {
		return fail(fmt.Errorf("failed to list issues: %w", err))
	}
	ids := make([]string, 0, len(issues))
	for i := range issues {
		ids = append(ids, issues[i].ID)
	}
	activity, err := database.LatestIssueActivity(ids)
	if err != nil {
		return fail(fmt.Errorf("failed to read issue activity: %w", err))
	}

	now := time.Now()
	report := sweepReport{selectorKey: "stale", selectorValue: stale}

	for i := range issues {
		issue := &issues[i]
		holder := issue.ImplementerSession

		if holder == "" {
			report.unresolved = append(report.unresolved, unresolvedClaim{
				id:     issue.ID,
				reason: "no implementer session recorded",
			})
			continue
		}

		holderSession, known := byID[holder]
		lastActive, source, ok := claimLiveness(holderSession, known, issue, activity[issue.ID])
		if !ok {
			why := fmt.Sprintf("implementer session %s is not in this database and the issue carries no usable timestamp", holder)
			if known {
				why = fmt.Sprintf("implementer session %s has no usable activity timestamp", holder)
			}
			report.unresolved = append(report.unresolved, unresolvedClaim{
				id: issue.ID, holder: holder, reason: why,
			})
			continue
		}

		idle := now.Sub(lastActive)
		if idle <= maxIdle {
			continue
		}

		// The caller, or another session row in its own identity lineage, is
		// never released by a sweep: `td session --new` is a routine move, and
		// releasing what the caller is holding would be self-sabotage. But an
		// idle lineage claim is not nothing, and dropping it here made the
		// sweep lie — "no claims idle longer than 2h" while three were 72h
		// dead. Report it so the operator can act on it explicitly.
		if kin[holder] {
			report.unresolved = append(report.unresolved, unresolvedClaim{
				id:     issue.ID,
				holder: holder,
				reason: fmt.Sprintf("held by session %s in the caller's identity lineage, idle %s (threshold %s, measured from %s); never released by a sweep — release it explicitly with `td unstart --session %s --force`",
					holder, formatIdle(idle), stale, source, holder),
			})
			continue
		}

		report.claims = append(report.claims, staleClaim{
			issue:        issue,
			holder:       holder,
			idle:         idle,
			lastActivity: lastActive,
			source:       source,
		})
	}

	return emitSweep(database, sess, scope, report, reason, force, isJSON,
		func(claim staleClaim) string {
			msg := fmt.Sprintf("claim released: implementer session %s idle %s (threshold %s, measured from %s)",
				claim.holder, formatIdle(claim.idle), stale, claim.source)
			if reason != "" {
				msg += "; " + reason
			}
			return msg
		})
}

// sweepFailer reports a whole-command error exactly once: --json callers get
// the envelope Execute emits from the returned error, human callers get the
// message here and a sentinel that adds nothing further.
func sweepFailer(isJSON bool) func(error) error {
	return func(err error) error {
		if isJSON {
			return err
		}
		output.Error("%v", err)
		return fmt.Errorf("%w", errSilentExit)
	}
}

// emitSweep performs the release (under force) and renders the result. Both
// sweeps share it so their --json envelopes have the same shape and their
// human output the same vocabulary.
func emitSweep(database *db.DB, sess *session.Session, scope db.SessionStateScope,
	report sweepReport, reason string, force, isJSON bool, logMessage func(staleClaim) string) error {

	released := make([]staleClaim, 0, len(report.claims))
	skipped := 0
	failed := 0

	if force {
		reqs := make([]db.ClaimRelease, 0, len(report.claims))
		for _, claim := range report.claims {
			reqs = append(reqs, db.ClaimRelease{
				IssueID:        claim.issue.ID,
				ExpectedHolder: claim.holder,
				LogMessage:     logMessage(claim),
			})
		}
		outcomes, err := releaseClaims(database, sess, scope, reqs)
		if err != nil {
			return sweepFailer(isJSON)(fmt.Errorf("failed to release claims: %w", err))
		}
		for i, out := range outcomes {
			switch {
			case out.Err != nil:
				failTransition(isJSON, output.ErrCodeDatabaseError, "failed to release %s: %v", out.IssueID, out.Err)
				failed++
			case out.Skipped:
				// The claim moved between selection and the write. It is not
				// ours to release and it is not a failure — but it must not be
				// counted as released either.
				skipped++
				report.unresolved = append(report.unresolved, unresolvedClaim{
					id:     out.IssueID,
					holder: report.claims[i].holder,
					reason: out.Reason,
				})
			default:
				released = append(released, report.claims[i])
			}
		}
	}

	reported := report.claims
	verb := "would_release"
	if force {
		reported = released
		verb = "released"
	}
	action := fmt.Sprintf("%s_%s_claims", verb, sweepNoun(report.selectorKey))
	// A non-empty unresolved list is part of the outcome, not a detail a
	// caller has to remember to inspect: a supervisor that only reads
	// `action` must still be able to see that something was left behind.
	if len(report.unresolved) > 0 {
		action += "_with_unresolved"
	}

	if isJSON {
		rows := make([]map[string]any, 0, len(reported))
		for _, claim := range reported {
			row := map[string]any{
				"id":            claim.issue.ID,
				"title":         claim.issue.Title,
				"session":       claim.holder,
				"idle_seconds":  int64(claim.idle.Seconds()),
				"last_activity": claim.lastActivity.UTC().Format(time.RFC3339),
			}
			if claim.source != "" {
				row["last_activity_source"] = claim.source
			}
			rows = append(rows, row)
		}
		unresolved := make([]map[string]any, 0, len(report.unresolved))
		for _, u := range report.unresolved {
			unresolved = append(unresolved, map[string]any{
				"id":      u.id,
				"session": u.holder,
				"reason":  u.reason,
			})
		}
		payload := map[string]any{
			report.selectorKey: report.selectorValue,
			"forced":           force,
			"count":            len(reported),
			"claims":           rows,
			"unresolved":       unresolved,
			"unresolved_count": len(unresolved),
		}
		if err := output.EmitResult(action, payload); err != nil {
			return err
		}
	} else {
		selector := fmt.Sprintf("session %s", report.selectorValue)
		if report.selectorKey == "stale" {
			selector = fmt.Sprintf("a holder idle longer than %s", report.selectorValue)
		}
		switch {
		case len(report.claims) == 0:
			fmt.Printf("No claims held by %s.\n", selector)
		case !force:
			fmt.Printf("Will release %d claim(s) held by %s:\n", len(report.claims), selector)
			for _, claim := range report.claims {
				fmt.Printf("  - %s (session %s, idle %s)\n", claim.issue.ID, claim.holder, formatIdle(claim.idle))
			}
			fmt.Println("\nRun with --force to release.")
		default:
			for _, claim := range released {
				fmt.Printf("RELEASED %s → open (session %s, idle %s)\n", claim.issue.ID, claim.holder, formatIdle(claim.idle))
			}
			fmt.Printf("\nReleased %d claim(s).\n", len(released))
			if skipped > 0 {
				fmt.Printf("Skipped %d claim(s) that moved to another session mid-sweep.\n", skipped)
			}
		}

		for _, u := range report.unresolved {
			output.Warning("not evaluated: %s (%s)", u.id, u.reason)
		}
		if len(report.unresolved) > 0 {
			fmt.Printf("\n%d claim(s) left unresolved; release them explicitly with `td unstart <id>`.\n", len(report.unresolved))
		}
	}

	// Nothing to reclaim is a success. Only a release that was attempted and
	// failed makes the command report failure.
	if len(released) == 0 && failed > 0 {
		return errUnstartAllFailed
	}
	return nil
}

func sweepNoun(selectorKey string) string {
	if selectorKey == "session" {
		return "session"
	}
	return "stale"
}

// formatIdle renders an idle duration at minute resolution for human output.
// JSON reports idle_seconds exactly and does not also carry this string: two
// renderings of the same quantity that disagree in the same object make a
// caller guess which one is authoritative.
func formatIdle(d time.Duration) string {
	return d.Truncate(time.Minute).String()
}

func init() {
	rootCmd.AddCommand(unstartCmd)

	unstartCmd.Flags().String("reason", "", "Reason for unstarting")
	unstartCmd.Flags().String("session", "", "Release every in_progress claim held by this session id (exact; no liveness guess)")
	unstartCmd.Flags().String("stale", "", "Release in_progress claims whose holder has been inactive longer than this (e.g. 2h, 90m, 1d). No default: must exceed your longest healthy tick. Only mutations move the signal — see docs/multi-agent-sessions.md")
	unstartCmd.Flags().Bool("force", false, "With --session/--stale, actually release the claims (default previews)")
}
