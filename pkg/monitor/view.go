package monitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/marcus/td/internal/models"
)

// renderView renders the complete TUI view
func (m Model) renderView() string {
	if m.Width == 0 || m.Height == 0 {
		return "Loading..."
	}

	// Handle small terminal sizes gracefully
	if m.Width < MinWidth || m.Height < MinHeight {
		return m.renderCompact()
	}

	// Show error if database issue
	if m.Err != nil {
		return m.renderError()
	}

	if m.HelpOpen {
		base := m.renderBaseView()
		helpModal := m.renderHelp()
		return OverlayModal(base, helpModal, m.Width, m.Height)
	}

	// Render TDQ help modal if open (using declarative modal)
	if m.ShowTDQHelp && m.TDQHelpModal != nil && m.TDQHelpMouseHandler != nil {
		base := m.renderBaseView()
		tdqHelpContent := m.TDQHelpModal.Render(m.Width, m.Height, m.TDQHelpMouseHandler)
		return OverlayModal(base, tdqHelpContent, m.Width, m.Height)
	}

	// Render getting started modal if open (using declarative modal)
	if m.GettingStartedOpen && m.GettingStartedModal != nil && m.GettingStartedMouseHandler != nil {
		base := m.renderBaseView()
		gettingStartedContent := m.GettingStartedModal.Render(m.Width, m.Height, m.GettingStartedMouseHandler)
		return OverlayModal(base, gettingStartedContent, m.Width, m.Height)
	}

	// Render sync prompt modal if open (using declarative modal)
	if m.SyncPromptOpen && m.SyncPromptModal != nil {
		base := m.renderBaseView()
		syncPromptContent := m.SyncPromptModal.Render(m.Width, m.Height, m.SyncPromptMouse)
		return OverlayModal(base, syncPromptContent, m.Width, m.Height)
	}

	// Render base view (panels + footer)
	base := m.renderBaseView()

	// Overlay form modal if open
	if m.FormOpen && m.FormState != nil {
		form := m.renderFormModal()
		return OverlayModal(base, form, m.Width, m.Height)
	}

	// Overlay delete confirmation dialog if open
	if m.ConfirmOpen {
		confirm := m.renderDeleteConfirmation()
		return OverlayModal(base, confirm, m.Width, m.Height)
	}

	// Overlay close confirmation dialog if open (declarative modal)
	if m.CloseConfirmOpen && m.CloseConfirmModal != nil && m.CloseConfirmMouseHandler != nil {
		confirm := m.CloseConfirmModal.Render(m.Width, m.Height, m.CloseConfirmMouseHandler)
		return OverlayModal(base, confirm, m.Width, m.Height)
	}

	// Overlay stats modal if open
	if m.StatsOpen {
		stats := m.renderStatsModal()
		return OverlayModal(base, stats, m.Width, m.Height)
	}

	// Overlay handoffs modal if open
	if m.HandoffsOpen {
		handoffs := m.renderHandoffsModal()
		return OverlayModal(base, handoffs, m.Width, m.Height)
	}

	// Overlay board editor if open (on top of board picker)
	if m.BoardEditorOpen && m.BoardEditorModal != nil && m.BoardEditorMouseHandler != nil {
		boardEditor := m.BoardEditorModal.Render(m.Width, m.Height, m.BoardEditorMouseHandler)
		return OverlayModal(base, boardEditor, m.Width, m.Height)
	}

	// Overlay board picker if open
	if m.BoardPickerOpen {
		picker := m.renderBoardPicker()
		return OverlayModal(base, picker, m.Width, m.Height)
	}

	// Overlay modal if open
	if m.ModalOpen() {
		modal := m.renderModal()
		return OverlayModal(base, modal, m.Width, m.Height)
	}

	return base
}

// renderBaseView renders the panels and footer without any modal overlay.
// This is the background content used for dimmed modal overlays.
func (m Model) renderBaseView() string {
	// Render search bar if active or has query
	searchBar := m.renderSearchBar()
	searchBarHeight := 0
	if searchBar != "" {
		searchBarHeight = 2 // Content + border
	}

	// Calculate panel heights (3 panels + footer + optional search bar)
	footerHeight := 3
	if m.Embedded {
		footerHeight = 0
	}
	availableHeight := m.Height - footerHeight - searchBarHeight

	// Calculate individual panel heights from ratios
	panelHeights := [3]int{
		int(float64(availableHeight) * m.PaneHeights[0]),
		int(float64(availableHeight) * m.PaneHeights[1]),
		int(float64(availableHeight) * m.PaneHeights[2]),
	}
	// Adjust last panel to absorb rounding errors
	panelHeights[2] = availableHeight - panelHeights[0] - panelHeights[1]

	// Render each panel with its specific height
	currentWork := m.renderCurrentWorkPanel(panelHeights[0])
	activity := m.renderActivityPanel(panelHeights[2])
	taskList := m.renderTaskListPanel(panelHeights[1])

	// Stack panels vertically (Current Work → Task List → Activity)
	panels := lipgloss.JoinVertical(lipgloss.Left,
		currentWork,
		taskList,
		activity,
	)

	// Add search bar if present
	var content string
	if searchBar != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, searchBar, panels)
	} else {
		content = panels
	}

	// Add footer (unless embedded in sidecar)
	if m.Embedded {
		return content
	}
	footer := m.renderFooter()
	return lipgloss.JoinVertical(lipgloss.Left, content, footer)
}

// renderCompact renders a minimal view for small terminals
func (m Model) renderCompact() string {
	var s strings.Builder

	s.WriteString("td monitor (resize for full view)\n\n")

	// Show just focused issue and counts
	if m.FocusedIssue != nil {
		s.WriteString(fmt.Sprintf("Focus: %s\n", m.FocusedIssue.ID))
	}

	s.WriteString(fmt.Sprintf("In Progress: %d\n", len(m.InProgress)))
	s.WriteString(fmt.Sprintf("Ready: %d | Review: %d | Rework: %d | Blocked: %d\n",
		len(m.TaskList.Ready),
		len(m.TaskList.Reviewable),
		len(m.TaskList.NeedsRework),
		len(m.TaskList.Blocked)))

	s.WriteString("\nq:quit r:refresh ?:help")

	return s.String()
}

// renderError renders an error message
func (m Model) renderError() string {
	return fmt.Sprintf("Error: %v\n\nPress r to retry, q to quit", m.Err)
}

// renderCurrentWorkPanel renders the current work panel (Panel 1)
func (m Model) renderCurrentWorkPanel(height int) string {
	var content strings.Builder

	totalRows := len(m.CurrentWorkRows)
	if totalRows == 0 {
		content.WriteString(subtleStyle.Render("No current work"))
		content.WriteString("\n")
		return m.wrapPanel("CURRENT WORK", content.String(), height, PanelCurrentWork)
	}

	cursor := m.Cursor[PanelCurrentWork]
	isActive := m.ActivePanel == PanelCurrentWork
	offset := m.ScrollOffset[PanelCurrentWork]
	maxLines := height - 3 // Account for title + border

	// Determine scroll indicators needed BEFORE clamping
	needsScroll := totalRows > maxLines
	showUpIndicator := needsScroll && offset > 0

	// Calculate effective maxLines with indicators
	effectiveMaxLines := maxLines
	if showUpIndicator {
		effectiveMaxLines--
	}
	// Reserve space for down indicator if content exceeds visible area
	if needsScroll && offset+effectiveMaxLines < totalRows {
		effectiveMaxLines--
	}

	// Clamp offset using effective maxLines (accounts for indicators)
	if offset > totalRows-effectiveMaxLines && totalRows > effectiveMaxLines {
		offset = totalRows - effectiveMaxLines
	}
	if offset < 0 {
		offset = 0
	}

	// Recalculate indicators after clamping
	showUpIndicator = needsScroll && offset > 0
	effectiveMaxLines = maxLines
	if showUpIndicator {
		effectiveMaxLines--
	}
	hasMoreBelow := needsScroll && offset+effectiveMaxLines < totalRows
	if hasMoreBelow {
		effectiveMaxLines--
	}

	// Build title with position if scrollable
	panelTitle := "CURRENT WORK"
	if needsScroll {
		endPos := offset + effectiveMaxLines
		if endPos > totalRows {
			endPos = totalRows
		}
		panelTitle = fmt.Sprintf("CURRENT WORK (%d-%d of %d)", offset+1, endPos, totalRows)
	}

	// Show up indicator if scrolled down
	if showUpIndicator {
		content.WriteString(subtleStyle.Render("  ▲ more above"))
		content.WriteString("\n")
	}

	rowIdx := 0
	linesWritten := 0

	// Focused issue (first row if present)
	if m.FocusedIssue != nil {
		if rowIdx >= offset && linesWritten < effectiveMaxLines {
			line := titleStyle.Render("FOCUSED: ") + m.formatIssueCompact(m.FocusedIssue)
			if isActive && cursor == rowIdx {
				line = highlightRow(line, m.Width-4)
			}
			content.WriteString(line)
			content.WriteString("\n")
			linesWritten++
		}
		rowIdx++
	}

	// In-progress issues (skip focused if it's duplicated)
	if len(m.InProgress) > 0 && linesWritten < effectiveMaxLines {
		// Only show header if in visible range
		if rowIdx >= offset || (m.FocusedIssue != nil && offset == 0) {
			if linesWritten < effectiveMaxLines {
				content.WriteString("\n")
				content.WriteString(sectionHeader.Render("IN PROGRESS:"))
				content.WriteString("\n")
				linesWritten += 2
			}
		}

		for _, issue := range m.InProgress {
			// Skip focused issue if it's also in progress
			if m.FocusedIssue != nil && issue.ID == m.FocusedIssue.ID {
				continue
			}
			if rowIdx >= offset && linesWritten < effectiveMaxLines {
				line := "  " + m.formatIssueCompact(&issue)
				if isActive && cursor == rowIdx {
					line = highlightRow(line, m.Width-4)
				}
				content.WriteString(line)
				content.WriteString("\n")
				linesWritten++
			}
			rowIdx++
		}
	}

	// Show down indicator if more content below
	if hasMoreBelow {
		content.WriteString(subtleStyle.Render("  ▼ more below"))
		content.WriteString("\n")
	}

	return m.wrapPanel(panelTitle, content.String(), height, PanelCurrentWork)
}

// activityTableStyleFunc returns a StyleFunc for the activity table
// that highlights the selected row when the panel is active.
// visibleCursor is the cursor position relative to visible rows (cursor - offset).
func (m Model) activityTableStyleFunc(visibleCursor int, isActive bool, colWidths []int) table.StyleFunc {
	return func(row, col int) lipgloss.Style {
		style := lipgloss.NewStyle()

		// Header row (row == -1 in lipgloss/table)
		if row == table.HeaderRow {
			style = activityTableHeaderStyle
		}

		// Selected row highlight (only when panel is active)
		if isActive && row == visibleCursor && row != table.HeaderRow {
			style = activityTableSelectedStyle
		}

		if col >= 0 && col < len(colWidths) && colWidths[col] > 0 {
			style = style.Width(colWidths[col])
		}

		return style
	}
}

// formatActivityRow formats an activity item as table columns.
// Returns: [Time, Session, Type, Issue, Message+Title]
// Cells are pre-styled with ANSI codes for colors.
// Note: Add trailing space to cells to ensure proper column separation
// when ANSI codes affect width calculation.
func (m Model) formatActivityRow(item ActivityItem, messageWidth int) []string {
	// Pre-styled cells using existing style functions
	timestamp := timestampStyle.Render(item.Timestamp.Format("15:04"))
	session := subtleStyle.Render(truncateSession(item.SessionID))
	badge := formatActivityBadge(item.Type) // existing function with styling
	issueID := ""
	if item.IssueID != "" {
		issueID = titleStyle.Render(truncateString(item.IssueID, activityColIssueWidth))
	}

	// Build message with optional title suffix (use bullet instead of pipe)
	message := item.Message
	if item.IssueTitle != "" {
		availableForTitle := messageWidth - len(message) - 3 // " • "
		if availableForTitle > 10 {
			message = message + " " + subtleStyle.Render("• "+truncateString(item.IssueTitle, availableForTitle))
		} else {
			// Truncate message to fit some title
			msgWidth := messageWidth - 13 // " • " + 10 char title
			if msgWidth > 0 {
				message = truncateString(message, msgWidth) + " " + subtleStyle.Render("• "+truncateString(item.IssueTitle, 10))
			}
		}
	}
	message = truncateString(message, messageWidth)

	return []string{timestamp, session, badge, issueID, message}
}

// renderActivityPanel renders the activity log panel (Panel 2) using lipgloss/table
func (m Model) renderActivityPanel(height int) string {
	totalRows := len(m.Activity)
	if totalRows == 0 {
		content := subtleStyle.Render("No recent activity")
		return m.wrapPanel("ACTIVITY LOG", content, height, PanelActivity)
	}

	cursor := m.Cursor[PanelActivity]
	isActive := m.ActivePanel == PanelActivity
	offset := m.ScrollOffset[PanelActivity]

	layout := activityTableMetrics(height)
	dataRowsVisible := layout.dataRowsVisible

	// Clamp offset
	maxOffset := totalRows - dataRowsVisible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}

	endIdx := offset + dataRowsVisible
	if endIdx > totalRows {
		endIdx = totalRows
	}
	hasMoreBelow := endIdx < totalRows

	// Build table title with position indicator
	panelTitle := "ACTIVITY LOG"
	if totalRows > dataRowsVisible {
		endPos := offset + dataRowsVisible
		if endPos > totalRows {
			endPos = totalRows
		}
		panelTitle = fmt.Sprintf("ACTIVITY LOG (%d-%d of %d)", offset+1, endPos, totalRows)
	}

	// Calculate message column width
	// Fixed columns: base widths + 1 space each for separation
	contentWidth := m.Width - 4 // panel border + padding
	timeWidth := activityColTimeWidth + 1
	sessionWidth := activityColSessionWidth + 1
	typeWidth := activityColTypeWidth + 1
	issueWidth := activityColIssueWidth + 1
	fixedWidth := timeWidth + sessionWidth + typeWidth + issueWidth
	messageWidth := contentWidth - fixedWidth
	if messageWidth < 15 {
		messageWidth = 15
	}
	colWidths := []int{
		timeWidth,
		sessionWidth,
		typeWidth,
		issueWidth,
		0, // message column expands to fill
	}

	// Create table with headers
	t := table.New().
		Headers("Time", "Sess", "Type", "Issue", "Message").
		Width(contentWidth).
		StyleFunc(m.activityTableStyleFunc(cursor-offset, isActive, colWidths)).
		Border(lipgloss.HiddenBorder()).
		BorderHeader(false).
		BorderRow(false).
		BorderColumn(false).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false)

	startIdx := offset
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx = offset + dataRowsVisible
	if endIdx > totalRows {
		endIdx = totalRows
	}
	visibleRows := endIdx - startIdx
	if visibleRows < 0 {
		visibleRows = 0
	}

	rows := make([][]string, visibleRows)
	for i := 0; i < visibleRows; i++ {
		rows[i] = m.formatActivityRow(m.Activity[startIdx+i], messageWidth)
	}
	t.Rows(rows...)

	// Build content with table and scroll indicator
	var content strings.Builder
	content.WriteString(t.Render())
	if hasMoreBelow {
		moreCount := totalRows - endIdx
		content.WriteString("\n")
		content.WriteString(subtleStyle.Render(fmt.Sprintf("  ↓ %d more below", moreCount)))
	}

	return m.wrapPanel(panelTitle, content.String(), height, PanelActivity)
}

// renderTaskListPanel renders the task list panel (Panel 3)
// Uses flattened TaskListRows for selection support
func (m Model) renderTaskListPanel(height int) string {
	// If in board mode, render board view in this panel
	if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
		if m.BoardMode.ViewMode == BoardViewSwimlanes {
			return m.renderBoardSwimlanesView(height)
		}
		return m.renderTaskListBoardView(height)
	}

	var content strings.Builder

	totalRows := len(m.TaskListRows)

	// Build sort indicator
	sortIndicator := ""
	switch m.SortMode {
	case SortByCreatedDesc:
		sortIndicator = " [by:created]"
	case SortByUpdatedDesc:
		sortIndicator = " [by:updated]"
	}

	if totalRows == 0 {
		panelTitle := "TASK LIST" + sortIndicator
		if m.SearchQuery != "" || m.IncludeClosed {
			panelTitle = "TASK LIST" + sortIndicator + " (no matches)"
		}
		content.WriteString(subtleStyle.Render("No tasks available"))
		return m.wrapPanel(panelTitle, content.String(), height, PanelTaskList)
	}

	cursor := m.Cursor[PanelTaskList]
	isActive := m.ActivePanel == PanelTaskList
	offset := m.ScrollOffset[PanelTaskList]
	maxLines := height - 3 // Account for title + border

	// Determine scroll indicators needed BEFORE clamping
	needsScroll := totalRows > maxLines
	showUpIndicator := needsScroll && offset > 0

	// Calculate effective maxLines with indicators
	effectiveMaxLines := maxLines
	if showUpIndicator {
		effectiveMaxLines--
	}
	// Reserve space for down indicator if content exceeds visible area
	if needsScroll && offset+effectiveMaxLines < totalRows {
		effectiveMaxLines--
	}

	// Clamp offset using effective maxLines (accounts for indicators)
	if offset > totalRows-effectiveMaxLines && totalRows > effectiveMaxLines {
		offset = totalRows - effectiveMaxLines
	}
	if offset < 0 {
		offset = 0
	}

	// Recalculate indicators after clamping
	showUpIndicator = needsScroll && offset > 0
	effectiveMaxLines = maxLines
	if showUpIndicator {
		effectiveMaxLines--
	}
	hasMoreBelow := needsScroll && offset+effectiveMaxLines < totalRows
	if hasMoreBelow {
		effectiveMaxLines--
	}

	// Build title with position if scrollable
	panelTitle := "TASK LIST" + sortIndicator
	if needsScroll {
		endPos := offset + effectiveMaxLines
		if endPos > totalRows {
			endPos = totalRows
		}
		panelTitle = fmt.Sprintf("TASK LIST%s (%d-%d of %d)", sortIndicator, offset+1, endPos, totalRows)
	} else if m.SearchQuery != "" || m.IncludeClosed {
		panelTitle = fmt.Sprintf("TASK LIST%s (%d results)", sortIndicator, totalRows)
	}

	// Show up indicator if scrolled down
	if showUpIndicator {
		content.WriteString(subtleStyle.Render("  ▲ more above"))
		content.WriteString("\n")
	}

	// Track current category for section headers
	var currentCategory TaskListCategory
	linesWritten := 0

	for i, row := range m.TaskListRows {
		if linesWritten >= effectiveMaxLines {
			break
		}

		// Skip rows before offset
		if i < offset {
			currentCategory = row.Category // Track category even when skipping
			continue
		}

		// Add category header when category changes
		if row.Category != currentCategory {
			if linesWritten > 0 && linesWritten < effectiveMaxLines {
				content.WriteString("\n")
				linesWritten++
				if linesWritten >= effectiveMaxLines {
					break
				}
			}
			header := m.formatCategoryHeader(row.Category)
			content.WriteString(header)
			content.WriteString("\n")
			linesWritten++
			currentCategory = row.Category
			if linesWritten >= effectiveMaxLines {
				break
			}
		}

		// Format row with category tag and selection highlight
		tag := m.formatCategoryTag(row.Category)
		issueStr := m.formatIssueShort(&row.Issue)
		line := fmt.Sprintf("%s %s", tag, issueStr)

		if isActive && cursor == i {
			line = highlightRow(line, m.Width-4)
		}

		content.WriteString(line)
		content.WriteString("\n")
		linesWritten++
	}

	// Show down indicator if more content below
	if hasMoreBelow {
		content.WriteString(subtleStyle.Render("  ▼ more below"))
		content.WriteString("\n")
	}

	return m.wrapPanel(panelTitle, content.String(), height, PanelTaskList)
}

// renderTaskListBoardView renders board issues in the Task List panel
func (m Model) renderTaskListBoardView(height int) string {
	var content strings.Builder
	contentWidth := m.Width - 4 // Account for border and padding

	totalRows := len(m.BoardMode.Issues)

	// Empty state
	if totalRows == 0 {
		boardName := "Board"
		if m.BoardMode.Board != nil {
			boardName = m.BoardMode.Board.Name
		}
		panelTitle := fmt.Sprintf("BOARD: %s [backlog] (0)", boardName)
		content.WriteString(subtleStyle.Render("No issues match the board query"))
		content.WriteString("\n\n")
		content.WriteString(subtleStyle.Render("Try adjusting the status filter with 'c' or 'F'"))
		return m.wrapPanel(panelTitle, content.String(), height, PanelTaskList)
	}

	cursor := m.BoardMode.Cursor
	isActive := m.ActivePanel == PanelTaskList
	offset := m.BoardMode.ScrollOffset
	maxLines := height - 3 // Account for title + border

	// Determine scroll indicators needed BEFORE clamping
	needsScroll := totalRows > maxLines
	showUpIndicator := needsScroll && offset > 0

	// Calculate effective maxLines with indicators
	effectiveMaxLines := maxLines
	if showUpIndicator {
		effectiveMaxLines--
	}
	// Reserve space for down indicator if content exceeds visible area
	if needsScroll && offset+effectiveMaxLines < totalRows {
		effectiveMaxLines--
	}

	// Clamp offset
	if offset > totalRows-effectiveMaxLines && totalRows > effectiveMaxLines {
		offset = totalRows - effectiveMaxLines
	}
	if offset < 0 {
		offset = 0
	}

	// Recalculate indicators after clamping
	showUpIndicator = needsScroll && offset > 0
	effectiveMaxLines = maxLines
	if showUpIndicator {
		effectiveMaxLines--
	}
	hasMoreBelow := needsScroll && offset+effectiveMaxLines < totalRows
	if hasMoreBelow {
		effectiveMaxLines--
	}

	// Build title with board name, view mode indicator, and position info
	boardName := "Board"
	if m.BoardMode.Board != nil {
		boardName = m.BoardMode.Board.Name
	}
	var panelTitle string
	if needsScroll {
		endPos := offset + effectiveMaxLines
		if endPos > totalRows {
			endPos = totalRows
		}
		panelTitle = fmt.Sprintf("BOARD: %s [backlog] (%d-%d of %d)", boardName, offset+1, endPos, totalRows)
	} else {
		panelTitle = fmt.Sprintf("BOARD: %s [backlog] (%d)", boardName, totalRows)
	}

	// Show up indicator if scrolled down
	if showUpIndicator {
		content.WriteString(subtleStyle.Render(fmt.Sprintf("  ↑ %d more above", offset)))
		content.WriteString("\n")
	}

	// Render visible issues
	endIdx := offset + effectiveMaxLines
	if endIdx > totalRows {
		endIdx = totalRows
	}

	for i := offset; i < endIdx; i++ {
		biv := m.BoardMode.Issues[i]
		issue := biv.Issue

		// Position indicator (muted color like timestamps)
		var posIndicator string
		if biv.HasPosition {
			posIndicator = timestampStyle.Render(fmt.Sprintf("%3d", biv.Position)) + " "
		} else {
			posIndicator = timestampStyle.Render("  •") + " "
		}

		// Status tag, type, ID, priority (matching swimlanes format)
		tag := m.formatCategoryTag(TaskListCategory(biv.Category))
		typeStr := formatTypeIcon(issue.Type)
		idStr := subtleStyle.Render(issue.ID)
		priStr := formatPriority(issue.Priority)

		// Title (truncated)
		title := issue.Title
		maxTitleLen := contentWidth - 38 // Leave room for indicators + ID
		if maxTitleLen < 10 {
			maxTitleLen = 10
		}
		if len(title) > maxTitleLen {
			title = title[:maxTitleLen-3] + "..."
		}

		// Build line: position + tag + type + id + priority + title
		line := fmt.Sprintf("%s%s %s %s %s %s",
			posIndicator,
			tag,
			typeStr,
			idStr,
			priStr,
			title,
		)

		// Highlight if cursor is on this row
		if isActive && i == cursor {
			line = highlightRow(line, m.Width-4)
		}

		content.WriteString(line)
		content.WriteString("\n")
	}

	// Show down indicator if more items below
	if hasMoreBelow {
		content.WriteString(subtleStyle.Render(fmt.Sprintf("  ↓ %d more below", totalRows-endIdx)))
		content.WriteString("\n")
	}

	return m.wrapPanel(panelTitle, content.String(), height, PanelTaskList)
}

// renderBoardSwimlanesView renders board issues grouped by status category (swimlanes view)
func (m Model) renderBoardSwimlanesView(height int) string {
	var content strings.Builder

	totalRows := len(m.BoardMode.SwimlaneRows)

	// Build sort indicator
	sortIndicator := ""
	switch m.SortMode {
	case SortByCreatedDesc:
		sortIndicator = " [by:created]"
	case SortByUpdatedDesc:
		sortIndicator = " [by:updated]"
	}

	// Empty state
	if totalRows == 0 {
		boardName := "Board"
		if m.BoardMode.Board != nil {
			boardName = m.BoardMode.Board.Name
		}
		panelTitle := fmt.Sprintf("BOARD: %s [swimlanes]%s (0)", boardName, sortIndicator)
		content.WriteString(subtleStyle.Render("No issues match the board query"))
		content.WriteString("\n\n")
		content.WriteString(subtleStyle.Render("Try adjusting the status filter with 'c' or 'F'"))
		return m.wrapPanel(panelTitle, content.String(), height, PanelTaskList)
	}

	cursor := m.BoardMode.SwimlaneCursor
	isActive := m.ActivePanel == PanelTaskList
	offset := m.BoardMode.SwimlaneScroll
	maxLines := height - 3 // Account for title + border

	// Determine scroll indicators needed BEFORE clamping
	needsScroll := totalRows > maxLines
	showUpIndicator := needsScroll && offset > 0

	// Calculate effective maxLines with indicators
	effectiveMaxLines := maxLines
	if showUpIndicator {
		effectiveMaxLines--
	}
	// Reserve space for down indicator if content exceeds visible area
	if needsScroll && offset+effectiveMaxLines < totalRows {
		effectiveMaxLines--
	}

	// Clamp offset using effective maxLines (accounts for indicators)
	if offset > totalRows-effectiveMaxLines && totalRows > effectiveMaxLines {
		offset = totalRows - effectiveMaxLines
	}
	if offset < 0 {
		offset = 0
	}

	// Recalculate indicators after clamping
	showUpIndicator = needsScroll && offset > 0
	effectiveMaxLines = maxLines
	if showUpIndicator {
		effectiveMaxLines--
	}
	hasMoreBelow := needsScroll && offset+effectiveMaxLines < totalRows
	if hasMoreBelow {
		effectiveMaxLines--
	}

	// Build title with board name, view mode indicator, and position info
	boardName := "Board"
	if m.BoardMode.Board != nil {
		boardName = m.BoardMode.Board.Name
	}
	var panelTitle string
	if needsScroll {
		endPos := offset + effectiveMaxLines
		if endPos > totalRows {
			endPos = totalRows
		}
		panelTitle = fmt.Sprintf("BOARD: %s [swimlanes]%s (%d-%d of %d)", boardName, sortIndicator, offset+1, endPos, totalRows)
	} else {
		panelTitle = fmt.Sprintf("BOARD: %s [swimlanes]%s (%d)", boardName, sortIndicator, totalRows)
	}

	// Show up indicator if scrolled down
	if showUpIndicator {
		content.WriteString(subtleStyle.Render("  ▲ more above"))
		content.WriteString("\n")
	}

	// Track current category for section headers
	var currentCategory TaskListCategory
	linesWritten := 0

	for i, row := range m.BoardMode.SwimlaneRows {
		if linesWritten >= effectiveMaxLines {
			break
		}

		// Skip rows before offset
		if i < offset {
			currentCategory = row.Category // Track category even when skipping
			continue
		}

		// Add category header when category changes
		if row.Category != currentCategory {
			if linesWritten > 0 && linesWritten < effectiveMaxLines {
				content.WriteString("\n")
				linesWritten++
				if linesWritten >= effectiveMaxLines {
					break
				}
			}
			header := m.formatSwimlaneCategoryHeader(row.Category)
			content.WriteString(header)
			content.WriteString("\n")
			linesWritten++
			currentCategory = row.Category
			if linesWritten >= effectiveMaxLines {
				break
			}
		}

		// Format row with category tag and selection highlight
		tag := m.formatCategoryTag(row.Category)
		issueStr := m.formatIssueShort(&row.Issue)
		line := fmt.Sprintf("%s %s", tag, issueStr)

		if isActive && cursor == i {
			line = highlightRow(line, m.Width-4)
		}

		content.WriteString(line)
		content.WriteString("\n")
		linesWritten++
	}

	// Show down indicator if more content below
	if hasMoreBelow {
		content.WriteString(subtleStyle.Render("  ▼ more below"))
		content.WriteString("\n")
	}

	return m.wrapPanel(panelTitle, content.String(), height, PanelTaskList)
}

// formatSwimlaneCategoryHeader returns the section header for a swimlane category
func (m Model) formatSwimlaneCategoryHeader(cat TaskListCategory) string {
	count := 0
	switch cat {
	case CategoryReviewable:
		count = len(m.BoardMode.SwimlaneData.Reviewable)
		return reviewAlertStyle.Render("★ REVIEWABLE") + fmt.Sprintf(" (%d):", count)
	case CategoryNeedsRework:
		count = len(m.BoardMode.SwimlaneData.NeedsRework)
		return reworkColor.Render("⚠ NEEDS REWORK") + fmt.Sprintf(" (%d):", count)
	case CategoryReady:
		count = len(m.BoardMode.SwimlaneData.Ready)
		return readyHeaderStyle.Render("READY") + fmt.Sprintf(" (%d):", count)
	case CategoryBlocked:
		count = len(m.BoardMode.SwimlaneData.Blocked)
		return blockedHeaderStyle.Render("BLOCKED") + fmt.Sprintf(" (%d):", count)
	case CategoryClosed:
		count = len(m.BoardMode.SwimlaneData.Closed)
		return subtleStyle.Render("CLOSED") + fmt.Sprintf(" (%d):", count)
	}
	return ""
}

// formatCategoryHeader returns the section header for a category
func (m Model) formatCategoryHeader(cat TaskListCategory) string {
	count := 0
	switch cat {
	case CategoryReviewable:
		count = len(m.TaskList.Reviewable)
		return reviewAlertStyle.Render("★ REVIEWABLE") + fmt.Sprintf(" (%d):", count)
	case CategoryNeedsRework:
		count = len(m.TaskList.NeedsRework)
		return reworkColor.Render("⚠ NEEDS REWORK") + fmt.Sprintf(" (%d):", count)
	case CategoryReady:
		count = len(m.TaskList.Ready)
		return readyHeaderStyle.Render("READY") + fmt.Sprintf(" (%d):", count)
	case CategoryBlocked:
		count = len(m.TaskList.Blocked)
		return blockedHeaderStyle.Render("BLOCKED") + fmt.Sprintf(" (%d):", count)
	case CategoryClosed:
		count = len(m.TaskList.Closed)
		return subtleStyle.Render("CLOSED") + fmt.Sprintf(" (%d):", count)
	}
	return ""
}

// formatCategoryTag returns a short tag for inline display
func (m Model) formatCategoryTag(cat TaskListCategory) string {
	switch cat {
	case CategoryReviewable:
		return reviewColor.Render("[REV]")
	case CategoryNeedsRework:
		return reworkColor.Render("[RWK]")
	case CategoryReady:
		return readyColor.Render("[RDY]")
	case CategoryBlocked:
		return blockedColor.Render("[BLK]")
	case CategoryClosed:
		return subtleStyle.Render("[CLS]")
	}
	return ""
}

// renderModal renders the centered issue details modal
func (m Model) renderModal() string {
	modal := m.CurrentModal()
	if modal == nil {
		return ""
	}

	// Calculate modal dimensions (80% of terminal, capped)
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
	if modalHeight < 15 {
		modalHeight = 15
	}

	contentWidth := modalWidth - 4 // Account for border and padding

	var content strings.Builder

	// Loading state
	if modal.Loading {
		content.WriteString(subtleStyle.Render("Loading..."))
		return m.wrapModalWithDepth(content.String(), modalWidth, modalHeight)
	}

	// Error state
	if modal.Error != nil {
		content.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", modal.Error)))
		content.WriteString("\n\n")
		content.WriteString(subtleStyle.Render("Press esc to close"))
		return m.wrapModalWithDepth(content.String(), modalWidth, modalHeight)
	}

	// No issue loaded
	if modal.Issue == nil {
		content.WriteString(subtleStyle.Render("No issue data"))
		return m.wrapModalWithDepth(content.String(), modalWidth, modalHeight)
	}

	issue := modal.Issue

	// Build all content lines for scrolling
	var lines []string

	// Header: ID and Title
	lines = append(lines, titleStyle.Render(issue.ID)+" "+issue.Title)
	lines = append(lines, "")

	// Parent epic (if exists) - selectable row
	if modal.ParentEpic != nil {
		epicText := "Epic: " + modal.ParentEpic.ID + " " +
			truncateString(modal.ParentEpic.Title, contentWidth-20)
		if modal.ParentEpicFocused {
			lines = append(lines, parentEpicFocusedStyle.Render("> "+epicText)+" [Enter:open]")
		} else {
			lines = append(lines, parentEpicStyle.Render("  "+epicText))
		}
		lines = append(lines, "")
	}

	// Status line: status, type, priority, points, created date
	statusLine := fmt.Sprintf("%s  %s  %s",
		formatStatus(issue.Status),
		formatTypeIcon(issue.Type),
		formatPriority(issue.Priority))
	if issue.Points > 0 {
		statusLine += fmt.Sprintf("  %dpts", issue.Points)
	}
	// Add created timestamp in subtle style
	statusLine += subtleStyle.Render(fmt.Sprintf("  created %s", issue.CreatedAt.Format("2006-01-02 15:04")))
	// Add closed timestamp if closed
	if issue.ClosedAt != nil {
		statusLine += subtleStyle.Render(fmt.Sprintf("  closed %s", issue.ClosedAt.Format("2006-01-02 15:04")))
	}
	lines = append(lines, statusLine)

	// Labels
	if len(issue.Labels) > 0 {
		labelStr := subtleStyle.Render("Labels: ") + strings.Join(issue.Labels, ", ")
		lines = append(lines, labelStr)
	}

	// Implementer/Reviewer
	if issue.ImplementerSession != "" {
		lines = append(lines, subtleStyle.Render("Impl: ")+truncateSession(issue.ImplementerSession))
	}
	if issue.ReviewerSession != "" {
		lines = append(lines, subtleStyle.Render("Review: ")+truncateSession(issue.ReviewerSession))
	}

	lines = append(lines, "")

	// Epic tasks section (if this is an epic with children)
	if issue.Type == models.TypeEpic && len(modal.EpicTasks) > 0 {
		header := fmt.Sprintf("TASKS IN EPIC (%d)", len(modal.EpicTasks))
		if modal.TaskSectionFocused {
			header = epicTasksFocusedStyle.Render(header + " [j/k:nav Enter:open Tab:scroll]")
		} else {
			header = sectionHeader.Render(header + " [Tab:focus]")
		}
		lines = append(lines, header)

		for i, task := range modal.EpicTasks {
			prefix := "  "
			taskLine := fmt.Sprintf("%s %s %s %s",
				formatTypeIcon(task.Type),
				subtleStyle.Render(task.ID),
				formatStatus(task.Status),
				truncateString(task.Title, contentWidth-29))

			if modal.TaskSectionFocused && i == modal.EpicTasksCursor {
				taskLine = epicTaskSelectedStyle.Render("> " + formatTypeIcon(task.Type) + " " + task.ID + " " + formatStatus(task.Status) + " " + truncateString(task.Title, contentWidth-29))
			} else {
				taskLine = prefix + taskLine
			}
			lines = append(lines, taskLine)
		}
		lines = append(lines, "")
	}

	// Description (use pre-rendered markdown from model)
	if issue.Description != "" {
		lines = append(lines, sectionHeader.Render("DESCRIPTION"))
		rendered := modal.DescRender
		if rendered == "" {
			rendered = issue.Description // fallback if not rendered yet
		}
		for _, line := range strings.Split(rendered, "\n") {
			lines = append(lines, line)
		}
		lines = append(lines, "")
	}

	// Acceptance criteria (use pre-rendered markdown from model)
	if issue.Acceptance != "" {
		lines = append(lines, sectionHeader.Render("ACCEPTANCE CRITERIA"))
		rendered := modal.AcceptRender
		if rendered == "" {
			rendered = issue.Acceptance // fallback if not rendered yet
		}
		for _, line := range strings.Split(rendered, "\n") {
			lines = append(lines, line)
		}
		lines = append(lines, "")
	}

	// Blocked by (dependencies) - split into active blockers vs resolved
	if len(modal.BlockedBy) > 0 {
		var activeBlockers, resolvedDeps []models.Issue
		for _, dep := range modal.BlockedBy {
			if dep.Status == models.StatusClosed {
				resolvedDeps = append(resolvedDeps, dep)
			} else {
				activeBlockers = append(activeBlockers, dep)
			}
		}

		// Show active blockers prominently
		if len(activeBlockers) > 0 {
			modal.BlockedByStartLine = len(lines) // Track section start for mouse clicks
			header := fmt.Sprintf("⚠ BLOCKED BY (%d)", len(activeBlockers))
			if modal.BlockedBySectionFocused {
				header = blockedBySectionFocusedStyle.Render(header + " [j/k:nav Enter:open Tab:next]")
			} else {
				header = blockedColor.Render(header + " [Tab:focus]")
			}
			lines = append(lines, header)

			for i, dep := range activeBlockers {
				depLine := fmt.Sprintf("%s %s %s %s",
					formatTypeIcon(dep.Type),
					titleStyle.Render(dep.ID),
					formatStatus(dep.Status),
					truncateString(dep.Title, contentWidth-24))
				if modal.BlockedBySectionFocused && i == modal.BlockedByCursor {
					depLine = blockedBySelectedStyle.Render("> " + depLine)
				} else {
					depLine = "  " + depLine
				}
				lines = append(lines, depLine)
			}
			modal.BlockedByEndLine = len(lines) // Track section end for mouse clicks
			lines = append(lines, "")
		}

		// Show resolved dependencies dimmed
		if len(resolvedDeps) > 0 {
			lines = append(lines, subtleStyle.Render(fmt.Sprintf("✓ RESOLVED DEPS (%d)", len(resolvedDeps))))
			for _, dep := range resolvedDeps {
				depLine := subtleStyle.Render(fmt.Sprintf("  %s %s",
					dep.ID,
					truncateString(dep.Title, contentWidth-15)))
				lines = append(lines, depLine)
			}
			lines = append(lines, "")
		}
	}

	// Blocks (dependents)
	if len(modal.Blocks) > 0 {
		modal.BlocksStartLine = len(lines) // Track section start for mouse clicks
		header := fmt.Sprintf("BLOCKS (%d)", len(modal.Blocks))
		if modal.BlocksSectionFocused {
			header = blocksSectionFocusedStyle.Render(header + " [j/k:nav Enter:open Tab:next]")
		} else {
			header = sectionHeader.Render(header + " [Tab:focus]")
		}
		lines = append(lines, header)

		for i, dep := range modal.Blocks {
			depLine := fmt.Sprintf("%s %s %s %s",
				formatTypeIcon(dep.Type),
				titleStyle.Render(dep.ID),
				formatStatus(dep.Status),
				truncateString(dep.Title, contentWidth-24))
			if modal.BlocksSectionFocused && i == modal.BlocksCursor {
				depLine = blocksSelectedStyle.Render("> " + depLine)
			} else {
				depLine = "  " + depLine
			}
			lines = append(lines, depLine)
		}
		modal.BlocksEndLine = len(lines) // Track section end for mouse clicks
		lines = append(lines, "")
	}

	// Latest handoff
	if modal.Handoff != nil {
		lines = append(lines, sectionHeader.Render("LATEST HANDOFF"))
		lines = append(lines, timestampStyle.Render(modal.Handoff.Timestamp.Format("2006-01-02 15:04"))+" "+
			subtleStyle.Render(truncateSession(modal.Handoff.SessionID)))
		if len(modal.Handoff.Done) > 0 {
			lines = append(lines, readyColor.Render("Done:"))
			for _, item := range modal.Handoff.Done {
				lines = append(lines, "  • "+item)
			}
		}
		if len(modal.Handoff.Remaining) > 0 {
			lines = append(lines, reviewColor.Render("Remaining:"))
			for _, item := range modal.Handoff.Remaining {
				lines = append(lines, "  • "+item)
			}
		}
		if len(modal.Handoff.Uncertain) > 0 {
			lines = append(lines, blockedColor.Render("Uncertain:"))
			for _, item := range modal.Handoff.Uncertain {
				lines = append(lines, "  • "+item)
			}
		}
		lines = append(lines, "")
	}

	// Recent logs
	if len(modal.Logs) > 0 {
		lines = append(lines, sectionHeader.Render(fmt.Sprintf("RECENT LOGS (%d)", len(modal.Logs))))
		for _, log := range modal.Logs {
			lines = append(lines, renderLogLines(log, contentWidth)...)
		}
	}

	// Comments
	if len(modal.Comments) > 0 {
		lines = append(lines, sectionHeader.Render(fmt.Sprintf("COMMENTS (%d)", len(modal.Comments))))
		for _, c := range modal.Comments {
			line := timestampStyle.Render(c.CreatedAt.Format("01-02 15:04")) + " " +
				subtleStyle.Render(truncateSession(c.SessionID)) + " " +
				truncateString(c.Text, contentWidth-25)
			lines = append(lines, line)
		}
	}

	// Apply scroll offset
	visibleHeight := modalHeight - 4 // Account for border and footer
	totalLines := len(lines)

	// Clamp scroll
	maxScroll := totalLines - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := modal.Scroll
	if scroll > maxScroll {
		scroll = maxScroll
	}

	// Get visible lines
	endIdx := scroll + visibleHeight
	if endIdx > totalLines {
		endIdx = totalLines
	}
	visibleLines := lines[scroll:endIdx]

	// Build content
	content.WriteString(strings.Join(visibleLines, "\n"))

	// Add scroll indicator if needed
	if totalLines > visibleHeight {
		content.WriteString("\n")
		scrollInfo := subtleStyle.Render(fmt.Sprintf("─ %d/%d ─", scroll+1, totalLines))
		content.WriteString(scrollInfo)
	}

	return m.wrapModalWithDepth(content.String(), modalWidth, modalHeight)
}

// wrapStatsModal wraps stats content in a modal box
func (m Model) wrapStatsModal(content string, width, height int) string {
	// Use custom renderer if provided (for embedded mode with custom theming)
	if m.ModalRenderer != nil {
		// Add vertical padding to match lipgloss Padding(1, 2) behavior.
		// Custom renderer only handles horizontal padding, so we add blank lines
		// for top/bottom padding manually.
		paddedContent := "\n" + content + "\n"
		// Add 2 to width/height: lipgloss Width/Height = content area, renderer expects outer with borders
		return m.ModalRenderer(paddedContent, width+2, height+2, ModalTypeStats, 1)
	}

	// Default lipgloss rendering
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(width).
		Height(height)

	return modalStyle.Render(content)
}

// renderStatsModal renders the stats modal using the declarative modal library
func (m Model) renderStatsModal() string {
	// Use declarative modal when available and data is ready
	if m.StatsModal != nil && !m.StatsLoading && m.StatsError == nil &&
		m.StatsData != nil && m.StatsData.Error == nil {
		return m.StatsModal.Render(m.Width, m.Height, m.StatsMouseHandler)
	}

	// Fallback to legacy rendering for loading/error states
	return m.renderStatsModalLegacy()
}

// renderStatsModalLegacy is the legacy rendering for loading/error states
func (m Model) renderStatsModalLegacy() string {
	// Calculate modal dimensions (80% of terminal, capped)
	modalWidth := m.Width * 80 / 100
	if modalWidth > 100 {
		modalWidth = 100
	}
	if modalWidth < 50 {
		modalWidth = 50
	}
	modalHeight := m.Height * 80 / 100
	if modalHeight > 40 {
		modalHeight = 40
	}
	if modalHeight < 20 {
		modalHeight = 20
	}

	var content strings.Builder

	// Loading state
	if m.StatsLoading {
		content.WriteString(subtleStyle.Render("Loading statistics..."))
		return m.wrapStatsModal(content.String(), modalWidth, modalHeight)
	}

	// Error state
	if m.StatsError != nil || m.StatsData == nil || m.StatsData.Error != nil {
		var errMsg string
		if m.StatsError != nil {
			errMsg = m.StatsError.Error()
		} else if m.StatsData != nil && m.StatsData.Error != nil {
			errMsg = m.StatsData.Error.Error()
		} else {
			errMsg = "Unknown error"
		}
		content.WriteString(errorStyle.Render(fmt.Sprintf("Error: %s", errMsg)))
		content.WriteString("\n\n")
		content.WriteString(subtleStyle.Render("Press esc to close"))
		return m.wrapStatsModal(content.String(), modalWidth, modalHeight)
	}

	if m.StatsData == nil || m.StatsData.ExtendedStats == nil {
		content.WriteString(subtleStyle.Render("No stats available"))
		return m.wrapStatsModal(content.String(), modalWidth, modalHeight)
	}

	// Should not reach here - data ready means we use declarative modal
	return m.wrapStatsModal("Loading...", modalWidth, modalHeight)
}

// renderStatsContent renders the statistics content for the declarative modal.
// This is called from the Custom section and returns all content lines.
// The modal library handles scrolling automatically.
func (m Model) renderStatsContent(contentWidth int) string {
	// Handle missing data gracefully (shouldn't happen, but be safe)
	if m.StatsData == nil || m.StatsData.ExtendedStats == nil {
		return subtleStyle.Render("No stats available")
	}

	stats := m.StatsData.ExtendedStats
	var lines []string

	// Status bar chart
	lines = append(lines, sectionHeader.Render("STATUS BREAKDOWN"))
	lines = append(lines, m.renderStatusBarChart(stats, contentWidth))
	lines = append(lines, "")

	// Type breakdown (compact)
	typeBreakdown := m.formatTypeBreakdown(stats)
	if typeBreakdown != "" {
		lines = append(lines, sectionHeader.Render("BY TYPE"))
		lines = append(lines, typeBreakdown)
		lines = append(lines, "")
	}

	// Priority breakdown (compact)
	priorityBreakdown := m.formatPriorityBreakdown(stats)
	if priorityBreakdown != "" {
		lines = append(lines, sectionHeader.Render("BY PRIORITY"))
		lines = append(lines, priorityBreakdown)
		lines = append(lines, "")
	}

	// Summary stats
	lines = append(lines, sectionHeader.Render("SUMMARY"))
	lines = append(lines, fmt.Sprintf("%s Total: %d", statsTableLabel.Render("  "), stats.Total))
	lines = append(lines, fmt.Sprintf("%s Points: %d", statsTableLabel.Render("  "), stats.TotalPoints))
	if stats.Total > 0 {
		lines = append(lines, fmt.Sprintf("%s Avg Points: %.1f", statsTableLabel.Render("  "), stats.AvgPointsPerTask))
	}
	completionPct := int(stats.CompletionRate * 100)
	lines = append(lines, fmt.Sprintf("%s Completion: %d%%", statsTableLabel.Render("  "), completionPct))
	lines = append(lines, "")

	// Timeline
	lines = append(lines, sectionHeader.Render("TIMELINE"))
	if stats.OldestOpen != nil {
		age := time.Since(stats.OldestOpen.CreatedAt)
		ageDays := int(age.Hours() / 24)
		lines = append(lines, fmt.Sprintf("%s Oldest open: %s (%dd)", statsTableLabel.Render("  "),
			stats.OldestOpen.ID, ageDays))
	}
	if stats.LastClosed != nil {
		lines = append(lines, fmt.Sprintf("%s Last closed: %s", statsTableLabel.Render("  "),
			stats.LastClosed.ID))
	}
	lines = append(lines, fmt.Sprintf("%s Created today: %d", statsTableLabel.Render("  "), stats.CreatedToday))
	lines = append(lines, fmt.Sprintf("%s Created this week: %d", statsTableLabel.Render("  "), stats.CreatedThisWeek))
	lines = append(lines, "")

	// Activity
	lines = append(lines, sectionHeader.Render("ACTIVITY"))
	lines = append(lines, fmt.Sprintf("%s Total logs: %d", statsTableLabel.Render("  "), stats.TotalLogs))
	lines = append(lines, fmt.Sprintf("%s Total handoffs: %d", statsTableLabel.Render("  "), stats.TotalHandoffs))
	if stats.MostActiveSession != "" {
		lines = append(lines, fmt.Sprintf("%s Most active: %s", statsTableLabel.Render("  "),
			truncateSession(stats.MostActiveSession)))
	}

	return strings.Join(lines, "\n")
}

// renderHandoffsModal renders the handoffs modal
func (m Model) renderHandoffsModal() string {
	// Use declarative modal when available and data is ready
	if m.HandoffsModal != nil && !m.HandoffsLoading && m.HandoffsError == nil && len(m.HandoffsData) > 0 {
		return m.HandoffsModal.Render(m.Width, m.Height, m.HandoffsMouseHandler)
	}

	// Fallback to legacy rendering for loading/error/empty states
	return m.renderHandoffsModalLegacy()
}

// renderHandoffsModalLegacy is the legacy rendering for loading/error/empty states
func (m Model) renderHandoffsModalLegacy() string {
	// Calculate modal dimensions (80% of terminal, capped)
	modalWidth := m.Width * 80 / 100
	if modalWidth > 100 {
		modalWidth = 100
	}
	if modalWidth < 50 {
		modalWidth = 50
	}
	modalHeight := m.Height * 80 / 100
	if modalHeight > 40 {
		modalHeight = 40
	}
	if modalHeight < 15 {
		modalHeight = 15
	}

	var content strings.Builder

	// Loading state
	if m.HandoffsLoading {
		content.WriteString(subtleStyle.Render("Loading handoffs..."))
		return m.wrapHandoffsModal(content.String(), modalWidth, modalHeight)
	}

	// Error state
	if m.HandoffsError != nil {
		content.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.HandoffsError)))
		content.WriteString("\n\n")
		content.WriteString(subtleStyle.Render("Press esc to close"))
		return m.wrapHandoffsModal(content.String(), modalWidth, modalHeight)
	}

	// Empty state
	if len(m.HandoffsData) == 0 {
		content.WriteString(subtleStyle.Render("No handoffs found"))
		return m.wrapHandoffsModal(content.String(), modalWidth, modalHeight)
	}

	// This should not be reached in practice (declarative modal handles this case)
	content.WriteString(subtleStyle.Render("Loading..."))
	return m.wrapHandoffsModal(content.String(), modalWidth, modalHeight)
}

// wrapHandoffsModal wraps content in a modal box with green border
func (m Model) wrapHandoffsModal(content string, width, height int) string {
	footer := subtleStyle.Render("↑↓:select  Enter:open issue  Esc:close  r:refresh")
	inner := lipgloss.JoinVertical(lipgloss.Left, content, "", footer)

	// Use custom renderer if provided (for embedded mode with custom theming)
	if m.ModalRenderer != nil {
		// Add vertical padding to match lipgloss Padding(1, 2) behavior.
		// Custom renderer only handles horizontal padding, so we add blank lines
		// for top/bottom padding manually.
		paddedInner := "\n" + inner + "\n"
		// Add 2 to width/height: lipgloss Width/Height = content area, renderer expects outer with borders
		return m.ModalRenderer(paddedInner, width+2, height+2, ModalTypeHandoffs, 1)
	}

	// Default lipgloss rendering
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("42")). // Green for handoffs
		Padding(1, 2).
		Width(width).
		Height(height)

	return modalStyle.Render(inner)
}

// renderBoardPicker renders the board picker modal
func (m Model) renderBoardPicker() string {
	// Use declarative modal when available
	if m.BoardPickerModal != nil && m.BoardPickerMouseHandler != nil && len(m.AllBoards) > 0 {
		return m.BoardPickerModal.Render(m.Width, m.Height, m.BoardPickerMouseHandler)
	}

	// Fallback: render empty state or loading state
	modalWidth := m.Width * 60 / 100
	if modalWidth > 80 {
		modalWidth = 80
	}
	if modalWidth < 40 {
		modalWidth = 40
	}
	modalHeight := m.Height * 60 / 100
	if modalHeight > 30 {
		modalHeight = 30
	}
	if modalHeight < 10 {
		modalHeight = 10
	}

	var content strings.Builder

	// Empty state
	if len(m.AllBoards) == 0 {
		content.WriteString(subtleStyle.Render("No boards found"))
		content.WriteString("\n\n")
		content.WriteString(subtleStyle.Render("Create a board with: td board create <name>"))
	} else {
		// Loading state (modal not yet created)
		content.WriteString(subtleStyle.Render("Loading boards..."))
	}

	return m.wrapBoardPickerModal(content.String(), modalWidth, modalHeight)
}

// wrapBoardPickerModal wraps board picker content in a styled modal
func (m Model) wrapBoardPickerModal(content string, width, height int) string {
	footer := subtleStyle.Render("↑↓:select  Enter:open  Esc:close")
	inner := lipgloss.JoinVertical(lipgloss.Left, content, "", footer)

	// Use custom renderer if provided (for embedded mode with custom theming)
	if m.ModalRenderer != nil {
		// Add vertical padding to match lipgloss Padding(1, 2) behavior.
		// Custom renderer only handles horizontal padding, so we add blank lines
		// for top/bottom padding manually.
		paddedInner := "\n" + inner + "\n"
		// Add 2 to width/height: lipgloss Width/Height = content area, renderer expects outer with borders
		return m.ModalRenderer(paddedInner, width+2, height+2, ModalTypeBoardPicker, 1)
	}

	// Default lipgloss rendering
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("212")). // Purple
		Padding(1, 2).
		Width(width).
		Height(height)

	return modalStyle.Render(inner)
}

// renderFormModal renders the form modal using huh form
func (m Model) renderFormModal() string {
	if m.FormState == nil || m.FormState.Form == nil {
		return ""
	}

	modalWidth, modalHeight := m.formModalDimensions()

	// Set form width to match modal content area (modalWidth minus Padding(1,2) = 4 horizontal chars)
	formWidth := modalWidth - 4
	if formWidth > 0 {
		m.FormState.Width = formWidth
		m.FormState.Form.WithWidth(formWidth)
	}

	// Render the huh form
	formView := m.FormState.Form.View()

	// Interactive buttons
	submitFocused := m.FormState.ButtonFocus == formButtonFocusSubmit
	cancelFocused := m.FormState.ButtonFocus == formButtonFocusCancel
	submitHovered := m.FormState.ButtonHover == 1
	cancelHovered := m.FormState.ButtonHover == 2
	buttons := renderButtonPair("Submit", "Cancel", submitFocused, cancelFocused, submitHovered, cancelHovered, false, false)

	// Build footer with key hints (truncated to fit modal content width)
	var footerParts []string
	if m.FormState.ShowExtended {
		footerParts = append(footerParts, subtleStyle.Render("Ctrl+X:hide extended"))
	} else {
		footerParts = append(footerParts, subtleStyle.Render("Ctrl+X:show extended"))
	}
	footerParts = append(footerParts, subtleStyle.Render("Tab:next  Shift+Tab:prev  Enter:select"))
	footerParts = append(footerParts, subtleStyle.Render("Ctrl+S:submit  Esc:cancel"))
	footer := strings.Join(footerParts, "  ")
	if lipgloss.Width(footer) > formWidth {
		footer = lipgloss.NewStyle().MaxWidth(formWidth).Render(footer)
	}

	// Combine form and footer
	inner := lipgloss.JoinVertical(lipgloss.Left, formView, "", buttons, "", footer)

	// Use custom renderer if provided (for embedded mode with custom theming)
	if m.ModalRenderer != nil {
		// Add vertical padding to match lipgloss Padding(1, 2) behavior.
		// Custom renderer only handles horizontal padding, so we add blank lines
		// for top/bottom padding manually.
		paddedInner := "\n" + inner + "\n"
		// Add 2 to width/height: lipgloss Width/Height = content area, renderer expects outer with borders
		return m.ModalRenderer(paddedInner, modalWidth+2, modalHeight+2, ModalTypeForm, 1)
	}

	// Default lipgloss rendering
	// Select border color - cyan for forms (different from issue modals)
	borderColor := lipgloss.Color("45") // Cyan

	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 2).
		Width(modalWidth).
		Height(modalHeight)

	return modalStyle.Render(inner)
}

// renderStatusBarChart renders a horizontal bar chart for status breakdown
func (m Model) renderStatusBarChart(stats *models.ExtendedStats, width int) string {
	var lines []string

	statuses := []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
		models.StatusBlocked,
		models.StatusInReview,
		models.StatusClosed,
	}

	// Find max count for scaling
	var maxCount int
	for _, status := range statuses {
		if count := stats.ByStatus[status]; count > maxCount {
			maxCount = count
		}
	}

	if maxCount == 0 {
		maxCount = 1 // Avoid division by zero
	}

	// Bar width (account for label and count)
	barWidth := width - 20

	for _, status := range statuses {
		count := stats.ByStatus[status]

		// Calculate bar length (proportional to max)
		barLen := 0
		if count > 0 && maxCount > 0 {
			barLen = (count * barWidth) / maxCount
		}

		// Build bar with appropriate color from pre-created styles
		statusColor := statusChartStyles[status]

		// Build filled and empty segments
		filled := strings.Repeat(statsBarFilled, barLen)
		empty := strings.Repeat(statsBarEmpty, barWidth-barLen)
		bar := statusColor.Render(filled) + subtleStyle.Render(empty)

		// Format label and count
		label := fmt.Sprintf("%-11s", string(status))
		countStr := fmt.Sprintf("%2d", count)

		line := fmt.Sprintf("  %s %s %s", label, bar, countStr)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// formatTypeBreakdown formats a compact type breakdown
func (m Model) formatTypeBreakdown(stats *models.ExtendedStats) string {
	types := []models.Type{
		models.TypeBug,
		models.TypeFeature,
		models.TypeTask,
		models.TypeEpic,
		models.TypeChore,
	}

	var parts []string
	for _, t := range types {
		count := stats.ByType[t]
		if count > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", t, count))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return statsTableLabel.Render("  ") + strings.Join(parts, "  ")
}

// formatPriorityBreakdown formats a compact priority breakdown
func (m Model) formatPriorityBreakdown(stats *models.ExtendedStats) string {
	priorities := []models.Priority{
		models.PriorityP0,
		models.PriorityP1,
		models.PriorityP2,
		models.PriorityP3,
		models.PriorityP4,
	}

	var parts []string
	for _, p := range priorities {
		count := stats.ByPriority[p]
		if count > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", p, count))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return statsTableLabel.Render("  ") + strings.Join(parts, "  ")
}

// wrapModal wraps content in a modal box with border (deprecated, use wrapModalWithDepth)
func (m Model) wrapModal(content string, width, height int) string {
	return m.wrapModalWithDepth(content, width, height)
}

// wrapModalWithDepth wraps content in a modal box with depth-aware styling
func (m Model) wrapModalWithDepth(content string, width, height int) string {
	depth := m.ModalDepth()

	// Build footer with breadcrumb if depth > 1
	var footerParts []string

	// Add status message if present (not in embedded mode - sidecar handles toasts)
	if m.StatusMessage != "" && !m.Embedded {
		footerParts = append(footerParts, readyColor.Render(m.StatusMessage))
	}

	// Add breadcrumb for stacked modals
	if breadcrumb := m.ModalBreadcrumb(); breadcrumb != "" {
		footerParts = append(footerParts, breadcrumbStyle.Render(breadcrumb))
	}

	// Add key hints
	modal := m.CurrentModal()
	if modal != nil && modal.TaskSectionFocused {
		footerParts = append(footerParts, subtleStyle.Render("↑↓:navigate  Enter:open  Tab:scroll  Esc:close"))
	} else if depth > 1 {
		// Show Tab hint if this is an epic with tasks
		if modal != nil && modal.Issue != nil && modal.Issue.Type == models.TypeEpic && len(modal.EpicTasks) > 0 {
			footerParts = append(footerParts, subtleStyle.Render("↑↓:scroll  Tab:tasks  Esc:back  r:refresh"))
		} else {
			footerParts = append(footerParts, subtleStyle.Render("↑↓:scroll  Esc:back  r:refresh"))
		}
	} else {
		footerParts = append(footerParts, subtleStyle.Render(m.Keymap.ModalFooterHelp()))
	}

	footer := strings.Join(footerParts, "\n")
	inner := lipgloss.JoinVertical(lipgloss.Left, content, "", footer)

	// Use custom renderer if provided (for embedded mode with custom theming)
	if m.ModalRenderer != nil {
		// Add vertical padding to match lipgloss Padding(1, 2) behavior.
		// Custom renderer only handles horizontal padding, so we add blank lines
		// for top/bottom padding manually.
		paddedInner := "\n" + inner + "\n"
		// Add 2 to width/height: lipgloss Width/Height = content area, renderer expects outer with borders
		return m.ModalRenderer(paddedInner, width+2, height+2, ModalTypeIssue, depth)
	}

	// Default lipgloss rendering
	// Select border color based on depth
	var borderColor lipgloss.Color
	switch depth {
	case 1:
		borderColor = primaryColor // Purple/Magenta (212)
	case 2:
		borderColor = lipgloss.Color("45") // Cyan
	default:
		borderColor = lipgloss.Color("214") // Orange for depth 3+
	}

	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 2).
		Width(width).
		Height(height)

	return modalStyle.Render(inner)
}

// wrapConfirmationModal wraps content in a confirmation modal box with standard styling
func (m Model) wrapConfirmationModal(content string, width int) string {
	// Calculate height from content lines + padding (1 top + 1 bottom)
	height := strings.Count(content, "\n") + 1 + 2

	// Use custom renderer if provided (for embedded mode with custom theming)
	if m.ModalRenderer != nil {
		// Add vertical padding to match lipgloss Padding(1, 2) behavior.
		// Custom renderer only handles horizontal padding, so we add blank lines
		// for top/bottom padding manually.
		paddedContent := "\n" + content + "\n"
		// Add 2 to width/height: lipgloss Width/Height = content area, renderer expects outer with borders
		return m.ModalRenderer(paddedContent, width+2, height+2, ModalTypeConfirmation, 1)
	}

	// Default lipgloss rendering
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(errorColor).
		Padding(1, 2).
		Width(width)

	return modalStyle.Render(content)
}

// renderDeleteConfirmation renders the delete confirmation dialog using the declarative modal library
func (m Model) renderDeleteConfirmation() string {
	// Use declarative modal when available
	if m.DeleteConfirmModal != nil && m.DeleteConfirmMouseHandler != nil {
		return m.DeleteConfirmModal.Render(m.Width, m.Height, m.DeleteConfirmMouseHandler)
	}

	// Fallback to legacy rendering (should not happen in normal flow)
	return m.renderDeleteConfirmationLegacy()
}

// renderDeleteConfirmationLegacy is the legacy rendering for the delete confirmation dialog
// Kept for backward compatibility and edge cases
func (m Model) renderDeleteConfirmationLegacy() string {
	width := 40
	if len(m.ConfirmTitle) > 30 {
		width = len(m.ConfirmTitle) + 10
	}
	if width > 60 {
		width = 60
	}

	var content strings.Builder

	// Title
	action := "Delete"
	if m.ConfirmAction != "delete" {
		action = m.ConfirmAction
	}
	content.WriteString(titleStyle.Render(fmt.Sprintf("%s %s?", action, m.ConfirmIssueID)))
	content.WriteString("\n")

	// Issue title (truncated to fit on one line)
	// Content width is width - 6 (border 2 + padding 4), minus 2 for quotes
	maxTitleLen := width - 10
	if maxTitleLen < 20 {
		maxTitleLen = 20
	}
	title := m.ConfirmTitle
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-3] + "..."
	}
	content.WriteString(subtleStyle.Render(fmt.Sprintf("\"%s\"", title)))
	content.WriteString("\n\n")

	// Interactive buttons
	yesFocused := m.ConfirmButtonFocus == 0
	noFocused := m.ConfirmButtonFocus == 1
	yesHovered := m.ConfirmButtonHover == 1
	noHovered := m.ConfirmButtonHover == 2

	yesBtn := renderButton("Yes", yesFocused, yesHovered, true) // Danger button for destructive action
	noBtn := renderButton("No", noFocused, noHovered, false)

	content.WriteString(yesBtn)
	content.WriteString("  ")
	content.WriteString(noBtn)
	content.WriteString("\n\n")

	// Shortcut hints
	content.WriteString(subtleStyle.Render("Tab:switch  Y/N:quick  Esc:cancel"))

	return m.wrapConfirmationModal(content.String(), width)
}

// Legacy renderCloseConfirmation removed - close confirmation now uses declarative modal

// wrapText wraps text to fit within maxWidth
func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}

	var lines []string
	words := strings.Fields(text)
	var currentLine string

	for _, word := range words {
		if currentLine == "" {
			currentLine = word
		} else if len(currentLine)+1+len(word) <= maxWidth {
			currentLine += " " + word
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}

func renderLogLines(log models.Log, contentWidth int) []string {
	prefix := timestampStyle.Render(log.Timestamp.Format("01-02 15:04")) + " " +
		subtleStyle.Render(truncateSession(log.SessionID)) + " "
	prefixWidth := lipgloss.Width(prefix)
	messageWidth := contentWidth - prefixWidth
	if messageWidth < 1 {
		messageWidth = 1
	}

	wrappedMessage := cellbuf.Wrap(log.Message, messageWidth, "")
	messageLines := strings.Split(wrappedMessage, "\n")

	indent := strings.Repeat(" ", prefixWidth)
	lines := make([]string, 0, len(messageLines))
	for i, line := range messageLines {
		if i == 0 {
			lines = append(lines, prefix+line)
		} else {
			lines = append(lines, indent+line)
		}
	}

	return lines
}

// Error style for modal
var errorStyle = lipgloss.NewStyle().Foreground(errorColor)

// renderSearchBar renders the search input bar when search mode is active
func (m Model) renderSearchBar() string {
	if !m.SearchMode && m.SearchQuery == "" {
		return ""
	}

	var sb strings.Builder

	// Icon: triangle with color indicating state
	// Pink when in search mode, orange when filter active, subtle otherwise
	pinkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")) // Pink
	if m.SearchMode {
		sb.WriteString(pinkStyle.Render("▸"))
		sb.WriteString(" ")
	} else {
		// Orange triangle to match the active filter query
		sb.WriteString(searchQueryActiveStyle.Render("▸"))
		sb.WriteString(" ")
	}

	// Render the textinput (includes cursor and query)
	if m.SearchMode {
		sb.WriteString(m.SearchInput.View())
	} else {
		// Not in search mode but have a query - show it bright to indicate active filtering
		sb.WriteString(searchQueryActiveStyle.Render(m.SearchQuery))
	}

	// Closed indicator
	if m.IncludeClosed {
		numClosed := len(m.TaskList.Closed)
		sb.WriteString("  ")
		sb.WriteString(subtleStyle.Render(fmt.Sprintf("[%d closed]", numClosed)))
	}

	// Hint
	padding := m.Width - lipgloss.Width(sb.String()) - 12
	if padding > 0 {
		sb.WriteString(strings.Repeat(" ", padding))
	}
	sb.WriteString(subtleStyle.Render("[Esc:exit]"))

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Render(sb.String())
}

// renderFooter renders the footer with key bindings and refresh time
func (m Model) renderFooter() string {
	// Use board-specific footer when in board mode
	var keysStr string
	if m.TaskListMode == TaskListModeBoard {
		keysStr = m.Keymap.BoardFooterHelp()
	} else {
		keysStr = m.Keymap.FooterHelp()
	}
	keys := helpStyle.Render(keysStr)

	// Show active sessions indicator
	sessionsIndicator := ""
	if len(m.ActiveSessions) > 0 {
		sessionsIndicator = activeSessionStyle.Render(fmt.Sprintf(" %d active ", len(m.ActiveSessions)))
	}

	// Show prominent handoff alert if new handoffs occurred
	handoffAlert := ""
	if len(m.RecentHandoffs) > 0 {
		handoffAlert = handoffAlertStyle.Render(fmt.Sprintf(" [%d HANDOFF] ", len(m.RecentHandoffs)))
	}

	// Show prominent review alert if items need review
	reviewAlert := ""
	if len(m.TaskList.Reviewable) > 0 {
		reviewAlert = reviewAlertStyle.Render(fmt.Sprintf(" [%d TO REVIEW] ", len(m.TaskList.Reviewable)))
	}

	// Show update available notification
	updateNotif := ""
	if m.UpdateAvail != nil {
		updateNotif = updateAvailStyle.Render(fmt.Sprintf(" [UPDATE: %s] ", m.UpdateAvail.LatestVersion))
	}

	// Show status message toast (yank confirmation, errors, etc.)
	statusToast := ""
	if m.StatusMessage != "" {
		style := toastStyle
		if m.StatusIsError {
			style = toastErrorStyle
		}
		statusToast = style.Render(fmt.Sprintf(" %s ", m.StatusMessage))
	}

	refresh := timestampStyle.Render(fmt.Sprintf("Last: %s", m.LastRefresh.Format("15:04:05")))

	// Calculate spacing
	padding := m.Width - lipgloss.Width(keys) - lipgloss.Width(sessionsIndicator) - lipgloss.Width(handoffAlert) - lipgloss.Width(reviewAlert) - lipgloss.Width(updateNotif) - lipgloss.Width(statusToast) - lipgloss.Width(refresh) - 2
	if padding < 0 {
		padding = 0
	}

	return fmt.Sprintf(" %s%s%s%s%s%s%s%s", keys, strings.Repeat(" ", padding), sessionsIndicator, handoffAlert, reviewAlert, updateNotif, statusToast, refresh)
}

// renderHelp renders the help modal with scrolling support
func (m Model) renderHelp() string {
	// Calculate modal dimensions (80% of terminal, clamped)
	modalWidth := m.Width * 80 / 100
	if modalWidth > 80 {
		modalWidth = 80
	}
	if modalWidth < 50 {
		modalWidth = 50
	}
	modalHeight := m.Height * 80 / 100
	if modalHeight > 40 {
		modalHeight = 40
	}
	if modalHeight < 15 {
		modalHeight = 15
	}

	// Get help text and split into lines
	helpText := m.Keymap.GenerateHelp()
	allLines := strings.Split(helpText, "\n")

	// Calculate visible area
	visibleHeight := modalHeight - 4 // Account for border and footer
	totalLines := len(allLines)
	scroll := m.HelpScroll

	// Clamp scroll
	maxScroll := totalLines - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}

	// Build visible content
	var content strings.Builder

	// Show up indicator if scrolled down
	if scroll > 0 {
		content.WriteString(subtleStyle.Render(fmt.Sprintf("  ▲ %d more above\n", scroll)))
		visibleHeight-- // Reduce visible lines for indicator
	}

	// Get visible lines
	endIdx := scroll + visibleHeight
	if endIdx > totalLines {
		endIdx = totalLines
	}
	if scroll < totalLines {
		for i := scroll; i < endIdx; i++ {
			content.WriteString(allLines[i])
			if i < endIdx-1 {
				content.WriteString("\n")
			}
		}
	}

	// Show down indicator if more content below
	linesBelow := totalLines - endIdx
	if linesBelow > 0 {
		content.WriteString("\n")
		content.WriteString(subtleStyle.Render(fmt.Sprintf("  ▼ %d more below", linesBelow)))
	}

	// Build footer with scroll info
	var footerParts []string
	if totalLines > visibleHeight {
		scrollInfo := subtleStyle.Render(fmt.Sprintf("─ %d/%d ─", scroll+1, totalLines))
		footerParts = append(footerParts, scrollInfo)
	}
	footerParts = append(footerParts, subtleStyle.Render("j/k:scroll  Ctrl+d/u:½page  G/gg:end/start  ?/Esc:close"))
	footer := strings.Join(footerParts, "  ")

	// Combine content and footer
	inner := lipgloss.JoinVertical(lipgloss.Left, content.String(), "", footer)

	// Style the modal
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("141")). // Purple for help
		Padding(1, 2).
		Width(modalWidth).
		Height(modalHeight)

	return modalStyle.Render(inner)
}

// determinePanelState determines the visual state of a panel for theming
func (m Model) determinePanelState(panel Panel) PanelState {
	// Check divider states first (more specific)
	dividerForPanel := -1
	if panel == PanelCurrentWork {
		dividerForPanel = 0
	} else if panel == PanelTaskList {
		dividerForPanel = 1
	}

	if dividerForPanel >= 0 {
		if m.DraggingDivider == dividerForPanel {
			return PanelStateDividerActive
		}
		if m.DividerHover == dividerForPanel && m.DraggingDivider < 0 {
			return PanelStateDividerHover
		}
	}

	// Then check panel states
	if m.ActivePanel == panel {
		return PanelStateActive
	}
	if m.HoverPanel == panel {
		return PanelStateHover
	}
	return PanelStateNormal
}

// wrapPanel wraps content in a panel with title and border
func (m Model) wrapPanel(title, content string, height int, panel Panel) string {
	// Use custom renderer if provided (for embedded mode with custom theming)
	if m.PanelRenderer != nil {
		state := m.determinePanelState(panel)
		// Render title
		titleStr := panelTitleStyle.Render(title)
		// Calculate content width
		contentWidth := m.Width - 4 // Account for border and padding
		// Truncate/pad content to fit
		lines := strings.Split(content, "\n")
		contentHeight := height - 3 // Title + border
		// Pad or truncate lines
		for len(lines) < contentHeight {
			lines = append(lines, "")
		}
		if len(lines) > contentHeight {
			lines = lines[:contentHeight]
		}
		// Ensure each line fits width
		for i, line := range lines {
			if lipgloss.Width(line) > contentWidth {
				lines[i] = truncateString(line, contentWidth)
			}
		}
		body := strings.Join(lines, "\n")
		// Combine title and body
		inner := lipgloss.JoinVertical(lipgloss.Left, titleStr, body)
		// Pass outer width (m.Width) - renderer expects outer dimensions including borders
		return m.PanelRenderer(inner, m.Width, height, state)
	}

	// Default lipgloss rendering
	style := panelStyle
	if m.ActivePanel == panel {
		style = activePanelStyle
	} else if m.HoverPanel == panel {
		style = hoverPanelStyle
	}

	// Override style for divider drag/hover feedback
	// Divider 0 is bottom of PanelCurrentWork, Divider 1 is bottom of PanelTaskList
	dividerForPanel := -1
	if panel == PanelCurrentWork {
		dividerForPanel = 0
	} else if panel == PanelTaskList {
		dividerForPanel = 1
	}

	if dividerForPanel >= 0 {
		if m.DraggingDivider == dividerForPanel {
			style = dividerActivePanelStyle
		} else if m.DividerHover == dividerForPanel && m.DraggingDivider < 0 {
			style = dividerHoverPanelStyle
		}
	}

	// Render title
	titleStr := panelTitleStyle.Render(title)

	// Calculate content width
	contentWidth := m.Width - 4 // Account for border and padding

	// Truncate/pad content to fit
	lines := strings.Split(content, "\n")
	contentHeight := height - 3 // Title + border

	// Pad or truncate lines
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}
	if len(lines) > contentHeight {
		lines = lines[:contentHeight]
	}

	// Ensure each line fits width
	for i, line := range lines {
		if lipgloss.Width(line) > contentWidth {
			lines[i] = truncateString(line, contentWidth)
		}
	}

	body := strings.Join(lines, "\n")

	// Combine title and body
	inner := lipgloss.JoinVertical(lipgloss.Left, titleStr, body)

	return style.Width(m.Width - 2).Render(inner)
}

// formatIssueCompact formats an issue in a compact single-line format
func (m Model) formatIssueCompact(issue *models.Issue) string {
	parts := []string{
		formatTypeIcon(issue.Type),
		titleStyle.Render(issue.ID),
		formatPriority(issue.Priority),
		issue.Title,
	}

	if issue.ImplementerSession != "" {
		parts = append(parts, subtleStyle.Render(fmt.Sprintf("(%s)", truncateSession(issue.ImplementerSession))))
	}

	return strings.Join(parts, " ")
}

// formatIssueShort formats an issue in a short format
func (m Model) formatIssueShort(issue *models.Issue) string {
	typeIcon := formatTypeIcon(issue.Type)
	idStr := subtleStyle.Render(issue.ID)
	priorityStr := formatPriority(issue.Priority)

	// Calculate available width for title:
	// m.Width - 4 (panel border) - 8 (row prefix "  [TAG] ") - typeIcon - ID - priority - 3 spaces
	overhead := 4 + 8 + 2 + lipgloss.Width(idStr) + lipgloss.Width(priorityStr) + 3
	titleWidth := m.Width - overhead
	if titleWidth < 20 {
		titleWidth = 20 // minimum reasonable width
	}

	return fmt.Sprintf("%s %s %s %s", typeIcon, idStr, priorityStr, truncateString(issue.Title, titleWidth))
}

// truncateString truncates a string to maxLen with ellipsis (ANSI-aware)
func truncateString(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	if lipgloss.Width(s) <= maxLen {
		return s
	}
	// Use ANSI-aware truncation to handle styled text properly
	return ansi.Truncate(s, maxLen-3, "...")
}

// truncateSession shortens a session ID for display
func truncateSession(sessionID string) string {
	if len(sessionID) <= 10 {
		return sessionID
	}
	return sessionID[:10]
}

// Color styles for task list sections
var (
	readyColor   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	reviewColor  = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	blockedColor = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	reworkColor  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Orange/warning

	// Prominent style for review alert in footer
	reviewAlertStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("141"))

	// Header styles for category sections (matching reviewAlertStyle pattern)
	readyHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("42")) // Green bg

	blockedHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("255")).
				Background(lipgloss.Color("196")) // Red bg

	// Prominent style for handoff alert - green background
	handoffAlertStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("42"))

	// Style for active sessions indicator - cyan text
	activeSessionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("45"))

	// Style for update available notification - yellow/gold
	updateAvailStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("214"))
)
