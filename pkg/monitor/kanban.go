package monitor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/internal/models"
)

// kanbanColumnOrder defines the order of columns in the kanban view.
// This is derived from the board swimlane categories so that any future
// status additions are automatically included.
var kanbanColumnOrder = []TaskListCategory{
	CategoryReviewable,
	CategoryNeedsRework,
	CategoryReady,
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
	case CategoryReady:
		return "READY"
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
func kanbanColumnColor(cat TaskListCategory) lipgloss.Color {
	switch cat {
	case CategoryReviewable:
		return secondaryColor // purple (in_review)
	case CategoryNeedsRework:
		return warningColor // orange (needs action)
	case CategoryReady:
		return cyanColor // cyan (open/ready)
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
	case CategoryReady:
		return data.Ready
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
}

// kanbanMoveLeft moves the cursor to the previous column, clamping row to valid range.
func (m *Model) kanbanMoveLeft() {
	if m.KanbanCol > 0 {
		m.KanbanCol--
		m.clampKanbanRow()
	}
}

// kanbanMoveRight moves the cursor to the next column, clamping row to valid range.
func (m *Model) kanbanMoveRight() {
	if m.KanbanCol < len(kanbanColumnOrder)-1 {
		m.KanbanCol++
		m.clampKanbanRow()
	}
}

// kanbanMoveDown moves the cursor down within the current column.
func (m *Model) kanbanMoveDown() {
	cat := kanbanColumnOrder[m.KanbanCol]
	issues := kanbanColumnIssues(m.BoardMode.SwimlaneData, cat)
	if m.KanbanRow < len(issues)-1 {
		m.KanbanRow++
	}
}

// kanbanMoveUp moves the cursor up within the current column.
func (m *Model) kanbanMoveUp() {
	if m.KanbanRow > 0 {
		m.KanbanRow--
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

// renderKanbanView renders the full kanban overlay content.
func (m Model) renderKanbanView() string {
	data := m.BoardMode.SwimlaneData

	// Calculate dimensions
	modalWidth := m.Width * 90 / 100
	if modalWidth < 60 {
		modalWidth = m.Width - 2
	}
	if modalWidth > 160 {
		modalWidth = 160
	}
	modalHeight := m.Height * 85 / 100
	if modalHeight < 12 {
		modalHeight = m.Height - 2
	}
	if modalHeight > 50 {
		modalHeight = 50
	}

	// Inner content width (minus border + padding)
	contentWidth := modalWidth - 4
	numCols := len(kanbanColumnOrder)
	// Column separators: 1 char between each column
	separatorWidth := numCols - 1
	colWidth := (contentWidth - separatorWidth) / numCols
	if colWidth < minKanbanColWidth {
		colWidth = minKanbanColWidth
	}

	// Recalculate actual width to fit columns
	actualContentWidth := colWidth*numCols + separatorWidth

	// Build header with board name
	boardName := "Board"
	if m.BoardMode.Board != nil {
		boardName = m.BoardMode.Board.Name
	}
	titleText := kanbanTitleStyle.Render(fmt.Sprintf(" Kanban: %s ", boardName))
	hintText := kanbanHintStyle.Render("  h/l:cols  j/k:rows  enter:open  esc:close")

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

	// Available height for cards (subtract header, divider, column headers, divider, footer hint)
	availableCardHeight := modalHeight - 6
	if availableCardHeight < kanbanCardHeight {
		availableCardHeight = kanbanCardHeight
	}

	// Build card area - each column rendered in parallel
	maxVisibleCards := availableCardHeight / kanbanCardHeight
	if maxVisibleCards < 1 {
		maxVisibleCards = 1
	}

	// Compute scroll offset for the selected column so the cursor is always visible.
	// Non-selected columns always start from row 0.
	selectedColScroll := 0
	cat := kanbanColumnOrder[m.KanbanCol]
	colIssues := kanbanColumnIssues(data, cat)
	if m.KanbanRow >= maxVisibleCards {
		selectedColScroll = m.KanbanRow - maxVisibleCards + 1
	}
	if selectedColScroll > len(colIssues)-maxVisibleCards {
		selectedColScroll = len(colIssues) - maxVisibleCards
	}
	if selectedColScroll < 0 {
		selectedColScroll = 0
	}

	// Build the card lines row by row
	var cardLines []string
	for visRow := 0; visRow < maxVisibleCards; visRow++ {
		// Each card takes kanbanCardHeight lines
		for cardLine := 0; cardLine < kanbanCardHeight; cardLine++ {
			var cells []string
			for colIdx, colCat := range kanbanColumnOrder {
				issues := kanbanColumnIssues(data, colCat)

				// Apply scroll offset only to the selected column
				dataRow := visRow
				if colIdx == m.KanbanCol {
					dataRow = visRow + selectedColScroll
				}

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
	for _, line := range cardLines {
		content.WriteString(line)
		content.WriteString("\n")
	}

	// Render in a modal box
	boxContent := content.String()
	// Trim trailing newline
	boxContent = strings.TrimRight(boxContent, "\n")

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

// renderKanbanBox wraps content in a styled box for the kanban view.
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
