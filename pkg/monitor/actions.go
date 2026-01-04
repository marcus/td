package monitor

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// markForReview marks the selected issue for review
// Works from modal view, CurrentWork panel, or TaskList panel
// Accepts both in_progress and open (ready) issues
func (m Model) markForReview() (tea.Model, tea.Cmd) {
	var issueID string
	var issue *models.Issue

	// Check if a modal is open - use that issue
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		issueID = modal.IssueID
		issue = modal.Issue
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

	// Only allow marking in_progress or open issues for review
	if issue.Status != models.StatusInProgress && issue.Status != models.StatusOpen {
		return m, nil
	}

	// Update status
	issue.Status = models.StatusInReview
	if err := m.DB.UpdateIssue(issue); err != nil {
		return m, nil
	}

	// Log action for undo
	m.DB.LogAction(&models.ActionLog{
		SessionID:  m.SessionID,
		ActionType: models.ActionReview,
		EntityType: "issue",
		EntityID:   issueID,
	})

	// Cascade up to parent epic if all siblings are ready
	m.DB.CascadeUpParentStatus(issueID, models.StatusInReview, m.SessionID)

	// If we're in a modal, close it since the issue moved to review
	if m.ModalOpen() {
		m.closeModal()
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

	m.ConfirmOpen = true
	m.ConfirmAction = "delete"
	m.ConfirmIssueID = issueID
	m.ConfirmTitle = issue.Title

	return m, nil
}

// executeDelete performs the actual deletion after confirmation
func (m Model) executeDelete() (tea.Model, tea.Cmd) {
	if m.ConfirmIssueID == "" {
		m.ConfirmOpen = false
		return m, nil
	}

	deletedID := m.ConfirmIssueID

	// Delete issue
	if err := m.DB.DeleteIssue(deletedID); err != nil {
		m.ConfirmOpen = false
		return m, nil
	}

	// Log action for undo
	m.DB.LogAction(&models.ActionLog{
		SessionID:  m.SessionID,
		ActionType: models.ActionDelete,
		EntityType: "issue",
		EntityID:   deletedID,
	})

	m.ConfirmOpen = false
	m.ConfirmIssueID = ""
	m.ConfirmTitle = ""
	m.ConfirmAction = ""

	// Close modal if we just deleted the issue being viewed
	if modal := m.CurrentModal(); modal != nil && modal.IssueID == deletedID {
		m.closeModal()
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

	// Can't approve your own issues
	if issue.ImplementerSession == m.SessionID {
		return m, nil
	}

	// Update status
	now := time.Now()
	issue.Status = models.StatusClosed
	issue.ReviewerSession = m.SessionID
	issue.ClosedAt = &now
	if err := m.DB.UpdateIssue(issue); err != nil {
		return m, nil
	}

	// Log action for undo
	m.DB.LogAction(&models.ActionLog{
		SessionID:  m.SessionID,
		ActionType: models.ActionApprove,
		EntityType: "issue",
		EntityID:   issue.ID,
	})

	// Cascade up to parent epic if all siblings are closed
	m.DB.CascadeUpParentStatus(issue.ID, models.StatusClosed, m.SessionID)

	// Clear the saved ID so cursor stays at the same position after refresh
	// The item will move to Closed, and we want cursor at same index for next item
	m.SelectedID[PanelTaskList] = ""

	return m, m.fetchData()
}

// closeIssue closes the selected issue directly (workflow shortcut)
// Works from both main panel selection and modal view
func (m Model) closeIssue() (tea.Model, tea.Cmd) {
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

	// Can't close already-closed issues
	if issue.Status == models.StatusClosed {
		return m, nil
	}

	// Update status
	now := time.Now()
	issue.Status = models.StatusClosed
	issue.ClosedAt = &now
	if err := m.DB.UpdateIssue(issue); err != nil {
		return m, nil
	}

	// Log action for undo
	m.DB.LogAction(&models.ActionLog{
		SessionID:  m.SessionID,
		ActionType: models.ActionClose,
		EntityType: "issue",
		EntityID:   issueID,
	})

	// Cascade up to parent epic if all siblings are closed
	m.DB.CascadeUpParentStatus(issueID, models.StatusClosed, m.SessionID)

	// If we're in a modal, close it since the issue is now closed
	if m.ModalOpen() {
		m.closeModal()
	}

	return m, m.fetchData()
}

// reopenIssue reopens a closed issue
func (m Model) reopenIssue() (tea.Model, tea.Cmd) {
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

	// Can only reopen closed issues
	if issue.Status != models.StatusClosed {
		m.StatusMessage = "Issue is not closed"
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		})
	}

	// Update status
	issue.Status = models.StatusOpen
	issue.ClosedAt = nil
	if err := m.DB.UpdateIssue(issue); err != nil {
		m.StatusMessage = "Failed to reopen: " + err.Error()
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		})
	}

	// Log action for undo
	m.DB.LogAction(&models.ActionLog{
		SessionID:  m.SessionID,
		ActionType: models.ActionReopen,
		EntityType: "issue",
		EntityID:   issueID,
	})

	m.StatusMessage = "REOPENED " + issueID
	m.StatusIsError = false

	// If in modal, update the modal issue
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		modal.Issue.Status = models.StatusOpen
		modal.Issue.ClosedAt = nil
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

	if err := copyToClipboard(markdown); err != nil {
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

	if err := copyToClipboard(issueID); err != nil {
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
