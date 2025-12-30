package monitor

import (
	"fmt"
	"strings"
	"time"

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

	if m.ShowTDQHelp {
		return m.renderTDQHelp()
	}

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
	panelHeight := availableHeight / 3

	// Render each panel
	currentWork := m.renderCurrentWorkPanel(panelHeight)
	activity := m.renderActivityPanel(panelHeight)
	taskList := m.renderTaskListPanel(panelHeight)

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
	var base string
	if m.Embedded {
		base = content
	} else {
		footer := m.renderFooter()
		base = lipgloss.JoinVertical(lipgloss.Left, content, footer)
	}

	// Overlay confirmation dialog if open
	if m.ConfirmOpen {
		confirm := m.renderConfirmation()
		return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, confirm,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(lipgloss.Color("0")))
	}

	// Overlay stats modal if open
	if m.StatsOpen {
		stats := m.renderStatsModal()
		return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, stats,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(lipgloss.Color("0")))
	}

	// Overlay modal if open
	if m.ModalOpen() {
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

	// Clamp offset
	if offset > totalRows-maxLines && totalRows > maxLines {
		offset = totalRows - maxLines
	}
	if offset < 0 {
		offset = 0
	}

	// Build title with position if scrollable
	panelTitle := "CURRENT WORK"
	needsScroll := totalRows > maxLines
	if needsScroll {
		endPos := offset + maxLines
		if endPos > totalRows {
			endPos = totalRows
		}
		panelTitle = fmt.Sprintf("CURRENT WORK (%d-%d of %d)", offset+1, endPos, totalRows)
	}

	// Show up indicator if scrolled down
	if needsScroll && offset > 0 {
		content.WriteString(subtleStyle.Render("  ▲ more above"))
		content.WriteString("\n")
		maxLines-- // Reserve line for indicator
	}

	rowIdx := 0
	linesWritten := 0

	// Focused issue (first row if present)
	if m.FocusedIssue != nil {
		if rowIdx >= offset && linesWritten < maxLines {
			line := titleStyle.Render("FOCUSED: ") + m.formatIssueCompact(m.FocusedIssue)
			if isActive && cursor == rowIdx {
				line = selectedRowStyle.Render("> " + line)
			}
			content.WriteString(line)
			content.WriteString("\n")
			linesWritten++
		}
		rowIdx++
	}

	// In-progress issues (skip focused if it's duplicated)
	if len(m.InProgress) > 0 && linesWritten < maxLines {
		// Only show header if in visible range
		if rowIdx >= offset || (m.FocusedIssue != nil && offset == 0) {
			if linesWritten < maxLines {
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
			if rowIdx >= offset && linesWritten < maxLines {
				line := "  " + m.formatIssueCompact(&issue)
				if isActive && cursor == rowIdx {
					line = selectedRowStyle.Render("> " + m.formatIssueCompact(&issue))
				}
				content.WriteString(line)
				content.WriteString("\n")
				linesWritten++
			}
			rowIdx++
		}
	}

	// Show down indicator if more content below
	if needsScroll && offset+maxLines < totalRows {
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

	// Clamp offset
	if offset > totalRows-maxLines && totalRows > maxLines {
		offset = totalRows - maxLines
	}
	if offset < 0 {
		offset = 0
	}

	// Build title with position if scrollable
	panelTitle := "ACTIVITY LOG"
	needsScroll := totalRows > maxLines
	if needsScroll {
		endPos := offset + maxLines
		if endPos > totalRows {
			endPos = totalRows
		}
		panelTitle = fmt.Sprintf("ACTIVITY LOG (%d-%d of %d)", offset+1, endPos, totalRows)
	}

	// Show up indicator if scrolled down
	if needsScroll && offset > 0 {
		content.WriteString(subtleStyle.Render("  ▲ more above"))
		content.WriteString("\n")
		maxLines-- // Reserve line for indicator
	}

	// Reserve line for down indicator if needed
	hasMoreBelow := needsScroll && offset+maxLines < totalRows
	if hasMoreBelow {
		maxLines--
	}

	visible := m.visibleItems(totalRows, offset, maxLines)
	for i := offset; i < offset+visible && i < totalRows; i++ {
		item := m.Activity[i]
		line := m.formatActivityItem(item)
		if isActive && cursor == i {
			line = selectedRowStyle.Render("> " + line)
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

	// Clamp offset
	if offset > totalRows-maxLines && totalRows > maxLines {
		offset = totalRows - maxLines
	}
	if offset < 0 {
		offset = 0
	}

	// Build title with position if scrollable
	panelTitle := "TASK LIST" + sortIndicator
	needsScroll := totalRows > maxLines
	if needsScroll {
		endPos := offset + maxLines
		if endPos > totalRows {
			endPos = totalRows
		}
		panelTitle = fmt.Sprintf("TASK LIST%s (%d-%d of %d)", sortIndicator, offset+1, endPos, totalRows)
	} else if m.SearchQuery != "" || m.IncludeClosed {
		panelTitle = fmt.Sprintf("TASK LIST%s (%d results)", sortIndicator, totalRows)
	}

	// Show up indicator if scrolled down
	if needsScroll && offset > 0 {
		content.WriteString(subtleStyle.Render("  ▲ more above"))
		content.WriteString("\n")
		maxLines-- // Reserve line for indicator
	}

	// Reserve line for down indicator if needed
	hasMoreBelow := needsScroll && offset+maxLines < totalRows
	if hasMoreBelow {
		maxLines--
	}

	// Track current category for section headers
	var currentCategory TaskListCategory
	linesWritten := 0

	for i, row := range m.TaskListRows {
		if linesWritten >= maxLines {
			break
		}

		// Skip rows before offset
		if i < offset {
			currentCategory = row.Category // Track category even when skipping
			continue
		}

		// Add category header when category changes
		if row.Category != currentCategory {
			if linesWritten > 0 && linesWritten < maxLines {
				content.WriteString("\n")
				linesWritten++
				if linesWritten >= maxLines {
					break
				}
			}
			header := m.formatCategoryHeader(row.Category)
			content.WriteString(header)
			content.WriteString("\n")
			linesWritten++
			currentCategory = row.Category
			if linesWritten >= maxLines {
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
		linesWritten++
	}

	// Show down indicator if more content below
	if hasMoreBelow {
		content.WriteString(subtleStyle.Render("  ▼ more below"))
		content.WriteString("\n")
	}

	return m.wrapPanel(panelTitle, content.String(), height, PanelTaskList)
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
			taskLine := fmt.Sprintf("%s %s %s",
				subtleStyle.Render(task.ID),
				formatStatus(task.Status),
				truncateString(task.Title, contentWidth-25))

			if modal.TaskSectionFocused && i == modal.EpicTasksCursor {
				taskLine = epicTaskSelectedStyle.Render("> " + task.ID + " " + formatStatus(task.Status) + " " + truncateString(task.Title, contentWidth-25))
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
			lines = append(lines, blockedColor.Render(fmt.Sprintf("⚠ BLOCKED BY (%d)", len(activeBlockers))))
			for _, dep := range activeBlockers {
				depLine := fmt.Sprintf("  %s %s %s",
					titleStyle.Render(dep.ID),
					formatStatus(dep.Status),
					truncateString(dep.Title, contentWidth-20))
				lines = append(lines, depLine)
			}
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
		lines = append(lines, sectionHeader.Render(fmt.Sprintf("BLOCKS (%d)", len(modal.Blocks))))
		for _, dep := range modal.Blocks {
			depLine := fmt.Sprintf("  %s %s %s",
				titleStyle.Render(dep.ID),
				formatStatus(dep.Status),
				truncateString(dep.Title, contentWidth-20))
			lines = append(lines, depLine)
		}
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

		// Build bar with appropriate color
		var statusColor lipgloss.Style
		switch status {
		case models.StatusOpen:
			statusColor = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))
		case models.StatusInProgress:
			statusColor = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		case models.StatusBlocked:
			statusColor = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		case models.StatusInReview:
			statusColor = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
		case models.StatusClosed:
			statusColor = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
		}

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

// renderConfirmation renders the confirmation dialog
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

	// Issue title (truncated)
	title := m.ConfirmTitle
	if len(title) > width-4 {
		title = title[:width-7] + "..."
	}
	content.WriteString(subtleStyle.Render(fmt.Sprintf("\"%s\"", title)))
	content.WriteString("\n\n")

	// Options
	content.WriteString("[Y]es  [N]o")

	confirmStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(errorColor).
		Padding(1, 2).
		Width(width)

	return confirmStyle.Render(content.String())
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

// renderSearchBar renders the search input bar when search mode is active
func (m Model) renderSearchBar() string {
	if !m.SearchMode && m.SearchQuery == "" {
		return ""
	}

	var sb strings.Builder

	// Icon and query display
	if m.SearchMode {
		sb.WriteString("🔍 ")
	} else {
		sb.WriteString(subtleStyle.Render("🔍 "))
	}

	if m.SearchQuery == "" {
		sb.WriteString(subtleStyle.Render("Search: "))
	} else {
		sb.WriteString("Search: ")
		sb.WriteString(m.SearchQuery)
	}

	// Cursor if in search mode
	if m.SearchMode {
		sb.WriteString("█")
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
	return helpStyle.Render(m.Keymap.GenerateHelp())
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
	idStr := subtleStyle.Render(issue.ID)
	priorityStr := formatPriority(issue.Priority)

	// Calculate available width for title:
	// m.Width - 4 (panel border) - 8 (row prefix "  [TAG] ") - ID - priority - 2 spaces
	overhead := 4 + 8 + lipgloss.Width(idStr) + lipgloss.Width(priorityStr) + 2
	titleWidth := m.Width - overhead
	if titleWidth < 20 {
		titleWidth = 20 // minimum reasonable width
	}

	return fmt.Sprintf("%s %s %s", idStr, priorityStr, truncateString(issue.Title, titleWidth))
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
