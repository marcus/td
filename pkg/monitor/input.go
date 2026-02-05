package monitor

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/td/internal/config"
)

// HitTestPanel returns which panel contains the point (x, y), or -1 if none
func (m Model) HitTestPanel(x, y int) Panel {
	for panel, bounds := range m.PanelBounds {
		if bounds.Contains(x, y) {
			return panel
		}
	}
	return -1
}

// HitTestRow returns the row index within a panel for a given y coordinate, or -1 if none.
// Accounts for scroll indicators, category headers, and separator lines.
func (m Model) HitTestRow(panel Panel, y int) int {
	bounds, ok := m.PanelBounds[panel]
	if !ok {
		return -1
	}

	// Content starts after top border (1 line) and title (1 line)
	contentY := bounds.Y + 2
	if y < contentY {
		return -1
	}

	// Calculate relative position within content area
	relY := y - contentY

	// Delegate to panel-specific hit testing
	switch panel {
	case PanelTaskList:
		return m.hitTestTaskListRow(relY)
	case PanelCurrentWork:
		return m.hitTestCurrentWorkRow(relY)
	case PanelActivity:
		return m.hitTestActivityRow(relY)
	}
	return -1
}

// hitTestTaskListRow maps a y position to a TaskListRows index, accounting for headers
func (m Model) hitTestTaskListRow(relY int) int {
	// In board mode, use simpler 1:1 mapping (no category headers)
	if m.TaskListMode == TaskListModeBoard {
		return m.hitTestBoardRow(relY)
	}

	if len(m.TaskListRows) == 0 {
		return -1
	}

	offset := m.ScrollOffset[PanelTaskList]
	totalRows := len(m.TaskListRows)

	// Calculate maxLines the same way as renderTaskListPanel
	bounds := m.PanelBounds[PanelTaskList]
	maxLines := bounds.H - 3 // Account for title + border (matches view)

	// Determine scroll indicators needed BEFORE clamping (matches view logic)
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

	// Clamp offset (matches view logic)
	if offset > totalRows-effectiveMaxLines && totalRows > effectiveMaxLines {
		offset = totalRows - effectiveMaxLines
	}
	if offset < 0 {
		offset = 0
	}

	// Recalculate indicators after clamping (matches view logic)
	showUpIndicator = needsScroll && offset > 0
	effectiveMaxLines = maxLines
	if showUpIndicator {
		effectiveMaxLines--
	}
	hasBottomIndicator := needsScroll && offset+effectiveMaxLines < totalRows
	if hasBottomIndicator {
		effectiveMaxLines--
	}

	// Account for "▲ more above" indicator
	linePos := 0
	if showUpIndicator {
		if relY == 0 {
			return -1 // Clicked on scroll indicator
		}
		linePos = 1
	}

	// If clicking on the bottom indicator line, return -1
	if hasBottomIndicator && relY >= maxLines-1 {
		return -1 // Clicked on bottom scroll indicator
	}

	// Walk through visible rows, tracking line position
	var currentCategory TaskListCategory
	if offset > 0 && offset <= len(m.TaskListRows) {
		currentCategory = m.TaskListRows[offset-1].Category
	}

	for i := offset; i < len(m.TaskListRows); i++ {
		row := m.TaskListRows[i]

		// Category header takes lines
		if row.Category != currentCategory {
			if i > offset {
				linePos++ // Blank separator line
			}
			if relY == linePos {
				return -1 // Clicked on header
			}
			linePos++ // Header line
			currentCategory = row.Category
		}

		// Check if this row matches
		if relY == linePos {
			return i
		}
		linePos++

		// Stop if we've gone past visible area
		if linePos >= maxLines {
			break
		}
	}

	return -1
}

// hitTestBoardRow maps a y position to a board row index
// Handles both swimlanes view (with category headers) and backlog view (simple 1:1)
func (m Model) hitTestBoardRow(relY int) int {
	if m.BoardMode.ViewMode == BoardViewSwimlanes {
		return m.hitTestSwimlaneRow(relY)
	}
	return m.hitTestBacklogRow(relY)
}

// hitTestBacklogRow maps a y position to a BoardMode.Issues index (simple 1:1 mapping)
func (m Model) hitTestBacklogRow(relY int) int {
	if len(m.BoardMode.Issues) == 0 {
		return -1
	}

	offset := m.BoardMode.ScrollOffset
	totalRows := len(m.BoardMode.Issues)

	// Calculate maxLines same as renderTaskListBoardView
	bounds := m.PanelBounds[PanelTaskList]
	maxLines := bounds.H - 3 // Account for title + border

	// Determine scroll indicators
	needsScroll := totalRows > maxLines
	showUpIndicator := needsScroll && offset > 0

	// Account for scroll indicator at top
	linePos := 0
	if showUpIndicator {
		if relY == 0 {
			return -1 // Clicked on scroll indicator
		}
		linePos = 1
	}

	// Simple 1:1 mapping for backlog rows (no category headers)
	rowIdx := relY - linePos + offset
	if rowIdx >= 0 && rowIdx < totalRows {
		return rowIdx
	}
	return -1
}

// hitTestSwimlaneRow maps a y position to a BoardMode.SwimlaneRows index
// Accounts for category headers and separator lines (matches renderBoardSwimlanesView)
func (m Model) hitTestSwimlaneRow(relY int) int {
	if len(m.BoardMode.SwimlaneRows) == 0 {
		return -1
	}

	offset := m.BoardMode.SwimlaneScroll
	totalRows := len(m.BoardMode.SwimlaneRows)

	// Calculate maxLines same as renderBoardSwimlanesView
	bounds := m.PanelBounds[PanelTaskList]
	maxLines := bounds.H - 3 // Account for title + border

	// Determine scroll indicators (matches view logic)
	needsScroll := totalRows > maxLines
	showUpIndicator := needsScroll && offset > 0

	// Calculate effective maxLines with indicators
	effectiveMaxLines := maxLines
	if showUpIndicator {
		effectiveMaxLines--
	}
	if needsScroll && offset+effectiveMaxLines < totalRows {
		effectiveMaxLines--
	}

	// Clamp offset (matches view logic)
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
	hasBottomIndicator := needsScroll && offset+effectiveMaxLines < totalRows
	if hasBottomIndicator {
		effectiveMaxLines--
	}

	// Account for "▲ more above" indicator
	linePos := 0
	if showUpIndicator {
		if relY == 0 {
			return -1 // Clicked on scroll indicator
		}
		linePos = 1
	}

	// If clicking on the bottom indicator line, return -1
	if hasBottomIndicator && relY >= maxLines-1 {
		return -1 // Clicked on bottom scroll indicator
	}

	// Walk through visible rows, tracking line position (matches view rendering)
	var currentCategory TaskListCategory
	if offset > 0 && offset <= len(m.BoardMode.SwimlaneRows) {
		currentCategory = m.BoardMode.SwimlaneRows[offset-1].Category
	}

	for i := offset; i < len(m.BoardMode.SwimlaneRows); i++ {
		row := m.BoardMode.SwimlaneRows[i]

		// Category header takes lines (blank separator + header)
		if row.Category != currentCategory {
			if i > offset {
				linePos++ // Blank separator line
			}
			if relY == linePos {
				return -1 // Clicked on header
			}
			linePos++ // Header line
			currentCategory = row.Category
		}

		// Check if this row matches
		if relY == linePos {
			return i
		}
		linePos++

		// Stop if we've gone past visible area
		if linePos >= maxLines {
			break
		}
	}

	return -1
}

// hitTestCurrentWorkRow maps a y position to a CurrentWorkRows index
// Accounts for blank line + "IN PROGRESS:" header between focused and in-progress sections
func (m Model) hitTestCurrentWorkRow(relY int) int {
	if len(m.CurrentWorkRows) == 0 {
		return -1
	}

	offset := m.ScrollOffset[PanelCurrentWork]

	// Account for "▲ more above" indicator
	linePos := 0
	if offset > 0 {
		if relY == 0 {
			return -1
		}
		linePos = 1
	}

	// Count in-progress issues (excluding focused if duplicate)
	inProgressCount := len(m.InProgress)
	if m.FocusedIssue != nil {
		for _, issue := range m.InProgress {
			if issue.ID == m.FocusedIssue.ID {
				inProgressCount--
				break
			}
		}
	}

	// Track position through the panel layout
	rowIdx := 0

	// Focused issue row (if present)
	if m.FocusedIssue != nil {
		if rowIdx >= offset {
			if relY == linePos {
				return rowIdx
			}
			linePos++
		}
		rowIdx++
	}

	// "IN PROGRESS:" section header (blank line + margin-top + header = 3 lines)
	// Note: sectionHeader style has MarginTop(1), adding an extra blank line
	if inProgressCount > 0 {
		// Only show header if we're past offset or at start
		if rowIdx >= offset || (m.FocusedIssue != nil && offset == 0) {
			// Blank line (explicit \n in renderCurrentWorkPanel)
			if relY == linePos {
				return -1 // clicked on blank line
			}
			linePos++
			// Additional blank line from sectionHeader's MarginTop(1)
			if relY == linePos {
				return -1 // clicked on margin-top blank line
			}
			linePos++
			// Header line
			if relY == linePos {
				return -1 // clicked on header
			}
			linePos++
		}

		// In-progress issue rows
		for i := 0; i < inProgressCount; i++ {
			if rowIdx >= offset {
				if relY == linePos {
					return rowIdx
				}
				linePos++
			}
			rowIdx++
		}
	}

	return -1
}

// hitTestActivityRow maps a y position to an Activity index.
// Account for table header row(s) at top of content area.
func (m Model) hitTestActivityRow(relY int) int {
	if len(m.Activity) == 0 {
		return -1
	}

	bounds, ok := m.PanelBounds[PanelActivity]
	if !ok {
		return -1
	}

	// relY is relative to panel content area (after panel title/border)
	// The lipgloss table with BorderHeader(false) and hidden borders renders:
	// - Row 0: Header row ("Time", "Sess", etc.)
	// - Row 1+: Data rows (no separator line when borders are hidden)
	const tableHeaderRows = activityTableHeaderRows

	layout := activityTableMetrics(bounds.H)
	tableHeight := layout.tableHeight
	dataRowsVisible := layout.dataRowsVisible

	offset := m.ScrollOffset[PanelActivity]
	maxOffset := len(m.Activity) - dataRowsVisible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}

	// Ignore the scroll indicator / padding line below the table.
	if relY >= tableHeight {
		return -1
	}

	if relY < tableHeaderRows {
		return -1 // Click on table header area - no selection
	}

	// Convert to data row index
	dataRowY := relY - tableHeaderRows
	rowIdx := dataRowY + offset

	if rowIdx >= 0 && rowIdx < len(m.Activity) {
		return rowIdx
	}
	return -1
}

// HitTestDivider returns which divider (0 or 1) contains the point, or -1 if none
func (m Model) HitTestDivider(x, y int) int {
	for i, bounds := range m.DividerBounds {
		if bounds.Contains(x, y) {
			return i
		}
	}
	return -1
}

// buildCurrentWorkRows builds the flattened list of current work panel rows
func (m *Model) buildCurrentWorkRows() {
	m.CurrentWorkRows = nil
	if m.FocusedIssue != nil {
		m.CurrentWorkRows = append(m.CurrentWorkRows, m.FocusedIssue.ID)
	}
	for _, issue := range m.InProgress {
		// Skip focused issue if it's also in progress (avoid duplicate)
		if m.FocusedIssue != nil && issue.ID == m.FocusedIssue.ID {
			continue
		}
		m.CurrentWorkRows = append(m.CurrentWorkRows, issue.ID)
	}
}

// buildTaskListRows builds the flattened list of task list rows with category metadata
func (m *Model) buildTaskListRows() {
	m.TaskListRows = nil
	// Order: Reviewable, NeedsRework, Ready, Blocked, Closed (matches display order)
	for _, issue := range m.TaskList.Reviewable {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: issue, Category: CategoryReviewable})
	}
	for _, issue := range m.TaskList.NeedsRework {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: issue, Category: CategoryNeedsRework})
	}
	for _, issue := range m.TaskList.Ready {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: issue, Category: CategoryReady})
	}
	for _, issue := range m.TaskList.Blocked {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: issue, Category: CategoryBlocked})
	}
	for _, issue := range m.TaskList.Closed {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: issue, Category: CategoryClosed})
	}
}

// restoreCursors restores cursor positions from saved issue IDs after data refresh
func (m *Model) restoreCursors() {
	// Current Work panel
	if savedID := m.SelectedID[PanelCurrentWork]; savedID != "" {
		found := false
		for i, id := range m.CurrentWorkRows {
			if id == savedID {
				m.Cursor[PanelCurrentWork] = i
				found = true
				break
			}
		}
		if !found {
			m.clampCursor(PanelCurrentWork)
		}
	} else {
		m.clampCursor(PanelCurrentWork)
	}

	// Task List panel
	if savedID := m.SelectedID[PanelTaskList]; savedID != "" {
		found := false
		for i, row := range m.TaskListRows {
			if row.Issue.ID == savedID {
				m.Cursor[PanelTaskList] = i
				found = true
				break
			}
		}
		if !found {
			m.clampCursor(PanelTaskList)
		}
	} else {
		m.clampCursor(PanelTaskList)
	}

	// Activity panel
	m.clampCursor(PanelActivity)

	// Ensure scroll offsets keep the cursor visible after refresh
	// UNLESS user has independently scrolled the viewport with mouse wheel
	if !m.ScrollIndependent[PanelCurrentWork] {
		m.ensureCursorVisible(PanelCurrentWork)
	}
	if !m.ScrollIndependent[PanelTaskList] {
		m.ensureCursorVisible(PanelTaskList)
	}
	if !m.ScrollIndependent[PanelActivity] {
		m.ensureCursorVisible(PanelActivity)
	}
}

// clampCursor ensures cursor is within valid bounds for a panel
func (m *Model) clampCursor(panel Panel) {
	count := m.rowCount(panel)
	if count == 0 {
		m.Cursor[panel] = 0
		if m.ScrollIndependent != nil {
			m.ScrollIndependent[panel] = false
		}
		return
	}
	if m.Cursor[panel] >= count {
		m.Cursor[panel] = count - 1
		if m.ScrollIndependent != nil {
			m.ScrollIndependent[panel] = false
		}
	}
	if m.Cursor[panel] < 0 {
		m.Cursor[panel] = 0
		if m.ScrollIndependent != nil {
			m.ScrollIndependent[panel] = false
		}
	}
}

// rowCount returns the number of selectable rows in a panel
func (m Model) rowCount(panel Panel) int {
	switch panel {
	case PanelCurrentWork:
		return len(m.CurrentWorkRows)
	case PanelActivity:
		return len(m.Activity)
	case PanelTaskList:
		if m.TaskListMode == TaskListModeBoard {
			if m.BoardMode.ViewMode == BoardViewSwimlanes {
				return len(m.BoardMode.SwimlaneRows)
			}
			return len(m.BoardMode.Issues)
		}
		return len(m.TaskListRows)
	}
	return 0
}

// moveCursor moves the cursor in the active panel by delta, clamping to bounds
func (m *Model) moveCursor(delta int) {
	panel := m.ActivePanel
	count := m.rowCount(panel)
	if count == 0 {
		return
	}

	newPos := m.Cursor[panel] + delta
	if newPos < 0 {
		newPos = 0
	}
	if newPos >= count {
		newPos = count - 1
	}
	m.Cursor[panel] = newPos
	if m.ScrollIndependent != nil {
		m.ScrollIndependent[panel] = false
	}

	// Save the selected issue ID for persistence across refresh
	m.saveSelectedID(panel)

	// Auto-scroll viewport to keep cursor visible
	m.ensureCursorVisible(panel)
}

// ensureCursorVisible adjusts ScrollOffset to keep cursor in viewport
func (m *Model) ensureCursorVisible(panel Panel) {
	cursor := m.Cursor[panel]
	offset := m.ScrollOffset[panel]
	visibleHeight := m.visibleHeightForPanel(panel)

	if visibleHeight <= 0 {
		return
	}

	// Calculate effective height accounting for dynamic factors
	effectiveHeight := visibleHeight

	// For task list panel, account for category header lines
	if panel == PanelTaskList {
		headerLines := m.categoryHeaderLinesBetween(offset, cursor)
		effectiveHeight -= headerLines
	}

	if effectiveHeight < 1 {
		effectiveHeight = 1
	}

	// Scroll down if cursor below viewport
	if cursor >= offset+effectiveHeight {
		newOffset := cursor - effectiveHeight + 1
		// For panels with scroll indicators (not activity panel),
		// "▲ more above" will appear taking 1 line, so scroll 1 extra
		if panel != PanelActivity && offset == 0 && newOffset > 0 {
			newOffset++ // Compensate for "more above" indicator appearing
		}
		m.ScrollOffset[panel] = newOffset
	}
	// Scroll up if cursor above viewport
	if cursor < offset {
		m.ScrollOffset[panel] = cursor
	}

	// Clamp scroll offset to valid range
	maxOffset := m.maxScrollOffset(panel)
	if m.ScrollOffset[panel] > maxOffset {
		m.ScrollOffset[panel] = maxOffset
	}
	if m.ScrollOffset[panel] < 0 {
		m.ScrollOffset[panel] = 0
	}
}

// categoryHeaderLinesBetween counts how many lines are consumed by category
// headers between two row indices in TaskListRows. Each category transition
// adds a blank line (if not first) plus a header line.
func (m Model) categoryHeaderLinesBetween(startIdx, endIdx int) int {
	if len(m.TaskListRows) == 0 || startIdx >= endIdx {
		return 0
	}
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > len(m.TaskListRows) {
		endIdx = len(m.TaskListRows)
	}

	lines := 0
	var currentCategory TaskListCategory
	if startIdx > 0 {
		currentCategory = m.TaskListRows[startIdx-1].Category
	}

	for i := startIdx; i < endIdx; i++ {
		row := m.TaskListRows[i]
		if row.Category != currentCategory {
			if i > startIdx || startIdx > 0 {
				lines++ // blank line before category (except first visible)
			}
			lines++ // header line
			currentCategory = row.Category
		}
	}
	return lines
}

// visibleHeightForPanel calculates visible rows for a panel
func (m Model) visibleHeightForPanel(panel Panel) int {
	if m.Height == 0 {
		return 10 // Default fallback
	}

	// Match calculation from renderView()
	searchBarHeight := 0
	if m.SearchMode || m.SearchQuery != "" {
		searchBarHeight = 2
	}
	footerHeight := 3
	if m.Embedded {
		footerHeight = 0
	}
	availableHeight := m.Height - footerHeight - searchBarHeight

	// Get panel height based on dynamic pane ratios
	// IMPORTANT: Must match renderView() calculation exactly, including rounding behavior
	var panelHeight int
	panel0 := int(float64(availableHeight) * m.PaneHeights[0])
	panel1 := int(float64(availableHeight) * m.PaneHeights[1])
	switch panel {
	case PanelCurrentWork:
		panelHeight = panel0
	case PanelTaskList:
		panelHeight = panel1
	case PanelActivity:
		// Activity panel absorbs rounding errors (matches renderView)
		panelHeight = availableHeight - panel0 - panel1
	default:
		panelHeight = availableHeight / 3
	}

	if panel == PanelActivity {
		return activityTableMetrics(panelHeight).dataRowsVisible
	}

	// Account for panel chrome on non-activity panels:
	// - title (1) + border (2) = 3 lines base overhead
	// - scroll indicators (2) = 5 total
	return panelHeight - 5
}

// saveSelectedID saves the currently selected issue ID for a panel
func (m *Model) saveSelectedID(panel Panel) {
	switch panel {
	case PanelCurrentWork:
		if m.Cursor[panel] < len(m.CurrentWorkRows) {
			m.SelectedID[panel] = m.CurrentWorkRows[m.Cursor[panel]]
		}
	case PanelTaskList:
		if m.Cursor[panel] < len(m.TaskListRows) {
			m.SelectedID[panel] = m.TaskListRows[m.Cursor[panel]].Issue.ID
		}
	case PanelActivity:
		if m.Cursor[panel] < len(m.Activity) && m.Activity[m.Cursor[panel]].IssueID != "" {
			m.SelectedID[panel] = m.Activity[m.Cursor[panel]].IssueID
		}
	}
}

// SelectedIssueID returns the issue ID of the currently selected row in a panel
func (m Model) SelectedIssueID(panel Panel) string {
	switch panel {
	case PanelCurrentWork:
		if m.Cursor[panel] < len(m.CurrentWorkRows) {
			return m.CurrentWorkRows[m.Cursor[panel]]
		}
	case PanelTaskList:
		if m.TaskListMode == TaskListModeBoard {
			if m.BoardMode.ViewMode == BoardViewSwimlanes {
				cursor := m.BoardMode.SwimlaneCursor
				if cursor >= 0 && cursor < len(m.BoardMode.SwimlaneRows) {
					return m.BoardMode.SwimlaneRows[cursor].Issue.ID
				}
			} else {
				cursor := m.BoardMode.Cursor
				if cursor >= 0 && cursor < len(m.BoardMode.Issues) {
					return m.BoardMode.Issues[cursor].Issue.ID
				}
			}
			return ""
		}
		if m.Cursor[panel] < len(m.TaskListRows) {
			return m.TaskListRows[m.Cursor[panel]].Issue.ID
		}
	case PanelActivity:
		if m.Cursor[panel] < len(m.Activity) {
			return m.Activity[m.Cursor[panel]].IssueID
		}
	}
	return ""
}

// updatePanelBounds recalculates panel bounds based on current dimensions.
// Called when window size changes to enable accurate mouse hit-testing.
func (m *Model) updatePanelBounds() {
	if m.Width == 0 || m.Height == 0 {
		return
	}

	// Match layout calculation from renderView()
	searchBarHeight := 0
	if m.SearchMode || m.SearchQuery != "" {
		searchBarHeight = 2
	}
	footerHeight := 3
	if m.Embedded {
		footerHeight = 0
	}
	availableHeight := m.Height - footerHeight - searchBarHeight

	// Calculate panel heights from ratios
	panelHeights := [3]int{
		int(float64(availableHeight) * m.PaneHeights[0]),
		int(float64(availableHeight) * m.PaneHeights[1]),
		int(float64(availableHeight) * m.PaneHeights[2]),
	}
	// Adjust last panel to absorb rounding errors
	panelHeights[2] = availableHeight - panelHeights[0] - panelHeights[1]

	// Calculate Y positions for each panel (stacked vertically)
	// Order: search bar (optional) → Current Work → Task List → Activity → footer
	y := searchBarHeight

	m.PanelBounds[PanelCurrentWork] = Rect{X: 0, Y: y, W: m.Width, H: panelHeights[0]}
	y += panelHeights[0]

	// First divider (between Current Work and Task List)
	// 3px hit region centered on the border
	m.DividerBounds[0] = Rect{X: 0, Y: y - 1, W: m.Width, H: 3}

	m.PanelBounds[PanelTaskList] = Rect{X: 0, Y: y, W: m.Width, H: panelHeights[1]}
	y += panelHeights[1]

	// Second divider (between Task List and Activity)
	m.DividerBounds[1] = Rect{X: 0, Y: y - 1, W: m.Width, H: 3}

	m.PanelBounds[PanelActivity] = Rect{X: 0, Y: y, W: m.Width, H: panelHeights[2]}
}

// handleMouse processes mouse events for panel selection and row clicking
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Handle mouse wheel scroll in modals/overlays
	if msg.Action == tea.MouseActionPress {
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			delta := 3
			if msg.Button == tea.MouseButtonWheelUp {
				delta = -3
			}

			// Route scroll to help modal
			if m.HelpOpen {
				m.HelpScroll += delta
				m.clampHelpScroll()
				return m, nil
			}

			// Route scroll to getting started modal - just ignore scroll
			if m.GettingStartedOpen {
				return m, nil
			}

			// Route scroll to sync prompt modal - just ignore scroll
			if m.SyncPromptOpen {
				return m, nil
			}

			// Route scroll to appropriate modal
			if modal := m.CurrentModal(); modal != nil {
				// Mouse wheel always scrolls modal content (use j/k for task list navigation)
				modal.Scroll += delta
				if modal.Scroll < 0 {
					modal.Scroll = 0
				}
				maxScroll := m.modalMaxScroll(modal)
				if modal.Scroll > maxScroll {
					modal.Scroll = maxScroll
				}
				return m, nil
			}

			if m.StatsOpen {
				m.StatsScroll += delta
				if m.StatsScroll < 0 {
					m.StatsScroll = 0
				}
				return m, nil
			}

			if m.HandoffsOpen {
				m.HandoffsScroll += delta
				if m.HandoffsScroll < 0 {
					m.HandoffsScroll = 0
				}
				return m, nil
			}

			if m.BoardPickerOpen {
				// Route scroll to declarative modal if available
				if m.BoardPickerModal != nil && m.BoardPickerMouseHandler != nil {
					_ = m.BoardPickerModal.HandleMouse(msg, m.BoardPickerMouseHandler)
				}
				// During loading, ignore scroll
				return m, nil
			}
		}
	}

	// Handle Sync Prompt modal mouse events (declarative modal)
	if m.SyncPromptOpen && m.SyncPromptModal != nil && m.SyncPromptMouse != nil {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			action := m.SyncPromptModal.HandleMouse(msg, m.SyncPromptMouse)
			if action != "" {
				return m, m.handleSyncPromptAction(action)
			}
			return m, nil
		}
		// Handle motion for hover states
		if msg.Action == tea.MouseActionMotion {
			_ = m.SyncPromptModal.HandleMouse(msg, m.SyncPromptMouse)
			return m, nil
		}
	}

	// Handle Getting Started modal mouse events (declarative modal)
	if m.GettingStartedOpen && m.GettingStartedModal != nil && m.GettingStartedMouseHandler != nil {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			action := m.GettingStartedModal.HandleMouse(msg, m.GettingStartedMouseHandler)
			if action != "" {
				return m.handleGettingStartedAction(action)
			}
			return m, nil
		}
		// Handle motion for hover states
		if msg.Action == tea.MouseActionMotion {
			_ = m.GettingStartedModal.HandleMouse(msg, m.GettingStartedMouseHandler)
			return m, nil
		}
	}

	// Handle TDQ Help modal mouse events (declarative modal)
	if m.ShowTDQHelp && m.TDQHelpModal != nil && m.TDQHelpMouseHandler != nil {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			action := m.TDQHelpModal.HandleMouse(msg, m.TDQHelpMouseHandler)
			if action != "" {
				return m.handleTDQHelpAction(action)
			}
			return m, nil
		}
		// Handle motion for hover states
		if msg.Action == tea.MouseActionMotion {
			_ = m.TDQHelpModal.HandleMouse(msg, m.TDQHelpMouseHandler)
			return m, nil
		}
	}

	// Handle Stats modal mouse events (declarative modal)
	if m.StatsOpen && m.StatsModal != nil && m.StatsMouseHandler != nil && !m.StatsLoading && m.StatsError == nil {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			action := m.StatsModal.HandleMouse(msg, m.StatsMouseHandler)
			if action != "" {
				return m.handleStatsAction(action)
			}
			return m, nil
		}
		// Handle motion for hover states
		if msg.Action == tea.MouseActionMotion {
			_ = m.StatsModal.HandleMouse(msg, m.StatsMouseHandler)
			return m, nil
		}
	}

	// Handle Handoffs modal mouse events (declarative modal)
	if m.HandoffsOpen && m.HandoffsModal != nil && m.HandoffsMouseHandler != nil && !m.HandoffsLoading && m.HandoffsError == nil && len(m.HandoffsData) > 0 {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			action := m.HandoffsModal.HandleMouse(msg, m.HandoffsMouseHandler)
			if action != "" {
				return m.handleHandoffsAction(action)
			}
			return m, nil
		}
		// Handle motion for hover states
		if msg.Action == tea.MouseActionMotion {
			_ = m.HandoffsModal.HandleMouse(msg, m.HandoffsMouseHandler)
			return m, nil
		}
	}

	// Handle left-click in modal for section selection
	if m.ModalOpen() && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		return m.handleModalClick(msg.X, msg.Y)
	}

	// Handle Delete confirmation modal mouse events (declarative modal)
	if m.ConfirmOpen && m.DeleteConfirmModal != nil && m.DeleteConfirmMouseHandler != nil {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			action := m.DeleteConfirmModal.HandleMouse(msg, m.DeleteConfirmMouseHandler)
			if action != "" {
				return m.handleDeleteConfirmAction(action)
			}
			return m, nil
		}
		// Handle motion for hover states
		if msg.Action == tea.MouseActionMotion {
			_ = m.DeleteConfirmModal.HandleMouse(msg, m.DeleteConfirmMouseHandler)
			return m, nil
		}
	}

	// Handle Close confirmation modal mouse events (declarative modal)
	if m.CloseConfirmOpen && m.CloseConfirmModal != nil && m.CloseConfirmMouseHandler != nil {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			action := m.CloseConfirmModal.HandleMouse(msg, m.CloseConfirmMouseHandler)
			if action != "" {
				return m.handleCloseConfirmAction(action)
			}
			return m, nil
		}
		// Handle motion for hover states
		if msg.Action == tea.MouseActionMotion {
			_ = m.CloseConfirmModal.HandleMouse(msg, m.CloseConfirmMouseHandler)
			return m, nil
		}
	}
	if m.FormOpen && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		return m.handleFormDialogClick(msg.X, msg.Y)
	}

	// Handle Board editor modal mouse events (declarative modal)
	if m.BoardEditorOpen && m.BoardEditorModal != nil && m.BoardEditorMouseHandler != nil {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			action := m.BoardEditorModal.HandleMouse(msg, m.BoardEditorMouseHandler)
			if action != "" {
				return m.handleBoardEditorAction(action)
			}
			return m, nil
		}
		// Handle motion for hover states
		if msg.Action == tea.MouseActionMotion {
			_ = m.BoardEditorModal.HandleMouse(msg, m.BoardEditorMouseHandler)
			return m, nil
		}
	}

	// Handle Board picker modal mouse events (declarative modal)
	if m.BoardPickerOpen && m.BoardPickerModal != nil && m.BoardPickerMouseHandler != nil && len(m.AllBoards) > 0 {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			action := m.BoardPickerModal.HandleMouse(msg, m.BoardPickerMouseHandler)
			if action != "" {
				return m.handleBoardPickerAction(action)
			}
			return m, nil
		}
		// Handle motion for hover states
		if msg.Action == tea.MouseActionMotion {
			_ = m.BoardPickerModal.HandleMouse(msg, m.BoardPickerMouseHandler)
			return m, nil
		}
	}

	// Handle mouse motion for hover states on confirmation dialogs
	// Note: CloseConfirm hover is now handled by declarative modal above
	if m.FormOpen && msg.Action == tea.MouseActionMotion {
		return m.handleFormDialogHover(msg.X, msg.Y)
	}

	// Ignore other mouse events when modals/overlays are open
	if m.ModalOpen() || m.StatsOpen || m.HandoffsOpen || m.ConfirmOpen || m.CloseConfirmOpen || m.FormOpen || m.BoardPickerOpen || m.BoardEditorOpen || m.HelpOpen || m.ShowTDQHelp || m.GettingStartedOpen || m.SyncPromptOpen {
		return m, nil
	}

	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			// Check divider hit first (highest priority)
			divider := m.HitTestDivider(msg.X, msg.Y)
			if divider >= 0 {
				return m.startDividerDrag(divider, msg.Y)
			}
			return m.handleMouseClick(msg.X, msg.Y)
		}
		// Handle mouse wheel (reported as button press)
		if msg.Button == tea.MouseButtonWheelUp {
			return m.handleMouseWheel(msg.X, msg.Y, -3)
		}
		if msg.Button == tea.MouseButtonWheelDown {
			return m.handleMouseWheel(msg.X, msg.Y, 3)
		}

	case tea.MouseActionRelease:
		if m.DraggingDivider >= 0 {
			return m.endDividerDrag()
		}

	case tea.MouseActionMotion:
		// Handle divider dragging
		if m.DraggingDivider >= 0 {
			return m.updateDividerDrag(msg.Y)
		}

		// Track divider hover for visual feedback
		divider := m.HitTestDivider(msg.X, msg.Y)
		if divider != m.DividerHover {
			m.DividerHover = divider
		}

		// Track panel hover for visual feedback
		panel := m.HitTestPanel(msg.X, msg.Y)
		if panel != m.HoverPanel {
			m.HoverPanel = panel
		}
		return m, nil
	}

	return m, nil
}

// startDividerDrag begins dragging a divider
func (m Model) startDividerDrag(divider int, y int) (tea.Model, tea.Cmd) {
	m.DraggingDivider = divider
	m.DragStartY = y
	m.DragStartHeights = m.PaneHeights
	return m, nil
}

// updateDividerDrag updates pane heights during drag
func (m Model) updateDividerDrag(y int) (tea.Model, tea.Cmd) {
	if m.DraggingDivider < 0 {
		return m, nil
	}

	// Calculate available height
	searchBarHeight := 0
	if m.SearchMode || m.SearchQuery != "" {
		searchBarHeight = 2
	}
	footerHeight := 3
	if m.Embedded {
		footerHeight = 0
	}
	availableHeight := m.Height - footerHeight - searchBarHeight
	if availableHeight <= 0 {
		return m, nil // Terminal too small for resize
	}

	// Calculate delta as a ratio
	deltaY := y - m.DragStartY
	deltaRatio := float64(deltaY) / float64(availableHeight)

	// Get starting heights
	newHeights := m.DragStartHeights

	// Apply delta based on which divider is being dragged
	// Divider 0: between pane 0 and pane 1
	// Divider 1: between pane 1 and pane 2
	if m.DraggingDivider == 0 {
		// Moving divider 0 affects panes 0 and 1
		newHeights[0] = m.DragStartHeights[0] + deltaRatio
		newHeights[1] = m.DragStartHeights[1] - deltaRatio
	} else {
		// Moving divider 1 affects panes 1 and 2
		newHeights[1] = m.DragStartHeights[1] + deltaRatio
		newHeights[2] = m.DragStartHeights[2] - deltaRatio
	}

	// Enforce minimum 10% per pane (only check affected panes)
	const minHeight = 0.1
	p1, p2 := m.DraggingDivider, m.DraggingDivider+1
	for _, i := range []int{p1, p2} {
		if newHeights[i] < minHeight {
			deficit := minHeight - newHeights[i]
			newHeights[i] = minHeight
			// Take from the other affected pane
			other := p2
			if i == p2 {
				other = p1
			}
			newHeights[other] -= deficit
		}
	}

	// Re-check affected panes; abort if constraints can't be satisfied
	if newHeights[p1] < minHeight || newHeights[p2] < minHeight {
		return m, nil
	}

	// Normalize to ensure sum = 1.0
	sum := newHeights[0] + newHeights[1] + newHeights[2]
	for i := range newHeights {
		newHeights[i] /= sum
	}

	m.PaneHeights = newHeights
	m.updatePanelBounds()
	return m, nil
}

// endDividerDrag finishes dragging and persists the new heights
func (m Model) endDividerDrag() (tea.Model, tea.Cmd) {
	m.DraggingDivider = -1
	m.DividerHover = -1

	// Persist to config asynchronously
	return m, m.savePaneHeightsAsync()
}

// savePaneHeightsAsync returns a command that saves pane heights to config
func (m Model) savePaneHeightsAsync() tea.Cmd {
	heights := m.PaneHeights
	baseDir := m.BaseDir
	return func() tea.Msg {
		err := config.SetPaneHeights(baseDir, heights)
		return PaneHeightsSavedMsg{Error: err}
	}
}

// handleMouseWheel scrolls the panel under the cursor
func (m Model) handleMouseWheel(x, y, delta int) (tea.Model, tea.Cmd) {
	panel := m.HitTestPanel(x, y)
	if panel < 0 {
		return m, nil
	}

	// Scroll the hovered panel (better UX than requiring active panel)
	count := m.rowCount(panel)
	if count == 0 {
		return m, nil
	}

	// For Task List in board mode, use appropriate scroll offset based on view mode
	if panel == PanelTaskList && m.TaskListMode == TaskListModeBoard {
		if m.BoardMode.ViewMode == BoardViewSwimlanes {
			// Swimlanes view uses SwimlaneScroll
			newOffset := m.BoardMode.SwimlaneScroll + delta
			if newOffset < 0 {
				newOffset = 0
			}
			// Calculate max offset for swimlanes
			visibleHeight := m.visibleHeightForPanel(panel)
			maxOffset := len(m.BoardMode.SwimlaneRows) - visibleHeight
			if maxOffset < 0 {
				maxOffset = 0
			}
			if newOffset > maxOffset {
				newOffset = maxOffset
			}
			m.BoardMode.SwimlaneScroll = newOffset
		} else {
			// Backlog view uses ScrollOffset
			newOffset := m.BoardMode.ScrollOffset + delta
			if newOffset < 0 {
				newOffset = 0
			}
			maxOffset := m.maxScrollOffset(panel)
			if newOffset > maxOffset {
				newOffset = maxOffset
			}
			m.BoardMode.ScrollOffset = newOffset
		}
		return m, nil
	}

	// Update scroll offset
	newOffset := m.ScrollOffset[panel] + delta
	if newOffset < 0 {
		newOffset = 0
	}

	// Clamp to max valid offset (accounts for visible height and category headers)
	maxOffset := m.maxScrollOffset(panel)
	if newOffset > maxOffset {
		newOffset = maxOffset
	}

	m.ScrollOffset[panel] = newOffset
	m.ScrollIndependent[panel] = true

	// Keep cursor visible when mouse scrolling
	// This provides better UX than allowing cursor to go off-screen
	if m.Cursor != nil {
		visibleHeight := m.visibleHeightForPanel(panel)
		cursor := m.Cursor[panel]

		// If cursor went above viewport, move it to first visible row
		if cursor < newOffset {
			m.Cursor[panel] = newOffset
		}
		// If cursor went below viewport, move it to last visible row
		if cursor >= newOffset+visibleHeight {
			m.Cursor[panel] = newOffset + visibleHeight - 1
			if m.Cursor[panel] >= count {
				m.Cursor[panel] = count - 1
			}
		}
	}

	return m, nil
}

// maxScrollOffset returns the maximum valid scroll offset for a panel
func (m Model) maxScrollOffset(panel Panel) int {
	count := m.rowCount(panel)
	visibleHeight := m.visibleHeightForPanel(panel)

	if panel == PanelTaskList {
		// In board mode, use simple calculation (no category headers)
		if m.TaskListMode == TaskListModeBoard {
			maxOffset := count - visibleHeight
			if maxOffset < 0 {
				maxOffset = 0
			}
			return maxOffset
		}

		// TaskList has category headers that consume extra lines
		// Calculate total display lines including headers and separators
		totalLines := m.taskListTotalLines()
		if totalLines <= visibleHeight {
			return 0 // No scrolling needed
		}
		// Find the smallest offset such that content from offset to end
		// fits within visibleHeight. Walk backwards from the end.
		for offset := count - 1; offset >= 0; offset-- {
			linesFromOffset := m.taskListLinesFromOffset(offset)
			if linesFromOffset > visibleHeight {
				// Previous offset was the right one
				if offset+1 < count {
					return offset + 1
				}
				return offset
			}
		}
		return 0
	}

	// For other panels, simple calculation
	maxOffset := count - visibleHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	return maxOffset
}

// taskListTotalLines calculates total display lines for TaskList including headers
func (m Model) taskListTotalLines() int {
	if len(m.TaskListRows) == 0 {
		return 0
	}

	lines := 0
	var currentCategory TaskListCategory
	for i, row := range m.TaskListRows {
		if row.Category != currentCategory {
			if i > 0 {
				lines++ // Blank separator
			}
			lines++ // Category header
			currentCategory = row.Category
		}
		lines++ // The row itself
	}
	return lines
}

// taskListLinesFromOffset calculates display lines needed from a given offset to end
func (m Model) taskListLinesFromOffset(offset int) int {
	if len(m.TaskListRows) == 0 || offset >= len(m.TaskListRows) {
		return 0
	}

	lines := 0
	var currentCategory TaskListCategory
	// Track category from before offset
	if offset > 0 {
		currentCategory = m.TaskListRows[offset-1].Category
	}

	for i := offset; i < len(m.TaskListRows); i++ {
		row := m.TaskListRows[i]
		if row.Category != currentCategory {
			if i > offset || offset > 0 {
				lines++ // Blank separator (not before first visible if at offset 0)
			}
			lines++ // Category header
			currentCategory = row.Category
		}
		lines++ // The row itself
	}
	return lines
}

// handleMouseClick handles left-click events
func (m Model) handleMouseClick(x, y int) (tea.Model, tea.Cmd) {
	panel := m.HitTestPanel(x, y)
	if panel < 0 {
		return m, nil
	}

	row := m.HitTestRow(panel, y)
	now := time.Now()

	// Check for double-click (same panel+row within 400ms)
	isDoubleClick := panel == m.LastClickPanel &&
		row == m.LastClickRow &&
		row >= 0 &&
		now.Sub(m.LastClickTime) < 400*time.Millisecond

	// Update click tracking
	m.LastClickTime = now
	m.LastClickPanel = panel
	m.LastClickRow = row

	// Click on panel: activate it
	if m.ActivePanel != panel {
		m.ActivePanel = panel
		m.clampCursor(panel)
		m.ScrollIndependent[panel] = false
		m.ensureCursorVisible(panel)
	}

	// Select the clicked row
	if row >= 0 {
		// In board mode, update the appropriate cursor based on view mode
		if panel == PanelTaskList && m.TaskListMode == TaskListModeBoard {
			if m.BoardMode.ViewMode == BoardViewSwimlanes {
				// Swimlanes view uses SwimlaneCursor
				if row != m.BoardMode.SwimlaneCursor {
					m.BoardMode.SwimlaneCursor = row
					m.ensureSwimlaneCursorVisible()
				}
			} else {
				// Backlog view uses Cursor
				if row != m.BoardMode.Cursor {
					m.BoardMode.Cursor = row
				}
			}
		} else if row != m.Cursor[panel] {
			m.Cursor[panel] = row
			m.ScrollIndependent[panel] = false
			m.saveSelectedID(panel)
			m.ensureCursorVisible(panel)
		}
	}

	// Double-click opens issue details
	if isDoubleClick {
		// In board mode, use board-specific open
		if panel == PanelTaskList && m.TaskListMode == TaskListModeBoard {
			return m.openIssueFromBoard()
		}
		return m.openModal()
	}

	return m, nil
}

// Legacy handlers removed - close confirmation now uses declarative modal

// handleFormDialogClick handles mouse clicks on the form modal buttons
func (m Model) handleFormDialogClick(x, y int) (tea.Model, tea.Cmd) {
	if m.FormState == nil || m.FormState.Form == nil {
		return m, nil
	}

	modalWidth, modalHeight := m.formModalDimensions()
	formView := m.FormState.Form.View()
	formHeight := lipgloss.Height(formView)

	modalOuterWidth := modalWidth + 2
	modalOuterHeight := modalHeight + 2

	modalX := (m.Width - modalOuterWidth) / 2
	modalY := (m.Height - modalOuterHeight) / 2

	contentStartY := modalY + 2
	buttonRowY := contentStartY + formHeight + 1

	if y != buttonRowY {
		return m, nil
	}

	contentStartX := modalX + 3
	submitWidth := lipgloss.Width(renderButton("Submit", false, false, false))
	cancelWidth := lipgloss.Width(renderButton("Cancel", false, false, false))

	submitStartX := contentStartX
	submitEndX := submitStartX + submitWidth
	cancelStartX := submitEndX + 2
	cancelEndX := cancelStartX + cancelWidth

	if x >= submitStartX && x < submitEndX {
		return m.submitForm()
	}
	if x >= cancelStartX && x < cancelEndX {
		m.closeForm()
		return m, nil
	}

	return m, nil
}

// handleFormDialogHover handles mouse hover on the form modal buttons
func (m Model) handleFormDialogHover(x, y int) (tea.Model, tea.Cmd) {
	if m.FormState == nil || m.FormState.Form == nil {
		return m, nil
	}

	modalWidth, modalHeight := m.formModalDimensions()
	formView := m.FormState.Form.View()
	formHeight := lipgloss.Height(formView)

	modalOuterWidth := modalWidth + 2
	modalOuterHeight := modalHeight + 2

	modalX := (m.Width - modalOuterWidth) / 2
	modalY := (m.Height - modalOuterHeight) / 2

	contentStartY := modalY + 2
	buttonRowY := contentStartY + formHeight + 1

	m.FormState.ButtonHover = 0

	if y != buttonRowY {
		return m, nil
	}

	contentStartX := modalX + 3
	submitWidth := lipgloss.Width(renderButton("Submit", false, false, false))
	cancelWidth := lipgloss.Width(renderButton("Cancel", false, false, false))

	submitStartX := contentStartX
	submitEndX := submitStartX + submitWidth
	cancelStartX := submitEndX + 2
	cancelEndX := cancelStartX + cancelWidth

	if x >= submitStartX && x < submitEndX {
		m.FormState.ButtonHover = 1
	} else if x >= cancelStartX && x < cancelEndX {
		m.FormState.ButtonHover = 2
	}

	return m, nil
}

