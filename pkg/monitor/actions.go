package monitor

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/workflow"
)

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
				m.DB.UpdateIssueLogged(child, m.SessionID, models.ActionReview)
				m.DB.AddLog(&models.Log{
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
		// Refresh the modal issue data and epic tasks list
		return m, tea.Batch(m.fetchData(), m.fetchIssueDetails(modal.IssueID))
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
	m.DB.AddLog(&models.Log{
		IssueID:   issueID,
		SessionID: m.SessionID,
		Message:   logMsg,
		Type:      models.LogTypeProgress,
	})

	// Cascade DOWN to descendants if this is a parent issue (epic)
	if hasChildren, _ := m.DB.HasChildren(issueID); hasChildren {
		descendants, err := m.DB.GetDescendantIssues(issueID, []models.Status{
			models.StatusOpen,
			models.StatusInProgress,
			models.StatusInReview,
		})
		if err == nil && len(descendants) > 0 {
			now := time.Now()
			for _, child := range descendants {
				child.Status = models.StatusClosed
				child.ClosedAt = &now
				if child.ImplementerSession == "" {
					child.ImplementerSession = m.SessionID
				}
				m.DB.UpdateIssueLogged(child, m.SessionID, models.ActionClose)
				m.DB.AddLog(&models.Log{
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
		// If we closed an epic task (not the modal's main issue), refresh to update the list
		// If we closed the main issue, also refresh to show updated status
		return m, tea.Batch(m.fetchData(), m.fetchIssueDetails(modal.IssueID))
	}

	return m, m.fetchData()
}

// approveIssue approves/closes the selected reviewable issue
func (m Model) approveIssue() (tea.Model, tea.Cmd) {
	// Must be in Task List panel
	if m.ActivePanel != PanelTaskList {
		return m, nil
	}

	cursor := m.Cursor[PanelTaskList]
	if cursor >= len(m.TaskListRows) {
		return m, nil
	}

	row := m.TaskListRows[cursor]
	// Only allow approving reviewable issues
	if row.Category != CategoryReviewable {
		return m, nil
	}

	issue, err := m.DB.GetIssue(row.Issue.ID)
	if err != nil || issue == nil {
		return m, nil
	}

	// Validate transition with state machine
	sm := workflow.DefaultMachine()
	if !sm.IsValidTransition(issue.Status, models.StatusClosed) {
		return m, nil
	}

	// Can't approve your own issues
	if issue.ImplementerSession == m.SessionID {
		return m, nil
	}

	// Update status
	now := time.Now()
	issue.Status = models.StatusClosed
	issue.ReviewerSession = m.SessionID
	issue.ClosedAt = &now
	if err := m.DB.UpdateIssueLogged(issue, m.SessionID, models.ActionApprove); err != nil {
		return m, nil
	}

	// Record session action for bypass prevention
	m.DB.RecordSessionAction(issue.ID, m.SessionID, models.ActionSessionReviewed)

	// Cascade DOWN to descendants if this is a parent issue (epic)
	if hasChildren, _ := m.DB.HasChildren(issue.ID); hasChildren {
		descendants, err := m.DB.GetDescendantIssues(issue.ID, []models.Status{
			models.StatusOpen,
			models.StatusInProgress,
			models.StatusInReview,
		})
		if err == nil && len(descendants) > 0 {
			now := time.Now()
			for _, child := range descendants {
				child.Status = models.StatusClosed
				child.ClosedAt = &now
				child.ReviewerSession = m.SessionID
				if child.ImplementerSession == "" {
					child.ImplementerSession = m.SessionID
				}
				m.DB.UpdateIssueLogged(child, m.SessionID, models.ActionApprove)
				m.DB.AddLog(&models.Log{
					IssueID:   child.ID,
					SessionID: m.SessionID,
					Message:   "Cascaded approval from " + issue.ID,
					Type:      models.LogTypeProgress,
				})
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
		return m, tea.Batch(
			tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} }),
			m.fetchData(),
			m.fetchIssueDetails(modal.IssueID),
		)
	}

	return m, tea.Batch(
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} }),
		m.fetchData(),
	)
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
