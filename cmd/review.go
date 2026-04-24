package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/reviewpolicy"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/workflow"
	"github.com/spf13/cobra"
)

// clearFocusIfNeeded clears focus if the focused issue matches
func clearFocusIfNeeded(baseDir, issueID string) {
	focusedID, _ := config.GetFocus(baseDir)
	if focusedID == issueID {
		config.ClearFocus(baseDir)
	}
}

// SubmitReviewResult holds the result of a review submission
type SubmitReviewResult struct {
	Success bool
	Message string
}

const autoReviewHandoffMessage = "Auto-generated for review submission"

func newAutoReviewHandoff(issueID, sessionID string) *models.Handoff {
	return &models.Handoff{
		IssueID:   issueID,
		SessionID: sessionID,
		Done:      []string{autoReviewHandoffMessage},
		Remaining: []string{},
		Decisions: []string{},
		Uncertain: []string{},
	}
}

// submitIssueForReview submits a single issue for review with proper validation,
// logging, and undo support. This is the shared logic for both reviewCmd and
// ws handoff --review.
func submitIssueForReview(database *db.DB, issue *models.Issue, sess *session.Session, baseDir string, logMsg string) SubmitReviewResult {
	fromStatus := issue.Status

	// Validate transition with state machine
	sm := workflow.DefaultMachine()
	ctx := &workflow.TransitionContext{
		Issue:      issue,
		FromStatus: issue.Status,
		ToStatus:   models.StatusInReview,
		SessionID:  sess.ID,
		Context:    workflow.ContextCLI,
	}
	_, err := sm.Validate(ctx)
	if err != nil {
		if issue.Status == models.StatusInReview {
			return SubmitReviewResult{
				Success: false,
				Message: fmt.Sprintf("cannot review %s: already in review", issue.ID) + "\n" + reviewFollowupGuidance(issue),
			}
		}
		return SubmitReviewResult{
			Success: false,
			Message: fmt.Sprintf("cannot review %s: %v", issue.ID, err),
		}
	}
	if !sm.IsValidTransition(issue.Status, models.StatusInReview) {
		return SubmitReviewResult{
			Success: false,
			Message: fmt.Sprintf("cannot review %s: invalid transition from %s", issue.ID, issue.Status),
		}
	}

	// Update issue (atomic update + action log).
	// Batch 1c stamps review_requested_by_session so delegated-mode closers
	// (Step 2) can verify the caller is the orchestrator that submitted this
	// review cycle.
	issue.Status = models.StatusInReview
	if issue.ImplementerSession == "" {
		issue.ImplementerSession = sess.ID
	}
	issue.ReviewRequestedBySession = sess.ID

	if err := database.UpdateIssueLoggedIfStatus(issue, fromStatus, sess.ID, models.ActionReview); err != nil {
		return SubmitReviewResult{
			Success: false,
			Message: describeStaleTransitionUpdate(database, "review", issue.ID, err, reviewFollowupGuidance),
		}
	}

	// Add session log
	if logMsg == "" {
		logMsg = "Submitted for review"
	}
	if err := database.AddLog(&models.Log{
		IssueID:   issue.ID,
		SessionID: sess.ID,
		Message:   logMsg,
		Type:      models.LogTypeProgress,
	}); err != nil {
		output.Warning("add log failed: %v", err)
	}

	// Clear focus if this was the focused issue
	clearFocusIfNeeded(baseDir, issue.ID)

	return SubmitReviewResult{Success: true}
}

func shouldWarnAboutAutoHandoff(database *db.DB, issueID, sessionID string) bool {
	logs, err := database.GetLogs(issueID, 10)
	if err != nil {
		return true
	}

	const substantiveContextChars = 16
	totalChars := 0
	for _, log := range logs {
		if log.SessionID != sessionID {
			continue
		}

		if !isSubstantiveReviewContextLog(log) {
			continue
		}

		totalChars += len([]rune(strings.TrimSpace(log.Message)))
		if log.Type != models.LogTypeProgress {
			return false
		}
		if totalChars >= substantiveContextChars {
			return false
		}
	}

	return true
}

func isSubstantiveReviewContextLog(log models.Log) bool {
	message := strings.TrimSpace(log.Message)
	if message == "" {
		return false
	}

	switch log.Type {
	case models.LogTypeDecision, models.LogTypeHypothesis, models.LogTypeTried, models.LogTypeResult, models.LogTypeBlocker, models.LogTypeSecurity:
		return true
	case models.LogTypeProgress, models.LogTypeOrchestration:
		return !isRoutineWorkflowLogMessage(message)
	default:
		return true
	}
}

func isRoutineWorkflowLogMessage(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		return true
	}

	switch normalized {
	case "started work", "submitted for review", "approved", "rejected", "closed":
		return true
	}

	prefixes := []string{
		"submitted for review via ",
		"approved: ",
		"rejected: ",
		"closed: ",
		"cascaded review from ",
		"auto-unblocked (",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}

	return false
}

var reviewCmd = &cobra.Command{
	Use:     "review [issue-id...]",
	Aliases: []string{"submit", "finish"},
	Short:   "Submit one or more issues for review",
	Long: `Submits the issue(s) for review. If no handoff exists, a minimal one is
auto-created (consider using 'td handoff' for better documentation).

The submitting session is recorded as 'review_requested_by_session' on the
issue. Under review_policy_mode=delegated, an active independent approval is
the close gate, so any session may close after a reviewer records approval.

For epics/parent issues, automatically cascades to all open/in_progress
descendants. Cascaded children don't require individual handoffs.

Supports bulk operations:
  td review td-abc1 td-abc2 td-abc3    # Submit multiple issues for review`,
	GroupID: "workflow",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		jsonOutput, _ := cmd.Flags().GetBool("json")

		database, err := db.Open(baseDir)
		if err != nil {
			if jsonOutput {
				output.JSONError(output.ErrCodeDatabaseError, err.Error())
			} else {
				output.Error("%v", err)
			}
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			if jsonOutput {
				output.JSONError(output.ErrCodeNoActiveSession, err.Error())
			} else {
				output.Error("%v", err)
			}
			return err
		}

		reviewed := 0
		skipped := 0
		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				if jsonOutput {
					output.JSONError(output.ErrCodeNotFound, err.Error())
				} else {
					output.Warning("issue not found: %s", issueID)
				}
				skipped++
				continue
			}

			// Check for handoff - auto-create if missing
			handoff, err := database.GetLatestHandoff(issueID)
			if err != nil || handoff == nil {
				// Auto-create minimal handoff
				autoHandoff := newAutoReviewHandoff(issueID, sess.ID)
				if err := database.AddHandoff(autoHandoff); err != nil {
					if jsonOutput {
						output.JSONError(output.ErrCodeDatabaseError, fmt.Sprintf("failed to create handoff: %v", err))
					} else {
						output.Error("failed to create handoff for %s: %v", issueID, err)
					}
					skipped++
					continue
				}
				if shouldWarnAboutAutoHandoff(database, issueID, sess.ID) {
					output.Warning("auto-created minimal handoff for %s - consider using 'td handoff' for better documentation", issueID)
				}
			}

			// Handle --minor flag
			if minor, _ := cmd.Flags().GetBool("minor"); minor {
				issue.Minor = true
			}

			// Prepare log message (supports --reason, --message, --comment, --note, --notes)
			reason := approvalReason(cmd)
			logMsg := "Submitted for review"
			if reason != "" {
				logMsg = reason
			}

			// Use shared function for consistent validation, logging, and undo support
			result := submitIssueForReview(database, issue, sess, baseDir, logMsg)
			if !result.Success {
				if jsonOutput {
					output.JSONError(output.ErrCodeDatabaseError, result.Message)
				} else {
					output.Warning("%s", result.Message)
				}
				skipped++
				continue
			}

			fmt.Printf("REVIEW REQUESTED %s (session: %s)\n", issueID, sess.ID)

			// Cascade to descendants if this is a parent issue
			hasChildren, _ := database.HasChildren(issueID)
			if hasChildren {
				descendants, err := database.GetDescendantIssues(issueID, []models.Status{
					models.StatusOpen,
					models.StatusInProgress,
				})
				if err == nil && len(descendants) > 0 {
					cascaded := 0
					for _, child := range descendants {
						cascadeResult := submitIssueForReview(database, child, sess, baseDir, fmt.Sprintf("Cascaded review from %s", issueID))
						if !cascadeResult.Success {
							output.Warning("failed to cascade review to %s: %s", child.ID, cascadeResult.Message)
							continue
						}
						cascaded++
					}

					if cascaded > 0 {
						fmt.Printf("  + %d descendant(s) also marked for review\n", cascaded)
					}
				}
			}

			// Cascade up: if all siblings are in_review (or closed), update parent epic
			if count, ids := database.CascadeUpParentStatus(issueID, models.StatusInReview, sess.ID); count > 0 {
				for _, id := range ids {
					fmt.Printf("  ↑ Parent %s auto-cascaded to %s\n", id, models.StatusInReview)
				}
			}

			reviewed++
		}

		if len(args) > 1 {
			fmt.Printf("\nReviewed %d, skipped %d\n", reviewed, skipped)
		}
		return nil
	},
}

func approvalReason(cmd *cobra.Command) string {
	// Precedence: --reason > --message > --note > --notes > --comment
	for _, flag := range []string{"reason", "message", "note", "notes", "comment"} {
		v, _ := cmd.Flags().GetString(flag)
		if v != "" {
			return v
		}
	}
	return ""
}

func describeStaleTransitionUpdate(database *db.DB, action, issueID string, err error, guidance func(*models.Issue) string) string {
	var staleErr *db.StaleIssueStatusError
	if !errors.As(err, &staleErr) {
		return fmt.Sprintf("failed to update %s: %v", issueID, err)
	}

	current := &models.Issue{ID: issueID, Status: staleErr.Actual}
	if database != nil {
		if refreshed, refreshErr := database.GetIssue(issueID); refreshErr == nil {
			current = refreshed
		}
	}

	lines := []string{
		fmt.Sprintf("cannot %s %s: status changed from %s to %s in another session", action, issueID, staleErr.Expected, current.Status),
		fmt.Sprintf("  Current status: %s", current.Status),
	}
	if recent := recentWorkflowTransitionContext(database, issueID); recent != "" {
		lines = append(lines, recent)
	}
	lines = append(lines, guidance(current))
	return strings.Join(lines, "\n")
}

func recentWorkflowTransitionContext(database *db.DB, issueID string) string {
	if database == nil {
		return ""
	}

	logs, err := database.GetLogs(issueID, 5)
	if err != nil {
		return ""
	}

	for i := len(logs) - 1; i >= 0; i-- {
		log := logs[i]
		if summary, ok := summarizeWorkflowTransition(log.Message); ok {
			return fmt.Sprintf("  Recent transition: %s by %s at %s", summary, log.SessionID, log.Timestamp.Format("2006-01-02 15:04"))
		}
	}

	return ""
}

func summarizeWorkflowTransition(message string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(message))
	switch {
	case normalized == "approved" || strings.HasPrefix(normalized, "approved:") || strings.Contains(normalized, "] approved "):
		return "approved", true
	case normalized == "rejected" || strings.HasPrefix(normalized, "rejected:"):
		return "rejected", true
	case normalized == "reopened" || strings.HasPrefix(normalized, "reopened:"):
		return "reopened", true
	case normalized == "closed" || strings.HasPrefix(normalized, "closed:") || strings.Contains(normalized, "] closed "):
		return "closed", true
	case normalized == "submitted for review" || strings.HasPrefix(normalized, "submitted for review:") || strings.HasPrefix(normalized, "cascaded review from "):
		return "submitted for review", true
	default:
		return "", false
	}
}

func describeReviewerNoop(database *db.DB, action string, issue *models.Issue) string {
	if issue == nil {
		return ""
	}

	header := ""
	switch action {
	case "approve":
		header = fmt.Sprintf("already approved/closed %s", issue.ID)
	case "reject":
		header = fmt.Sprintf("already reopened %s", issue.ID)
	default:
		header = fmt.Sprintf("already handled %s", issue.ID)
	}

	lines := []string{
		header,
		fmt.Sprintf("  Current status: %s", issue.Status),
	}
	if recent := recentWorkflowTransitionContext(database, issue.ID); recent != "" {
		lines = append(lines, recent)
	}
	if action == "approve" {
		lines = append(lines, approveFollowupGuidance(issue))
	} else {
		lines = append(lines, rejectFollowupGuidance(issue))
	}
	return strings.Join(lines, "\n")
}

func closeFollowupGuidance(issue *models.Issue) string {
	if issue == nil {
		return "  Submit for review: td review "
	}
	switch issue.Status {
	case models.StatusInReview:
		return fmt.Sprintf("  Already in review: td approve %s", issue.ID)
	case models.StatusClosed:
		return fmt.Sprintf("  Already closed: td show %s", issue.ID)
	}
	return fmt.Sprintf("  Submit for review: td review %s", issue.ID)
}

// reviewFollowupGuidance returns the next workflow command after a failed
// review submission attempt.
func reviewFollowupGuidance(issue *models.Issue) string {
	if issue == nil {
		return "  Submit for review: td review "
	}
	switch issue.Status {
	case models.StatusInReview:
		return fmt.Sprintf("  Already in review: td approve %s", issue.ID)
	case models.StatusClosed:
		return fmt.Sprintf("  Already closed: td show %s", issue.ID)
	}
	return fmt.Sprintf("  Submit for review: td review %s", issue.ID)
}

// approveFollowupGuidance returns the next workflow command after a failed
// approval attempt.
func approveFollowupGuidance(issue *models.Issue) string {
	if issue == nil {
		return "  Submit for review first: td review "
	}
	switch issue.Status {
	case models.StatusInReview:
		return fmt.Sprintf("  Approve it: td approve %s", issue.ID)
	case models.StatusClosed:
		return fmt.Sprintf("  Already approved/closed: td show %s", issue.ID)
	}
	return fmt.Sprintf("  Submit for review first: td review %s", issue.ID)
}

func rejectFollowupGuidance(issue *models.Issue) string {
	if issue == nil {
		return "  Already reopened: td show "
	}
	switch issue.Status {
	case models.StatusOpen:
		return fmt.Sprintf("  Already reopened: td show %s", issue.ID)
	case models.StatusInReview:
		return fmt.Sprintf("  Reject it: td reject %s", issue.ID)
	case models.StatusClosed:
		return fmt.Sprintf("  Already closed: td show %s", issue.ID)
	}
	return fmt.Sprintf("  Submit for review first: td review %s", issue.ID)
}

var approveCmd = &cobra.Command{
	Use:   "approve [issue-id...]",
	Short: "Approve and close one or more issues, or record a review",
	Long: `Approves the issue(s). You cannot review your own implementation, but you
can close after an independent review has been recorded. 'td approve' operates
in one of three modes:

  Mode A: Review + close (default)
    Caller is an eligible reviewer and no active approval exists.
    Records the approval AND closes the issue in one transaction.

  Mode B: Record-only approval (--record-only, delegated mode only)
    Caller is an eligible reviewer. Records an approval review without
    closing. Requires --reason. Use this when a reviewer sub-agent should
    attest the work and the orchestrator / implementer / review-requester
    closes later. Pass --decision changes_requested to record a non-approving
    review instead of an approval.

  Mode C: Close using recorded approval (delegated mode only)
    An active approval review already exists. Any session may close.
    --reason is required if the closing session is not the same as the
    reviewer-of-record.

Flag summary:
  --record-only                       record an approval review without closing
  --decision approved|changes_requested  review decision (use with --record-only)
  --reason "..."                      required for --record-only and delegated close
  --all                               approve all reviewable issues for this session

Examples:
  td approve td-abc1 td-abc2 td-abc3                       # Approve multiple
  td approve --all                                         # Approve all reviewable
  td approve td-abc1 --record-only --reason "looks good"   # Record approval only
  td approve td-abc1 --record-only --decision changes_requested --reason "fix X"

To surface issues reviewed by a sub-agent that you can close, use
  td reviewable --include-approved`,
	GroupID: "workflow",
	Args:    cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		jsonOutput, _ := cmd.Flags().GetBool("json")
		all, _ := cmd.Flags().GetBool("all")
		recordOnly, _ := cmd.Flags().GetBool("record-only")
		decisionFlag, _ := cmd.Flags().GetString("decision")
		balancedPolicy := balancedReviewPolicyEnabled(baseDir)
		mode, err := resolveReviewPolicyMode(baseDir)
		if err != nil {
			output.Error("review_policy_mode: %v", err)
			return err
		}

		// Record-only is only meaningful under delegated mode.
		if recordOnly && mode != reviewpolicy.ModeDelegated {
			msg := "--record-only requires review_policy_mode=delegated"
			if jsonOutput {
				output.JSONError(output.ErrCodeInvalidInput, msg)
			} else {
				output.Error("%s", msg)
			}
			return fmt.Errorf("%s", msg)
		}

		// Validate --decision value when set.
		decision := reviewpolicy.DecisionApproved
		if decisionFlag != "" {
			switch decisionFlag {
			case reviewpolicy.DecisionApproved:
				decision = reviewpolicy.DecisionApproved
			case reviewpolicy.DecisionChangesRequested:
				decision = reviewpolicy.DecisionChangesRequested
			default:
				msg := fmt.Sprintf("invalid --decision %q (want approved|changes_requested)", decisionFlag)
				if jsonOutput {
					output.JSONError(output.ErrCodeInvalidInput, msg)
				} else {
					output.Error("%s", msg)
				}
				return fmt.Errorf("%s", msg)
			}
		}
		if decision == reviewpolicy.DecisionChangesRequested && !recordOnly {
			msg := "--decision changes_requested requires --record-only"
			if jsonOutput {
				output.JSONError(output.ErrCodeInvalidInput, msg)
			} else {
				output.Error("%s", msg)
			}
			return fmt.Errorf("%s", msg)
		}

		// Build list of issue IDs to approve
		var issueIDs []string
		if all {
			// Get all issues the current session can act on: reviewable work,
			// plus delegated-mode ready-to-close work when not recording a review.
			issues, err := approvalCandidateIssues(database, baseDir, sess.ID, !recordOnly)
			if err != nil {
				output.Error("failed to list reviewable issues: %v", err)
				return err
			}
			for _, issue := range issues {
				issueIDs = append(issueIDs, issue.ID)
			}
		} else {
			issueIDs = args
			if len(issueIDs) == 0 {
				issues, err := approvalCandidateIssues(database, baseDir, sess.ID, !recordOnly)
				if err != nil {
					output.Error("failed to list reviewable issues: %v", err)
					return err
				}
				switch len(issues) {
				case 0:
					output.Error("no issues to approve. Provide issue IDs or use --all")
					return fmt.Errorf("no issues specified")
				case 1:
					issueIDs = []string{issues[0].ID}
				default:
					output.Error("no issue ID specified. Multiple issues awaiting your review:")
					for _, issue := range issues {
						fmt.Printf("  %s: %s\n", issue.ID, issue.Title)
					}
					fmt.Printf("\nUsage: td approve <issue-id>\n")
					fmt.Printf("Or use: td approve --all\n")
					return fmt.Errorf("issue ID required")
				}
			}
		}

		if len(issueIDs) == 0 {
			output.Error("no issues to approve. Provide issue IDs or use --all")
			return fmt.Errorf("no issues specified")
		}

		approved := 0
		skipped := 0
		for _, issueID := range issueIDs {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				if jsonOutput {
					output.JSONError(output.ErrCodeNotFound, err.Error())
				} else {
					output.Warning("issue not found: %s", issueID)
				}
				skipped++
				continue
			}

			// Validate transition with state machine
			sm := workflow.DefaultMachine()
			if issue.Status == models.StatusClosed {
				message := describeReviewerNoop(database, "approve", issue)
				if jsonOutput {
					if err := output.JSON(map[string]interface{}{
						"id":      issueID,
						"status":  string(issue.Status),
						"action":  "already approved/closed",
						"message": message,
					}); err != nil {
						output.JSONError(output.ErrCodeDatabaseError, err.Error())
					}
				} else if !all {
					output.Warning("%s", message)
				}
				skipped++
				continue
			}

			// Look up active approval (delegated-mode routing input).
			var activeApproval *models.IssueReview
			if mode == reviewpolicy.ModeDelegated {
				activeApproval, _ = database.GetActiveApprovalReview(issueID)
			}

			// Record-only path (Mode B): do NOT transition status.
			if recordOnly {
				reason := approvalReason(cmd)
				if reason == "" {
					msg := fmt.Sprintf("--record-only requires --reason for %s", issueID)
					if jsonOutput {
						output.JSONError(output.ErrCodeInvalidInput, msg)
					} else if !all {
						output.Error("%s", msg)
					}
					skipped++
					continue
				}
				// Minor issues bypass review entirely — they self-review and close in
				// one step. Record-only reviews on minor issues are semantically
				// meaningless and would leave orphan review rows, so reject up-front.
				if issue.Minor {
					msg := fmt.Sprintf("minor issues do not require reviews: %s", issueID)
					if jsonOutput {
						output.JSONError(output.ErrCodeInvalidInput, msg)
					} else if !all {
						output.Warning("%s", msg)
					}
					skipped++
					continue
				}
				if issue.Status != models.StatusInReview {
					msg := fmt.Sprintf("cannot record review: %s is not in review (currently %s)", issueID, issue.Status)
					if jsonOutput {
						output.JSONError(output.ErrCodeDatabaseError, msg)
					} else if !all {
						output.Warning("%s", msg)
					}
					skipped++
					continue
				}
				if approvedDec := reviewpolicy.DecisionApproved; decision == approvedDec && activeApproval != nil {
					msg := fmt.Sprintf("cannot record approval: %s already has an active approval (review %s by %s) — close it with `td approve %s`", issueID, activeApproval.ID, activeApproval.ReviewerSession, issueID)
					if jsonOutput {
						output.JSONError(output.ErrCodeDatabaseError, msg)
					} else if !all {
						output.Warning("%s", msg)
					}
					skipped++
					continue
				}

				wasInvolved, err := database.WasSessionInvolved(issueID, sess.ID)
				if err != nil {
					output.Warning("failed to check session history for %s: %v", issueID, err)
					wasInvolved = true
				}
				wasImplInvolved, implErr := database.WasSessionImplementationInvolved(issueID, sess.ID)
				if implErr != nil {
					output.Warning("failed to check implementation history for %s: %v", issueID, implErr)
					wasImplInvolved = true
				}

				// Record-only is delegated-mode-only (gated above), so pass mode
				// directly instead of hardcoding balanced. Under delegated a prior
				// reviewer may re-review after a reject/re-review cycle; the
				// balanced fallback would incorrectly block via WasAnyInvolved.
				eligibility := evaluateApproveEligibilityWithMode(issue, sess.ID, wasInvolved, wasImplInvolved, mode)
				if !eligibility.Allowed {
					if !all {
						if jsonOutput {
							output.JSONError(output.ErrCodeCannotSelfApprove, eligibility.RejectionMessage)
						} else {
							output.Error("%s", eligibility.RejectionMessage)
						}
					}
					skipped++
					continue
				}

				// Supersede any existing non-superseded review rows before
				// we insert a new row so audit history always has at most
				// one active row per issue.
				priorActive := ""
				if pa, _ := database.GetActiveApprovalReview(issueID); pa != nil {
					priorActive = pa.ID
				}
				if err := database.SupersedeActiveReviews(issueID); err != nil {
					output.Warning("failed to supersede prior reviews for %s: %v", issueID, err)
				}

				reviewID, err := database.CreateIssueReview(issueID, sess.ID, decision, reason, issue.ReviewRequestedBySession)
				if err != nil {
					output.Error("failed to record review: %v", err)
					skipped++
					continue
				}

				// For approved decisions, stamp reviewer_session/reviewed_at
				// on the issue so status/reviewable surfaces pick it up.
				// For changes_requested, do NOT stamp — that would masquerade
				// as a real approval.
				actionType := models.ActionReviewApprove
				if decision == reviewpolicy.DecisionChangesRequested {
					actionType = models.ActionReviewChangesRequested
				} else {
					now := time.Now()
					issue.ReviewerSession = sess.ID
					issue.ReviewedAt = &now
				}

				if err := database.UpdateIssueLoggedWithReviewMeta(issue, models.StatusInReview, sess.ID, actionType, reviewID, priorActive); err != nil {
					output.Warning("%s", describeStaleTransitionUpdate(database, "record review", issueID, err, approveFollowupGuidance))
					skipped++
					continue
				}

				sessionAction := models.ActionSessionReviewApproved
				if decision == reviewpolicy.DecisionChangesRequested {
					sessionAction = models.ActionSessionReviewChangesRequested
				}
				if err := database.RecordSessionAction(issueID, sess.ID, sessionAction); err != nil {
					output.Warning("failed to record session history: %v", err)
				}

				logMsg := fmt.Sprintf("Review recorded (%s): %s", decision, reason)
				if err := database.AddLog(&models.Log{
					IssueID:   issueID,
					SessionID: sess.ID,
					Message:   logMsg,
					Type:      models.LogTypeProgress,
				}); err != nil {
					output.Warning("add log failed: %v", err)
				}

				fmt.Printf("REVIEW RECORDED %s (decision: %s, reviewer: %s)\n", issueID, decision, sess.ID)
				approved++
				continue
			}

			// Mode C: close using recorded approval (delegated mode only).
			if mode == reviewpolicy.ModeDelegated && activeApproval != nil {
				wasInvolved, err := database.WasSessionInvolved(issueID, sess.ID)
				if err != nil {
					output.Warning("failed to check session history for %s: %v", issueID, err)
					wasInvolved = true
				}
				wasImplInvolved, implErr := database.WasSessionImplementationInvolved(issueID, sess.ID)
				if implErr != nil {
					output.Warning("failed to check implementation history for %s: %v", issueID, implErr)
					wasImplInvolved = true
				}
				hasImplHistoryForIssue, histErr := database.HasImplementationHistory(issueID)
				if histErr != nil {
					output.Warning("failed to check issue impl history for %s: %v", issueID, histErr)
					hasImplHistoryForIssue = true
				}

				closeDec := evaluateCloseEligibilityForBaseDir(baseDir, issue, sess.ID, wasInvolved, wasImplInvolved, hasImplHistoryForIssue, true /*hasActiveApproval*/)
				if !closeDec.Allowed {
					msg := closeDec.RejectionMessage
					if msg == "" {
						msg = fmt.Sprintf("cannot close %s: no active independent approval review exists", issueID)
					}
					if jsonOutput {
						output.JSONError(output.ErrCodeCannotSelfApprove, msg)
					} else if !all {
						output.Error("%s", msg)
						output.Error("  The issue has a recorded approval by %s (review %s).", activeApproval.ReviewerSession, activeApproval.ID)
						output.Error("  Delegated mode allows any session to close after that approval; missing or stale approval records block close.")
					}
					skipped++
					continue
				}

				reason := approvalReason(cmd)
				closerIsReviewer := activeApproval.ReviewerSession == sess.ID
				if !closerIsReviewer && reason == "" {
					msg := fmt.Sprintf("close-using-recorded-approval requires --reason when closer (%s) != reviewer (%s) for %s", sess.ID, activeApproval.ReviewerSession, issueID)
					if jsonOutput {
						output.JSONError(output.ErrCodeInvalidInput, msg)
					} else if !all {
						output.Error("%s", msg)
					}
					skipped++
					continue
				}

				// Close: status -> closed, stamp closed_by_session/closed_at.
				// Do NOT overwrite reviewer_session / reviewed_at.
				issue.Status = models.StatusClosed
				issue.ClosedBySession = sess.ID
				now := time.Now()
				issue.ClosedAt = &now

				if err := database.UpdateIssueLoggedWithReviewMeta(issue, models.StatusInReview, sess.ID, models.ActionCloseAfterReview, "", ""); err != nil {
					output.Warning("%s", describeStaleTransitionUpdate(database, "approve", issueID, err, approveFollowupGuidance))
					skipped++
					continue
				}

				if err := database.RecordSessionAction(issueID, sess.ID, models.ActionSessionClosed); err != nil {
					output.Warning("failed to record session history: %v", err)
				}

				logMsg := fmt.Sprintf("Closed after review %s (by %s)", activeApproval.ID, activeApproval.ReviewerSession)
				if reason != "" {
					logMsg = logMsg + ": " + reason
				}
				if err := database.AddLog(&models.Log{
					IssueID:   issueID,
					SessionID: sess.ID,
					Message:   logMsg,
					Type:      models.LogTypeProgress,
				}); err != nil {
					output.Warning("add log failed: %v", err)
				}

				clearFocusIfNeeded(baseDir, issueID)
				fmt.Printf("APPROVED %s (closed by %s using review by %s)\n", issueID, sess.ID, activeApproval.ReviewerSession)

				if count, ids := database.CascadeUpParentStatus(issueID, models.StatusClosed, sess.ID); count > 0 {
					for _, id := range ids {
						fmt.Printf("  ↑ Parent %s auto-cascaded to %s\n", id, models.StatusClosed)
					}
				}
				if count, ids := database.CascadeUnblockDependents(issueID, sess.ID); count > 0 {
					for _, id := range ids {
						fmt.Printf("  ↓ Dependent %s auto-unblocked\n", id)
					}
				}
				approved++
				continue
			}

			// Mode A (fall-through): direct reviewer + close.
			if !sm.IsValidTransition(issue.Status, models.StatusClosed) {
				if !all {
					if jsonOutput {
						output.JSONError(output.ErrCodeDatabaseError, fmt.Sprintf("cannot approve %s: invalid transition from %s", issueID, issue.Status))
					} else {
						output.Warning("cannot approve %s: invalid transition from %s", issueID, issue.Status)
						fmt.Println(approveFollowupGuidance(issue))
					}
				}
				skipped++
				continue
			}

			reason := approvalReason(cmd)

			// Check session involvement (conservative on DB errors).
			wasInvolved, err := database.WasSessionInvolved(issueID, sess.ID)
			if err != nil {
				output.Warning("failed to check session history for %s: %v", issueID, err)
				wasInvolved = true // Conservative: assume involvement on error
			}

			wasImplementationInvolved := false
			if balancedPolicy && !issue.Minor {
				implInvolved, implErr := database.WasSessionImplementationInvolved(issueID, sess.ID)
				if implErr != nil {
					output.Warning("failed to check implementation history for %s: %v", issueID, implErr)
					wasImplementationInvolved = true // Conservative: assume implementation involvement
				} else {
					wasImplementationInvolved = implInvolved
				}
			}
			if mode == reviewpolicy.ModeDelegated && !issue.Minor {
				implInvolved, implErr := database.WasSessionImplementationInvolved(issueID, sess.ID)
				if implErr != nil {
					output.Warning("failed to check implementation history for %s: %v", issueID, implErr)
					wasImplementationInvolved = true
				} else {
					wasImplementationInvolved = implInvolved
				}
			}

			// Route through mode-aware wrapper so delegated mode honors the
			// "prior reviewers may re-review" rule rather than falling into
			// balanced's WasAnyInvolved block.
			eligibility := evaluateApproveEligibilityWithMode(issue, sess.ID, wasInvolved, wasImplementationInvolved, mode)
			if !eligibility.Allowed {
				if !all { // Only show error for explicit requests
					if jsonOutput {
						output.JSONError(output.ErrCodeCannotSelfApprove, eligibility.RejectionMessage)
					} else {
						output.Error("%s", eligibility.RejectionMessage)
					}
				}
				skipped++
				continue
			}

			if eligibility.RequiresReason && reason == "" {
				msg := fmt.Sprintf("creator approval exception requires --reason for %s", issueID)
				if jsonOutput {
					output.JSONError(output.ErrCodeInvalidInput, msg)
				} else if !all {
					output.Error("%s", msg)
				} else {
					output.Warning("skipping %s: creator approval exception requires --reason", issueID)
				}
				skipped++
				continue
			}

			// Direct reviewer-close: one transaction for approve+close.
			issue.Status = models.StatusClosed
			issue.ReviewerSession = sess.ID
			issue.ClosedBySession = sess.ID
			now := time.Now()
			issue.ReviewedAt = &now
			issue.ClosedAt = &now

			// Supersede any stale (changes_requested) rows and snapshot the
			// prior-active review id for undo.
			priorActive := ""
			if pa, _ := database.GetActiveApprovalReview(issueID); pa != nil {
				priorActive = pa.ID
			}
			if err := database.SupersedeActiveReviews(issueID); err != nil {
				output.Warning("failed to supersede prior reviews for %s: %v", issueID, err)
			}

			// Create the approval row first so we can record its id in
			// the action_log payload via UpdateIssueLoggedWithReviewMeta.
			reviewID, err := database.CreateIssueReview(
				issueID, sess.ID, reviewpolicy.DecisionApproved, reason, issue.ReviewRequestedBySession,
			)
			if err != nil {
				output.Warning("failed to record issue review: %v", err)
			}

			if err := database.UpdateIssueLoggedWithReviewMeta(issue, models.StatusInReview, sess.ID, models.ActionApprove, reviewID, priorActive); err != nil {
				output.Warning("%s", describeStaleTransitionUpdate(database, "approve", issueID, err, approveFollowupGuidance))
				skipped++
				continue
			}

			// Record session action for bypass prevention
			if err := database.RecordSessionAction(issueID, sess.ID, models.ActionSessionReviewed); err != nil {
				output.Warning("failed to record session history: %v", err)
			}

			// Log (supports --reason, --message, --comment)
			logMsg := "Approved"
			logType := models.LogTypeProgress
			if reason != "" {
				logMsg = "Approved: " + reason
			}
			if eligibility.CreatorException {
				agentInfo := sess.AgentType
				if agentInfo == "" {
					agentInfo = "Unknown Agent"
				}
				logMsg = fmt.Sprintf("[%s] Approved (CREATOR EXCEPTION: %s)", agentInfo, reason)
				logType = models.LogTypeSecurity
				db.LogSecurityEvent(baseDir, db.SecurityEvent{
					IssueID:   issueID,
					SessionID: sess.ID,
					AgentType: sess.AgentType,
					Reason:    "creator_approval_exception: " + reason,
				})
			}

			if err := database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      logType,
			}); err != nil {
				output.Warning("add log failed: %v", err)
			}

			// Clear focus if this was the focused issue
			clearFocusIfNeeded(baseDir, issueID)

			if eligibility.CreatorException {
				fmt.Printf("APPROVED %s (reviewer: %s, creator exception)\n", issueID, sess.ID)
			} else {
				fmt.Printf("APPROVED %s (reviewer: %s)\n", issueID, sess.ID)
			}

			// Cascade up: if all siblings are closed, update parent epic
			if count, ids := database.CascadeUpParentStatus(issueID, models.StatusClosed, sess.ID); count > 0 {
				for _, id := range ids {
					fmt.Printf("  ↑ Parent %s auto-cascaded to %s\n", id, models.StatusClosed)
				}
			}

			// Auto-unblock dependents whose dependencies are now all closed
			if count, ids := database.CascadeUnblockDependents(issueID, sess.ID); count > 0 {
				for _, id := range ids {
					fmt.Printf("  ↓ Dependent %s auto-unblocked\n", id)
				}
			}

			approved++
		}

		if len(issueIDs) > 1 {
			fmt.Printf("\nApproved %d, skipped %d\n", approved, skipped)
		}
		return nil
	},
}

var rejectCmd = &cobra.Command{
	Use:   "reject [issue-id...]",
	Short: "Reject and return to open",
	Long: `Rejects the issue(s) and returns them to open status so they can be
picked up again by td next.

Supports bulk operations:
  td reject td-abc1 td-abc2    # Reject multiple issues`,
	GroupID: "workflow",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		jsonOutput, _ := cmd.Flags().GetBool("json")

		database, err := db.Open(baseDir)
		if err != nil {
			if jsonOutput {
				output.JSONError(output.ErrCodeDatabaseError, err.Error())
			} else {
				output.Error("%v", err)
			}
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			if jsonOutput {
				output.JSONError(output.ErrCodeNoActiveSession, err.Error())
			} else {
				output.Error("%v", err)
			}
			return err
		}

		rejected := 0
		skipped := 0
		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				if jsonOutput {
					output.JSONError(output.ErrCodeNotFound, err.Error())
				} else {
					output.Warning("issue not found: %s", issueID)
				}
				skipped++
				continue
			}

			// Reject is only valid from in_review (matches HTTP API behavior)
			if issue.Status == models.StatusOpen {
				message := describeReviewerNoop(database, "reject", issue)
				if jsonOutput {
					if err := output.JSON(map[string]interface{}{
						"id":      issueID,
						"status":  string(issue.Status),
						"action":  "already reopened",
						"message": message,
					}); err != nil {
						output.JSONError(output.ErrCodeDatabaseError, err.Error())
					}
				} else {
					output.Warning("%s", message)
				}
				skipped++
				continue
			}
			if issue.Status != models.StatusInReview {
				if jsonOutput {
					output.JSONError(output.ErrCodeDatabaseError, fmt.Sprintf("cannot reject %s: must be in_review (currently %s)", issueID, issue.Status))
				} else {
					output.Warning("cannot reject %s: must be in_review (currently %s)", issueID, issue.Status)
				}
				skipped++
				continue
			}

			// Update issue: reset to open so td next can pick it up again.
			// Step 2 clears reviewer_session / reviewed_at / review_requested_by_session
			// and supersedes any active approval review so a later re-review
			// cycle does not inherit a stale approval or requester stamp.
			issue.Status = models.StatusOpen
			issue.ImplementerSession = ""
			issue.ReviewerSession = ""
			issue.ReviewedAt = nil
			issue.ReviewRequestedBySession = ""

			if err := database.SupersedeActiveReviews(issueID); err != nil {
				output.Warning("failed to supersede active reviews for %s: %v", issueID, err)
			}

			if err := database.UpdateIssueLoggedIfStatus(issue, models.StatusInReview, sess.ID, models.ActionReject); err != nil {
				if jsonOutput {
					output.JSONError(output.ErrCodeDatabaseError, err.Error())
				} else {
					output.Warning("%s", describeStaleTransitionUpdate(database, "reject", issueID, err, rejectFollowupGuidance))
				}
				skipped++
				continue
			}

			// Log (supports --reason, --message, --comment, --note, --notes)
			reason := approvalReason(cmd)
			logMsg := "Rejected"
			if reason != "" {
				logMsg = "Rejected: " + reason
			}

			if err := database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      models.LogTypeProgress,
			}); err != nil {
				output.Warning("add log failed: %v", err)
			}

			if jsonOutput {
				result := map[string]interface{}{
					"id":      issueID,
					"status":  "open",
					"action":  "rejected",
					"session": sess.ID,
				}
				if reason != "" {
					result["reason"] = reason
				}
				output.JSON(result)
			} else {
				fmt.Printf("REJECTED %s → open\n", issueID)
			}
			rejected++
		}

		if len(args) > 1 && !jsonOutput {
			fmt.Printf("\nRejected %d, skipped %d\n", rejected, skipped)
		}
		return nil
	},
}

var closeCmd = &cobra.Command{
	Use:     "close [issue-id...]",
	Aliases: []string{"done", "complete"},
	Short:   "Admin close: duplicates, won't-fix, cleanup (NOT for reviewed work)",
	Long: `Closes the issue(s) directly. Admin-only scope: duplicates, won't-fix,
or cleanup of never-implemented issues.

DO NOT use 'td close' to finish reviewed implementation work. Reviewed work
must flow through 'td review' -> 'td approve' so an independent review is
recorded. An independent review is required; the close may be delegated to
any session via 'td approve'.

Under review_policy_mode=delegated:
  - in_review issues cannot be closed via 'td close'; use 'td approve' instead.
  - non-minor open|in_progress|blocked issues with implementation history
    require --admin (or --self-close-exception) to close.

Self-closing issues you implemented requires --self-close-exception "reason".

Examples:
  td close td-abc1                                       # Close (fails if you implemented it)
  td close td-abc1 -m "duplicate of td-xyz"              # Close unworked issue with reason
  td close td-abc1 --self-close-exception "trivial fix"  # Override for implemented work
  td close td-abc1 --admin "duplicate"                   # Admin close (delegated mode)
  td done                                                # Close focused issue (if set)`,
	GroupID: "workflow",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		// If no args provided, try to use focused issue
		if len(args) == 0 {
			focusedID, err := config.GetFocus(baseDir)
			if err != nil || focusedID == "" {
				output.Error("no issue specified and no focused issue")
				fmt.Println("  Usage: td close <issue-id>")
				fmt.Println("  Or set focus first: td focus <issue-id>")
				return fmt.Errorf("no issue specified")
			}
			args = []string{focusedID}
		}

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Get self-close-exception flag once
		selfCloseException, _ := cmd.Flags().GetString("self-close-exception")
		adminReason, _ := cmd.Flags().GetString("admin")
		mode, _ := resolveReviewPolicyMode(baseDir)

		closed := 0
		skipped := 0
		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Warning("issue not found: %s", issueID)
				skipped++
				continue
			}

			// Validate transition with state machine
			sm := workflow.DefaultMachine()
			if !sm.IsValidTransition(issue.Status, models.StatusClosed) {
				output.Warning("cannot close %s: invalid transition from %s", issueID, issue.Status)
				skipped++
				continue
			}

			// Step 2 close-path hardening: reject `in_review -> closed` via
			// td close unless the issue is minor. Non-minor review work must
			// go through td approve so the review-attestation path records
			// reviewer and closer separately.
			if issue.Status == models.StatusInReview && !issue.Minor {
				output.Error("cannot close %s: issue is in review; use 'td approve %s' to close reviewed work", issueID, issueID)
				output.Error("  'td close' is the admin path for duplicates/won't-fix/cleanup; it cannot bypass review.")
				skipped++
				continue
			}

			// Delegated-mode extra gate: non-minor issues with implementation
			// history cannot be closed via the admin path without --admin or
			// --self-close-exception. This preserves the admin escape hatch
			// while making the policy boundary explicit.
			if mode == reviewpolicy.ModeDelegated && !issue.Minor &&
				(issue.Status == models.StatusOpen || issue.Status == models.StatusInProgress || issue.Status == models.StatusBlocked) {
				hasHist, _ := database.HasImplementationHistory(issueID)
				if hasHist && adminReason == "" && selfCloseException == "" {
					output.Error("cannot close %s: delegated mode requires --admin or --self-close-exception for issues with implementation history", issueID)
					output.Error("  Use 'td review' -> 'td approve' for completed work, or pass --admin \"duplicate|won't-fix|...\"")
					skipped++
					continue
				}
			}

			wasInvolved, err := database.WasSessionInvolved(issueID, sess.ID)
			if err != nil {
				output.Warning("failed to check session history for %s: %v", issueID, err)
				wasInvolved = true // Conservative: assume involvement on error
			}

			wasImplementationInvolved, err := database.WasSessionImplementationInvolved(issueID, sess.ID)
			if err != nil {
				output.Warning("failed to check implementation history for %s: %v", issueID, err)
				wasImplementationInvolved = true // Conservative: assume implementation involvement on error
			}

			hasImplementationHistory, err := database.HasImplementationHistory(issueID)
			if err != nil {
				output.Warning("failed to check issue implementation history for %s: %v", issueID, err)
				hasImplementationHistory = true // Conservative: assume implementation history on error
			}

			eligibility := evaluateCloseEligibility(issue, sess.ID, wasInvolved, wasImplementationInvolved, hasImplementationHistory)

			if !eligibility.Allowed {
				if selfCloseException == "" && adminReason == "" {
					output.Error("%s", eligibility.RejectionMessage)
					output.Error("%s", closeFollowupGuidance(issue))
					skipped++
					continue
				}
				output.Warning("SELF-CLOSE EXCEPTION: %s", issueID)
				output.Warning("  Reason: %s", selfCloseException)
			}

			// Update issue (atomic update + action log)
			fromStatus := issue.Status
			issue.Status = models.StatusClosed
			issue.ClosedBySession = sess.ID
			now := time.Now()
			issue.ClosedAt = &now

			if err := database.UpdateIssueLoggedIfStatus(issue, fromStatus, sess.ID, models.ActionClose); err != nil {
				output.Warning("%s", describeStaleTransitionUpdate(database, "close", issueID, err, closeFollowupGuidance))
				skipped++
				continue
			}

			// Log (supports --reason, --comment, --message, and --self-close-exception)
			reason := approvalReason(cmd)
			logMsg := "Closed"
			logType := models.LogTypeProgress

			if !eligibility.Allowed && selfCloseException != "" {
				agentInfo := sess.AgentType
				if agentInfo == "" {
					agentInfo = "Unknown Agent"
				}
				logMsg = fmt.Sprintf("[%s] Closed (SELF-CLOSE EXCEPTION: %s)", agentInfo, selfCloseException)
				logType = models.LogTypeSecurity

				// Also log to the separate security audit file
				db.LogSecurityEvent(baseDir, db.SecurityEvent{
					IssueID:   issueID,
					SessionID: sess.ID,
					AgentType: sess.AgentType,
					Reason:    selfCloseException,
				})
			} else if adminReason != "" {
				logMsg = fmt.Sprintf("Closed (ADMIN: %s)", adminReason)
			} else if reason != "" {
				logMsg = "Closed: " + reason
			}

			if err := database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      logType,
			}); err != nil {
				output.Warning("add log failed: %v", err)
			}

			// Clear focus if this was the focused issue
			clearFocusIfNeeded(baseDir, issueID)

			if !eligibility.Allowed && selfCloseException != "" {
				fmt.Printf("CLOSED %s (self-close exception)\n", issueID)
			} else {
				fmt.Printf("CLOSED %s\n", issueID)
			}

			// Cascade up: if all siblings are closed, update parent epic
			if count, ids := database.CascadeUpParentStatus(issueID, models.StatusClosed, sess.ID); count > 0 {
				for _, id := range ids {
					fmt.Printf("  ↑ Parent %s auto-cascaded to %s\n", id, models.StatusClosed)
				}
			}

			// Auto-unblock dependents whose dependencies are now all closed
			if count, ids := database.CascadeUnblockDependents(issueID, sess.ID); count > 0 {
				for _, id := range ids {
					fmt.Printf("  ↓ Dependent %s auto-unblocked\n", id)
				}
			}

			closed++
		}

		if len(args) > 1 {
			fmt.Printf("\nClosed %d, skipped %d\n", closed, skipped)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(reviewCmd)
	rootCmd.AddCommand(approveCmd)
	rootCmd.AddCommand(rejectCmd)
	rootCmd.AddCommand(closeCmd)

	reviewCmd.Flags().StringP("reason", "m", "", "Reason for submitting")
	reviewCmd.Flags().String("message", "", "Reason for submitting (alias for --reason)")
	reviewCmd.Flags().String("comment", "", "Reason for submitting (alias for --reason)")
	reviewCmd.Flags().String("note", "", "Reason for submitting (alias for --reason)")
	reviewCmd.Flags().String("notes", "", "Reason for submitting (alias for --reason)")
	reviewCmd.Flags().Bool("json", false, "JSON output")
	reviewCmd.Flags().Bool("minor", false, "Mark as minor task (allows self-review)")
	approveCmd.Flags().StringP("reason", "m", "", "Reason for approval")
	approveCmd.Flags().String("message", "", "Reason for approval (alias for --reason)")
	approveCmd.Flags().StringP("comment", "c", "", "Reason for approval (alias for --message)")
	approveCmd.Flags().String("note", "", "Reason for approval (alias for --reason)")
	approveCmd.Flags().String("notes", "", "Reason for approval (alias for --reason)")
	approveCmd.Flags().Bool("json", false, "JSON output")
	approveCmd.Flags().Bool("all", false, "Approve all reviewable issues")
	approveCmd.Flags().Bool("record-only", false, "Record an approval review without closing (delegated mode)")
	approveCmd.Flags().String("decision", "", "Review decision: approved (default) | changes_requested (use with --record-only)")
	rejectCmd.Flags().StringP("reason", "m", "", "Reason for rejection")
	rejectCmd.Flags().StringP("comment", "c", "", "Reason for rejection (alias for --reason)")
	rejectCmd.Flags().String("message", "", "Reason for rejection (alias for --reason)")
	rejectCmd.Flags().String("note", "", "Reason for rejection (alias for --reason)")
	rejectCmd.Flags().String("notes", "", "Reason for rejection (alias for --reason)")
	rejectCmd.Flags().Bool("json", false, "JSON output")
	closeCmd.Flags().StringP("reason", "m", "", "Reason for closing")
	closeCmd.Flags().String("comment", "", "Reason for closing (alias for --reason)")
	closeCmd.Flags().String("message", "", "Reason for closing (alias for --reason)")
	closeCmd.Flags().StringP("note", "n", "", "Reason for closing (alias for --reason)")
	closeCmd.Flags().String("notes", "", "Reason for closing (alias for --reason)")
	closeCmd.Flags().String("self-close-exception", "", "Override review requirement when closing own work (requires reason)")
	closeCmd.Flags().String("admin", "", "Admin close: override delegated-mode impl-history gate for duplicates/won't-fix/cleanup (requires reason)")
}
