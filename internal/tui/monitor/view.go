package monitor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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

	if m.ShowHelp {
		return m.renderHelp()
	}

	// Calculate panel heights (3 panels + footer)
	availableHeight := m.Height - 3 // Leave room for footer
	panelHeight := availableHeight / 3

	// Render each panel
	currentWork := m.renderCurrentWorkPanel(panelHeight)
	activity := m.renderActivityPanel(panelHeight)
	taskList := m.renderTaskListPanel(panelHeight)

	// Stack panels vertically
	panels := lipgloss.JoinVertical(lipgloss.Left,
		currentWork,
		activity,
		taskList,
	)

	// Add footer
	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, panels, footer)
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
	s.WriteString(fmt.Sprintf("Ready: %d | Review: %d | Blocked: %d\n",
		len(m.TaskList.Ready),
		len(m.TaskList.Reviewable),
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

	// Focused issue
	if m.FocusedIssue != nil {
		content.WriteString(titleStyle.Render("FOCUSED: "))
		content.WriteString(m.formatIssueCompact(m.FocusedIssue))
		content.WriteString("\n")
	} else {
		content.WriteString(subtleStyle.Render("No focused issue"))
		content.WriteString("\n")
	}

	// In-progress issues
	if len(m.InProgress) > 0 {
		content.WriteString("\n")
		content.WriteString(sectionHeader.Render("IN PROGRESS:"))
		content.WriteString("\n")

		offset := m.ScrollOffset[PanelCurrentWork]
		visible := m.visibleItems(len(m.InProgress), offset, height-4)

		for i := offset; i < offset+visible && i < len(m.InProgress); i++ {
			issue := m.InProgress[i]
			content.WriteString("  ")
			content.WriteString(m.formatIssueCompact(&issue))
			content.WriteString("\n")
		}
	}

	return m.wrapPanel("CURRENT WORK", content.String(), height, PanelCurrentWork)
}

// renderActivityPanel renders the activity log panel (Panel 2)
func (m Model) renderActivityPanel(height int) string {
	var content strings.Builder

	if len(m.Activity) == 0 {
		content.WriteString(subtleStyle.Render("No recent activity"))
	} else {
		offset := m.ScrollOffset[PanelActivity]
		visible := m.visibleItems(len(m.Activity), offset, height-2)

		for i := offset; i < offset+visible && i < len(m.Activity); i++ {
			item := m.Activity[i]
			content.WriteString(m.formatActivityItem(item))
			content.WriteString("\n")
		}
	}

	return m.wrapPanel("ACTIVITY LOG", content.String(), height, PanelActivity)
}

// renderTaskListPanel renders the task list panel (Panel 3)
// Shows Reviewable section FIRST when items need review to draw attention
func (m Model) renderTaskListPanel(height int) string {
	var content strings.Builder
	lines := 0
	maxLines := height - 2

	// Reviewable section - shown FIRST when there are items to review
	if len(m.TaskList.Reviewable) > 0 && lines < maxLines {
		content.WriteString(reviewAlertStyle.Render("★ REVIEWABLE"))
		content.WriteString(fmt.Sprintf(" (%d):\n", len(m.TaskList.Reviewable)))
		lines++

		for _, issue := range m.TaskList.Reviewable {
			if lines >= maxLines {
				break
			}
			content.WriteString("  ")
			content.WriteString(m.formatIssueShort(&issue))
			content.WriteString("\n")
			lines++
		}
	}

	// Ready section
	if len(m.TaskList.Ready) > 0 && lines < maxLines {
		if lines > 0 {
			content.WriteString("\n")
			lines++
		}
		content.WriteString(readyColor.Render("READY"))
		content.WriteString(fmt.Sprintf(" (%d):\n", len(m.TaskList.Ready)))
		lines++

		for _, issue := range m.TaskList.Ready {
			if lines >= maxLines {
				break
			}
			content.WriteString("  ")
			content.WriteString(m.formatIssueShort(&issue))
			content.WriteString("\n")
			lines++
		}
	}

	// Blocked section
	if len(m.TaskList.Blocked) > 0 && lines < maxLines {
		if lines > 0 {
			content.WriteString("\n")
			lines++
		}
		content.WriteString(blockedColor.Render("BLOCKED"))
		content.WriteString(fmt.Sprintf(" (%d):\n", len(m.TaskList.Blocked)))
		lines++

		for _, issue := range m.TaskList.Blocked {
			if lines >= maxLines {
				break
			}
			content.WriteString("  ")
			content.WriteString(m.formatIssueShort(&issue))
			content.WriteString("\n")
			lines++
		}
	}

	if content.Len() == 0 {
		content.WriteString(subtleStyle.Render("No tasks available"))
	}

	return m.wrapPanel("TASK LIST", content.String(), height, PanelTaskList)
}

// renderFooter renders the footer with key bindings and refresh time
func (m Model) renderFooter() string {
	keys := helpStyle.Render("q:quit  tab:switch  j/k:scroll  r:refresh  ?:help")

	// Show prominent review alert if items need review
	reviewAlert := ""
	if len(m.TaskList.Reviewable) > 0 {
		reviewAlert = reviewAlertStyle.Render(fmt.Sprintf(" [%d TO REVIEW] ", len(m.TaskList.Reviewable)))
	}

	refresh := timestampStyle.Render(fmt.Sprintf("Last: %s", m.LastRefresh.Format("15:04:05")))

	// Calculate spacing
	padding := m.Width - lipgloss.Width(keys) - lipgloss.Width(reviewAlert) - lipgloss.Width(refresh) - 2
	if padding < 0 {
		padding = 0
	}

	return fmt.Sprintf(" %s%s%s%s", keys, strings.Repeat(" ", padding), reviewAlert, refresh)
}

// renderHelp renders the help overlay
func (m Model) renderHelp() string {
	help := `
MONITOR TUI - Key Bindings

NAVIGATION:
  Tab / Shift+Tab   Switch between panels
  1 / 2 / 3         Jump to panel
  j / k             Scroll down/up in active panel

ACTIONS:
  r                 Force refresh
  q / Ctrl+C        Quit

Press ? to close help
`
	return helpStyle.Render(help)
}

// wrapPanel wraps content in a panel with title and border
func (m Model) wrapPanel(title, content string, height int, panel Panel) string {
	style := panelStyle
	if m.ActivePanel == panel {
		style = activePanelStyle
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
	return fmt.Sprintf("%s %s %s",
		subtleStyle.Render(issue.ID),
		formatPriority(issue.Priority),
		truncateString(issue.Title, 40),
	)
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

	msg := truncateString(item.Message, m.Width-40)

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

	// Prominent style for review alert in footer
	reviewAlertStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("141"))
)
