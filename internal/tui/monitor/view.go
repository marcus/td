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

	base := lipgloss.JoinVertical(lipgloss.Left, panels, footer)

	// Overlay modal if open
	if m.ModalOpen {
		modal := m.renderModal()
		return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, modal,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(lipgloss.Color("0")))
	}

	return base
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

	if len(m.CurrentWorkRows) == 0 {
		content.WriteString(subtleStyle.Render("No current work"))
		content.WriteString("\n")
		return m.wrapPanel("CURRENT WORK", content.String(), height, PanelCurrentWork)
	}

	cursor := m.Cursor[PanelCurrentWork]
	isActive := m.ActivePanel == PanelCurrentWork
	rowIdx := 0

	// Focused issue (first row if present)
	if m.FocusedIssue != nil {
		line := titleStyle.Render("FOCUSED: ") + m.formatIssueCompact(m.FocusedIssue)
		if isActive && cursor == rowIdx {
			line = selectedRowStyle.Render("> " + line)
		}
		content.WriteString(line)
		content.WriteString("\n")
		rowIdx++
	}

	// In-progress issues (skip focused if it's duplicated)
	if len(m.InProgress) > 0 {
		content.WriteString("\n")
		content.WriteString(sectionHeader.Render("IN PROGRESS:"))
		content.WriteString("\n")

		for _, issue := range m.InProgress {
			// Skip focused issue if it's also in progress
			if m.FocusedIssue != nil && issue.ID == m.FocusedIssue.ID {
				continue
			}
			line := "  " + m.formatIssueCompact(&issue)
			if isActive && cursor == rowIdx {
				line = selectedRowStyle.Render("> " + m.formatIssueCompact(&issue))
			}
			content.WriteString(line)
			content.WriteString("\n")
			rowIdx++
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
		cursor := m.Cursor[PanelActivity]
		isActive := m.ActivePanel == PanelActivity
		offset := m.ScrollOffset[PanelActivity]
		visible := m.visibleItems(len(m.Activity), offset, height-2)

		for i := offset; i < offset+visible && i < len(m.Activity); i++ {
			item := m.Activity[i]
			line := m.formatActivityItem(item)
			if isActive && cursor == i {
				line = selectedRowStyle.Render("> " + line)
			}
			content.WriteString(line)
			content.WriteString("\n")
		}
	}

	return m.wrapPanel("ACTIVITY LOG", content.String(), height, PanelActivity)
}

// renderTaskListPanel renders the task list panel (Panel 3)
// Uses flattened TaskListRows for selection support
func (m Model) renderTaskListPanel(height int) string {
	var content strings.Builder

	if len(m.TaskListRows) == 0 {
		content.WriteString(subtleStyle.Render("No tasks available"))
		return m.wrapPanel("TASK LIST", content.String(), height, PanelTaskList)
	}

	cursor := m.Cursor[PanelTaskList]
	isActive := m.ActivePanel == PanelTaskList
	maxLines := height - 2

	// Track current category for section headers
	var currentCategory TaskListCategory
	lines := 0

	for i, row := range m.TaskListRows {
		if lines >= maxLines {
			break
		}

		// Add category header when category changes
		if row.Category != currentCategory {
			if lines > 0 {
				content.WriteString("\n")
				lines++
				if lines >= maxLines {
					break
				}
			}
			header := m.formatCategoryHeader(row.Category)
			content.WriteString(header)
			content.WriteString("\n")
			lines++
			currentCategory = row.Category
			if lines >= maxLines {
				break
			}
		}

		// Format row with category tag and selection highlight
		tag := m.formatCategoryTag(row.Category)
		issueStr := m.formatIssueShort(&row.Issue)
		line := fmt.Sprintf("  %s %s", tag, issueStr)

		if isActive && cursor == i {
			line = selectedRowStyle.Render("> " + tag + " " + issueStr)
		}

		content.WriteString(line)
		content.WriteString("\n")
		lines++
	}

	return m.wrapPanel("TASK LIST", content.String(), height, PanelTaskList)
}

// formatCategoryHeader returns the section header for a category
func (m Model) formatCategoryHeader(cat TaskListCategory) string {
	count := 0
	switch cat {
	case CategoryReviewable:
		count = len(m.TaskList.Reviewable)
		return reviewAlertStyle.Render("★ REVIEWABLE") + fmt.Sprintf(" (%d):", count)
	case CategoryReady:
		count = len(m.TaskList.Ready)
		return readyColor.Render("READY") + fmt.Sprintf(" (%d):", count)
	case CategoryBlocked:
		count = len(m.TaskList.Blocked)
		return blockedColor.Render("BLOCKED") + fmt.Sprintf(" (%d):", count)
	}
	return ""
}

// formatCategoryTag returns a short tag for inline display
func (m Model) formatCategoryTag(cat TaskListCategory) string {
	switch cat {
	case CategoryReviewable:
		return reviewColor.Render("[REV]")
	case CategoryReady:
		return readyColor.Render("[RDY]")
	case CategoryBlocked:
		return blockedColor.Render("[BLK]")
	}
	return ""
}

// renderModal renders the centered issue details modal
func (m Model) renderModal() string {
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
	if m.ModalLoading {
		content.WriteString(subtleStyle.Render("Loading..."))
		return m.wrapModal(content.String(), modalWidth, modalHeight)
	}

	// Error state
	if m.ModalError != nil {
		content.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.ModalError)))
		content.WriteString("\n\n")
		content.WriteString(subtleStyle.Render("Press esc to close"))
		return m.wrapModal(content.String(), modalWidth, modalHeight)
	}

	// No issue loaded
	if m.ModalIssue == nil {
		content.WriteString(subtleStyle.Render("No issue data"))
		return m.wrapModal(content.String(), modalWidth, modalHeight)
	}

	issue := m.ModalIssue

	// Build all content lines for scrolling
	var lines []string

	// Header: ID and Title
	lines = append(lines, titleStyle.Render(issue.ID)+" "+issue.Title)
	lines = append(lines, "")

	// Status line: status, type, priority, points
	statusLine := fmt.Sprintf("%s  %s  %s",
		formatStatus(issue.Status),
		subtleStyle.Render(string(issue.Type)),
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

	// Description
	if issue.Description != "" {
		lines = append(lines, sectionHeader.Render("DESCRIPTION"))
		// Word-wrap description
		for _, line := range wrapText(issue.Description, contentWidth) {
			lines = append(lines, line)
		}
		lines = append(lines, "")
	}

	// Acceptance criteria
	if issue.Acceptance != "" {
		lines = append(lines, sectionHeader.Render("ACCEPTANCE CRITERIA"))
		for _, line := range wrapText(issue.Acceptance, contentWidth) {
			lines = append(lines, line)
		}
		lines = append(lines, "")
	}

	// Blocked by (dependencies)
	if len(m.ModalBlockedBy) > 0 {
		lines = append(lines, sectionHeader.Render(fmt.Sprintf("BLOCKED BY (%d)", len(m.ModalBlockedBy))))
		for _, dep := range m.ModalBlockedBy {
			depLine := fmt.Sprintf("  %s %s %s",
				titleStyle.Render(dep.ID),
				formatStatus(dep.Status),
				truncateString(dep.Title, contentWidth-20))
			lines = append(lines, depLine)
		}
		lines = append(lines, "")
	}

	// Blocks (dependents)
	if len(m.ModalBlocks) > 0 {
		lines = append(lines, sectionHeader.Render(fmt.Sprintf("BLOCKS (%d)", len(m.ModalBlocks))))
		for _, dep := range m.ModalBlocks {
			depLine := fmt.Sprintf("  %s %s %s",
				titleStyle.Render(dep.ID),
				formatStatus(dep.Status),
				truncateString(dep.Title, contentWidth-20))
			lines = append(lines, depLine)
		}
		lines = append(lines, "")
	}

	// Latest handoff
	if m.ModalHandoff != nil {
		lines = append(lines, sectionHeader.Render("LATEST HANDOFF"))
		lines = append(lines, timestampStyle.Render(m.ModalHandoff.Timestamp.Format("2006-01-02 15:04"))+" "+
			subtleStyle.Render(truncateSession(m.ModalHandoff.SessionID)))
		if len(m.ModalHandoff.Done) > 0 {
			lines = append(lines, readyColor.Render("Done:"))
			for _, item := range m.ModalHandoff.Done {
				lines = append(lines, "  • "+item)
			}
		}
		if len(m.ModalHandoff.Remaining) > 0 {
			lines = append(lines, reviewColor.Render("Remaining:"))
			for _, item := range m.ModalHandoff.Remaining {
				lines = append(lines, "  • "+item)
			}
		}
		if len(m.ModalHandoff.Uncertain) > 0 {
			lines = append(lines, blockedColor.Render("Uncertain:"))
			for _, item := range m.ModalHandoff.Uncertain {
				lines = append(lines, "  • "+item)
			}
		}
		lines = append(lines, "")
	}

	// Recent logs
	if len(m.ModalLogs) > 0 {
		lines = append(lines, sectionHeader.Render(fmt.Sprintf("RECENT LOGS (%d)", len(m.ModalLogs))))
		for _, log := range m.ModalLogs {
			logLine := timestampStyle.Render(log.Timestamp.Format("01-02 15:04")) + " " +
				subtleStyle.Render(truncateSession(log.SessionID)) + " " +
				truncateString(log.Message, contentWidth-25)
			lines = append(lines, logLine)
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
	scroll := m.ModalScroll
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

// wrapModal wraps content in a modal box with border
func (m Model) wrapModal(content string, width, height int) string {
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(width).
		Height(height)

	// Add footer with key hints
	footer := subtleStyle.Render("↑↓:scroll  esc:close  r:refresh")

	inner := lipgloss.JoinVertical(lipgloss.Left, content, "", footer)

	return modalStyle.Render(inner)
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

// Error style for modal
var errorStyle = lipgloss.NewStyle().Foreground(errorColor)

// renderFooter renders the footer with key bindings and refresh time
func (m Model) renderFooter() string {
	keys := helpStyle.Render("q:quit  tab:switch  ↑↓:select  enter:details  r:refresh  ?:help")

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

	refresh := timestampStyle.Render(fmt.Sprintf("Last: %s", m.LastRefresh.Format("15:04:05")))

	// Calculate spacing
	padding := m.Width - lipgloss.Width(keys) - lipgloss.Width(sessionsIndicator) - lipgloss.Width(handoffAlert) - lipgloss.Width(reviewAlert) - lipgloss.Width(refresh) - 2
	if padding < 0 {
		padding = 0
	}

	return fmt.Sprintf(" %s%s%s%s%s%s", keys, strings.Repeat(" ", padding), sessionsIndicator, handoffAlert, reviewAlert, refresh)
}

// renderHelp renders the help overlay
func (m Model) renderHelp() string {
	help := `
MONITOR TUI - Key Bindings

NAVIGATION:
  Tab / Shift+Tab   Switch between panels
  1 / 2 / 3         Jump to panel
  ↑ / ↓             Select row in active panel
  j / k             Scroll viewport
  Enter             Open issue details

MODAL:
  ↑ / ↓ / j / k     Scroll modal content
  Esc / Enter       Close modal
  r                 Refresh data

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

	// Prominent style for handoff alert - green background
	handoffAlertStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("42"))

	// Style for active sessions indicator - cyan text
	activeSessionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("45"))
)
