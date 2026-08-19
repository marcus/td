package monitor

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/marcus/td/internal/models"
)

// Monitor list rows share one column layout across the task list, board, and
// swimlane panes:
//
//	<gutter><key> <type> <priority> <title>[ <trailing>]
//
// The gutter is always rowGutterWidth columns wide so the selection caret can
// be drawn without shifting the rest of the row, and so rows sit visually
// indented beneath their (flush-left) section header.
const (
	rowGutterWidth   = 2
	rowCaretGlyph    = "❯"
	issueKeyMinWidth = 9
	issueKeyMaxWidth = 16
	rowMinTitleWidth = 12
)

// issueKeyColumnWidth returns the shared width of the issue-key column for a
// set of rows, so keys and every column after them line up within a pane.
func issueKeyColumnWidth(ids []string) int {
	width := issueKeyMinWidth
	for _, id := range ids {
		if n := lipgloss.Width(id); n > width {
			width = n
		}
	}
	if width > issueKeyMaxWidth {
		width = issueKeyMaxWidth
	}
	return width
}

// taskListKeyWidth returns the key column width for the main task list rows.
func (m Model) taskListKeyWidth() int {
	ids := make([]string, 0, len(m.TaskListRows))
	for _, row := range m.TaskListRows {
		ids = append(ids, row.Issue.ID)
	}
	return issueKeyColumnWidth(ids)
}

// swimlaneKeyWidth returns the key column width for board swimlane rows.
func (m Model) swimlaneKeyWidth() int {
	ids := make([]string, 0, len(m.BoardMode.SwimlaneRows))
	for _, row := range m.BoardMode.SwimlaneRows {
		ids = append(ids, row.Issue.ID)
	}
	return issueKeyColumnWidth(ids)
}

// rowGutter renders the leading caret gutter for a list row.
func (m Model) rowGutter(selected bool) string {
	if !selected {
		return strings.Repeat(" ", rowGutterWidth)
	}
	return m.renderStyles().rowCaret.Render(rowCaretGlyph) + strings.Repeat(" ", rowGutterWidth-1)
}

// formatIssueRow renders a single issue in the monitor's column layout. The
// status a row carries is conveyed by the section it sits in, so no status tag
// is repeated inline. availWidth is the pane's content width; trailing is
// optional right-hand meta (session, timing) appended after the title.
func (m Model) formatIssueRow(issue *models.Issue, selected bool, keyWidth, availWidth int, trailing string) string {
	styles := m.renderStyles()

	gutter := m.rowGutter(selected)
	key := issue.ID
	if pad := keyWidth - lipgloss.Width(key); pad > 0 {
		key += strings.Repeat(" ", pad)
	}
	typeIcon := m.formatTypeIcon(issue.Type)
	priority := m.formatPriority(issue.Priority)

	used := rowGutterWidth + lipgloss.Width(key) + 1 +
		lipgloss.Width(typeIcon) + 1 + lipgloss.Width(priority) + 1
	trailingWidth := 0
	if trailing != "" {
		trailingWidth = lipgloss.Width(trailing) + 1
	}
	titleWidth := availWidth - used - trailingWidth
	if titleWidth < rowMinTitleWidth {
		titleWidth = rowMinTitleWidth
	}

	line := gutter + styles.subtle.Render(key) + " " + typeIcon + " " + priority + " " +
		truncateString(issue.Title, titleWidth)
	if trailing != "" {
		line += " " + trailing
	}
	return line
}

// formatSectionHeaderLine renders a flush-left section header: a themed label,
// a rule filling the pane, and the section's item count on the right.
func (m Model) formatSectionHeaderLine(label string, count int, labelStyle lipgloss.Style) string {
	styles := m.renderStyles()
	head := labelStyle.Bold(true).Render(label)
	countStr := styles.subtle.Render(strconv.Itoa(count))

	// Pane content is m.Width-4 wide (border + padding). wrapPanel renders
	// the panel with a fixed width, and lipgloss soft-wraps a line that
	// reaches that edge, which would drop the count onto its own line — so
	// build the rule two columns short of the content width.
	width := m.Width - 6
	used := lipgloss.Width(head) + lipgloss.Width(countStr) + 2
	if width-used < 3 {
		return head + " " + countStr
	}
	rule := styles.sectionRule.Render(strings.Repeat("─", width-used))
	return head + " " + rule + " " + countStr
}
