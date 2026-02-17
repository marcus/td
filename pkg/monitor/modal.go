package monitor

import (
	"fmt"
	"strings"

	"encoding/json"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/pkg/monitor/modal"
	"github.com/marcus/td/pkg/monitor/mouse"
)

// openModal opens the details modal for the currently selected issue
func (m Model) openModal() (tea.Model, tea.Cmd) {
	issueID := m.SelectedIssueID(m.ActivePanel)
	if issueID == "" {
		return m, nil
	}

	return m.pushModal(issueID, m.ActivePanel)
}

// pushModal pushes a new modal onto the stack
func (m Model) pushModal(issueID string, sourcePanel Panel) (tea.Model, tea.Cmd) {
	entry := ModalEntry{
		IssueID:     issueID,
		SourcePanel: sourcePanel,
		Loading:     true,
	}
	m.ModalStack = append(m.ModalStack, entry)

	return m, m.fetchIssueDetails(issueID)
}

// closeModal pops the top modal from the stack
func (m *Model) closeModal() {
	if len(m.ModalStack) > 0 {
		m.ModalStack = m.ModalStack[:len(m.ModalStack)-1]
	}
}

// ModalOpen returns true if any modal is open
func (m Model) ModalOpen() bool {
	return len(m.ModalStack) > 0
}

// ModalDepth returns the current modal stack depth (0 = no modal)
func (m Model) ModalDepth() int {
	return len(m.ModalStack)
}

// CurrentModal returns a pointer to the current (top) modal entry, or nil if none
func (m *Model) CurrentModal() *ModalEntry {
	if len(m.ModalStack) == 0 {
		return nil
	}
	return &m.ModalStack[len(m.ModalStack)-1]
}

// ModalSourcePanel returns the source panel of the base modal (depth 1)
func (m Model) ModalSourcePanel() Panel {
	if len(m.ModalStack) == 0 {
		return PanelCurrentWork
	}
	return m.ModalStack[0].SourcePanel
}

// ModalBreadcrumb returns a breadcrumb string for the modal stack
func (m Model) ModalBreadcrumb() string {
	if len(m.ModalStack) <= 1 {
		return ""
	}
	var parts []string
	for _, entry := range m.ModalStack {
		if entry.Issue != nil {
			parts = append(parts, string(entry.Issue.Type)+": "+entry.IssueID)
		} else {
			parts = append(parts, entry.IssueID)
		}
	}
	return strings.Join(parts, " > ")
}

// navigateModal moves to the prev/next issue in the navigation scope or source panel's list
// Works at depth 1 for panel lists, or at any depth when NavigationScope is set
func (m Model) navigateModal(delta int) (tea.Model, tea.Cmd) {
	modal := m.CurrentModal()
	if modal == nil {
		return m, nil
	}

	// Get the list of issue IDs to navigate through
	var issueIDs []string
	usingScopedNavigation := len(modal.NavigationScope) > 0

	if usingScopedNavigation {
		// Use scoped navigation (e.g., epic children)
		for _, issue := range modal.NavigationScope {
			issueIDs = append(issueIDs, issue.ID)
		}
	} else {
		// Only allow panel navigation at depth 1
		if m.ModalDepth() != 1 {
			return m, nil
		}

		// Get the list of issue IDs for the source panel
		switch m.ModalSourcePanel() {
		case PanelCurrentWork:
			issueIDs = m.CurrentWorkRows
		case PanelTaskList:
			for _, row := range m.TaskListRows {
				issueIDs = append(issueIDs, row.Issue.ID)
			}
		case PanelActivity:
			// For activity, collect unique issue IDs
			seen := make(map[string]bool)
			for _, item := range m.Activity {
				if item.IssueID != "" && !seen[item.IssueID] {
					seen[item.IssueID] = true
					issueIDs = append(issueIDs, item.IssueID)
				}
			}
		}
	}

	if len(issueIDs) == 0 {
		return m, nil
	}

	// Find current position
	currentIdx := -1
	for i, id := range issueIDs {
		if id == modal.IssueID {
			currentIdx = i
			break
		}
	}

	if currentIdx == -1 {
		return m, nil
	}

	// Calculate new position with bounds
	newIdx := currentIdx + delta
	if newIdx < 0 || newIdx >= len(issueIDs) {
		return m, nil // At boundary, don't wrap
	}

	// Navigate to new issue - replace the current modal entry
	newIssueID := issueIDs[newIdx]
	// Preserve NavigationScope when navigating within scope
	savedScope := modal.NavigationScope

	modal.IssueID = newIssueID
	modal.Scroll = 0
	modal.Loading = true
	modal.Error = nil
	modal.Issue = nil
	modal.Handoff = nil
	modal.Logs = nil
	modal.BlockedBy = nil
	modal.Blocks = nil
	modal.EpicTasks = nil
	modal.EpicTasksCursor = 0
	modal.TaskSectionFocused = false
	modal.ParentEpic = nil
	modal.ParentEpicFocused = false
	modal.DescRender = ""
	modal.AcceptRender = ""
	modal.NavigationScope = savedScope

	// Update cursor position in source panel (only for non-scoped navigation at depth 1)
	if !usingScopedNavigation {
		m.Cursor[m.ModalSourcePanel()] = newIdx
		m.saveSelectedID(m.ModalSourcePanel())
	}

	return m, m.fetchIssueDetails(newIssueID)
}

// estimateModalContentLines estimates the number of content lines in a modal
// This is used to clamp scroll values and prevent over-scrolling
func (m Model) estimateModalContentLines(modal *ModalEntry) int {
	if modal == nil || modal.Issue == nil {
		return 10 // Minimal default
	}

	lines := 0
	issue := modal.Issue

	// Header + status section
	lines += 5 // ID, title, blank, status, blank

	// Parent epic
	if modal.ParentEpic != nil {
		lines += 2
	}

	// Labels, implementer, reviewer
	if len(issue.Labels) > 0 {
		lines++
	}
	if issue.ImplementerSession != "" {
		lines++
	}
	if issue.ReviewerSession != "" {
		lines++
	}
	if issue.DeferUntil != nil {
		lines++
	}
	if issue.DueDate != nil {
		lines++
	}
	if issue.DeferCount > 0 {
		lines++
	}
	lines++ // Blank

	// Epic tasks
	if issue.Type == models.TypeEpic && len(modal.EpicTasks) > 0 {
		lines += 1 + len(modal.EpicTasks) + 1 // Header + tasks + blank
	}

	// Description - use rendered version if available, otherwise estimate from raw
	if issue.Description != "" {
		lines++ // Header
		if modal.DescRender != "" {
			lines += strings.Count(modal.DescRender, "\n") + 1
		} else {
			lines += strings.Count(issue.Description, "\n") + 1
		}
		lines++ // Blank
	}

	// Acceptance criteria
	if issue.Acceptance != "" {
		lines++ // Header
		if modal.AcceptRender != "" {
			lines += strings.Count(modal.AcceptRender, "\n") + 1
		} else {
			lines += strings.Count(issue.Acceptance, "\n") + 1
		}
		lines++ // Blank
	}

	// Dependencies and blockers
	lines += len(modal.BlockedBy) + len(modal.Blocks)
	if len(modal.BlockedBy) > 0 {
		lines += 2 // Header + blank
	}
	if len(modal.Blocks) > 0 {
		lines += 2 // Header + blank
	}

	// Handoff
	if modal.Handoff != nil {
		lines += 2 // Header + blank
		lines += len(modal.Handoff.Done)
		lines += len(modal.Handoff.Remaining)
		lines += len(modal.Handoff.Decisions)
		lines += len(modal.Handoff.Uncertain)
	}

	// Logs
	if len(modal.Logs) > 0 {
		lines++ // Header
		contentWidth := m.modalContentWidth()
		for _, log := range modal.Logs {
			lines += len(renderLogLines(log, contentWidth))
		}
	}

	// Comments
	if len(modal.Comments) > 0 {
		lines += 1 + len(modal.Comments) // Header + comments
	}

	return lines
}

// modalMaxScroll returns the maximum scroll value for a modal
func (m Model) modalMaxScroll(modal *ModalEntry) int {
	if modal == nil {
		return 0
	}

	// Calculate visible height (same as view)
	modalHeight := m.Height * 80 / 100
	if modalHeight > 40 {
		modalHeight = 40
	}
	if modalHeight < 15 {
		modalHeight = 15
	}
	visibleHeight := modalHeight - 4 // Account for border and footer

	maxScroll := modal.ContentLines - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	return maxScroll
}

// modalContentWidth returns the content width for modal text rendering.
// This must match the width calculation in renderModal (view.go).
func (m Model) modalContentWidth() int {
	// Modal is 80% of terminal width, capped 40-100
	modalWidth := m.Width * 80 / 100
	if modalWidth > 100 {
		modalWidth = 100
	}
	if modalWidth < 40 {
		modalWidth = 40
	}
	// Content width accounts for border (2) and padding (2) = 4
	contentWidth := modalWidth - 4
	if contentWidth < 30 {
		contentWidth = 30
	}
	return contentWidth
}

// renderMarkdownAsync returns a command that renders markdown in background
func (m Model) renderMarkdownAsync(issueID, desc, accept string, width int) tea.Cmd {
	theme := m.MarkdownTheme // capture for closure
	return func() tea.Msg {
		return MarkdownRenderedMsg{
			IssueID:      issueID,
			DescRender:   preRenderMarkdown(desc, width, theme),
			AcceptRender: preRenderMarkdown(accept, width, theme),
		}
	}
}

// preRenderMarkdown renders markdown once (expensive operation).
// If theme is nil, uses td's default ANSI 256 color palette.
func preRenderMarkdown(text string, width int, theme *MarkdownThemeConfig) string {
	if text == "" {
		return ""
	}

	// Use custom theme if provided, otherwise use td monitor default palette
	renderer, err := glamour.NewTermRenderer(getGlamourOptionsWithTheme(width, theme)...)
	if err != nil {
		return text // fallback to plain text
	}

	rendered, err := renderer.Render(text)
	if err != nil {
		return text // fallback to plain text
	}

	// Glamour adds lots of trailing newlines - strip them all
	return strings.TrimRight(rendered, "\n\r\t ")
}

// openStatsModal opens the stats modal and fetches stats data
func (m Model) openStatsModal() (tea.Model, tea.Cmd) {
	m.StatsOpen = true
	m.StatsScroll = 0
	m.StatsLoading = true
	m.StatsError = nil
	m.StatsData = nil

	// Create mouse handler (modal will be created when data loads)
	m.StatsMouseHandler = mouse.NewHandler()

	return m, m.fetchStats()
}

// closeStatsModal closes the stats modal and clears transient state
func (m *Model) closeStatsModal() {
	m.StatsOpen = false
	m.StatsScroll = 0
	m.StatsLoading = false
	m.StatsError = nil
	m.StatsData = nil
	m.StatsModal = nil
	m.StatsMouseHandler = nil
}

// createStatsModal builds the declarative modal for statistics.
// The content is rendered via a Custom section since it uses bar charts and complex layout.
func (m *Model) createStatsModal() *modal.Modal {
	// Calculate width based on terminal size (80% width, capped)
	modalWidth := m.Width * 80 / 100
	if modalWidth > 100 {
		modalWidth = 100
	}
	if modalWidth < 50 {
		modalWidth = 50
	}

	md := modal.New("Statistics",
		modal.WithWidth(modalWidth),
		modal.WithVariant(modal.VariantDefault), // Use primary color (green)
		modal.WithHints(false),                  // No hints, we have our own footer
	)

	// Use Custom section for the scrollable stats content
	md.AddSection(modal.Custom(
		func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
			return modal.RenderedSection{
				Content: m.renderStatsContent(contentWidth),
			}
		},
		nil, // No update handling needed
	))

	// Add Close button
	md.AddSection(modal.Buttons(
		modal.Btn(" Close ", "close"),
	))

	return md
}

// openTDQHelpModal opens the TDQ query help modal
func (m Model) openTDQHelpModal() Model {
	m.ShowTDQHelp = true
	m.TDQHelpModal = m.createTDQHelpModal()
	m.TDQHelpModal.Reset()
	m.TDQHelpMouseHandler = mouse.NewHandler()
	return m
}

// closeTDQHelpModal closes the TDQ query help modal
func (m *Model) closeTDQHelpModal() {
	m.ShowTDQHelp = false
	m.TDQHelpModal = nil
	m.TDQHelpMouseHandler = nil
}

// createTDQHelpModal builds the declarative modal for TDQ query syntax help.
func (m *Model) createTDQHelpModal() *modal.Modal {
	// Calculate width based on terminal size (70% width, capped)
	modalWidth := m.Width * 70 / 100
	if modalWidth > 80 {
		modalWidth = 80
	}
	if modalWidth < 50 {
		modalWidth = 50
	}

	md := modal.New("TDQ Query Syntax",
		modal.WithWidth(modalWidth),
		modal.WithVariant(modal.VariantInfo), // Cyan border for info
		modal.WithHints(false),               // No hints, we have our own footer
	)

	// Use Custom section for the help content
	md.AddSection(modal.Custom(
		func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
			return modal.RenderedSection{
				Content: m.Keymap.GenerateTDQHelp(),
			}
		},
		nil, // No update handling needed
	))

	// Add Close button
	md.AddSection(modal.Buttons(
		modal.Btn(" Close ", "close"),
	))

	return md
}

// openHandoffsModal opens the handoffs modal and fetches data
func (m Model) openHandoffsModal() (tea.Model, tea.Cmd) {
	m.HandoffsOpen = true
	m.HandoffsCursor = 0
	m.HandoffsScroll = 0
	m.HandoffsLoading = true
	m.HandoffsError = nil
	m.HandoffsData = nil

	// Create mouse handler (modal will be created when data loads)
	m.HandoffsMouseHandler = mouse.NewHandler()

	return m, m.fetchHandoffs()
}

// closeHandoffsModal closes the handoffs modal and clears state
func (m *Model) closeHandoffsModal() {
	m.HandoffsOpen = false
	m.HandoffsCursor = 0
	m.HandoffsScroll = 0
	m.HandoffsLoading = false
	m.HandoffsError = nil
	m.HandoffsData = nil
	m.HandoffsModal = nil
	m.HandoffsMouseHandler = nil
}

// createHandoffsModal builds the declarative modal for handoffs.
// This must be called after data loads since the list content depends on HandoffsData.
func (m *Model) createHandoffsModal() *modal.Modal {
	// Calculate width based on terminal size (80% width, capped)
	modalWidth := m.Width * 80 / 100
	if modalWidth > 100 {
		modalWidth = 100
	}
	if modalWidth < 50 {
		modalWidth = 50
	}

	md := modal.New("Recent Handoffs",
		modal.WithWidth(modalWidth),
		modal.WithVariant(modal.VariantDefault), // Green variant
		modal.WithHints(false),                  // No hints, we have our own footer
	)

	// Build list items from handoffs data
	items := make([]modal.ListItem, 0, len(m.HandoffsData))
	for i, h := range m.HandoffsData {
		// Format: [timestamp] [session] [issue_id] done:X remaining:Y
		timestamp := h.Timestamp.Format("01-02 15:04")
		session := truncateSession(h.SessionID)
		issueID := h.IssueID

		summary := fmt.Sprintf("done:%d remaining:%d", len(h.Done), len(h.Remaining))
		if len(h.Uncertain) > 0 {
			summary += fmt.Sprintf(" uncertain:%d", len(h.Uncertain))
		}

		label := fmt.Sprintf("%s %s %s %s", timestamp, session, issueID, summary)
		items = append(items, modal.ListItem{
			ID:    fmt.Sprintf("handoff-%d", i),
			Label: label,
			Data:  i, // Store index for action handling
		})
	}

	// Calculate max visible items based on modal height
	modalHeight := m.Height * 80 / 100
	if modalHeight > 40 {
		modalHeight = 40
	}
	if modalHeight < 15 {
		modalHeight = 15
	}
	// Account for title, buttons, padding, and borders
	maxVisible := modalHeight - 8
	if maxVisible < 3 {
		maxVisible = 3
	}
	if maxVisible > len(items) {
		maxVisible = len(items)
	}

	// Add list section with handoff items
	md.AddSection(modal.List("handoffs-list", items, &m.HandoffsCursor, modal.WithMaxVisible(maxVisible)))

	// Add buttons
	md.AddSection(modal.Spacer())
	md.AddSection(modal.Buttons(
		modal.Btn(" Open Issue ", "open"),
		modal.Btn(" Close ", "close"),
	))

	return md
}

// openIssueFromHandoffs opens the issue detail modal for the selected handoff
func (m Model) openIssueFromHandoffs() (tea.Model, tea.Cmd) {
	if m.HandoffsCursor >= len(m.HandoffsData) {
		return m, nil
	}
	issueID := m.HandoffsData[m.HandoffsCursor].IssueID
	m.closeHandoffsModal()
	return m.pushModal(issueID, PanelCurrentWork)
}

// openBoardPickerModal opens the board picker modal and fetches data
func (m Model) openBoardPickerModal() (Model, tea.Cmd) {
	m.BoardPickerOpen = true
	m.BoardPickerCursor = 0
	m.BoardPickerHover = -1

	// Create mouse handler (modal will be created when data loads)
	m.BoardPickerMouseHandler = mouse.NewHandler()

	return m, m.fetchBoards()
}

// closeBoardPickerModal closes the board picker modal and clears state
func (m *Model) closeBoardPickerModal() {
	m.BoardPickerOpen = false
	m.BoardPickerCursor = 0
	m.BoardPickerHover = -1
	m.BoardPickerModal = nil
	m.BoardPickerMouseHandler = nil
}

// createBoardPickerModal builds the declarative modal for board picker.
// This must be called after data loads since the list content depends on AllBoards.
func (m *Model) createBoardPickerModal() *modal.Modal {
	// Calculate width based on terminal size (60% width, capped)
	modalWidth := m.Width * 60 / 100
	if modalWidth > 80 {
		modalWidth = 80
	}
	if modalWidth < 40 {
		modalWidth = 40
	}

	md := modal.New(fmt.Sprintf("SELECT BOARD (%d)", len(m.AllBoards)),
		modal.WithWidth(modalWidth),
		modal.WithVariant(modal.VariantDefault), // Purple/primary color (212)
		modal.WithHints(false),                  // No hints, we have our own footer
	)

	// Build list items from boards data
	items := make([]modal.ListItem, 0, len(m.AllBoards))
	for i, b := range m.AllBoards {
		// Format board line
		name := b.Name
		if b.IsBuiltin {
			name += " (builtin)"
		}
		if b.Query != "" {
			queryPreview := b.Query
			if len(queryPreview) > 30 {
				queryPreview = queryPreview[:27] + "..."
			}
			name += " \u2022 " + queryPreview // bullet character
		}

		items = append(items, modal.ListItem{
			ID:    fmt.Sprintf("board-%d", i),
			Label: name,
			Data:  i, // Store index for action handling
		})
	}

	// Calculate max visible items based on modal height
	modalHeight := m.Height * 60 / 100
	if modalHeight > 30 {
		modalHeight = 30
	}
	if modalHeight < 10 {
		modalHeight = 10
	}
	// Account for title, buttons, padding, and borders
	maxVisible := modalHeight - 8
	if maxVisible < 3 {
		maxVisible = 3
	}
	if maxVisible > len(items) {
		maxVisible = len(items)
	}

	// Add list section with board items
	md.AddSection(modal.List("boards-list", items, &m.BoardPickerCursor, modal.WithMaxVisible(maxVisible)))

	// Add buttons
	md.AddSection(modal.Spacer())
	md.AddSection(modal.Buttons(
		modal.Btn(" Select ", "select"),
		modal.Btn(" Cancel ", "cancel"),
	))

	// Shortcuts footer
	md.AddSection(modal.Spacer())
	md.AddSection(modal.Text("Enter:select  e:edit  n:new  Esc:close"))

	return md
}

// openDeleteConfirmModal opens the delete confirmation modal
func (m Model) openDeleteConfirmModal(issueID, issueTitle string) Model {
	m.ConfirmOpen = true
	m.ConfirmAction = "delete"
	m.ConfirmIssueID = issueID
	m.ConfirmTitle = issueTitle

	// Create declarative modal and mouse handler
	m.DeleteConfirmModal = m.createDeleteConfirmModal()
	m.DeleteConfirmModal.Reset()
	m.DeleteConfirmMouseHandler = mouse.NewHandler()

	return m
}

// closeDeleteConfirmModal closes the delete confirmation modal and clears state
func (m *Model) closeDeleteConfirmModal() {
	m.ConfirmOpen = false
	m.ConfirmAction = ""
	m.ConfirmIssueID = ""
	m.ConfirmTitle = ""
	m.DeleteConfirmModal = nil
	m.DeleteConfirmMouseHandler = nil
}

// createDeleteConfirmModal builds the declarative modal for delete confirmation.
func (m *Model) createDeleteConfirmModal() *modal.Modal {
	// Calculate width based on issue title length (matches legacy behavior)
	width := 40
	if len(m.ConfirmTitle) > 30 {
		width = len(m.ConfirmTitle) + 10
	}
	if width > 60 {
		width = 60
	}

	// Build title based on action
	action := "Delete"
	if m.ConfirmAction != "" && m.ConfirmAction != "delete" {
		action = m.ConfirmAction
	}
	title := action + " " + m.ConfirmIssueID + "?"

	md := modal.New(title,
		modal.WithWidth(width),
		modal.WithVariant(modal.VariantDanger), // Red border for destructive action
		modal.WithHints(false),                 // We use custom hint text
	)

	// Truncate issue title to fit
	maxTitleLen := width - 10
	if maxTitleLen < 20 {
		maxTitleLen = 20
	}
	displayTitle := m.ConfirmTitle
	if len(displayTitle) > maxTitleLen {
		displayTitle = displayTitle[:maxTitleLen-3] + "..."
	}

	// Add issue title as text section (with quotes)
	md.AddSection(modal.Text("\"" + displayTitle + "\""))

	// Add spacer before buttons
	md.AddSection(modal.Spacer())

	// Add buttons - Yes is danger, No is normal
	md.AddSection(modal.Buttons(
		modal.Btn(" Yes ", "yes", modal.BtnDanger()),
		modal.Btn(" No ", "no"),
	))

	// Add custom hint text
	md.AddSection(modal.Spacer())
	md.AddSection(modal.Text("Tab:switch  Y/N:quick  Esc:cancel"))

	return md
}

// handleDeleteConfirmAction handles actions from the delete confirmation modal
func (m Model) handleDeleteConfirmAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "yes", "delete":
		return m.executeDelete()
	case "no", "cancel":
		m.closeDeleteConfirmModal()
		return m, nil
	}
	return m, nil
}

// openCloseConfirmModal opens the close confirmation modal
func (m Model) openCloseConfirmModal(issueID, issueTitle string) Model {
	m.CloseConfirmOpen = true
	m.CloseConfirmIssueID = issueID
	m.CloseConfirmTitle = issueTitle

	// Create textinput for reason
	m.CloseConfirmInput = textinput.New()
	m.CloseConfirmInput.Placeholder = "Optional: reason for closing"
	m.CloseConfirmInput.Width = 40

	// Create declarative modal and mouse handler
	m.CloseConfirmModal = m.createCloseConfirmModal()
	m.CloseConfirmModal.Reset()
	m.CloseConfirmMouseHandler = mouse.NewHandler()

	return m
}

// closeCloseConfirmModal closes the close confirmation modal and clears state
func (m *Model) closeCloseConfirmModal() {
	m.CloseConfirmOpen = false
	m.CloseConfirmIssueID = ""
	m.CloseConfirmTitle = ""
	m.CloseConfirmModal = nil
	m.CloseConfirmMouseHandler = nil
}

// createCloseConfirmModal builds the declarative modal for close confirmation.
func (m *Model) createCloseConfirmModal() *modal.Modal {
	// Calculate width based on issue title length (matches legacy behavior)
	width := 50
	if len(m.CloseConfirmTitle) > 40 {
		width = len(m.CloseConfirmTitle) + 10
	}
	if width > 60 {
		width = 60
	}

	title := fmt.Sprintf("Close %s?", m.CloseConfirmIssueID)

	md := modal.New(title,
		modal.WithWidth(width),
		modal.WithVariant(modal.VariantDanger),  // Red border for destructive action
		modal.WithHints(false),                  // We use custom hint text
		modal.WithPrimaryAction("confirm"),     // Enter on input submits confirm
	)

	// Truncate issue title to fit
	maxTitleLen := width - 10
	if maxTitleLen < 20 {
		maxTitleLen = 20
	}
	displayTitle := m.CloseConfirmTitle
	if len(displayTitle) > maxTitleLen {
		displayTitle = displayTitle[:maxTitleLen-3] + "..."
	}

	// Add issue title as text section (with quotes)
	md.AddSection(modal.Text("\"" + displayTitle + "\""))

	// Add spacer before input
	md.AddSection(modal.Spacer())

	// Add reason input with label
	md.AddSection(modal.InputWithLabel("reason", "Reason (optional):", &m.CloseConfirmInput,
		modal.WithSubmitOnEnter(true),
		modal.WithSubmitAction("confirm"),
	))

	// Add spacer before buttons
	md.AddSection(modal.Spacer())

	// Add buttons - Confirm is normal, Cancel is also normal
	md.AddSection(modal.Buttons(
		modal.Btn(" Confirm ", "confirm"),
		modal.Btn(" Cancel ", "cancel"),
	))

	// Add custom hint text
	md.AddSection(modal.Spacer())
	md.AddSection(modal.Text("Tab:switch  Enter:confirm  Esc:cancel"))

	return md
}

// handleCloseConfirmAction handles actions from the close confirmation modal
func (m Model) handleCloseConfirmAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "confirm":
		return m.executeCloseWithReason()
	case "cancel":
		m.closeCloseConfirmModal()
		return m, nil
	}
	return m, nil
}

// navigateEpicTask navigates to the prev/next task within the epic's task list
func (m Model) navigateEpicTask(delta int) (tea.Model, tea.Cmd) {
	modal := m.CurrentModal()
	if modal == nil || !modal.TaskSectionFocused || len(modal.EpicTasks) == 0 {
		return m, nil
	}

	// Calculate new cursor position
	newIdx := modal.EpicTasksCursor + delta
	if newIdx < 0 || newIdx >= len(modal.EpicTasks) {
		return m, nil // At boundary, don't wrap
	}

	// Update cursor and open the task with navigation scope
	modal.EpicTasksCursor = newIdx
	taskID := modal.EpicTasks[newIdx].ID
	return m.pushModalWithScope(taskID, m.ModalSourcePanel(), modal.EpicTasks)
}

// pushModalWithScope pushes a new modal with a navigation scope for l/r navigation
func (m Model) pushModalWithScope(issueID string, sourcePanel Panel, scope []models.Issue) (tea.Model, tea.Cmd) {
	entry := ModalEntry{
		IssueID:         issueID,
		SourcePanel:     sourcePanel,
		Loading:         true,
		NavigationScope: scope,
	}
	m.ModalStack = append(m.ModalStack, entry)

	return m, m.fetchIssueDetails(issueID)
}

// handleModalClick handles left-click events within a modal
func (m Model) handleModalClick(x, y int) (tea.Model, tea.Cmd) {
	modal := m.CurrentModal()
	if modal == nil {
		return m, nil
	}

	// Calculate modal bounds (centered, 80% width, capped)
	modalWidth := m.Width * 80 / 100
	if modalWidth > 100 {
		modalWidth = 100
	}
	if modalWidth < 40 {
		modalWidth = 40
	}
	modalHeight := m.Height * 80 / 100
	if modalHeight > 40 {
		modalHeight = 40
	}
	if modalHeight < 10 {
		modalHeight = 10
	}

	// Modal position (centered)
	modalX := (m.Width - modalWidth) / 2
	modalY := (m.Height - modalHeight) / 2

	// Check if click is inside modal
	if x < modalX || x >= modalX+modalWidth || y < modalY || y >= modalY+modalHeight {
		return m, nil // Click outside modal
	}

	// Convert click Y to line index (accounting for modal borders and scroll)
	// Modal has 1-line border, 1-line header, then content starts at line 2
	contentStartY := modalY + 3 // Border + title + blank line
	if y < contentStartY {
		return m, nil // Click on header
	}

	// Calculate which line was clicked
	clickedLine := (y - contentStartY) + modal.Scroll

	// Determine which section was clicked
	activeBlockers := filterActiveBlockers(modal.BlockedBy)

	// Check if click is in blocked-by section
	if len(activeBlockers) > 0 && clickedLine >= modal.BlockedByStartLine && clickedLine < modal.BlockedByEndLine {
		// Calculate which row within the section (header is first line)
		rowInSection := clickedLine - modal.BlockedByStartLine - 1 // -1 for header
		if rowInSection >= 0 && rowInSection < len(activeBlockers) {
			// Unfocus other sections
			modal.ParentEpicFocused = false
			modal.TaskSectionFocused = false
			modal.BlocksSectionFocused = false
			// Focus this section and set cursor
			modal.BlockedBySectionFocused = true
			modal.BlockedByCursor = rowInSection
		}
		return m, nil
	}

	// Check if click is in blocks section
	if len(modal.Blocks) > 0 && clickedLine >= modal.BlocksStartLine && clickedLine < modal.BlocksEndLine {
		// Calculate which row within the section (header is first line)
		rowInSection := clickedLine - modal.BlocksStartLine - 1 // -1 for header
		if rowInSection >= 0 && rowInSection < len(modal.Blocks) {
			// Unfocus other sections
			modal.ParentEpicFocused = false
			modal.TaskSectionFocused = false
			modal.BlockedBySectionFocused = false
			// Focus this section and set cursor
			modal.BlocksSectionFocused = true
			modal.BlocksCursor = rowInSection
		}
		return m, nil
	}

	// Click elsewhere in modal - unfocus all sections (return to scroll mode)
	modal.ParentEpicFocused = false
	modal.TaskSectionFocused = false
	modal.BlockedBySectionFocused = false
	modal.BlocksSectionFocused = false
	return m, nil
}

// openActivityDetailModal opens the adaptive detail modal for an activity item
func (m Model) openActivityDetailModal(item ActivityItem) (tea.Model, tea.Cmd) {
	m.ActivityDetailOpen = true
	m.ActivityDetailItem = &item
	m.ActivityDetailScroll = 0
	m.ActivityDetailMouseHandler = mouse.NewHandler()
	m.ActivityDetailModal = m.createActivityDetailModal()
	m.ActivityDetailModal.Reset()
	return m, nil
}

// closeActivityDetailModal closes the activity detail modal
func (m *Model) closeActivityDetailModal() {
	m.ActivityDetailOpen = false
	m.ActivityDetailItem = nil
	m.ActivityDetailScroll = 0
	m.ActivityDetailModal = nil
	m.ActivityDetailMouseHandler = nil
}

// createActivityDetailModal builds the declarative modal for activity detail
func (m *Model) createActivityDetailModal() *modal.Modal {
	item := m.ActivityDetailItem
	if item == nil {
		return nil
	}

	modalWidth := m.Width * 70 / 100
	if modalWidth > 90 {
		modalWidth = 90
	}
	if modalWidth < 45 {
		modalWidth = 45
	}

	title := activityDetailTitle(item)
	variant := activityDetailVariant(item)

	md := modal.New(title,
		modal.WithWidth(modalWidth),
		modal.WithVariant(variant),
		modal.WithHints(false),
	)

	// Timestamp + session header
	header := item.Timestamp.Format("2006-01-02 15:04:05")
	if item.SessionID != "" {
		header += "  session:" + truncateSession(item.SessionID)
	}
	md.AddSection(modal.Text(subtleStyle.Render(header)))
	md.AddSection(modal.Spacer())

	// Content section adapts based on type
	md.AddSection(modal.Custom(
		func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
			return modal.RenderedSection{
				Content: m.renderActivityDetailContent(contentWidth),
			}
		},
		nil,
	))

	// Buttons
	showOpenIssue := item.IssueID != "" && (item.Type != "action" || item.EntityType == "issue")
	if showOpenIssue {
		md.AddSection(modal.Buttons(
			modal.Btn(" Open Issue ", "open-issue"),
			modal.Btn(" Close ", "close"),
		))
	} else {
		md.AddSection(modal.Buttons(
			modal.Btn(" Close ", "close"),
		))
	}

	return md
}

// activityDetailTitle returns the modal title based on activity type
func activityDetailTitle(item *ActivityItem) string {
	switch item.Type {
	case "log":
		return "Log Entry"
	case "comment":
		return "Comment"
	case "action":
		return activityActionTitle(item)
	}
	return "Activity Detail"
}

// activityActionTitle returns the title for action-type activities
func activityActionTitle(item *ActivityItem) string {
	switch item.EntityType {
	case "issue":
		return "Issue Action: " + string(item.Action)
	case "issue_dependencies":
		return "Dependency Change"
	case "issue_files":
		return "File Link Change"
	case "board":
		return "Board Change"
	case "board_issue_positions":
		return "Board Position Change"
	case "work_session_issues":
		return "Work Session Tag"
	case "handoff":
		return "Handoff"
	case "note", "notes":
		return "Note Action: " + string(item.Action)
	case "logs":
		return "Log Created"
	case "comments":
		return "Comment Created"
	}
	return "Action: " + string(item.Action)
}

// activityDetailVariant returns the modal color variant for the activity type
func activityDetailVariant(item *ActivityItem) modal.Variant {
	switch item.Type {
	case "log":
		return modal.VariantInfo
	case "comment":
		return modal.VariantDefault
	case "action":
		return modal.VariantDefault
	}
	return modal.VariantDefault
}

// renderActivityDetailContent renders the adaptive content based on activity type
func (m Model) renderActivityDetailContent(contentWidth int) string {
	item := m.ActivityDetailItem
	if item == nil {
		return ""
	}

	var b strings.Builder

	switch item.Type {
	case "log":
		m.renderLogDetail(&b, item, contentWidth)
	case "comment":
		m.renderCommentDetail(&b, item, contentWidth)
	case "action":
		m.renderActionDetail(&b, item, contentWidth)
	}

	return b.String()
}

// renderLogDetail renders log entry detail
func (m Model) renderLogDetail(b *strings.Builder, item *ActivityItem, width int) {
	// Type badge
	if item.LogType != "" {
		badge := logTypeBadge(item.LogType)
		b.WriteString(badge + "\n\n")
	}

	// Full message
	b.WriteString(item.Message)

	// Issue link
	if item.IssueID != "" {
		b.WriteString("\n\n")
		b.WriteString(subtleStyle.Render("Issue: "))
		b.WriteString(lipgloss.NewStyle().Bold(true).Render(item.IssueID))
		if item.IssueTitle != "" {
			b.WriteString(" " + item.IssueTitle)
		}
	}
}

// renderCommentDetail renders comment detail
func (m Model) renderCommentDetail(b *strings.Builder, item *ActivityItem, width int) {
	// Full comment text
	b.WriteString(item.Message)

	// Issue link
	if item.IssueID != "" {
		b.WriteString("\n\n")
		b.WriteString(subtleStyle.Render("Issue: "))
		b.WriteString(lipgloss.NewStyle().Bold(true).Render(item.IssueID))
		if item.IssueTitle != "" {
			b.WriteString(" " + item.IssueTitle)
		}
	}
}

// renderActionDetail renders action detail based on entity type
func (m Model) renderActionDetail(b *strings.Builder, item *ActivityItem, width int) {
	// Action description
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(item.Message))
	b.WriteString("\n")

	switch item.EntityType {
	case "issue":
		renderIssueActionDiff(b, item)
	case "issue_dependencies":
		renderDependencyDetail(b, item)
	case "issue_files":
		renderFileDetail(b, item)
	case "board":
		renderBoardDetail(b, item)
	case "handoff":
		renderHandoffDetail(b, item)
	case "note", "notes":
		renderNoteDetail(b, item)
	default:
		renderGenericActionDetail(b, item)
	}

	// Issue link
	if item.IssueID != "" {
		b.WriteString("\n")
		b.WriteString(subtleStyle.Render("Entity: "))
		b.WriteString(lipgloss.NewStyle().Bold(true).Render(item.IssueID))
		if item.IssueTitle != "" {
			b.WriteString(" " + item.IssueTitle)
		}
	}
}

// renderIssueActionDiff shows before/after for issue state changes
func renderIssueActionDiff(b *strings.Builder, item *ActivityItem) {
	if item.PreviousData == "" && item.NewData == "" {
		return
	}

	var prev, next map[string]interface{}
	if item.PreviousData != "" {
		json.Unmarshal([]byte(item.PreviousData), &prev)
	}
	if item.NewData != "" {
		json.Unmarshal([]byte(item.NewData), &next)
	}

	// Show changed fields
	diffFields := []string{"title", "description", "status", "priority", "type", "labels", "parent_id", "acceptance", "points", "defer_until", "due_date"}
	changes := 0
	for _, field := range diffFields {
		prevVal := fmt.Sprintf("%v", prev[field])
		nextVal := fmt.Sprintf("%v", next[field])
		if prev[field] == nil {
			prevVal = ""
		}
		if next[field] == nil {
			nextVal = ""
		}
		if prevVal != nextVal && (prevVal != "" || nextVal != "") {
			if changes == 0 {
				b.WriteString("\n" + subtleStyle.Render("Changes:") + "\n")
			}
			b.WriteString(fmt.Sprintf("  %s: %s → %s\n", field, prevVal, nextVal))
			changes++
		}
	}
}

// renderDependencyDetail shows dependency relationship info
func renderDependencyDetail(b *strings.Builder, item *ActivityItem) {
	var data map[string]interface{}
	src := item.NewData
	if src == "" {
		src = item.PreviousData
	}
	if src != "" {
		json.Unmarshal([]byte(src), &data)
	}
	if data != nil {
		if issueID, ok := data["issue_id"].(string); ok {
			b.WriteString("\n" + subtleStyle.Render("Issue: ") + issueID)
		}
		if depID, ok := data["depends_on_id"].(string); ok {
			b.WriteString("\n" + subtleStyle.Render("Depends on: ") + depID)
		}
	}
}

// renderFileDetail shows file link info
func renderFileDetail(b *strings.Builder, item *ActivityItem) {
	var data map[string]interface{}
	src := item.NewData
	if src == "" {
		src = item.PreviousData
	}
	if src != "" {
		json.Unmarshal([]byte(src), &data)
	}
	if data != nil {
		if path, ok := data["file_path"].(string); ok {
			b.WriteString("\n" + subtleStyle.Render("File: ") + path)
		}
		if role, ok := data["role"].(string); ok {
			b.WriteString("\n" + subtleStyle.Render("Role: ") + role)
		}
	}
}

// renderBoardDetail shows board change info
func renderBoardDetail(b *strings.Builder, item *ActivityItem) {
	var data map[string]interface{}
	if item.NewData != "" {
		json.Unmarshal([]byte(item.NewData), &data)
	}
	if data != nil {
		if name, ok := data["name"].(string); ok {
			b.WriteString("\n" + subtleStyle.Render("Board: ") + name)
		}
		if query, ok := data["query"].(string); ok && query != "" {
			b.WriteString("\n" + subtleStyle.Render("Query: ") + query)
		}
	}
}

// renderHandoffDetail shows handoff done/remaining/decisions/uncertain
func renderHandoffDetail(b *strings.Builder, item *ActivityItem) {
	var data map[string]interface{}
	if item.NewData != "" {
		json.Unmarshal([]byte(item.NewData), &data)
	}
	if data == nil {
		return
	}

	renderHandoffSection(b, data, "done", "Done")
	renderHandoffSection(b, data, "remaining", "Remaining")
	renderHandoffSection(b, data, "decisions", "Decisions")
	renderHandoffSection(b, data, "uncertain", "Uncertain")

	if issueID, ok := data["issue_id"].(string); ok && issueID != "" {
		b.WriteString("\n" + subtleStyle.Render("Issue: ") + issueID)
	}
}

// renderHandoffSection renders a single handoff list section
func renderHandoffSection(b *strings.Builder, data map[string]interface{}, key, label string) {
	raw, ok := data[key]
	if !ok {
		return
	}

	// Handoff data may be a JSON string or an array
	var items []string
	switch v := raw.(type) {
	case string:
		json.Unmarshal([]byte(v), &items)
	case []interface{}:
		for _, i := range v {
			if s, ok := i.(string); ok {
				items = append(items, s)
			}
		}
	}

	if len(items) == 0 {
		return
	}

	b.WriteString("\n" + lipgloss.NewStyle().Bold(true).Render(label+":") + "\n")
	for _, item := range items {
		b.WriteString("  • " + item + "\n")
	}
}

// renderNoteDetail shows note content
func renderNoteDetail(b *strings.Builder, item *ActivityItem) {
	var data map[string]interface{}
	src := item.NewData
	if src == "" {
		src = item.PreviousData
	}
	if src != "" {
		json.Unmarshal([]byte(src), &data)
	}
	if data != nil {
		if title, ok := data["title"].(string); ok {
			b.WriteString("\n" + subtleStyle.Render("Title: ") + title)
		}
		if content, ok := data["content"].(string); ok && content != "" {
			preview := content
			if len(preview) > 200 {
				preview = preview[:197] + "..."
			}
			b.WriteString("\n" + subtleStyle.Render("Content: ") + preview)
		}
		if pinned, ok := data["pinned"].(bool); ok && pinned {
			b.WriteString("\n" + subtleStyle.Render("Pinned: ") + "yes")
		}
	}
}

// renderGenericActionDetail shows raw data for unknown action types
func renderGenericActionDetail(b *strings.Builder, item *ActivityItem) {
	if item.EntityType != "" {
		b.WriteString("\n" + subtleStyle.Render("Entity type: ") + item.EntityType)
	}
	if item.NewData != "" && len(item.NewData) < 200 {
		b.WriteString("\n" + subtleStyle.Render("Data: ") + item.NewData)
	}
}

// logTypeBadge returns a styled badge for log types
func logTypeBadge(logType models.LogType) string {
	style := lipgloss.NewStyle().Padding(0, 1)
	switch logType {
	case models.LogTypeProgress:
		return style.Background(lipgloss.Color("27")).Foreground(lipgloss.Color("255")).Render("PROGRESS")
	case models.LogTypeDecision:
		return style.Background(lipgloss.Color("135")).Foreground(lipgloss.Color("255")).Render("DECISION")
	case models.LogTypeBlocker:
		return style.Background(lipgloss.Color("196")).Foreground(lipgloss.Color("255")).Render("BLOCKER")
	case models.LogTypeHypothesis:
		return style.Background(lipgloss.Color("208")).Foreground(lipgloss.Color("255")).Render("HYPOTHESIS")
	case models.LogTypeTried:
		return style.Background(lipgloss.Color("214")).Foreground(lipgloss.Color("255")).Render("TRIED")
	case models.LogTypeResult:
		return style.Background(lipgloss.Color("40")).Foreground(lipgloss.Color("255")).Render("RESULT")
	case models.LogTypeOrchestration:
		return style.Background(lipgloss.Color("39")).Foreground(lipgloss.Color("255")).Render("ORCHESTRATION")
	case models.LogTypeSecurity:
		return style.Background(lipgloss.Color("160")).Foreground(lipgloss.Color("255")).Render("SECURITY")
	default:
		return style.Background(lipgloss.Color("240")).Foreground(lipgloss.Color("255")).Render(strings.ToUpper(string(logType)))
	}
}

// handleActivityDetailAction handles actions from the activity detail modal
func (m Model) handleActivityDetailAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "open-issue":
		if m.ActivityDetailItem != nil && m.ActivityDetailItem.IssueID != "" {
			issueID := m.ActivityDetailItem.IssueID
			m.closeActivityDetailModal()
			return m.pushModal(issueID, PanelActivity)
		}
	case "close", "cancel":
		m.closeActivityDetailModal()
	}
	return m, nil
}
