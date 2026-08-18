package monitor

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/internal/models"
)

// kanbanColumnOrder defines the order of columns in the kanban view.
// This is derived from the board swimlane categories so that any future
// status additions are automatically included.
var kanbanColumnOrder = []TaskListCategory{
	CategoryReviewable,
	CategoryReadyToClose,
	CategoryNeedsRework,
	CategoryInProgress,
	CategoryReady,
	CategoryPendingReview,
	CategoryPendingOther,
	CategoryBlocked,
	CategoryClosed,
}

// kanbanColumnLabel returns the display label for a kanban column.
func kanbanColumnLabel(cat TaskListCategory) string {
	switch cat {
	case CategoryReviewable:
		return "REVIEW"
	case CategoryReadyToClose:
		return "TO CLOSE"
	case CategoryNeedsRework:
		return "REWORK"
	case CategoryInProgress:
		return "WIP"
	case CategoryReady:
		return "READY"
	case CategoryPendingReview:
		return "P.REVIEW"
	case CategoryPendingOther:
		return "P.OTHER"
	case CategoryBlocked:
		return "BLOCKED"
	case CategoryClosed:
		return "CLOSED"
	default:
		return string(cat)
	}
}

// kanbanPinnedColumns are always shown so a new or sparse board still
// has a Review / WIP / Ready shape instead of collapsing to one fat lane.
var kanbanPinnedColumns = []TaskListCategory{
	CategoryReviewable,
	CategoryInProgress,
	CategoryReady,
}

// visibleKanbanColumns returns pinned columns plus any other occupied
// lane, in canonical order. Empty extras (rework, ready-to-close, …)
// stay hidden so they do not steal width.
func visibleKanbanColumns(data TaskListData) []TaskListCategory {
	want := make(map[TaskListCategory]bool, len(kanbanColumnOrder))
	for _, cat := range kanbanPinnedColumns {
		want[cat] = true
	}
	for _, cat := range kanbanColumnOrder {
		if len(kanbanColumnIssues(data, cat)) > 0 {
			want[cat] = true
		}
	}
	visible := make([]TaskListCategory, 0, len(want))
	for _, cat := range kanbanColumnOrder {
		if want[cat] {
			visible = append(visible, cat)
		}
	}
	return visible
}

func kanbanColumnIndex(cat TaskListCategory) int {
	for i, c := range kanbanColumnOrder {
		if c == cat {
			return i
		}
	}
	return -1
}

func (m Model) visibleKanbanColIndexes() []int {
	visible := visibleKanbanColumns(m.BoardMode.SwimlaneData)
	indexes := make([]int, len(visible))
	for i, cat := range visible {
		indexes[i] = kanbanColumnIndex(cat)
	}
	return indexes
}

// kanbanColumnIssues returns the issues for a given category from the swimlane data.
func kanbanColumnIssues(data TaskListData, cat TaskListCategory) []models.Issue {
	switch cat {
	case CategoryReviewable:
		return data.Reviewable
	case CategoryReadyToClose:
		return data.ReadyToClose
	case CategoryNeedsRework:
		return data.NeedsRework
	case CategoryInProgress:
		return data.InProgress
	case CategoryReady:
		return data.Ready
	case CategoryPendingReview:
		return data.PendingReview
	case CategoryPendingOther:
		return data.PendingOther
	case CategoryBlocked:
		return data.Blocked
	case CategoryClosed:
		return data.Closed
	default:
		return nil
	}
}

// openKanbanView opens the kanban overlay. Requires board mode to be active.
func (m Model) openKanbanView() (Model, tea.Cmd) {
	if m.TaskListMode != TaskListModeBoard || m.BoardMode.Board == nil {
		return m, nil
	}
	m.KanbanOpen = true
	m.KanbanCol = 0
	m.KanbanRow = 0
	m.KanbanFullscreen = false
	m.KanbanColScrolls = make([]int, len(kanbanColumnOrder))

	// Try to place cursor on the first non-empty column
	for i, cat := range kanbanColumnOrder {
		issues := kanbanColumnIssues(m.BoardMode.SwimlaneData, cat)
		if len(issues) > 0 {
			m.KanbanCol = i
			m.KanbanRow = 0
			break
		}
	}
	return m, nil
}

// closeKanbanView closes the kanban overlay.
func (m *Model) closeKanbanView() {
	m.KanbanOpen = false
	m.KanbanCol = 0
	m.KanbanRow = 0
	m.KanbanFullscreen = false
	m.KanbanColScrolls = nil
}

// kanbanMoveLeft moves the cursor to the previous visible column.
func (m *Model) kanbanMoveLeft() {
	visible := m.visibleKanbanColIndexes()
	for i, col := range visible {
		if col == m.KanbanCol && i > 0 {
			m.KanbanCol = visible[i-1]
			m.clampKanbanRow()
			m.ensureKanbanCursorVisible()
			return
		}
	}
}

// kanbanMoveRight moves the cursor to the next visible column.
func (m *Model) kanbanMoveRight() {
	visible := m.visibleKanbanColIndexes()
	for i, col := range visible {
		if col == m.KanbanCol && i < len(visible)-1 {
			m.KanbanCol = visible[i+1]
			m.clampKanbanRow()
			m.ensureKanbanCursorVisible()
			return
		}
	}
}

// kanbanMoveDown moves the cursor down within the current column.
func (m *Model) kanbanMoveDown() {
	cat := kanbanColumnOrder[m.KanbanCol]
	issues := kanbanColumnIssues(m.BoardMode.SwimlaneData, cat)
	if m.KanbanRow < len(issues)-1 {
		m.KanbanRow++
		m.ensureKanbanCursorVisible()
	}
}

// kanbanMoveUp moves the cursor up within the current column.
func (m *Model) kanbanMoveUp() {
	if m.KanbanRow > 0 {
		m.KanbanRow--
		m.ensureKanbanCursorVisible()
	}
}

// clampKanbanCol snaps the column cursor onto a visible column. Prefer the
// nearest column to the right so a disappearing lane does not jump backward
// past work that is still on the board.
func (m *Model) clampKanbanCol() {
	visible := m.visibleKanbanColIndexes()
	if len(visible) == 0 {
		m.KanbanCol = 0
		return
	}
	for _, col := range visible {
		if col == m.KanbanCol {
			return
		}
	}
	for _, col := range visible {
		if col > m.KanbanCol {
			m.KanbanCol = col
			m.KanbanRow = 0
			return
		}
	}
	m.KanbanCol = visible[len(visible)-1]
	m.KanbanRow = 0
}

// clampKanbanRow clamps the row cursor to valid range in the current column.
func (m *Model) clampKanbanRow() {
	if m.KanbanCol < 0 || m.KanbanCol >= len(kanbanColumnOrder) {
		m.KanbanCol = 0
	}
	cat := kanbanColumnOrder[m.KanbanCol]
	issues := kanbanColumnIssues(m.BoardMode.SwimlaneData, cat)
	if len(issues) == 0 {
		m.KanbanRow = 0
	} else if m.KanbanRow >= len(issues) {
		m.KanbanRow = len(issues) - 1
	}
}

// kanbanBoxFrameLines is the vertical chrome the box itself adds around
// the inner grid: 2 for the border, plus 2 padding newlines when a
// custom ModalRenderer is in use (sidecar). The card budget must leave
// this room or OverlayModal / the embedder clips the bottom rule.
func (m Model) kanbanBoxFrameLines() int {
	if m.ModalRenderer != nil {
		return 4
	}
	return 2
}

// kanbanDimensions computes layout dimensions for the kanban view.
func (m Model) kanbanDimensions() (modalWidth, modalHeight, colWidth, maxVisibleCards int) {
	maxInnerH := m.Height - m.kanbanBoxFrameLines()
	if maxInnerH < kanbanChromeLines+kanbanCardHeight {
		maxInnerH = kanbanChromeLines + kanbanCardHeight
	}

	if m.KanbanFullscreen {
		modalWidth = m.Width - 2
		modalHeight = maxInnerH
	} else {
		modalWidth = m.Width * 90 / 100
		if modalWidth < 60 {
			modalWidth = m.Width - 2
		}
		if modalWidth > 160 {
			modalWidth = 160
		}
		if modalWidth > m.Width-2 && m.Width > 2 {
			modalWidth = m.Width - 2
		}
		modalHeight = m.Height * 85 / 100
		if modalHeight < kanbanChromeLines+kanbanCardHeight {
			modalHeight = maxInnerH
		}
		if modalHeight > 50 {
			modalHeight = 50
		}
		if modalHeight > maxInnerH {
			modalHeight = maxInnerH
		}
	}

	// Width(modalWidth-2) plus border and padding wraps at modalWidth-6.
	contentWidth := modalWidth - 6
	numCols := len(visibleKanbanColumns(m.BoardMode.SwimlaneData))
	if numCols < 1 {
		numCols = 1
	}
	separatorWidth := numCols - 1
	colWidth = (contentWidth - separatorWidth) / numCols
	if colWidth < 1 {
		colWidth = 1
	}
	// Prefer a readable column, but never exceed the wrap budget — one
	// extra occupied lane used to inflate every grid line and clip the
	// bottom rule.
	if colWidth < minKanbanColWidth {
		fitted := minKanbanColWidth
		if fitted*numCols+separatorWidth > contentWidth {
			fitted = (contentWidth - separatorWidth) / numCols
			if fitted < 1 {
				fitted = 1
			}
		}
		colWidth = fitted
	}

	availableCardHeight := modalHeight - kanbanChromeLines
	if availableCardHeight < kanbanCardHeight {
		availableCardHeight = kanbanCardHeight
	}
	maxVisibleCards = availableCardHeight / kanbanCardHeight
	if maxVisibleCards < 1 {
		maxVisibleCards = 1
	}
	return
}

// ensureKanbanCursorVisible adjusts the scroll offset for the current column
// so that the cursor row is visible.
func (m *Model) ensureKanbanCursorVisible() {
	col := m.KanbanCol
	if col < 0 || col >= len(m.KanbanColScrolls) {
		return
	}
	_, _, _, maxVisible := m.kanbanDimensions()
	scroll := m.KanbanColScrolls[col]

	// Ensure cursor is within the visible window
	if m.KanbanRow < scroll {
		scroll = m.KanbanRow
	} else if m.KanbanRow >= scroll+maxVisible {
		scroll = m.KanbanRow - maxVisible + 1
	}

	// Clamp scroll to valid bounds
	cat := kanbanColumnOrder[col]
	issues := kanbanColumnIssues(m.BoardMode.SwimlaneData, cat)
	maxScroll := len(issues) - maxVisible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}

	m.KanbanColScrolls[col] = scroll
}

// openIssueFromKanban opens the issue detail modal for the currently selected kanban card.
func (m Model) openIssueFromKanban() (tea.Model, tea.Cmd) {
	if m.KanbanCol < 0 || m.KanbanCol >= len(kanbanColumnOrder) {
		return m, nil
	}
	cat := kanbanColumnOrder[m.KanbanCol]
	issues := kanbanColumnIssues(m.BoardMode.SwimlaneData, cat)
	if len(issues) == 0 || m.KanbanRow < 0 || m.KanbanRow >= len(issues) {
		return m, nil
	}
	issueID := issues[m.KanbanRow].ID
	return m.pushModal(issueID, PanelTaskList)
}

// kanbanCardHeight is the number of lines per card in the kanban view.
const kanbanCardHeight = 3

// minKanbanColWidth is the minimum column width to render.
const minKanbanColWidth = 16

// kanbanChromeLines is the fixed row count around the card grid: title,
// top rule, column headers, header rule, up hint, down hint, bottom rule.
const kanbanChromeLines = 7

// kanbanScrollInfo tracks whether a column has hidden content above/below.
type kanbanScrollInfo struct {
	hasAbove bool
	hasBelow bool
}

// renderKanbanView renders the full kanban overlay content.
func (m Model) renderKanbanView() string {
	styles := m.renderStyles()
	data := m.BoardMode.SwimlaneData

	modalWidth, modalHeight, colWidth, maxVisibleCards := m.kanbanDimensions()

	visible := visibleKanbanColumns(data)
	numCols := len(visible)
	if numCols < 1 {
		numCols = 1
	}
	separatorWidth := numCols - 1
	actualContentWidth := colWidth*numCols + separatorWidth

	// Build header with board name
	boardName := "Board"
	if m.BoardMode.Board != nil {
		boardName = m.BoardMode.Board.Name
	}
	titleText := styles.kanbanTitle.Render(fmt.Sprintf(" Kanban: %s ", boardName))
	fsHint := "f:fullscreen"
	if m.KanbanFullscreen {
		fsHint = "f:overlay"
	}
	hintText := styles.kanbanHint.Render(fmt.Sprintf("  h/l:cols  j/k:rows  enter:open  %s  esc:close", fsHint))

	header := titleText + hintText
	headerWidth := lipgloss.Width(header)
	if headerWidth > actualContentWidth {
		header = ansi.Truncate(header, actualContentWidth, "...")
	}

	// Build column headers
	var colHeaders []string
	for _, cat := range visible {
		issues := kanbanColumnIssues(data, cat)
		label := kanbanColumnLabel(cat)
		countStr := fmt.Sprintf(" (%d)", len(issues))

		headerStyle := styles.category[cat].Bold(true)

		// If this column is selected, underline the header
		if kanbanColumnIndex(cat) == m.KanbanCol {
			headerStyle = headerStyle.Underline(true)
		}

		text := headerStyle.Render(label + countStr)
		textWidth := lipgloss.Width(text)
		if textWidth > colWidth {
			text = ansi.Truncate(text, colWidth, "")
		} else if textWidth < colWidth {
			text = text + strings.Repeat(" ", colWidth-textWidth)
		}
		colHeaders = append(colHeaders, text)
	}

	// Separator character
	sep := styles.kanbanSeparator.Render("│")

	// Build the column header line
	headerLine := strings.Join(colHeaders, sep)

	// Build separator line
	divider := styles.kanbanSeparator.Render(strings.Repeat("─", actualContentWidth))

	// Compute per-visible-column scroll offsets from the order-indexed store.
	colScrolls := make([]int, numCols)
	for i, cat := range visible {
		orderIdx := kanbanColumnIndex(cat)
		if orderIdx >= 0 && orderIdx < len(m.KanbanColScrolls) {
			colScrolls[i] = m.KanbanColScrolls[orderIdx]
		}
		issues := kanbanColumnIssues(data, cat)
		maxScroll := len(issues) - maxVisibleCards
		if maxScroll < 0 {
			maxScroll = 0
		}
		if colScrolls[i] > maxScroll {
			colScrolls[i] = maxScroll
		}
		if colScrolls[i] < 0 {
			colScrolls[i] = 0
		}
	}
	// For selected column, ensure cursor is visible
	selVis := -1
	for i, cat := range visible {
		if kanbanColumnIndex(cat) == m.KanbanCol {
			selVis = i
			break
		}
	}
	if selVis >= 0 {
		scroll := colScrolls[selVis]
		if m.KanbanRow < scroll {
			scroll = m.KanbanRow
		} else if m.KanbanRow >= scroll+maxVisibleCards {
			scroll = m.KanbanRow - maxVisibleCards + 1
		}
		if scroll < 0 {
			scroll = 0
		}
		colScrolls[selVis] = scroll
	}

	// Build per-column scroll indicators
	scrollInfos := make([]kanbanScrollInfo, numCols)
	for i, colCat := range visible {
		issues := kanbanColumnIssues(data, colCat)
		scrollInfos[i] = kanbanScrollInfo{
			hasAbove: colScrolls[i] > 0,
			hasBelow: colScrolls[i]+maxVisibleCards < len(issues),
		}
	}

	// Build the card lines row by row
	var cardLines []string
	for visRow := 0; visRow < maxVisibleCards; visRow++ {
		// Each card takes kanbanCardHeight lines
		for cardLine := 0; cardLine < kanbanCardHeight; cardLine++ {
			var cells []string
			for colIdx, colCat := range visible {
				issues := kanbanColumnIssues(data, colCat)

				dataRow := visRow + colScrolls[colIdx]

				var cellContent string
				if dataRow < len(issues) {
					issue := issues[dataRow]
					isSelected := kanbanColumnIndex(colCat) == m.KanbanCol && dataRow == m.KanbanRow
					cellContent = m.renderKanbanCardLine(issue, cardLine, colWidth, isSelected)
				} else {
					cellContent = strings.Repeat(" ", colWidth)
				}

				cells = append(cells, cellContent)
			}
			cardLines = append(cardLines, strings.Join(cells, sep))
		}
	}

	// Build scroll indicator lines
	upIndicatorLine := m.renderKanbanScrollIndicatorLine(scrollInfos, colWidth, sep, true)
	downIndicatorLine := m.renderKanbanScrollIndicatorLine(scrollInfos, colWidth, sep, false)

	// Assemble full content. The last line is the bottom rule so the grid
	// does not sit open-ended when OverlayModal or an embedder clips slack.
	var body []string
	body = append(body, header, divider, headerLine, divider, upIndicatorLine)
	body = append(body, cardLines...)
	body = append(body, downIndicatorLine)
	for len(body)+1 < modalHeight {
		body = append(body, strings.Repeat(" ", actualContentWidth))
	}
	body = append(body, divider)
	for i, line := range body {
		if ansi.StringWidth(line) > actualContentWidth {
			body[i] = ansi.Truncate(line, actualContentWidth, "")
		}
	}
	boxContent := strings.Join(body, "\n")

	if m.KanbanFullscreen {
		return m.renderKanbanFullscreen(boxContent, modalWidth, modalHeight)
	}

	// Use the modal renderer if available, otherwise default lipgloss border
	if m.ModalRenderer != nil {
		// Add vertical padding to match lipgloss Padding behavior.
		// Custom renderer only handles horizontal padding, so we add blank lines manually.
		paddedContent := "\n" + boxContent + "\n"
		// Add 2 to width/height: lipgloss Width/Height = content area, renderer expects outer with borders
		return m.ModalRenderer(paddedContent, modalWidth+2, modalHeight+2, ModalTypeKanban, 1)
	}
	return m.renderKanbanBox(boxContent, modalWidth, modalHeight)
}

// renderKanbanScrollIndicatorLine renders a line of per-column scroll indicators.
// If isUp is true, renders "▲" indicators; otherwise renders "▼" indicators.
func (m Model) renderKanbanScrollIndicatorLine(scrollInfos []kanbanScrollInfo, colWidth int, sep string, isUp bool) string {
	var cells []string
	for _, info := range scrollInfos {
		show := info.hasAbove
		if !isUp {
			show = info.hasBelow
		}
		if show {
			arrow := "▲"
			if !isUp {
				arrow = "▼"
			}
			indicator := m.renderStyles().subtle.Render(arrow)
			indicatorWidth := lipgloss.Width(indicator)
			padding := colWidth - indicatorWidth
			if padding < 0 {
				padding = 0
			}
			// Center the indicator
			leftPad := padding / 2
			rightPad := padding - leftPad
			cells = append(cells, strings.Repeat(" ", leftPad)+indicator+strings.Repeat(" ", rightPad))
		} else {
			cells = append(cells, strings.Repeat(" ", colWidth))
		}
	}
	return strings.Join(cells, sep)
}

// renderKanbanBox wraps content in a styled box for the kanban overlay view.
func (m Model) renderKanbanBox(content string, width, height int) string {
	style := m.renderStyles().kanbanBox.
		Width(width - 2).
		Height(height)
	return style.Render(content)
}

// renderKanbanFullscreen renders the kanban content to fill the full viewport.
func (m Model) renderKanbanFullscreen(content string, width, height int) string {
	style := m.renderStyles().kanbanBox.
		Width(width - 2).
		Height(height)
	return style.Render(content)
}

// renderKanbanCardLine renders a single line of a kanban card.
// Line 0: type icon + truncated title (or priority + title)
// Line 1: issue ID + status
// Line 2: separator/empty
func (m Model) renderKanbanCardLine(issue models.Issue, line, width int, selected bool) string {
	cardWidth := width

	var content string
	switch line {
	case 0:
		// Type icon + title
		icon := m.formatTypeIcon(issue.Type)
		prio := m.formatPriority(issue.Priority)
		prefix := icon + " " + prio + " "
		prefixWidth := lipgloss.Width(prefix)
		titleWidth := cardWidth - prefixWidth
		if titleWidth < 4 {
			titleWidth = 4
		}
		title := issue.Title
		if lipgloss.Width(title) > titleWidth {
			title = ansi.Truncate(title, titleWidth-1, "…")
		}
		content = prefix + title

	case 1:
		// Issue ID + status badge
		idStr := m.renderStyles().timestamp.Render(issue.ID)
		statusStr := m.formatStatus(issue.Status)
		content = idStr + " " + statusStr

	case 2:
		// Empty line (card separator)
		content = ""
	}

	// Pad or truncate to card width
	contentWidth := lipgloss.Width(content)
	if contentWidth > cardWidth {
		content = ansi.Truncate(content, cardWidth, "…")
		contentWidth = lipgloss.Width(content)
	}
	if contentWidth < cardWidth {
		content = content + strings.Repeat(" ", cardWidth-contentWidth)
	}

	// Apply selection highlight
	if selected {
		content = m.highlightRow(content, cardWidth)
	}

	return content
}
