package monitor

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
	"github.com/marcus/td/internal/workflow"
)

// monitorApproveInputs bundles the facts the shared reviewpolicy package needs
// to decide whether a monitor session may approve the selected issue. Exposed
// (unexported symbol) so the parity test suite can construct the same input
// the runtime does without invoking bubbletea.
type monitorApproveInputs struct {
	Mode                     reviewpolicy.Mode
	Issue                    *models.Issue
	SessionID                string
	HasImplementationHistory bool
	WasAnyInvolved           bool
	HasActiveApproval        bool
	// SelfReviewAcknowledged mirrors the CLI's --self-review flag. In the
	// monitor it is set to true after the operator confirms the trusted-mode
	// "you implemented this; approve as self-review?" prompt. It is threaded
	// into the reviewpolicy decision so trusted-mode self-review becomes an
	// audited allow (SelfReview=true) instead of a reject.
	SelfReviewAcknowledged bool
}

// MonitorApproveDecisionForTest is the exported thin wrapper that the
// cross-surface parity suite (see cmd/parity_surface_test.go) uses to drive
// the monitor's decision path with synthetic inputs. Not intended for
// runtime callers; runtime code should use monitorApproveDecision +
// loadMonitorApproveInputs directly.
func MonitorApproveDecisionForTest(mode reviewpolicy.Mode, issue *models.Issue, sessionID string, hasImplementationHistory, wasAnyInvolved, hasActiveApproval, selfReviewAcknowledged bool) reviewpolicy.ReviewerEligibility {
	return monitorApproveDecision(monitorApproveInputs{
		Mode:                     mode,
		Issue:                    issue,
		SessionID:                sessionID,
		HasImplementationHistory: hasImplementationHistory,
		WasAnyInvolved:           wasAnyInvolved,
		HasActiveApproval:        hasActiveApproval,
		SelfReviewAcknowledged:   selfReviewAcknowledged,
	})
}

// monitorApproveDecision returns the reviewpolicy decision for the monitor's
// approval action. Batch 1c aligns the monitor's rule with CLI / serve /
// snapshot-query-source: instead of the pre-batch "only block the current
// implementer" check, the monitor now uses the same strict/balanced rule as
// the other surfaces under the default (strict) mode. This may reject
// approvals that the pre-batch monitor would have permitted; that's the
// intentional parity alignment called out in the batch plan.
func monitorApproveDecision(in monitorApproveInputs) reviewpolicy.ReviewerEligibility {
	isCreator := in.Issue != nil && in.Issue.CreatorSession != "" && in.Issue.CreatorSession == in.SessionID
	isImplementer := in.Issue != nil && in.Issue.ImplementerSession != "" && in.Issue.ImplementerSession == in.SessionID
	return reviewpolicy.EvaluateReviewerEligibility(reviewpolicy.ReviewerEligibilityInput{
		Mode:                     in.Mode,
		Issue:                    in.Issue,
		SessionID:                in.SessionID,
		SessionIsImplementer:     isImplementer,
		SessionIsCreator:         isCreator,
		HasImplementationHistory: in.HasImplementationHistory,
		HasActiveApproval:        in.HasActiveApproval,
		WasAnyInvolved:           in.WasAnyInvolved,
		SelfReviewAcknowledged:   in.SelfReviewAcknowledged,
	})
}

// loadMonitorApproveInputs reads the DB facts the monitor needs to run the
// shared reviewpolicy decision. Errors are handled conservatively — on any
// DB failure the returned inputs flag involvement as true so the caller
// rejects the approve rather than silently opening it. Used by both the
// runtime (approveIssue) and the parity test.
func loadMonitorApproveInputs(database *db.DB, baseDir, sessionID string, issue *models.Issue) monitorApproveInputs {
	mode := reviewpolicy.ModeStrict
	if baseDir != "" {
		if m, err := features.ResolveReviewPolicyMode(baseDir); err == nil {
			mode = m
		}
	}

	in := monitorApproveInputs{
		Mode:      mode,
		Issue:     issue,
		SessionID: sessionID,
	}
	if database == nil || issue == nil {
		in.WasAnyInvolved = true
		in.HasImplementationHistory = true
		return in
	}

	was, err := database.WasSessionInvolved(issue.ID, sessionID)
	if err != nil {
		in.WasAnyInvolved = true
	} else {
		in.WasAnyInvolved = was
	}
	impl, err := database.WasSessionImplementationInvolved(issue.ID, sessionID)
	if err != nil {
		in.HasImplementationHistory = true
	} else {
		in.HasImplementationHistory = impl
	}
	// Active approval lookup is only meaningful for delegated mode; the
	// strict/balanced paths ignore it. Query unconditionally so the parity
	// suite sees the same inputs the runtime passes.
	active, err := database.GetActiveApprovalReview(issue.ID)
	if err == nil && active != nil {
		in.HasActiveApproval = true
	}
	return in
}

// markForReview marks the selected issue for review
// Works from modal view, CurrentWork panel, or TaskList panel
// Accepts both in_progress and open (ready) issues
func (m Model) markForReview() (tea.Model, tea.Cmd) {
	var issueID string
	var issue *models.Issue

	// Check if a modal is open
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		// If task section is focused, use the highlighted epic task
		if modal.TaskSectionFocused && len(modal.EpicTasks) > 0 && modal.EpicTasksCursor < len(modal.EpicTasks) {
			task := modal.EpicTasks[modal.EpicTasksCursor]
			issueID = task.ID
			var err error
			issue, err = m.DB.GetIssue(issueID)
			if err != nil || issue == nil {
				return m, nil
			}
		} else {
			issueID = modal.IssueID
			issue = modal.Issue
		}
	} else {
		// Otherwise, use the selected issue from the active panel
		issueID = m.SelectedIssueID(m.ActivePanel)
		if issueID == "" {
			return m, nil
		}
		var err error
		issue, err = m.DB.GetIssue(issueID)
		if err != nil || issue == nil {
			return m, nil
		}
	}

	// Validate transition with state machine
	sm := workflow.DefaultMachine()
	if !sm.IsValidTransition(issue.Status, models.StatusInReview) {
		return m, nil
	}

	// Update status
	issue.Status = models.StatusInReview
	if issue.ImplementerSession == "" {
		issue.ImplementerSession = m.SessionID
	}
	if err := m.DB.UpdateIssueLogged(issue, m.SessionID, models.ActionReview); err != nil {
		return m, nil
	}

	// Cascade DOWN to descendants if this is a parent issue (epic)
	if hasChildren, _ := m.DB.HasChildren(issueID); hasChildren {
		descendants, err := m.DB.GetDescendantIssues(issueID, []models.Status{
			models.StatusOpen,
			models.StatusInProgress,
		})
		if err == nil && len(descendants) > 0 {
			for _, child := range descendants {
				child.Status = models.StatusInReview
				if child.ImplementerSession == "" {
					child.ImplementerSession = m.SessionID
				}
				_ = m.DB.UpdateIssueLogged(child, m.SessionID, models.ActionReview)
				_ = m.DB.AddLog(&models.Log{
					IssueID:   child.ID,
					SessionID: m.SessionID,
					Message:   "Cascaded review from " + issueID,
					Type:      models.LogTypeProgress,
				})
			}
		}
	}

	// Cascade up to parent epic if all siblings are ready
	m.DB.CascadeUpParentStatus(issueID, models.StatusInReview, m.SessionID)

	// If we're in a modal, refresh instead of closing to keep context
	if modal := m.CurrentModal(); modal != nil {
		if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
			return m, tea.Batch(m.fetchData(), m.fetchBoardIssues(m.BoardMode.Board.ID), m.fetchIssueDetails(modal.IssueID))
		}
		// Refresh the modal issue data and epic tasks list
		return m, tea.Batch(m.fetchData(), m.fetchIssueDetails(modal.IssueID))
	}

	if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
		return m, tea.Batch(m.fetchData(), m.fetchBoardIssues(m.BoardMode.Board.ID))
	}
	return m, m.fetchData()
}

// confirmDelete opens confirmation dialog for deleting selected issue
// Works from both main panel selection and modal view
func (m Model) confirmDelete() (tea.Model, tea.Cmd) {
	var issueID string
	var issue *models.Issue

	// Check if a modal is open - use that issue
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		issueID = modal.IssueID
		issue = modal.Issue
	} else {
		// Otherwise, use the selected issue from the panel
		issueID = m.SelectedIssueID(m.ActivePanel)
		if issueID == "" {
			return m, nil
		}
		var err error
		issue, err = m.DB.GetIssue(issueID)
		if err != nil || issue == nil {
			return m, nil
		}
	}

	// Use the new declarative modal function
	m = m.openDeleteConfirmModal(issueID, issue.Title)

	return m, nil
}

// executeDelete performs the actual deletion after confirmation
func (m Model) executeDelete() (tea.Model, tea.Cmd) {
	if m.ConfirmIssueID == "" {
		m.closeDeleteConfirmModal()
		return m, nil
	}

	deletedID := m.ConfirmIssueID

	// Delete issue (captures previous state and logs atomically)
	if err := m.DB.DeleteIssueLogged(deletedID, m.SessionID); err != nil {
		m.closeDeleteConfirmModal()
		return m, nil
	}

	// Close the delete confirmation modal
	m.closeDeleteConfirmModal()

	// Close modal if we just deleted the issue being viewed
	if modal := m.CurrentModal(); modal != nil && modal.IssueID == deletedID {
		m.closeModal()
	}

	if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
		return m, tea.Batch(m.fetchData(), m.fetchBoardIssues(m.BoardMode.Board.ID))
	}
	return m, m.fetchData()
}

// confirmClose opens confirmation dialog for closing selected issue
// Works from both main panel selection and modal view
func (m Model) confirmClose() (tea.Model, tea.Cmd) {
	var issueID string
	var issue *models.Issue

	// Check if a modal is open
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		// If task section is focused, use the highlighted epic task
		if modal.TaskSectionFocused && len(modal.EpicTasks) > 0 && modal.EpicTasksCursor < len(modal.EpicTasks) {
			task := modal.EpicTasks[modal.EpicTasksCursor]
			issueID = task.ID
			var err error
			issue, err = m.DB.GetIssue(issueID)
			if err != nil || issue == nil {
				return m, nil
			}
		} else {
			issueID = modal.IssueID
			issue = modal.Issue
		}
	} else {
		// Otherwise, use the selected issue from the panel
		issueID = m.SelectedIssueID(m.ActivePanel)
		if issueID == "" {
			return m, nil
		}
		var err error
		issue, err = m.DB.GetIssue(issueID)
		if err != nil || issue == nil {
			return m, nil
		}
	}

	// Can't close already-closed issues
	if issue.Status == models.StatusClosed {
		return m, nil
	}

	// Use the new declarative modal function
	m = m.openCloseConfirmModal(issueID, issue.Title)

	return m, nil
}

// executeCloseWithReason performs the actual close after confirmation
func (m Model) executeCloseWithReason() (tea.Model, tea.Cmd) {
	if m.CloseConfirmIssueID == "" {
		m.closeCloseConfirmModal()
		return m, nil
	}

	issueID := m.CloseConfirmIssueID
	reason := m.CloseConfirmInput.Value()

	// Get the issue
	issue, err := m.DB.GetIssue(issueID)
	if err != nil || issue == nil {
		m.closeCloseConfirmModal()
		return m, nil
	}

	// Validate transition with state machine
	sm := workflow.DefaultMachine()
	if !sm.IsValidTransition(issue.Status, models.StatusClosed) {
		m.closeCloseConfirmModal()
		return m, nil
	}

	// Update status
	now := time.Now()
	issue.Status = models.StatusClosed
	issue.ClosedAt = &now
	if err := m.DB.UpdateIssueLogged(issue, m.SessionID, models.ActionClose); err != nil {
		m.closeCloseConfirmModal()
		return m, nil
	}

	// Add progress log with optional reason
	logMsg := "Closed"
	if reason != "" {
		logMsg = "Closed: " + reason
	}
	_ = m.DB.AddLog(&models.Log{
		IssueID:   issueID,
		SessionID: m.SessionID,
		Message:   logMsg,
		Type:      models.LogTypeProgress,
	})

	// Cascade DOWN to descendants if this is a parent issue (epic).
	// Reuse the `now` timestamp captured above so the cascade shares one
	// close_at across all descendants.
	if hasChildren, _ := m.DB.HasChildren(issueID); hasChildren {
		descendants, err := m.DB.GetDescendantIssues(issueID, []models.Status{
			models.StatusOpen,
			models.StatusInProgress,
			models.StatusInReview,
		})
		if err == nil && len(descendants) > 0 {
			for _, child := range descendants {
				child.Status = models.StatusClosed
				child.ClosedAt = &now
				if child.ImplementerSession == "" {
					child.ImplementerSession = m.SessionID
				}
				_ = m.DB.UpdateIssueLogged(child, m.SessionID, models.ActionClose)
				_ = m.DB.AddLog(&models.Log{
					IssueID:   child.ID,
					SessionID: m.SessionID,
					Message:   "Cascaded close from " + issueID,
					Type:      models.LogTypeProgress,
				})
				m.DB.CascadeUnblockDependents(child.ID, m.SessionID)
			}
		}
	}

	// Cascade up to parent epic if all siblings are closed
	m.DB.CascadeUpParentStatus(issueID, models.StatusClosed, m.SessionID)

	// Auto-unblock dependents whose dependencies are now all closed
	m.DB.CascadeUnblockDependents(issueID, m.SessionID)

	// Close the confirmation modal
	m.closeCloseConfirmModal()

	// If we're in a modal, refresh instead of closing
	if modal := m.CurrentModal(); modal != nil {
		if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
			return m, tea.Batch(m.fetchData(), m.fetchBoardIssues(m.BoardMode.Board.ID), m.fetchIssueDetails(modal.IssueID))
		}
		// If we closed an epic task (not the modal's main issue), refresh to update the list
		// If we closed the main issue, also refresh to show updated status
		return m, tea.Batch(m.fetchData(), m.fetchIssueDetails(modal.IssueID))
	}

	if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
		return m, tea.Batch(m.fetchData(), m.fetchBoardIssues(m.BoardMode.Board.ID))
	}
	return m, m.fetchData()
}

// approveIssue approves/closes the selected reviewable issue.
// Under delegated mode, if an active approval already exists and the session
// closes, this acts as a close-after-review (Mode C) — the existing
// reviewer_session / reviewed_at fields are preserved and closed_by_session is
// stamped to the caller.
func (m Model) approveIssue() (tea.Model, tea.Cmd) {
	// Must be in Task List panel
	if m.ActivePanel != PanelTaskList {
		return m, nil
	}

	issueID := m.SelectedIssueID(m.ActivePanel)
	if issueID == "" {
		return m, nil
	}

	issue, err := m.DB.GetIssue(issueID)
	if err != nil || issue == nil {
		return m, nil
	}

	// Validate transition with state machine
	sm := workflow.DefaultMachine()
	if !sm.IsValidTransition(issue.Status, models.StatusClosed) {
		return m, nil
	}

	// Run the same reviewer-eligibility decision the CLI/serve/snapshot use.
	// This is an intentional alignment: the pre-batch monitor only blocked
	// the current implementer, while the CLI strict rule also blocks prior
	// involvement. Under default review_policy_mode=strict the monitor now
	// rejects approvals the CLI would reject too.
	inputs := loadMonitorApproveInputs(m.DB, m.BaseDir, m.SessionID, issue)

	// Close-after-review (Mode C, delegated/trusted): if an active approval
	// already exists, close preserving the prior reviewer_session/reviewed_at.
	if (inputs.Mode == reviewpolicy.ModeDelegated || inputs.Mode == reviewpolicy.ModeTrusted) && inputs.HasActiveApproval {
		closeIn := reviewpolicy.CloseEligibilityInput{
			Mode:                      inputs.Mode,
			Issue:                     issue,
			SessionID:                 m.SessionID,
			SessionIsImplementer:      issue.ImplementerSession != "" && issue.ImplementerSession == m.SessionID,
			SessionIsCreator:          issue.CreatorSession != "" && issue.CreatorSession == m.SessionID,
			SessionIsReviewerOfRecord: issue.ReviewerSession != "" && issue.ReviewerSession == m.SessionID,
			SessionIsReviewRequester:  issue.ReviewRequestedBySession != "" && issue.ReviewRequestedBySession == m.SessionID,
			HasImplementationHistory:  inputs.HasImplementationHistory,
			WasAnyInvolved:            inputs.WasAnyInvolved,
			HasActiveApproval:         true,
		}
		if reviewpolicy.EvaluateCloseEligibility(closeIn).Allowed {
			now := time.Now()
			issue.Status = models.StatusClosed
			issue.ClosedBySession = m.SessionID
			issue.ClosedAt = &now
			// Preserve existing ReviewerSession / ReviewedAt
			if err := m.DB.UpdateIssueLoggedWithReviewMeta(issue, models.StatusInReview, m.SessionID, models.ActionCloseAfterReview, "", ""); err != nil {
				return m, nil
			}
			_ = m.DB.RecordSessionAction(issue.ID, m.SessionID, models.ActionSessionClosed)
			m.DB.CascadeUpParentStatus(issue.ID, models.StatusClosed, m.SessionID)
			m.DB.CascadeUnblockDependents(issue.ID, m.SessionID)
			m.SelectedID[PanelTaskList] = ""
			if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
				return m, tea.Batch(m.fetchData(), m.fetchBoardIssues(m.BoardMode.Board.ID))
			}
			return m, m.fetchData()
		}
	}

	decision := monitorApproveDecision(inputs)
	if !decision.Allowed {
		// Trusted-mode self-review escape: the only reject the monitor can
		// turn into an allow is the implementer/has-history self-review under
		// trusted mode. Rather than silently blocking (the CLI requires the
		// --self-review flag) or silently self-approving, prompt the operator
		// for an explicit acknowledgement. Re-running with the ack set must
		// then be allowed (decision.SelfReview=true) — otherwise the reject is
		// for some other reason and we leave it blocked.
		if inputs.Mode == reviewpolicy.ModeTrusted {
			ackInputs := inputs
			ackInputs.SelfReviewAcknowledged = true
			if monitorApproveDecision(ackInputs).SelfReview {
				m = m.openSelfReviewConfirmModal(issue.ID, issue.Title)
				return m, nil
			}
		}
		return m, nil
	}

	return m.executeApproveClose(issue, decision.SelfReview)
}

// executeApproveClose performs the direct reviewer-close (Mode A) and any
// epic cascade for an approved issue. selfReview records the trusted-mode
// self-review audit bit on the issue_reviews row, identical to the CLI's
// `td approve --self-review` path.
func (m Model) executeApproveClose(issue *models.Issue, selfReview bool) (tea.Model, tea.Cmd) {
	// Direct reviewer-close (Mode A)
	now := time.Now()
	issue.Status = models.StatusClosed
	issue.ReviewerSession = m.SessionID
	issue.ClosedBySession = m.SessionID
	issue.ReviewedAt = &now
	issue.ClosedAt = &now
	if err := m.DB.UpdateIssueLogged(issue, m.SessionID, models.ActionApprove); err != nil {
		return m, nil
	}

	// Record session action for bypass prevention
	_ = m.DB.RecordSessionAction(issue.ID, m.SessionID, models.ActionSessionReviewed)

	// Also record an issue_reviews row so audit output distinguishes direct
	// reviewer-close from cascaded close. Best-effort: a missing review
	// write does not block the approve. selfReview stamps the audit bit
	// identically to the CLI.
	_, _ = m.DB.CreateIssueReview(issue.ID, m.SessionID, reviewpolicy.DecisionApproved, "", issue.ReviewRequestedBySession, selfReview)

	// Cascade DOWN to descendants if this is a parent issue (epic).
	// Reuse the `now` captured above so the whole cascade shares one
	// timestamp (approved / closed_at / reviewed_at) across the epic.
	if hasChildren, _ := m.DB.HasChildren(issue.ID); hasChildren {
		descendants, err := m.DB.GetDescendantIssues(issue.ID, []models.Status{
			models.StatusOpen,
			models.StatusInProgress,
			models.StatusInReview,
		})
		if err == nil && len(descendants) > 0 {
			for _, child := range descendants {
				child.Status = models.StatusClosed
				child.ClosedAt = &now
				child.ReviewerSession = m.SessionID
				child.ClosedBySession = m.SessionID
				child.ReviewedAt = &now
				if child.ImplementerSession == "" {
					child.ImplementerSession = m.SessionID
				}
				_ = m.DB.UpdateIssueLogged(child, m.SessionID, models.ActionApprove)
				_ = m.DB.AddLog(&models.Log{
					IssueID:   child.ID,
					SessionID: m.SessionID,
					Message:   "Cascaded approval from " + issue.ID,
					Type:      models.LogTypeProgress,
				})
				// Tag cascaded descendants with the named exemption so audit
				// output can distinguish them from individually-reviewed
				// closes. This is the "Cascade exemption" contract in the
				// plan's close-path-hardening section.
				_, _ = m.DB.CreateIssueReview(
					child.ID,
					m.SessionID,
					reviewpolicy.DecisionApprovedByParentCascade,
					"Cascaded approval from "+issue.ID,
					child.ReviewRequestedBySession,
					false,
				)
				m.DB.CascadeUnblockDependents(child.ID, m.SessionID)
			}
		}
	}

	// Cascade up to parent epic if all siblings are closed
	m.DB.CascadeUpParentStatus(issue.ID, models.StatusClosed, m.SessionID)

	// Auto-unblock dependents whose dependencies are now all closed
	m.DB.CascadeUnblockDependents(issue.ID, m.SessionID)

	// Clear the saved ID so cursor stays at the same position after refresh
	// The item will move to Closed, and we want cursor at same index for next item
	m.SelectedID[PanelTaskList] = ""

	if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
		return m, tea.Batch(m.fetchData(), m.fetchBoardIssues(m.BoardMode.Board.ID))
	}
	return m, m.fetchData()
}

// reopenIssue reopens a closed issue
func (m Model) reopenIssue() (tea.Model, tea.Cmd) {
	var issueID string
	var issue *models.Issue

	// Check if a modal is open
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		// If task section is focused, use the highlighted epic task
		if modal.TaskSectionFocused && len(modal.EpicTasks) > 0 && modal.EpicTasksCursor < len(modal.EpicTasks) {
			task := modal.EpicTasks[modal.EpicTasksCursor]
			issueID = task.ID
			var err error
			issue, err = m.DB.GetIssue(issueID)
			if err != nil || issue == nil {
				return m, nil
			}
		} else {
			issueID = modal.IssueID
			issue = modal.Issue
		}
	} else {
		// Otherwise, use the selected issue from the panel
		issueID = m.SelectedIssueID(m.ActivePanel)
		if issueID == "" {
			return m, nil
		}
		var err error
		issue, err = m.DB.GetIssue(issueID)
		if err != nil || issue == nil {
			return m, nil
		}
	}

	// Validate transition with state machine
	sm := workflow.DefaultMachine()
	if !sm.IsValidTransition(issue.Status, models.StatusOpen) {
		m.StatusMessage = "Cannot reopen from " + string(issue.Status)
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		})
	}

	// Update status
	issue.Status = models.StatusOpen
	issue.ReviewerSession = ""
	issue.ClosedAt = nil
	if err := m.DB.UpdateIssueLogged(issue, m.SessionID, models.ActionReopen); err != nil {
		m.StatusMessage = "Failed to reopen: " + err.Error()
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		})
	}

	m.StatusMessage = "REOPENED " + issueID
	m.StatusIsError = false

	// If in modal, refresh modal data
	if modal := m.CurrentModal(); modal != nil {
		// Update inline for immediate feedback
		if modal.Issue != nil && modal.IssueID == issueID {
			modal.Issue.Status = models.StatusOpen
			modal.Issue.ClosedAt = nil
		}
		cmds := []tea.Cmd{
			tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} }),
			m.fetchData(),
			m.fetchIssueDetails(modal.IssueID),
		}
		if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
			cmds = append(cmds, m.fetchBoardIssues(m.BoardMode.Board.ID))
		}
		return m, tea.Batch(cmds...)
	}

	cmds := []tea.Cmd{
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} }),
		m.fetchData(),
	}
	if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
		cmds = append(cmds, m.fetchBoardIssues(m.BoardMode.Board.ID))
	}
	return m, tea.Batch(cmds...)
}

// copyCurrentIssueToClipboard copies the current issue to clipboard as markdown
// Works from modal view or list views (PanelCurrentWork, PanelTaskList)
func (m Model) copyCurrentIssueToClipboard() (tea.Model, tea.Cmd) {
	var issue *models.Issue
	var epicTasks []models.Issue

	// Check if modal is open first - use that issue
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		issue = modal.Issue
		epicTasks = modal.EpicTasks
	} else {
		// Otherwise get the issue from the selected row in the active panel
		issueID := m.SelectedIssueID(m.ActivePanel)
		if issueID == "" {
			return m, nil
		}
		var err error
		issue, err = m.DB.GetIssue(issueID)
		if err != nil || issue == nil {
			return m, nil
		}
		// For epics in list view, fetch tasks
		if issue.Type == models.TypeEpic {
			epicTasks, _ = m.DB.ListIssues(db.ListIssuesOptions{EpicID: issue.ID})
		}
	}

	var markdown string
	if issue.Type == models.TypeEpic {
		markdown = formatEpicAsMarkdown(issue, epicTasks)
	} else {
		markdown = formatIssueAsMarkdown(issue)
	}

	clipFn := m.ClipboardFn
	if clipFn == nil {
		clipFn = copyToClipboard
	}
	if err := clipFn(markdown); err != nil {
		m.StatusMessage = "Copy failed: " + err.Error()
		m.StatusIsError = true
	} else {
		m.StatusMessage = "Yanked to clipboard"
		m.StatusIsError = false
	}

	// Clear status after 2 seconds
	return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return ClearStatusMsg{}
	})
}

// copyIssueIDToClipboard copies just the issue ID to clipboard
// Works from modal view or list views
func (m Model) copyIssueIDToClipboard() (tea.Model, tea.Cmd) {
	var issueID string

	// Check if modal is open first - use that issue
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		issueID = modal.Issue.ID
	} else {
		// Otherwise get the issue ID from the selected row in the active panel
		issueID = m.SelectedIssueID(m.ActivePanel)
	}

	if issueID == "" {
		return m, nil
	}

	clipFn := m.ClipboardFn
	if clipFn == nil {
		clipFn = copyToClipboard
	}
	if err := clipFn(issueID); err != nil {
		m.StatusMessage = "Copy failed: " + err.Error()
		m.StatusIsError = true
	} else {
		m.StatusMessage = "Yanked ID: " + issueID
		m.StatusIsError = false
	}

	// Clear status after 2 seconds
	return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return ClearStatusMsg{}
	})
}

// sendToWorktree emits a message for embedding contexts to handle
func (m Model) sendToWorktree() (tea.Model, tea.Cmd) {
	var issueID, title string

	// Priority: epic task cursor > modal issue > panel selection
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		if modal.TaskSectionFocused && len(modal.EpicTasks) > 0 &&
			modal.EpicTasksCursor < len(modal.EpicTasks) {
			task := modal.EpicTasks[modal.EpicTasksCursor]
			issueID, title = task.ID, task.Title
		} else {
			issueID, title = modal.IssueID, modal.Issue.Title
		}
	} else {
		issueID = m.SelectedIssueID(m.ActivePanel)
		if issueID == "" {
			return m, nil
		}
		issue, err := m.DB.GetIssue(issueID)
		if err != nil || issue == nil {
			return m, nil
		}
		title = issue.Title
	}

	return m, func() tea.Msg {
		return SendTaskToWorktreeMsg{TaskID: issueID, TaskTitle: title}
	}
}

// recordReviewAction opens the record-review reason prompt for the selected
// issue. Gated on review_policy_mode=delegated and reviewer eligibility — if
// either fails, the action is a no-op so the binding has no effect outside
// the expected conditions. See docs/plans/orchestrator-review-closure-plan.md
// under "Monitor / TUI Changes > Actions" for the split.
func (m Model) recordReviewAction() (tea.Model, tea.Cmd) {
	if m.ActivePanel != PanelTaskList {
		return m, nil
	}
	issueID := m.SelectedIssueID(m.ActivePanel)
	if issueID == "" {
		return m, nil
	}
	issue, err := m.DB.GetIssue(issueID)
	if err != nil || issue == nil {
		return m, nil
	}
	if issue.Status != models.StatusInReview {
		return m, nil
	}

	inputs := loadMonitorApproveInputs(m.DB, m.BaseDir, m.SessionID, issue)
	if inputs.Mode != reviewpolicy.ModeDelegated {
		m.StatusMessage = "record-review is only available under review_policy_mode=delegated"
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} })
	}
	if inputs.HasActiveApproval {
		m.StatusMessage = "review already recorded; press 'a' to close"
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} })
	}
	decision := monitorApproveDecision(inputs)
	if !decision.Allowed {
		if decision.RejectionMessage != "" {
			m.StatusMessage = decision.RejectionMessage
		} else {
			m.StatusMessage = "you are not eligible to review this issue"
		}
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} })
	}

	m = m.openRecordReviewModal(issue.ID, issue.Title)
	return m, nil
}

// executeRecordReview writes the issue_reviews row (decision=approved by
// default) and updates reviewer_session / reviewed_at. Status stays
// in_review — this is the record-only path. Called from the confirm button
// on the record-review modal.
func (m Model) executeRecordReview() (tea.Model, tea.Cmd) {
	if m.RecordReviewIssueID == "" {
		m.closeRecordReviewModal()
		return m, nil
	}
	issueID := m.RecordReviewIssueID
	reason := m.RecordReviewInput.Value()
	decision := m.RecordReviewDecision
	if decision == "" {
		decision = reviewpolicy.DecisionApproved
	}

	if reason == "" {
		m.StatusMessage = "record-review requires a reason"
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} })
	}

	issue, err := m.DB.GetIssue(issueID)
	if err != nil || issue == nil {
		m.closeRecordReviewModal()
		return m, nil
	}

	// Supersede any stale rows + snapshot prior-active id for undo.
	priorActive := ""
	if pa, _ := m.DB.GetActiveApprovalReview(issueID); pa != nil {
		priorActive = pa.ID
	}
	_ = m.DB.SupersedeActiveReviews(issueID)

	reviewID, err := m.DB.CreateIssueReview(issueID, m.SessionID, decision, reason, issue.ReviewRequestedBySession, false)
	if err != nil {
		m.closeRecordReviewModal()
		return m, nil
	}

	actionType := models.ActionReviewApprove
	if decision == reviewpolicy.DecisionChangesRequested {
		actionType = models.ActionReviewChangesRequested
	} else {
		now := time.Now()
		issue.ReviewerSession = m.SessionID
		issue.ReviewedAt = &now
	}

	if err := m.DB.UpdateIssueLoggedWithReviewMeta(issue, models.StatusInReview, m.SessionID, actionType, reviewID, priorActive); err != nil {
		m.closeRecordReviewModal()
		return m, nil
	}

	sessionAction := models.ActionSessionReviewApproved
	if decision == reviewpolicy.DecisionChangesRequested {
		sessionAction = models.ActionSessionReviewChangesRequested
	}
	_ = m.DB.RecordSessionAction(issueID, m.SessionID, sessionAction)

	_ = m.DB.AddLog(&models.Log{
		IssueID:   issueID,
		SessionID: m.SessionID,
		Message:   "Review recorded (" + decision + "): " + reason,
		Type:      models.LogTypeProgress,
	})

	m.closeRecordReviewModal()
	if decision == reviewpolicy.DecisionChangesRequested {
		m.StatusMessage = "Recorded changes-requested review on " + issueID
	} else {
		m.StatusMessage = "REVIEW RECORDED " + issueID
	}
	m.StatusIsError = false

	cmds := []tea.Cmd{
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} }),
		m.fetchData(),
	}
	if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
		cmds = append(cmds, m.fetchBoardIssues(m.BoardMode.Board.ID))
	}
	return m, tea.Batch(cmds...)
}

// executeSelfReviewApprove is the confirm handler for the trusted-mode
// self-review modal. The operator has acknowledged that they implemented the
// issue and chose to approve it as a self-review; this re-runs the eligibility
// decision with SelfReviewAcknowledged=true and, when the decision is the
// expected audited self-review allow, closes the issue with self_review=true.
func (m Model) executeSelfReviewApprove() (tea.Model, tea.Cmd) {
	issueID := m.SelfReviewConfirmIssueID
	m.closeSelfReviewConfirmModal()
	if issueID == "" {
		return m, nil
	}

	issue, err := m.DB.GetIssue(issueID)
	if err != nil || issue == nil {
		return m, nil
	}

	// Validate transition with state machine
	sm := workflow.DefaultMachine()
	if !sm.IsValidTransition(issue.Status, models.StatusClosed) {
		return m, nil
	}

	inputs := loadMonitorApproveInputs(m.DB, m.BaseDir, m.SessionID, issue)
	inputs.SelfReviewAcknowledged = true
	decision := monitorApproveDecision(inputs)
	// Only proceed on the audited self-review allow. If the decision is not a
	// self-review (e.g. mode flipped, or the session is no longer the
	// implementer) fall back to the normal allow check.
	if !decision.Allowed {
		return m, nil
	}
	return m.executeApproveClose(issue, decision.SelfReview)
}

// filterActiveBlockers returns only non-closed issues from a list of blockers
func filterActiveBlockers(blockers []models.Issue) []models.Issue {
	var active []models.Issue
	for _, b := range blockers {
		if b.Status != models.StatusClosed {
			active = append(active, b)
		}
	}
	return active
}
