package monitor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/workflow"
)

// openNewIssueForm opens the new issue form
// If an epic is selected/open, auto-populates parent field
func (m Model) openNewIssueForm() (tea.Model, tea.Cmd) {
	var parentID string

	// Check if we're in a modal viewing an epic
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		if modal.Issue.Type == models.TypeEpic {
			parentID = modal.Issue.ID
		}
	}

	// Create form state
	m.FormState = NewFormState(FormModeCreate, parentID)
	m.FormOpen = true
	m.FormScrollOffset = 0

	// Set form width for text wrapping (subtract modal horizontal padding)
	modalWidth, _ := m.formModalDimensions()
	formWidth := modalWidth - 4
	m.FormState.Width = formWidth
	m.FormState.Form.WithWidth(formWidth)

	// Initialize the form and load autofill data
	return m, tea.Batch(m.FormState.Form.Init(), loadAutofillData(m.DB))
}

// openEditIssueForm opens the edit form for the selected/modal issue
func (m Model) openEditIssueForm() (tea.Model, tea.Cmd) {
	var issue *models.Issue

	// If modal is open, edit that issue
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		issue = modal.Issue
	} else {
		// Otherwise, edit the selected issue from the panel
		issueID := m.SelectedIssueID(m.ActivePanel)
		if issueID == "" {
			return m, nil
		}
		var err error
		issue, err = m.DB.GetIssue(issueID)
		if err != nil || issue == nil {
			return m, nil
		}
	}

	// Create form state with issue data
	m.FormState = NewFormStateForEdit(issue)
	m.FormOpen = true
	m.FormScrollOffset = 0

	// Pre-populate dependencies from the database
	depIDs, _ := m.DB.GetDependencies(issue.ID)
	if len(depIDs) > 0 {
		m.FormState.Dependencies = strings.Join(depIDs, ", ")
		m.FormState.buildForm()
	}

	// Set form width for text wrapping (subtract modal horizontal padding)
	modalWidth, _ := m.formModalDimensions()
	formWidth := modalWidth - 4
	m.FormState.Width = formWidth
	m.FormState.Form.WithWidth(formWidth)

	// Initialize the form and load autofill data
	return m, tea.Batch(m.FormState.Form.Init(), loadAutofillData(m.DB))
}

// closeForm closes the form modal and clears state
func (m *Model) closeForm() {
	m.FormOpen = false
	m.FormState = nil
	m.FormScrollOffset = 0
}

// submitForm validates and submits the form
func (m Model) submitForm() (tea.Model, tea.Cmd) {
	if m.FormState == nil {
		return m, nil
	}

	// Get issue data from form
	issue := m.FormState.ToIssue()
	deps := m.FormState.GetDependencies()

	if m.FormState.Mode == FormModeCreate {
		// Create new issue with all fields
		issue.Status = models.StatusOpen
		if err := m.DB.CreateIssueLogged(issue, m.SessionID); err != nil {
			m.Err = err
			return m, nil
		}

		// Add dependencies
		for _, depID := range deps {
			if depID != "" {
				m.DB.AddDependencyLogged(issue.ID, depID, "depends_on", m.SessionID)
			}
		}

		m.closeForm()
		if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
			return m, tea.Batch(m.fetchData(), m.fetchBoardIssues(m.BoardMode.Board.ID))
		}
		return m, m.fetchData()

	} else if m.FormState.Mode == FormModeEdit {
		// Update existing issue
		existingIssue, err := m.DB.GetIssue(m.FormState.IssueID)
		if err != nil || existingIssue == nil {
			m.Err = err
			return m, nil
		}

		// Detect status change
		oldStatus := existingIssue.Status
		newStatus := models.Status(m.FormState.Status)
		statusChanged := oldStatus != newStatus

		// Validate status transition if changed
		if statusChanged {
			sm := workflow.DefaultMachine()
			if !sm.IsValidTransition(oldStatus, newStatus) {
				m.StatusMessage = fmt.Sprintf("Invalid transition: %s → %s", oldStatus, newStatus)
				m.StatusIsError = true
				return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
					return ClearStatusMsg{}
				})
			}
		}

		// Determine action type based on status transition
		actionType := models.ActionUpdate
		if statusChanged {
			actionType = statusTransitionAction(oldStatus, newStatus)
		}

		// Update fields
		existingIssue.Title = issue.Title
		existingIssue.Type = issue.Type
		existingIssue.Priority = issue.Priority
		existingIssue.Description = issue.Description
		existingIssue.Labels = issue.Labels
		existingIssue.ParentID = issue.ParentID
		existingIssue.Points = issue.Points
		existingIssue.Acceptance = issue.Acceptance
		existingIssue.Minor = issue.Minor

		// Apply status change with associated field updates
		if statusChanged {
			existingIssue.Status = newStatus

			switch {
			case newStatus == models.StatusClosed:
				now := time.Now()
				existingIssue.ClosedAt = &now
			case oldStatus == models.StatusClosed:
				// Reopening: clear close metadata
				existingIssue.ClosedAt = nil
				existingIssue.ReviewerSession = ""
			}

			if newStatus == models.StatusInReview && existingIssue.ImplementerSession == "" {
				existingIssue.ImplementerSession = m.SessionID
			}
		}

		if err := m.DB.UpdateIssueLogged(existingIssue, m.SessionID, actionType); err != nil {
			m.Err = err
			return m, nil
		}

		// Sync dependencies: diff old vs new, add/remove as needed
		newDeps := m.FormState.GetDependencies()
		oldDeps, _ := m.DB.GetDependencies(m.FormState.IssueID)
		oldSet := make(map[string]bool, len(oldDeps))
		for _, id := range oldDeps {
			oldSet[id] = true
		}
		newSet := make(map[string]bool, len(newDeps))
		for _, id := range newDeps {
			if id != "" {
				newSet[id] = true
			}
		}
		// Remove deps that were deleted
		for _, id := range oldDeps {
			if !newSet[id] {
				_ = m.DB.RemoveDependencyLogged(m.FormState.IssueID, id, m.SessionID)
			}
		}
		// Add deps that are new
		for _, id := range newDeps {
			if id != "" && !oldSet[id] {
				_ = m.DB.AddDependencyLogged(m.FormState.IssueID, id, "depends_on", m.SessionID)
			}
		}

		// Record session action for bypass prevention
		if statusChanged {
			var sessionAction models.IssueSessionAction
			switch {
			case oldStatus == models.StatusOpen && newStatus == models.StatusInProgress:
				sessionAction = models.ActionSessionStarted
			case oldStatus == models.StatusInProgress && newStatus == models.StatusOpen:
				sessionAction = models.ActionSessionUnstarted
			case oldStatus == models.StatusInReview && newStatus == models.StatusClosed:
				sessionAction = models.ActionSessionReviewed
			}
			if sessionAction != "" {
				m.DB.RecordSessionAction(existingIssue.ID, m.SessionID, sessionAction)
			}
		}

		m.closeForm()

		// Refresh modal if open
		if modal := m.CurrentModal(); modal != nil && modal.IssueID == existingIssue.ID {
			if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
				return m, tea.Batch(m.fetchData(), m.fetchBoardIssues(m.BoardMode.Board.ID), m.fetchIssueDetails(existingIssue.ID))
			}
			return m, tea.Batch(m.fetchData(), m.fetchIssueDetails(existingIssue.ID))
		}

		// Refresh board data if in board mode
		if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
			return m, tea.Batch(m.fetchData(), m.fetchBoardIssues(m.BoardMode.Board.ID))
		}

		return m, m.fetchData()
	}

	return m, nil
}

// openExternalEditor opens the Description field in an external editor
// Uses $VISUAL > $EDITOR > vim fallback
func (m Model) openExternalEditor() (tea.Model, tea.Cmd) {
	if m.FormState == nil {
		return m, nil
	}

	// Get editor from environment
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vim"
	}

	// Create temp file with .md extension for syntax highlighting
	tmpFile, err := os.CreateTemp("", "td-edit-*.md")
	if err != nil {
		m.StatusMessage = "Failed to create temp file: " + err.Error()
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		})
	}

	// Write current description content to temp file
	content := m.FormState.Description
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		m.StatusMessage = "Failed to write temp file: " + err.Error()
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		})
	}
	tmpFile.Close()

	tmpPath := tmpFile.Name()

	// Create editor command
	cmd := exec.Command(editor, tmpPath)

	// Use tea.ExecProcess to suspend TUI and run editor
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		// Read content from temp file
		data, readErr := os.ReadFile(tmpPath)
		os.Remove(tmpPath) // Clean up temp file

		if err != nil {
			return EditorFinishedMsg{
				Field: EditorFieldDescription,
				Error: err,
			}
		}
		if readErr != nil {
			return EditorFinishedMsg{
				Field: EditorFieldDescription,
				Error: readErr,
			}
		}

		return EditorFinishedMsg{
			Field:   EditorFieldDescription,
			Content: string(data),
		}
	})
}

// handleEditorFinished updates the form field after external editor closes
func (m Model) handleEditorFinished(msg EditorFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		m.StatusMessage = "Editor error: " + msg.Error.Error()
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		})
	}

	if m.FormState == nil {
		return m, nil
	}

	// Update the appropriate field based on which was edited
	switch msg.Field {
	case EditorFieldDescription:
		m.FormState.Description = msg.Content
	case EditorFieldAcceptance:
		m.FormState.Acceptance = msg.Content
	}

	// Rebuild the form to reflect the changes
	m.FormState.buildForm()

	m.StatusMessage = "Content updated from editor"
	m.StatusIsError = false
	return m, tea.Batch(
		m.FormState.Form.Init(),
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		}),
	)
}

// statusTransitionAction returns the appropriate ActionType for a status transition.
// Uses specific action types for well-known transitions to produce meaningful activity log entries.
func statusTransitionAction(from, to models.Status) models.ActionType {
	switch {
	case from == models.StatusOpen && to == models.StatusInProgress:
		return models.ActionStart
	case to == models.StatusInReview:
		return models.ActionReview
	case to == models.StatusClosed:
		return models.ActionClose
	case from == models.StatusClosed && to == models.StatusOpen:
		return models.ActionReopen
	case to == models.StatusBlocked:
		return models.ActionBlock
	case from == models.StatusBlocked && (to == models.StatusOpen || to == models.StatusInProgress):
		return models.ActionUnblock
	default:
		return models.ActionUpdate
	}
}

// formScrollForFocusedField computes the scroll offset needed to show the
// currently focused field within the visible area of the form modal.
// Returns the new FormScrollOffset value.
func (m Model) formScrollForFocusedField() int {
	if m.FormState == nil || m.FormState.Form == nil {
		return 0
	}

	// Get the focused field key and find its label in the rendered form
	focusedKey := m.FormState.focusedFieldKey()
	if focusedKey == "" {
		return m.FormScrollOffset
	}

	// Render form to string to find where the focused field appears
	formView := m.FormState.Form.View()

	// Build full inner content (mirrors renderFormModal)
	modalWidth, _ := m.formModalDimensions()
	formWidth := modalWidth - 4
	if formWidth > 0 {
		m.FormState.Form.WithWidth(formWidth)
	}

	submitFocused := m.FormState.ButtonFocus == formButtonFocusSubmit
	cancelFocused := m.FormState.ButtonFocus == formButtonFocusCancel
	buttons := renderButtonPair("Submit", "Cancel", submitFocused, cancelFocused, false, false, false, false)
	inner := lipgloss.JoinVertical(lipgloss.Left, formView, "", buttons)

	allLines := strings.Split(inner, "\n")
	totalLines := len(allLines)

	// Available visible lines (matching renderFormModal calculation)
	maxHeight := m.Height - 2
	availableLines := maxHeight - 4
	if availableLines < 5 {
		availableLines = 5
	}
	if totalLines <= availableLines {
		return 0 // No scroll needed
	}

	// Map focusedKey to a title substring we can search for in rendered output
	fieldTitles := map[string]string{
		formKeyTitle:        "Title",
		formKeyType:         "Type",
		formKeyPriority:     "Priority",
		formKeyDescription:  "Description",
		formKeyLabels:       "Labels",
		formKeyParent:       "Parent Epic",
		formKeyPoints:       "Story Points",
		formKeyAcceptance:   "Acceptance Criteria",
		formKeyMinor:        "Minor Issue",
		formKeyDependencies: "Dependencies",
		formKeyStatus:       "Status",
	}
	title, ok := fieldTitles[focusedKey]
	if !ok {
		return m.FormScrollOffset
	}

	// Find which line contains the focused field title
	fieldLine := -1
	for i, line := range allLines {
		// Strip ANSI for matching
		plain := stripANSISequences(line)
		if strings.Contains(plain, title) {
			fieldLine = i
			break
		}
	}
	if fieldLine < 0 {
		return m.FormScrollOffset
	}

	// Reserve 1 line for scroll indicator
	viewLines := availableLines - 1
	if viewLines < 1 {
		viewLines = 1
	}

	// Ensure the field is visible
	current := m.FormScrollOffset
	// If field is above the current window, scroll up
	if fieldLine < current {
		return fieldLine
	}
	// If field is below the current window, scroll down
	if fieldLine >= current+viewLines {
		newOffset := fieldLine - viewLines + 1
		// Also ensure buttons are accessible: if we're near the end, don't over-scroll
		maxScroll := totalLines - viewLines
		if maxScroll < 0 {
			maxScroll = 0
		}
		if newOffset > maxScroll {
			newOffset = maxScroll
		}
		return newOffset
	}
	return current
}

// stripANSISequences removes ANSI escape sequences from a string for plain-text matching
func stripANSISequences(s string) string {
	var result strings.Builder
	inEscape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				inEscape = false
			}
			continue
		}
		result.WriteByte(c)
	}
	return result.String()
}

// formScrollToBottom computes the scroll offset needed to show the bottom
// of the form (buttons row). Used when Submit/Cancel buttons are focused.
func (m Model) formScrollToBottom() int {
	if m.FormState == nil || m.FormState.Form == nil {
		return 0
	}

	modalWidth, _ := m.formModalDimensions()
	formWidth := modalWidth - 4
	if formWidth > 0 {
		m.FormState.Form.WithWidth(formWidth)
	}

	formView := m.FormState.Form.View()
	buttons := renderButtonPair("Submit", "Cancel", true, false, false, false, false, false)
	inner := lipgloss.JoinVertical(lipgloss.Left, formView, "", buttons)

	allLines := strings.Split(inner, "\n")
	totalLines := len(allLines)

	maxHeight := m.Height - 2
	availableLines := maxHeight - 4
	if availableLines < 5 {
		availableLines = 5
	}

	if totalLines <= availableLines {
		return 0
	}

	viewLines := availableLines - 1 // 1 for scroll indicator
	if viewLines < 1 {
		viewLines = 1
	}

	// Scroll to end so buttons are visible
	maxScroll := totalLines - viewLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	return maxScroll
}
