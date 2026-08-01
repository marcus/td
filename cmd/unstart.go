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

--stale reclaims claims whose holder is gone: it releases every in_progress
issue whose implementer session has been idle longer than the given duration.
An agent killed mid-work — by a usage limit, a wall-clock timeout, a crash —
never hands off, so its claim would otherwise leak and the work never returns
to the ready queue.

It previews by default and only acts with --force, and it has no default
threshold on purpose. td's liveness signal is the implementer session's last
activity, and a live agent can legitimately work for a long time without
touching td, so the threshold is the caller's judgement: it must exceed the
longest a healthy tick can run without a td command (i.e. your tick timeout).
Too short a value releases live agents' claims and two agents then work the
same issue. td never releases the calling session's own claim, and it never
releases a claim whose holder it cannot measure (no implementer recorded, or
the session row is absent — e.g. already removed by 'td session cleanup');
those are reported so you can act on them explicitly.

Examples:
  td unstart td-abc1                    # Unstart single issue
  td unstart td-abc1 td-abc2 td-abc3    # Unstart multiple issues
  td unstart --stale 2h                 # Preview claims idle longer than 2h
  td unstart --stale 2h --force --json  # Release them, report what was released`,
	GroupID: "workflow",
	Args: func(cmd *cobra.Command, args []string) error {
		// --stale selects its targets by idle time, so it takes no ids;
		// otherwise unstart names them and needs at least one.
		stale, _ := cmd.Flags().GetString("stale")
		if stale != "" {
			if len(args) > 0 {
				return fmt.Errorf("--stale selects issues by implementer idle time; do not also name issue ids")
			}
			return nil
		}
		// --force only gates the stale sweep. Accepting it silently on the
		// named path would advertise a safety flag that does nothing.
		if force, _ := cmd.Flags().GetBool("force"); force {
			return fmt.Errorf("--force applies to --stale; naming issue ids already unstarts them")
		}
		return cobra.MinimumNArgs(1)(cmd, args)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		isJSON := jsonMode(cmd)

		emitErr := func(format string, args ...interface{}) {
			if !isJSON {
				output.Error(format, args...)
			}
		}
		emitWarn := func(format string, args ...interface{}) {
			if !isJSON {
				output.Warning(format, args...)
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

		if stale, _ := cmd.Flags().GetString("stale"); stale != "" {
			force, _ := cmd.Flags().GetBool("force")
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

			// An already-open issue is the requested end state, so a repeated
			// unstart is an idempotent success — checked before the transition
			// validation, which has no open → open edge.
			if issue.Status == models.StatusOpen {
				noopTransition(isJSON, issueID, issue.Status, "already unstarted")
				noop++
				continue
			}

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

			logMsg := "Reverted to open"
			if reason != "" {
				logMsg = reason
			}

			if err := releaseClaim(database, sess, scope, issue, logMsg, emitWarn); err != nil {
				failTransition(isJSON, output.ErrCodeDatabaseError, "failed to update %s: %v", issueID, err)
				failed++
				continue
			}

			if isJSON {
				// Re-fetch the now-open issue and emit one JSON object per id
				// (NDJSON in the bulk case).
				unstarted2, ferr := database.GetIssue(issueID)
				if ferr != nil {
					unstarted2 = issue
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
			fmt.Printf("\nUnstarted %d, skipped %d\n", unstarted, failed+noop)
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

// releaseClaim performs the unstart mutation for one issue: it drops the
// implementer claim, moves the issue back to open, and records the reason in
// the issue's history. Both the named path and the --stale path go through it
// so a reclaimed claim is indistinguishable from a hand-released one except
// for the recorded reason.
func releaseClaim(database *db.DB, sess *session.Session, scope db.SessionStateScope, issue *models.Issue, logMsg string, warn func(string, ...interface{})) error {
	// Record session action BEFORE clearing ImplementerSession (for bypass
	// prevention). This tracks that this session touched the issue, even
	// though it is being unstarted.
	if err := database.RecordSessionAction(issue.ID, sess.ID, models.ActionSessionUnstarted); err != nil {
		warn("failed to record session history: %v", err)
	}

	issue.Status = models.StatusOpen
	issue.ImplementerSession = ""

	// Update issue (atomic update + action log)
	if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionReopen); err != nil {
		return err
	}

	_ = database.AddLog(&models.Log{
		IssueID:   issue.ID,
		SessionID: sess.ID,
		Message:   logMsg,
		Type:      models.LogTypeProgress,
	})

	// Clear focus if this was the focused issue
	focusedID, _ := database.GetFocus(scope)
	if focusedID == issue.ID {
		_ = database.ClearFocus(scope)
	}
	return nil
}

// staleClaim is one in_progress issue whose implementer session has been idle
// past the threshold.
type staleClaim struct {
	issue        *models.Issue
	holder       string
	idle         time.Duration
	lastActivity time.Time
}

// unresolvedClaim is an in_progress issue whose holder's liveness cannot be
// measured, so --stale deliberately leaves it alone.
type unresolvedClaim struct {
	id     string
	holder string
	reason string
}

// unstartStale releases claims whose implementer session has been idle longer
// than maxIdle. It previews by default and mutates only under force, matching
// `td session cleanup`.
func unstartStale(database *db.DB, sess *session.Session, scope db.SessionStateScope, stale, reason string, force, isJSON bool) error {
	emitWarn := func(format string, args ...interface{}) {
		if !isJSON {
			output.Warning(format, args...)
		}
	}
	// fail reports a whole-command error exactly once: --json callers get the
	// envelope Execute emits from the returned error, human callers get the
	// message here and a sentinel that adds nothing further.
	fail := func(err error) error {
		if isJSON {
			return err
		}
		output.Error("%v", err)
		return fmt.Errorf("%w", errSilentExit)
	}

	maxIdle, err := session.ParseDuration(stale)
	if err != nil {
		return fail(fmt.Errorf("invalid duration: %s", stale))
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

	issues, err := database.ListIssues(db.ListIssuesOptions{
		Status: []models.Status{models.StatusInProgress},
	})
	if err != nil {
		return fail(fmt.Errorf("failed to list issues: %w", err))
	}

	now := time.Now()
	var claims []staleClaim
	var unresolved []unresolvedClaim

	for i := range issues {
		issue := &issues[i]
		holder := issue.ImplementerSession

		switch {
		case holder == "":
			unresolved = append(unresolved, unresolvedClaim{
				id:     issue.ID,
				reason: "no implementer session recorded",
			})
			continue
		case holder == sess.ID:
			// The calling session is definitionally live.
			continue
		}

		holderSession, ok := byID[holder]
		if !ok {
			unresolved = append(unresolved, unresolvedClaim{
				id:     issue.ID,
				holder: holder,
				reason: fmt.Sprintf("implementer session %s is not in this database", holder),
			})
			continue
		}

		lastActive := holderSession.LastActive()
		idle := now.Sub(lastActive)
		if idle <= maxIdle {
			continue
		}

		claims = append(claims, staleClaim{
			issue:        issue,
			holder:       holder,
			idle:         idle,
			lastActivity: lastActive,
		})
	}

	released := make([]staleClaim, 0, len(claims))
	failed := 0

	if force {
		for _, claim := range claims {
			logMsg := fmt.Sprintf("claim released: implementer session %s idle %s (threshold %s)",
				claim.holder, formatIdle(claim.idle), stale)
			if reason != "" {
				logMsg += "; " + reason
			}
			if err := releaseClaim(database, sess, scope, claim.issue, logMsg, emitWarn); err != nil {
				failTransition(isJSON, output.ErrCodeDatabaseError, "failed to release %s: %v", claim.issue.ID, err)
				failed++
				continue
			}
			released = append(released, claim)
		}
	}

	if isJSON {
		reported := claims
		action := "would_release_stale_claims"
		if force {
			reported = released
			action = "released_stale_claims"
		}
		rows := make([]map[string]any, 0, len(reported))
		for _, claim := range reported {
			rows = append(rows, map[string]any{
				"id":            claim.issue.ID,
				"title":         claim.issue.Title,
				"session":       claim.holder,
				"idle_seconds":  int64(claim.idle.Seconds()),
				"idle":          formatIdle(claim.idle),
				"last_activity": claim.lastActivity.UTC().Format(time.RFC3339),
			})
		}
		skipped := make([]map[string]any, 0, len(unresolved))
		for _, u := range unresolved {
			skipped = append(skipped, map[string]any{
				"id":      u.id,
				"session": u.holder,
				"reason":  u.reason,
			})
		}
		if err := output.EmitResult(action, map[string]any{
			"stale":      stale,
			"forced":     force,
			"count":      len(reported),
			"claims":     rows,
			"unresolved": skipped,
		}); err != nil {
			return err
		}
	} else {
		switch {
		case len(claims) == 0:
			fmt.Printf("No claims held by a session idle longer than %s.\n", stale)
		case !force:
			fmt.Printf("Will release %d stale claim(s) idle longer than %s:\n", len(claims), stale)
			for _, claim := range claims {
				fmt.Printf("  - %s (session %s, idle %s)\n", claim.issue.ID, claim.holder, formatIdle(claim.idle))
			}
			fmt.Println("\nRun with --force to release.")
		default:
			for _, claim := range released {
				fmt.Printf("RELEASED %s → open (session %s, idle %s)\n", claim.issue.ID, claim.holder, formatIdle(claim.idle))
			}
			fmt.Printf("\nReleased %d stale claim(s).\n", len(released))
		}

		for _, u := range unresolved {
			emitWarn("not evaluated: %s (%s)", u.id, u.reason)
		}
	}

	// Nothing to reclaim is a success. Only a release that was attempted and
	// failed makes the command report failure.
	if len(released) == 0 && failed > 0 {
		return errUnstartAllFailed
	}
	return nil
}

// formatIdle renders an idle duration at minute resolution, so JSON and human
// output agree and neither carries misleading sub-second precision.
func formatIdle(d time.Duration) string {
	return d.Truncate(time.Minute).String()
}

func init() {
	rootCmd.AddCommand(unstartCmd)

	unstartCmd.Flags().String("reason", "", "Reason for unstarting")
	unstartCmd.Flags().String("stale", "", "Release in_progress claims whose implementer session has been idle longer than this (e.g. 2h, 90m, 1d). No default: must exceed your longest healthy tick")
	unstartCmd.Flags().Bool("force", false, "With --stale, actually release the claims (default previews)")
	unstartCmd.SilenceUsage = true
}
