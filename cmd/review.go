package cmd

import (
	"fmt"
	"time"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
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

// submitIssueForReview submits a single issue for review with proper validation,
// logging, and undo support. This is the shared logic for both reviewCmd and
// ws handoff --review.
func submitIssueForReview(database *db.DB, issue *models.Issue, sess *session.Session, baseDir string, logMsg string) SubmitReviewResult {
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

	// Update issue (atomic update + action log)
	issue.Status = models.StatusInReview
	if issue.ImplementerSession == "" {
		issue.ImplementerSession = sess.ID
	}

	if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionReview); err != nil {
		return SubmitReviewResult{
			Success: false,
			Message: fmt.Sprintf("failed to update %s: %v", issue.ID, err),
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

var reviewCmd = &cobra.Command{
	Use:     "review [issue-id...]",
	Aliases: []string{"submit", "finish"},
	Short:   "Submit one or more issues for review",
	Long: `Submits the issue(s) for review. If no handoff exists, a minimal one is
auto-created (consider using 'td handoff' for better documentation).

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
				autoHandoff := &models.Handoff{
					IssueID:   issueID,
					SessionID: sess.ID,
					Done:      []string{"Auto-generated for review submission"},
					Remaining: []string{},
					Decisions: []string{},
					Uncertain: []string{},
				}
				if err := database.AddHandoff(autoHandoff); err != nil {
					if jsonOutput {
						output.JSONError(output.ErrCodeDatabaseError, fmt.Sprintf("failed to create handoff: %v", err))
					} else {
						output.Error("failed to create handoff for %s: %v", issueID, err)
					}
					skipped++
					continue
				}
				output.Warning("auto-created minimal handoff for %s - consider using 'td handoff' for better documentation", issueID)
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

func closeFollowupGuidance(issue *models.Issue) string {
	if issue == nil {
		return "  Submit for review: td review "
	}
	if issue != nil && issue.Status == models.StatusInReview {
		return fmt.Sprintf("  Already in review: td approve %s", issue.ID)
	}
	return fmt.Sprintf("  Submit for review: td review %s", issue.ID)
}

// reviewFollowupGuidance returns the next workflow command after a failed
// review submission attempt.
func reviewFollowupGuidance(issue *models.Issue) string {
	if issue == nil {
		return "  Submit for review: td review "
	}
	if issue.Status == models.StatusInReview {
		return fmt.Sprintf("  Already in review: td approve %s", issue.ID)
	}
	return fmt.Sprintf("  Submit for review: td review %s", issue.ID)
}

// approveFollowupGuidance returns the next workflow command after a failed
// approval attempt.
func approveFollowupGuidance(issue *models.Issue) string {
	if issue == nil {
		return "  Submit for review first: td review "
	}
	if issue.Status == models.StatusInReview {
		return fmt.Sprintf("  Approve it: td approve %s", issue.ID)
	}
	return fmt.Sprintf("  Submit for review first: td review %s", issue.ID)
}

var approveCmd = &cobra.Command{
	Use:   "approve [issue-id...]",
	Short: "Approve and close one or more issues",
	Long: `Approves and closes the issue(s). Must be a different session than the implementer.

Supports bulk operations:
  td approve td-abc1 td-abc2 td-abc3    # Approve multiple issues
  td approve --all                      # Approve all reviewable issues`,
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
		balancedPolicy := balancedReviewPolicyEnabled(baseDir)

		// Build list of issue IDs to approve
		var issueIDs []string
		if all {
			// Get all issues reviewable by current policy.
			issues, err := database.ListIssues(reviewableByOptions(baseDir, sess.ID))
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
				issues, err := database.ListIssues(reviewableByOptions(baseDir, sess.ID))
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

			eligibility := evaluateApproveEligibility(issue, sess.ID, wasInvolved, wasImplementationInvolved, balancedPolicy)
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

			// Update issue (atomic update + action log)
			issue.Status = models.StatusClosed
			issue.ReviewerSession = sess.ID
			now := time.Now()
			issue.ClosedAt = &now

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionApprove); err != nil {
				output.Warning("failed to update %s: %v", issueID, err)
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
				logMsg = reason
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
			if issue.Status != models.StatusInReview {
				if jsonOutput {
					output.JSONError(output.ErrCodeDatabaseError, fmt.Sprintf("cannot reject %s: must be in_review (currently %s)", issueID, issue.Status))
				} else {
					output.Warning("cannot reject %s: must be in_review (currently %s)", issueID, issue.Status)
				}
				skipped++
				continue
			}

			// Update issue: reset to open so td next can pick it up again
			issue.Status = models.StatusOpen
			issue.ImplementerSession = ""

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionReject); err != nil {
				if jsonOutput {
					output.JSONError(output.ErrCodeDatabaseError, err.Error())
				} else {
					output.Warning("failed to update %s: %v", issueID, err)
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
	Short:   "Close one or more issues without review",
	Long: `Closes the issue(s) directly. For administrative use: duplicates, won't-fix, or cleanup.

IMPORTANT: Agents should use 'td review' + 'td approve' for completed work.
Self-closing issues you implemented requires --self-close-exception "reason".

Examples:
  td close td-abc1                                       # Close (fails if you implemented it)
  td close td-abc1 -m "duplicate of td-xyz"              # Close unworked issue with reason
  td close td-abc1 --self-close-exception "trivial fix"  # Override for implemented work
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
				if selfCloseException == "" {
					output.Error("%s", eligibility.RejectionMessage)
					output.Error("%s", closeFollowupGuidance(issue))
					skipped++
					continue
				}
				output.Warning("SELF-CLOSE EXCEPTION: %s", issueID)
				output.Warning("  Reason: %s", selfCloseException)
			}

			// Update issue (atomic update + action log)
			issue.Status = models.StatusClosed
			now := time.Now()
			issue.ClosedAt = &now

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionClose); err != nil {
				output.Warning("failed to update %s: %v", issueID, err)
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
}
