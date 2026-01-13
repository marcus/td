package monitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
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

	if m.ShowTDQHelp {
		base := m.renderBaseView()
		tdqModal := m.renderTDQHelp()
		return OverlayModal(base, tdqModal, m.Width, m.Height)
	}

	// Render base view (panels + footer)
	base := m.renderBaseView()

	// Overlay form modal if open
	if m.FormOpen && m.FormState != nil {
		form := m.renderFormModal()
		return OverlayModal(base, form, m.Width, m.Height)
	}

	// Overlay confirmation dialog if open
	if m.ConfirmOpen {
		confirm := m.renderConfirmation()
		return OverlayModal(base, confirm, m.Width, m.Height)
	}

	// Overlay close confirmation dialog if open
	if m.CloseConfirmOpen {
		confirm := m.renderCloseConfirmation()
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

// renderActivityPanel renders the activity log panel (Panel 2)
func (m Model) renderActivityPanel(height int) string {
	var content strings.Builder

	totalRows := len(m.Activity)
	if totalRows == 0 {
		content.WriteString(subtleStyle.Render("No recent activity"))
		return m.wrapPanel("ACTIVITY LOG", content.String(), height, PanelActivity)
	}

	cursor := m.Cursor[PanelActivity]
	isActive := m.ActivePanel == PanelActivity
	offset := m.ScrollOffset[PanelActivity]
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
	panelTitle := "ACTIVITY LOG"
	if needsScroll {
		endPos := offset + effectiveMaxLines
		if endPos > totalRows {
			endPos = totalRows
		}
		panelTitle = fmt.Sprintf("ACTIVITY LOG (%d-%d of %d)", offset+1, endPos, totalRows)
	}

	// Show up indicator if scrolled down
	if showUpIndicator {
		content.WriteString(subtleStyle.Render("  ▲ more above"))
		content.WriteString("\n")
	}

	visible := m.visibleItems(totalRows, offset, effectiveMaxLines)
	for i := offset; i < offset+visible && i < totalRows; i++ {
		item := m.Activity[i]
		line := m.formatActivityItem(item)
		if isActive && cursor == i {
			line = highlightRow(line, m.Width-4)
		}
		content.WriteString(line)
		content.WriteString("\n")
	}

	// Show down indicator if more content below
	if hasMoreBelow {
		content.WriteString(subtleStyle.Render("  ▼ more below"))
		content.WriteString("\n")
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
		line := fmt.Sprintf("  %s %s", tag, issueStr)

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
		tag := m.formatCategoryTag(statusToCategory(issue.Status))
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
		line := fmt.Sprintf("  %s %s", tag, issueStr)

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
		return readyColor.Render("READY") + fmt.Sprintf(" (%d):", count)
	case CategoryBlocked:
		count = len(m.BoardMode.SwimlaneData.Blocked)
		return blockedColor.Render("BLOCKED") + fmt.Sprintf(" (%d):", count)
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
		return readyColor.Render("READY") + fmt.Sprintf(" (%d):", count)
	case CategoryBlocked:
		count = len(m.TaskList.Blocked)
		return blockedColor.Render("BLOCKED") + fmt.Sprintf(" (%d):", count)
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

// statusToCategory maps issue status to TaskListCategory for display
func statusToCategory(status models.Status) TaskListCategory {
	switch status {
	case models.StatusOpen, models.StatusInProgress:
		return CategoryReady
	case models.StatusInReview:
		return CategoryReviewable
	case models.StatusBlocked:
		return CategoryBlocked
	case models.StatusClosed:
		return CategoryClosed
	}
	return CategoryReady
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

	// Status line: status, type, priority, points
	statusLine := fmt.Sprintf("%s  %s  %s",
		formatStatus(issue.Status),
		formatTypeIcon(issue.Type),
		formatPriority(issue.Priority))
	if issue.Points > 0 {
		statusLine += fmt.Sprintf("  %dpts", issue.Points)
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

// renderStatsModal renders the stats modal with statistics and bar charts
func (m Model) renderStatsModal() string {
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

	contentWidth := modalWidth - 4 // Account for border and padding

	var content strings.Builder

	// Loading state
	if m.StatsLoading {
		content.WriteString(subtleStyle.Render("Loading statistics..."))
		return m.wrapModal(content.String(), modalWidth, modalHeight)
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
		return m.wrapModal(content.String(), modalWidth, modalHeight)
	}

	if m.StatsData == nil || m.StatsData.ExtendedStats == nil {
		content.WriteString(subtleStyle.Render("No stats available"))
		return m.wrapModal(content.String(), modalWidth, modalHeight)
	}

	stats := m.StatsData.ExtendedStats

	// Build all content lines for scrolling
	var lines []string

	// Title
	lines = append(lines, titleStyle.Render("STATISTICS"))
	lines = append(lines, "")

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

	// Apply scroll offset
	visibleHeight := modalHeight - 4 // Account for border and footer
	totalLines := len(lines)

	// Clamp scroll
	maxScroll := totalLines - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := m.StatsScroll
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

	return m.wrapModal(content.String(), modalWidth, modalHeight)
}

// renderHandoffsModal renders the handoffs modal
func (m Model) renderHandoffsModal() string {
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

	contentWidth := modalWidth - 4 // Account for border and padding

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

	// Build content lines
	var lines []string
	lines = append(lines, titleStyle.Render(fmt.Sprintf("RECENT HANDOFFS (%d)", len(m.HandoffsData))))
	lines = append(lines, "")

	for i, h := range m.HandoffsData {
		// Format: [timestamp] [session] [issue_id] done:X remaining:Y
		timestamp := subtleStyle.Render(h.Timestamp.Format("01-02 15:04"))
		session := subtleStyle.Render(truncateSession(h.SessionID))
		issueID := titleStyle.Render(h.IssueID)

		summary := fmt.Sprintf("done:%d remaining:%d", len(h.Done), len(h.Remaining))
		if len(h.Uncertain) > 0 {
			summary += fmt.Sprintf(" uncertain:%d", len(h.Uncertain))
		}

		line := fmt.Sprintf("  %s %s %s %s", timestamp, session, issueID, summary)

		if i == m.HandoffsCursor {
			line = highlightRow(line, contentWidth)
		}

		lines = append(lines, truncateString(line, contentWidth))
	}

	// Apply scroll offset
	visibleHeight := modalHeight - 5 // border, title, footer
	totalLines := len(lines)

	maxScroll := totalLines - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := m.HandoffsScroll
	if scroll > maxScroll {
		scroll = maxScroll
	}

	// Auto-scroll to keep cursor visible
	if m.HandoffsCursor < scroll {
		scroll = m.HandoffsCursor
	} else if m.HandoffsCursor >= scroll+visibleHeight {
		scroll = m.HandoffsCursor - visibleHeight + 1
	}

	endIdx := scroll + visibleHeight
	if endIdx > totalLines {
		endIdx = totalLines
	}
	if scroll < 0 {
		scroll = 0
	}
	if scroll < totalLines {
		visibleLines := lines[scroll:endIdx]
		content.WriteString(strings.Join(visibleLines, "\n"))
	}

	// Scroll indicator
	if totalLines > visibleHeight {
		content.WriteString("\n")
		scrollInfo := subtleStyle.Render(fmt.Sprintf("─ %d/%d ─", scroll+1, totalLines))
		content.WriteString(scrollInfo)
	}

	return m.wrapHandoffsModal(content.String(), modalWidth, modalHeight)
}

// wrapHandoffsModal wraps content in a modal box with green border
func (m Model) wrapHandoffsModal(content string, width, height int) string {
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("42")). // Green for handoffs
		Padding(1, 2).
		Width(width).
		Height(height)

	footer := subtleStyle.Render("↑↓:select  Enter:open issue  Esc:close  r:refresh")
	inner := lipgloss.JoinVertical(lipgloss.Left, content, "", footer)

	return modalStyle.Render(inner)
}

// renderBoardPicker renders the board picker modal
func (m Model) renderBoardPicker() string {
	// Calculate modal dimensions (60% of terminal, capped)
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

	contentWidth := modalWidth - 4 // Account for border and padding

	var content strings.Builder

	// Empty state
	if len(m.AllBoards) == 0 {
		content.WriteString(subtleStyle.Render("No boards found"))
		content.WriteString("\n\n")
		content.WriteString(subtleStyle.Render("Create a board with: td board create <name>"))
		return m.wrapBoardPickerModal(content.String(), modalWidth, modalHeight)
	}

	// Build content lines
	var lines []string
	lines = append(lines, titleStyle.Render(fmt.Sprintf("SELECT BOARD (%d)", len(m.AllBoards))))
	lines = append(lines, "")

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
			name += " • " + subtleStyle.Render(queryPreview)
		}

		line := "  " + name

		if i == m.BoardPickerCursor {
			line = highlightRow(line, contentWidth)
		}

		lines = append(lines, truncateString(line, contentWidth))
	}

	content.WriteString(strings.Join(lines, "\n"))
	return m.wrapBoardPickerModal(content.String(), modalWidth, modalHeight)
}

// wrapBoardPickerModal wraps board picker content in a styled modal
func (m Model) wrapBoardPickerModal(content string, width, height int) string {
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("212")). // Purple
		Padding(1, 2).
		Width(width).
		Height(height)

	footer := subtleStyle.Render("↑↓:select  Enter:open  Esc:close")
	inner := lipgloss.JoinVertical(lipgloss.Left, content, "", footer)

	return modalStyle.Render(inner)
}

// renderFormModal renders the form modal using huh form
func (m Model) renderFormModal() string {
	if m.FormState == nil || m.FormState.Form == nil {
		return ""
	}

	modalWidth, modalHeight := m.formModalDimensions()

	// Render the huh form
	formView := m.FormState.Form.View()

	// Interactive buttons
	submitFocused := m.FormState.ButtonFocus == formButtonFocusSubmit
	cancelFocused := m.FormState.ButtonFocus == formButtonFocusCancel
	submitHovered := m.FormState.ButtonHover == 1
	cancelHovered := m.FormState.ButtonHover == 2
	buttons := renderButtonPair("Submit", "Cancel", submitFocused, cancelFocused, submitHovered, cancelHovered, false, false)

	// Build footer with key hints
	var footerParts []string
	if m.FormState.ShowExtended {
		footerParts = append(footerParts, subtleStyle.Render("Ctrl+X:hide extended"))
	} else {
		footerParts = append(footerParts, subtleStyle.Render("Ctrl+X:show extended"))
	}
	footerParts = append(footerParts, subtleStyle.Render("Tab:next  Shift+Tab:prev  Enter:select"))
	footerParts = append(footerParts, subtleStyle.Render("Ctrl+S:submit  Esc:cancel"))
	footer := strings.Join(footerParts, "  ")

	// Combine form and footer
	inner := lipgloss.JoinVertical(lipgloss.Left, formView, "", buttons, "", footer)

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

	return modalStyle.Render(inner)
}

// wrapConfirmationModal wraps content in a confirmation modal box with standard styling
func wrapConfirmationModal(content string, width int) string {
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(errorColor).
		Padding(1, 2).
		Width(width)

	return modalStyle.Render(content)
}

// renderConfirmation renders the confirmation dialog with interactive buttons
func (m Model) renderConfirmation() string {
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

	return wrapConfirmationModal(content.String(), width)
}

// renderCloseConfirmation renders the close confirmation dialog with optional reason input and interactive buttons
func (m Model) renderCloseConfirmation() string {
	width := 50
	if len(m.CloseConfirmTitle) > 40 {
		width = len(m.CloseConfirmTitle) + 10
	}
	if width > 60 {
		width = 60
	}

	var content strings.Builder

	// Title
	content.WriteString(titleStyle.Render(fmt.Sprintf("Close %s?", m.CloseConfirmIssueID)))
	content.WriteString("\n")

	// Issue title (truncated to fit on one line)
	// Content width is width - 6 (border 2 + padding 4), minus 2 for quotes
	maxTitleLen := width - 10
	if maxTitleLen < 20 {
		maxTitleLen = 20
	}
	title := m.CloseConfirmTitle
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-3] + "..."
	}
	content.WriteString(subtleStyle.Render(fmt.Sprintf("\"%s\"", title)))
	content.WriteString("\n\n")

	// Reason input with focus indicator
	inputLabel := "Reason (optional):"
	if m.CloseConfirmButtonFocus == 0 {
		inputLabel = "> " + inputLabel
	} else {
		inputLabel = "  " + inputLabel
	}
	content.WriteString(inputLabel + "\n")
	content.WriteString(m.CloseConfirmInput.View())
	content.WriteString("\n\n")

	// Interactive buttons
	confirmFocused := m.CloseConfirmButtonFocus == 1
	cancelFocused := m.CloseConfirmButtonFocus == 2
	confirmHovered := m.CloseConfirmButtonHover == 1
	cancelHovered := m.CloseConfirmButtonHover == 2

	confirmBtn := renderButton("Confirm", confirmFocused, confirmHovered, false)
	cancelBtn := renderButton("Cancel", cancelFocused, cancelHovered, false)

	content.WriteString(confirmBtn)
	content.WriteString("  ")
	content.WriteString(cancelBtn)
	content.WriteString("\n\n")

	// Shortcut hints
	content.WriteString(subtleStyle.Render("Tab:switch  Enter:confirm  Esc:cancel"))

	return wrapConfirmationModal(content.String(), width)
}

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

	// Icon: triangle, pink when active, subtle when inactive
	pinkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")) // Pink
	if m.SearchMode {
		sb.WriteString(pinkStyle.Render("▸"))
		sb.WriteString(" ")
	} else {
		sb.WriteString(subtleStyle.Render("▸ "))
	}

	// Render the textinput (includes cursor and query)
	if m.SearchMode {
		sb.WriteString(m.SearchInput.View())
	} else {
		// Not in search mode but have a query - show it without cursor
		sb.WriteString(m.SearchQuery)
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
	keys := helpStyle.Render(m.Keymap.FooterHelp())

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

// renderTDQHelp renders the TDQ query language help overlay
func (m Model) renderTDQHelp() string {
	return helpStyle.Render(m.Keymap.GenerateTDQHelp())
}

// wrapPanel wraps content in a panel with title and border
func (m Model) wrapPanel(title, content string, height int, panel Panel) string {
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

// formatActivityItem formats a single activity item
func (m Model) formatActivityItem(item ActivityItem) string {
	timestamp := timestampStyle.Render(item.Timestamp.Format("15:04"))
	session := subtleStyle.Render(truncateSession(item.SessionID))
	badge := formatActivityBadge(item.Type)
	issueID := ""
	if item.IssueID != "" {
		issueID = titleStyle.Render(item.IssueID) + " "
	}

	// Calculate fixed width: timestamp + session + badge + issueID + spaces + border
	fixedWidth := lipgloss.Width(timestamp) + lipgloss.Width(session) + lipgloss.Width(badge) + lipgloss.Width(issueID) + 4 + 4 // 4 spaces + 4 border

	// Calculate available space for message + title
	availableWidth := m.Width - fixedWidth
	msgWidth := lipgloss.Width(item.Message)

	// Format title with remaining space after message
	title := ""
	if item.IssueTitle != "" {
		separatorWidth := 3 // " | "
		minTitleWidth := 10 // minimum useful title width
		remainingWidth := availableWidth - msgWidth - separatorWidth

		if remainingWidth >= minTitleWidth {
			// Enough space - use full message, fill rest with title
			title = " " + subtleStyle.Render("| "+truncateString(item.IssueTitle, remainingWidth))
			return fmt.Sprintf("%s %s %s %s%s%s", timestamp, session, badge, issueID, item.Message, title)
		}
		// Not enough space - truncate message to make room for title
		titleWidth := minTitleWidth + separatorWidth
		msg := truncateString(item.Message, availableWidth-titleWidth)
		title = " " + subtleStyle.Render("| "+truncateString(item.IssueTitle, minTitleWidth))
		return fmt.Sprintf("%s %s %s %s%s%s", timestamp, session, badge, issueID, msg, title)
	}

	// No title - give all space to message
	msg := truncateString(item.Message, availableWidth)
	return fmt.Sprintf("%s %s %s %s%s", timestamp, session, badge, issueID, msg)
}

// visibleItems calculates how many items can be shown given scroll offset and height
func (m Model) visibleItems(total, offset, height int) int {
	remaining := total - offset
	if remaining > height {
		return height
	}
	return remaining
}

// truncateString truncates a string to maxLen with ellipsis
func truncateString(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	if lipgloss.Width(s) <= maxLen {
		return s
	}
	// Simple truncation - could be improved for multi-byte chars
	if len(s) > maxLen-3 {
		return s[:maxLen-3] + "..."
	}
	return s
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
