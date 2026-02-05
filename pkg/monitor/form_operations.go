package monitor

import (
	"os"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/td/internal/models"
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

	// Set form width for text wrapping (subtract modal horizontal padding)
	modalWidth, _ := m.formModalDimensions()
	formWidth := modalWidth - 4
	m.FormState.Width = formWidth
	m.FormState.Form.WithWidth(formWidth)

	// Initialize the form
	return m, m.FormState.Form.Init()
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

	// Set form width for text wrapping (subtract modal horizontal padding)
	modalWidth, _ := m.formModalDimensions()
	formWidth := modalWidth - 4
	m.FormState.Width = formWidth
	m.FormState.Form.WithWidth(formWidth)

	// Initialize the form
	return m, m.FormState.Form.Init()
}

// closeForm closes the form modal and clears state
func (m *Model) closeForm() {
	m.FormOpen = false
	m.FormState = nil
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
		return m, m.fetchData()

	} else if m.FormState.Mode == FormModeEdit {
		// Update existing issue
		existingIssue, err := m.DB.GetIssue(m.FormState.IssueID)
		if err != nil || existingIssue == nil {
			m.Err = err
			return m, nil
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

		if err := m.DB.UpdateIssueLogged(existingIssue, m.SessionID, models.ActionUpdate); err != nil {
			m.Err = err
			return m, nil
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
