package monitor

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	if len(m.TaskListRows) == 0 {
		return -1
	}

	offset := m.ScrollOffset[PanelTaskList]
	height := m.visibleHeightForPanel(PanelTaskList)

	// Account for "▲ more above" indicator
	linePos := 0
	if offset > 0 {
		if relY == 0 {
			return -1 // Clicked on scroll indicator
		}
		linePos = 1
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
		if linePos > height {
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

	// "IN PROGRESS:" section header (blank line + header = 2 lines)
	if inProgressCount > 0 {
		// Only show header if we're past offset or at start
		if rowIdx >= offset || (m.FocusedIssue != nil && offset == 0) {
			// Blank line
			if relY == linePos {
				return -1 // clicked on blank line
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

// hitTestActivityRow maps a y position to an Activity index
func (m Model) hitTestActivityRow(relY int) int {
	if len(m.Activity) == 0 {
		return -1
	}

	offset := m.ScrollOffset[PanelActivity]

	// Account for "▲ more above" indicator
	linePos := 0
	if offset > 0 {
		if relY == 0 {
			return -1
		}
		linePos = 1
	}

	// Simple 1:1 mapping for activity (no headers)
	rowIdx := relY - linePos + offset
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
	// Order: Reviewable, Ready, Blocked, Closed (matches display order)
	for _, issue := range m.TaskList.Reviewable {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: issue, Category: CategoryReviewable})
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
	m.ensureCursorVisible(PanelCurrentWork)
	m.ensureCursorVisible(PanelTaskList)
	m.ensureCursorVisible(PanelActivity)
}

// clampCursor ensures cursor is within valid bounds for a panel
func (m *Model) clampCursor(panel Panel) {
	count := m.rowCount(panel)
	if count == 0 {
		m.Cursor[panel] = 0
		return
	}
	if m.Cursor[panel] >= count {
		m.Cursor[panel] = count - 1
	}
	if m.Cursor[panel] < 0 {
		m.Cursor[panel] = 0
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
		// After scrolling, "▲ more above" will appear taking 1 line,
		// so we need to scroll 1 extra to compensate
		newOffset := cursor - effectiveHeight + 1
		if offset == 0 && newOffset > 0 {
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
	var panelHeight int
	switch panel {
	case PanelCurrentWork:
		panelHeight = int(float64(availableHeight) * m.PaneHeights[0])
	case PanelTaskList:
		panelHeight = int(float64(availableHeight) * m.PaneHeights[1])
	case PanelActivity:
		panelHeight = int(float64(availableHeight) * m.PaneHeights[2])
	default:
		panelHeight = availableHeight / 3
	}

	// Account for: title (1) + border (2) + scroll indicators (2)
	// Scroll indicators: "▲ more above" and "▼ more below" each take 1 line
	// when the list is scrollable. Reserve space for both to ensure cursor
	// stays visible during navigation.
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
		}
	}

	// Handle left-click in modal for section selection
	if m.ModalOpen() && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		return m.handleModalClick(msg.X, msg.Y)
	}

	// Ignore other mouse events when modals/overlays are open
	if m.ModalOpen() || m.StatsOpen || m.HandoffsOpen || m.ConfirmOpen || m.ShowHelp || m.ShowTDQHelp {
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

	// Update scroll offset
	newOffset := m.ScrollOffset[panel] + delta
	if newOffset < 0 {
		newOffset = 0
	}

	// Calculate max offset to allow scrolling to show all content
	// For TaskList, we need to account for category headers taking extra lines
	maxOffset := count - 1 // Allow scrolling until last item is at top
	if newOffset > maxOffset {
		newOffset = maxOffset
	}

	m.ScrollOffset[panel] = newOffset

	// NOTE: We intentionally do NOT call ensureCursorVisible here.
	// Mouse scrolling should scroll the view independently of the cursor.
	// The cursor can temporarily be off-screen; the user can use keyboard
	// or click to re-select a visible item.

	return m, nil
}

// maxScrollOffset returns the maximum valid scroll offset for a panel
func (m Model) maxScrollOffset(panel Panel) int {
	count := m.rowCount(panel)
	visibleHeight := m.visibleHeightForPanel(panel)

	if panel == PanelTaskList {
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
		m.ensureCursorVisible(panel)
	}

	// Select the clicked row
	if row >= 0 && row != m.Cursor[panel] {
		m.Cursor[panel] = row
		m.saveSelectedID(panel)
		m.ensureCursorVisible(panel)
	}

	// Double-click opens issue details
	if isDoubleClick {
		return m.openModal()
	}

	return m, nil
}
