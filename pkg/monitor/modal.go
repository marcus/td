package monitor

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/marcus/td/internal/models"
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
	return func() tea.Msg {
		return MarkdownRenderedMsg{
			IssueID:      issueID,
			DescRender:   preRenderMarkdown(desc, width),
			AcceptRender: preRenderMarkdown(accept, width),
		}
	}
}

// preRenderMarkdown renders markdown once (expensive operation)
func preRenderMarkdown(text string, width int) string {
	if text == "" {
		return ""
	}

	// Use dark style directly (avoid expensive auto-detection)
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(width),
	)
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

	return m, m.fetchStats()
}

// closeStatsModal closes the stats modal and clears transient state
func (m *Model) closeStatsModal() {
	m.StatsOpen = false
	m.StatsScroll = 0
	m.StatsLoading = false
	m.StatsError = nil
	m.StatsData = nil
}

// openHandoffsModal opens the handoffs modal and fetches data
func (m Model) openHandoffsModal() (tea.Model, tea.Cmd) {
	m.HandoffsOpen = true
	m.HandoffsCursor = 0
	m.HandoffsScroll = 0
	m.HandoffsLoading = true
	m.HandoffsError = nil
	m.HandoffsData = nil

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
