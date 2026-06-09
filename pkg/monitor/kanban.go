package monitor

import (
	"fmt"
	"image/color"
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
	CategoryNeedsRework,
	CategoryInProgress,
	CategoryReady,
	CategoryPendingReview,
	CategoryBlocked,
	CategoryClosed,
}

// kanbanColumnLabel returns the display label for a kanban column.
func kanbanColumnLabel(cat TaskListCategory) string {
	switch cat {
	case CategoryReviewable:
		return "REVIEW"
	case CategoryNeedsRework:
		return "REWORK"
	case CategoryInProgress:
		return "WIP"
	case CategoryReady:
		return "READY"
	case CategoryPendingReview:
		return "P.REVIEW"
	case CategoryBlocked:
		return "BLOCKED"
	case CategoryClosed:
		return "CLOSED"
	default:
		return string(cat)
	}
}

// kanbanColumnColor returns the header color for each column category.
// Colors are derived from the style variables defined in styles.go.
func kanbanColumnColor(cat TaskListCategory) color.Color {
	switch cat {
	case CategoryReviewable:
		return secondaryColor // purple (in_review)
	case CategoryNeedsRework:
		return warningColor // orange (needs action)
	case CategoryInProgress:
		return cyanColor // cyan (in_progress)
	case CategoryReady:
		return successColor // green (open/ready)
	case CategoryPendingReview:
		return lipgloss.Color("183") // light purple (pending review)
	case CategoryBlocked:
		return errorColor // red (blocked)
	case CategoryClosed:
		return mutedColor // gray (closed)
	default:
		return lipgloss.Color("255")
	}
}

// kanbanColumnIssues returns the issues for a given category from the swimlane data.
func kanbanColumnIssues(data TaskListData, cat TaskListCategory) []models.Issue {
	switch cat {
	case CategoryReviewable:
		return data.Reviewable
	case CategoryNeedsRework:
		return data.NeedsRework
	case CategoryInProgress:
		return data.InProgress
	case CategoryReady:
		return data.Ready
	case CategoryPendingReview:
		return data.PendingReview
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

// kanbanMoveLeft moves the cursor to the previous column, clamping row to valid range.
func (m *Model) kanbanMoveLeft() {
	if m.KanbanCol > 0 {
		m.KanbanCol--
		m.clampKanbanRow()
		m.ensureKanbanCursorVisible()
	}
}

// kanbanMoveRight moves the cursor to the next column, clamping row to valid range.
func (m *Model) kanbanMoveRight() {
	if m.KanbanCol < len(kanbanColumnOrder)-1 {
		m.KanbanCol++
		m.clampKanbanRow()
		m.ensureKanbanCursorVisible()
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

// clampKanbanRow clamps the row cursor to valid range in the current column.
func (m *Model) clampKanbanRow() {
	cat := kanbanColumnOrder[m.KanbanCol]
	issues := kanbanColumnIssues(m.BoardMode.SwimlaneData, cat)
	if len(issues) == 0 {
		m.KanbanRow = 0
	} else if m.KanbanRow >= len(issues) {
		m.KanbanRow = len(issues) - 1
	}
}

// kanbanDimensions computes layout dimensions for the kanban view.
func (m Model) kanbanDimensions() (modalWidth, modalHeight, colWidth, maxVisibleCards int) {
	if m.KanbanFullscreen {
		modalWidth = m.Width - 2
		modalHeight = m.Height
	} else {
		modalWidth = m.Width * 90 / 100
		if modalWidth < 60 {
			modalWidth = m.Width - 2
		}
		if modalWidth > 160 {
			modalWidth = 160
		}
		modalHeight = m.Height * 85 / 100
		if modalHeight < 12 {
			modalHeight = m.Height - 2
		}
		if modalHeight > 50 {
			modalHeight = 50
		}
	}

	contentWidth := modalWidth - 4
	numCols := len(kanbanColumnOrder)
	separatorWidth := numCols - 1
	colWidth = (contentWidth - separatorWidth) / numCols
	if colWidth < minKanbanColWidth {
		colWidth = minKanbanColWidth
	}

	// Available height for cards (subtract header, divider, column headers, divider,
	// up scroll indicator, down scroll indicator)
	availableCardHeight := modalHeight - 8
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

// kanbanScrollInfo tracks whether a column has hidden content above/below.
type kanbanScrollInfo struct {
	hasAbove bool
	hasBelow bool
}

// renderKanbanView renders the full kanban overlay content.
func (m Model) renderKanbanView() string {
	data := m.BoardMode.SwimlaneData

	modalWidth, modalHeight, colWidth, maxVisibleCards := m.kanbanDimensions()

	numCols := len(kanbanColumnOrder)
	separatorWidth := numCols - 1
	actualContentWidth := colWidth*numCols + separatorWidth

	// Build header with board name
	boardName := "Board"
	if m.BoardMode.Board != nil {
		boardName = m.BoardMode.Board.Name
	}
	titleText := kanbanTitleStyle.Render(fmt.Sprintf(" Kanban: %s ", boardName))
	fsHint := "f:fullscreen"
	if m.KanbanFullscreen {
		fsHint = "f:overlay"
	}
	hintText := kanbanHintStyle.Render(fmt.Sprintf("  h/l:cols  j/k:rows  enter:open  %s  esc:close", fsHint))

	header := titleText + hintText
	headerWidth := lipgloss.Width(header)
	if headerWidth > actualContentWidth {
		header = ansi.Truncate(header, actualContentWidth, "...")
	}

	// Build column headers
	var colHeaders []string
	for i, cat := range kanbanColumnOrder {
		issues := kanbanColumnIssues(data, cat)
		color := kanbanColumnColor(cat)
		label := kanbanColumnLabel(cat)
		countStr := fmt.Sprintf(" (%d)", len(issues))

		headerStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(color)

		// If this column is selected, underline the header
		if i == m.KanbanCol {
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
	sep := kanbanSepStyle.Render("│")

	// Build the column header line
	headerLine := strings.Join(colHeaders, sep)

	// Build separator line
	divider := kanbanSepStyle.Render(strings.Repeat("─", actualContentWidth))

	// Compute per-column scroll offsets. Use stored offsets for all columns
	// but ensure the selected column's cursor is visible.
	colScrolls := make([]int, numCols)
	for i := range kanbanColumnOrder {
		if i < len(m.KanbanColScrolls) {
			colScrolls[i] = m.KanbanColScrolls[i]
		}
		// Clamp scroll to valid bounds for this column
		colCat := kanbanColumnOrder[i]
		issues := kanbanColumnIssues(data, colCat)
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
	if m.KanbanCol >= 0 && m.KanbanCol < numCols {
		scroll := colScrolls[m.KanbanCol]
		if m.KanbanRow < scroll {
			scroll = m.KanbanRow
		} else if m.KanbanRow >= scroll+maxVisibleCards {
			scroll = m.KanbanRow - maxVisibleCards + 1
		}
		if scroll < 0 {
			scroll = 0
		}
		colScrolls[m.KanbanCol] = scroll
	}

	// Build per-column scroll indicators
	scrollInfos := make([]kanbanScrollInfo, numCols)
	for i, colCat := range kanbanColumnOrder {
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
			for colIdx, colCat := range kanbanColumnOrder {
				issues := kanbanColumnIssues(data, colCat)

				dataRow := visRow + colScrolls[colIdx]

				var cellContent string
				if dataRow < len(issues) {
					issue := issues[dataRow]
					isSelected := colIdx == m.KanbanCol && dataRow == m.KanbanRow
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

	// Assemble full content
	var content strings.Builder
	content.WriteString(header)
	content.WriteString("\n")
	content.WriteString(divider)
	content.WriteString("\n")
	content.WriteString(headerLine)
	content.WriteString("\n")
	content.WriteString(divider)
	content.WriteString("\n")
	content.WriteString(upIndicatorLine)
	content.WriteString("\n")
	for _, line := range cardLines {
		content.WriteString(line)
		content.WriteString("\n")
	}
	content.WriteString(downIndicatorLine)

	// Render in a modal box
	boxContent := content.String()
	// Trim trailing newline
	boxContent = strings.TrimRight(boxContent, "\n")

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
			indicator := subtleStyle.Render(arrow)
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
	borderColor := primaryColor
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(width - 2).
		MaxHeight(height)
	return style.Render(content)
}

// renderKanbanFullscreen renders the kanban content to fill the full viewport.
func (m Model) renderKanbanFullscreen(content string, width, height int) string {
	borderColor := primaryColor
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(width - 2).
		Height(height - 2)
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
		icon := formatTypeIcon(issue.Type)
		prio := formatPriority(issue.Priority)
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
		idStr := timestampStyle.Render(issue.ID)
		statusStr := formatStatus(issue.Status)
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
		content = highlightRow(content, cardWidth)
	}

	return content
}
