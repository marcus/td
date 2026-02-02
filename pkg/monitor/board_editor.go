package monitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/query"
	"github.com/marcus/td/pkg/monitor/modal"
	"github.com/marcus/td/pkg/monitor/mouse"
)

// openBoardEditor opens the board editor for the currently highlighted board in the picker
func (m Model) openBoardEditor() (Model, tea.Cmd) {
	if !m.BoardPickerOpen || len(m.AllBoards) == 0 {
		return m, nil
	}
	if m.BoardPickerCursor >= len(m.AllBoards) {
		return m, nil
	}
	board := m.AllBoards[m.BoardPickerCursor]
	m = m.openBoardEditorModal(&board)
	// Trigger initial query preview if board has a query
	if board.Query != "" {
		return m, m.boardEditorQueryPreview(board.Query)
	}
	return m, nil
}

// openBoardEditorCreate opens the board editor in create mode
func (m Model) openBoardEditorCreate() (Model, tea.Cmd) {
	m = m.openBoardEditorModal(nil)
	return m, nil
}

// openBoardEditorModal opens the board editor modal.
// board == nil means create mode; non-nil means edit (or info for builtin).
func (m Model) openBoardEditorModal(board *models.Board) Model {
	m.BoardEditorOpen = true
	m.BoardEditorBoard = board
	m.BoardEditorDeleteConfirm = false
	m.BoardEditorPreview = &boardEditorPreviewData{}

	// Determine mode
	if board == nil {
		m.BoardEditorMode = "create"
	} else if board.IsBuiltin {
		m.BoardEditorMode = "info"
	} else {
		m.BoardEditorMode = "edit"
	}

	// Initialize name input — stored as pointer so the modal's sections and
	// the bubbletea Model copies all reference the same underlying instance.
	nameInput := textinput.New()
	nameInput.Placeholder = "Board name"
	nameInput.Width = 40
	nameInput.CharLimit = 100
	if board != nil {
		nameInput.SetValue(board.Name)
	}
	m.BoardEditorNameInput = &nameInput

	// Initialize query textarea — stored as pointer for same reason.
	// Must set width before any Update/View calls to avoid zero-width panics.
	queryInput := textarea.New()
	queryInput.Placeholder = "TDQ query (optional)"
	queryInput.CharLimit = 500
	queryInput.ShowLineNumbers = false
	queryInput.SetWidth(40)
	queryInput.SetHeight(3)
	if board != nil {
		queryInput.SetValue(board.Query)
	}
	m.BoardEditorQueryInput = &queryInput

	// Focus name input initially
	m.BoardEditorNameInput.Focus()

	// Create modal
	m.BoardEditorModal = m.createBoardEditorModal()
	m.BoardEditorModal.Reset()
	m.BoardEditorMouseHandler = mouse.NewHandler()

	return m
}

// closeBoardEditorModal closes the board editor modal
func (m *Model) closeBoardEditorModal() {
	m.BoardEditorOpen = false
	m.BoardEditorMode = ""
	m.BoardEditorBoard = nil
	m.BoardEditorNameInput = nil
	m.BoardEditorQueryInput = nil
	m.BoardEditorModal = nil
	m.BoardEditorMouseHandler = nil
	m.BoardEditorDeleteConfirm = false
	m.BoardEditorPreview = nil
}

// createBoardEditorModal builds the declarative modal for board editing.
func (m *Model) createBoardEditorModal() *modal.Modal {
	// Calculate width: 70% of terminal, capped 50-90
	modalWidth := m.Width * 70 / 100
	if modalWidth > 90 {
		modalWidth = 90
	}
	if modalWidth < 50 {
		modalWidth = 50
	}

	// Determine title and variant
	var title string
	variant := modal.VariantDefault
	switch m.BoardEditorMode {
	case "create":
		title = "NEW BOARD"
	case "edit":
		title = "EDIT BOARD"
	case "info":
		title = "BOARD INFO"
		variant = modal.VariantInfo
	}

	md := modal.New(title,
		modal.WithWidth(modalWidth),
		modal.WithVariant(variant),
		modal.WithHints(false),
		modal.WithPrimaryAction("save"),
	)

	if m.BoardEditorMode == "info" {
		// Read-only info view for builtin boards
		if m.BoardEditorBoard != nil {
			md.AddSection(modal.Text("Name: " + m.BoardEditorBoard.Name))
			md.AddSection(modal.Spacer())
			if m.BoardEditorBoard.Query != "" {
				md.AddSection(modal.Text("Query: " + m.BoardEditorBoard.Query))
			} else {
				md.AddSection(modal.Text("Query: (none - shows all issues)"))
			}
			md.AddSection(modal.Spacer())
			md.AddSection(modal.Text("This is a builtin board and cannot be modified."))
		}
		md.AddSection(modal.Spacer())
		md.AddSection(modal.Buttons(
			modal.Btn(" Close ", "cancel"),
		))
		md.AddSection(modal.Spacer())
		md.AddSection(modal.Text("Esc:close"))
	} else {
		// Edit or Create mode
		md.AddSection(modal.InputWithLabel("name", "Name:", m.BoardEditorNameInput,
			modal.WithSubmitOnEnter(false),
		))
		md.AddSection(modal.Spacer())
		md.AddSection(modal.TextareaWithLabel("query", "Query:", m.BoardEditorQueryInput, 3))
		md.AddSection(modal.Spacer())

		// Live query preview section
		md.AddSection(modal.Custom(
			func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
				return modal.RenderedSection{
					Content: m.renderBoardEditorQueryPreview(contentWidth),
				}
			},
			nil,
		))
		md.AddSection(modal.Spacer())

		// TDQ Quick Reference section
		md.AddSection(modal.Custom(
			func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
				return modal.RenderedSection{
					Content: m.renderBoardEditorTDQRef(contentWidth),
				}
			},
			nil,
		))
		md.AddSection(modal.Spacer())

		// Buttons
		if m.BoardEditorMode == "edit" {
			md.AddSection(modal.Buttons(
				modal.Btn(" Save ", "save"),
				modal.Btn(" Delete ", "delete", modal.BtnDanger()),
				modal.Btn(" Cancel ", "cancel"),
			))
		} else {
			md.AddSection(modal.Buttons(
				modal.Btn(" Create ", "save"),
				modal.Btn(" Cancel ", "cancel"),
			))
		}

		md.AddSection(modal.Spacer())
		md.AddSection(modal.Text("Tab:switch  Ctrl+S:save  Esc:cancel"))
	}

	return md
}

// renderBoardEditorQueryPreview renders the live query preview section.
func (m *Model) renderBoardEditorQueryPreview(contentWidth int) string {
	var sb strings.Builder

	preview := m.BoardEditorPreview
	if preview == nil {
		sb.WriteString(subtleStyle.Render("Preview: (loading...)"))
		return sb.String()
	}

	if m.BoardEditorQueryInput == nil {
		sb.WriteString(subtleStyle.Render("Preview: (no query input)"))
		return sb.String()
	}
	queryStr := m.BoardEditorQueryInput.Value()
	if queryStr == "" {
		sb.WriteString(subtleStyle.Render("Preview: (empty query matches all issues)"))
		return sb.String()
	}

	if preview.Error != nil {
		errStyle := lipgloss.NewStyle().Foreground(errorColor)
		sb.WriteString(errStyle.Render("Error: " + preview.Error.Error()))
		return sb.String()
	}

	// Show results
	sb.WriteString(fmt.Sprintf("Matches: %d issue(s)", preview.Count))
	if len(preview.Titles) > 0 {
		for _, t := range preview.Titles {
			title := t
			maxLen := contentWidth - 4
			if maxLen > 0 && len(title) > maxLen {
				title = title[:maxLen-3] + "..."
			}
			sb.WriteString("\n  " + subtleStyle.Render("• "+title))
		}
		if preview.Count > len(preview.Titles) {
			sb.WriteString(fmt.Sprintf("\n  "+subtleStyle.Render("... and %d more"), preview.Count-len(preview.Titles)))
		}
	}

	return sb.String()
}

// renderBoardEditorTDQRef renders a condensed TDQ quick reference.
func (m *Model) renderBoardEditorTDQRef(contentWidth int) string {
	var sb strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(primaryColor)
	sb.WriteString(headerStyle.Render("TDQ Quick Reference") + "\n")
	sb.WriteString("─────────────────────────────\n")
	sb.WriteString("Fields: status, type, priority, labels, title\n")
	sb.WriteString("Status: open, in_progress, blocked, in_review, closed\n")
	sb.WriteString("Type:   bug, feature, task, epic, chore\n")
	sb.WriteString("Ops:    = != ~ < > <= >=\n")
	sb.WriteString("Logic:  AND OR NOT (grouping)\n")
	sb.WriteString("Funcs:  has(f), is(s), any(f,v1,v2), descendant_of(id)\n")
	sb.WriteString("Sort:   sort:priority  sort:-created  sort:-updated\n")
	sb.WriteString("Values: @me, today, -7d, EMPTY\n")
	sb.WriteString("─────────────────────────────\n")
	sb.WriteString(subtleStyle.Render("Example: type = bug AND priority <= P1"))

	return sb.String()
}

// handleBoardEditorAction handles actions from the board editor modal
func (m Model) handleBoardEditorAction(action string) (Model, tea.Cmd) {
	switch action {
	case "save":
		return m.executeBoardEditorSave()
	case "delete":
		if m.BoardEditorDeleteConfirm {
			return m.executeBoardEditorDelete()
		}
		// First press: show confirmation
		m.BoardEditorDeleteConfirm = true
		// Recreate modal to show delete confirmation state
		m.BoardEditorModal = m.createBoardEditorDeleteConfirmModal()
		m.BoardEditorModal.Reset()
		return m, nil
	case "delete-confirm":
		return m.executeBoardEditorDelete()
	case "delete-cancel":
		m.BoardEditorDeleteConfirm = false
		m.BoardEditorModal = m.createBoardEditorModal()
		m.BoardEditorModal.Reset()
		return m, nil
	case "cancel":
		m.closeBoardEditorModal()
		return m, nil
	}
	return m, nil
}

// createBoardEditorDeleteConfirmModal builds the delete confirmation overlay
func (m *Model) createBoardEditorDeleteConfirmModal() *modal.Modal {
	boardName := ""
	if m.BoardEditorBoard != nil {
		boardName = m.BoardEditorBoard.Name
	}

	md := modal.New("DELETE BOARD?",
		modal.WithWidth(50),
		modal.WithVariant(modal.VariantDanger),
		modal.WithHints(false),
	)

	md.AddSection(modal.Text(fmt.Sprintf("Delete board \"%s\"?", boardName)))
	md.AddSection(modal.Text("This cannot be undone."))
	md.AddSection(modal.Spacer())
	md.AddSection(modal.Buttons(
		modal.Btn(" Delete ", "delete-confirm", modal.BtnDanger()),
		modal.Btn(" Cancel ", "delete-cancel"),
	))
	md.AddSection(modal.Spacer())
	md.AddSection(modal.Text("Tab:switch  Enter:select  Esc:cancel"))

	return md
}

// executeBoardEditorSave saves or creates the board
func (m Model) executeBoardEditorSave() (Model, tea.Cmd) {
	if m.BoardEditorNameInput == nil || m.BoardEditorQueryInput == nil {
		return m, nil
	}
	name := strings.TrimSpace(m.BoardEditorNameInput.Value())
	queryStr := strings.TrimSpace(m.BoardEditorQueryInput.Value())

	if name == "" {
		m.StatusMessage = "Board name cannot be empty"
		m.StatusIsError = true
		return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} })
	}

	isNew := m.BoardEditorMode == "create"
	board := m.BoardEditorBoard

	return m, func() tea.Msg {
		if isNew {
			newBoard, err := m.DB.CreateBoardLogged(name, queryStr, m.SessionID)
			return BoardEditorSaveResultMsg{Board: newBoard, IsNew: true, Error: err}
		}
		// Update existing
		board.Name = name
		board.Query = queryStr
		err := m.DB.UpdateBoardLogged(board, m.SessionID)
		return BoardEditorSaveResultMsg{Board: board, IsNew: false, Error: err}
	}
}

// executeBoardEditorDelete deletes the board
func (m Model) executeBoardEditorDelete() (Model, tea.Cmd) {
	if m.BoardEditorBoard == nil {
		return m, nil
	}
	boardID := m.BoardEditorBoard.ID

	return m, func() tea.Msg {
		err := m.DB.DeleteBoardLogged(boardID, m.SessionID)
		return BoardEditorDeleteResultMsg{BoardID: boardID, Error: err}
	}
}

// boardEditorDebouncedPreview returns a debounced command for query preview (300ms)
func (m Model) boardEditorDebouncedPreview(queryStr string) tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return boardEditorDebounceMsg{Query: queryStr}
	})
}

// boardEditorQueryPreview returns a command that executes the query for live preview
func (m Model) boardEditorQueryPreview(queryStr string) tea.Cmd {
	return func() tea.Msg {
		if queryStr == "" {
			return BoardEditorQueryPreviewMsg{Query: queryStr}
		}

		issues, err := query.Execute(m.DB, queryStr, m.SessionID, query.ExecuteOptions{
			Limit: 6, // Get 6 to know if there are more than 5
		})
		if err != nil {
			return BoardEditorQueryPreviewMsg{Query: queryStr, Error: err}
		}

		titles := make([]string, 0, 5)
		for i, issue := range issues {
			if i >= 5 {
				break
			}
			titles = append(titles, issue.Title)
		}

		return BoardEditorQueryPreviewMsg{
			Query:  queryStr,
			Count:  len(issues),
			Titles: titles,
		}
	}
}
