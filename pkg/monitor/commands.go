package monitor

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/pkg/monitor/keymap"
)

// currentContext returns the keymap context based on current UI state
func (m Model) currentContext() keymap.Context {
	if m.HelpOpen {
		return keymap.ContextHelp
	}
	if m.ConfirmOpen {
		return keymap.ContextConfirm
	}
	if m.FormOpen {
		return keymap.ContextForm
	}
	if m.HandoffsOpen {
		return keymap.ContextHandoffs
	}
	if m.StatsOpen {
		return keymap.ContextStats
	}
	if m.ModalOpen() {
		if modal := m.CurrentModal(); modal != nil {
			// Check if parent epic row is focused
			if modal.ParentEpicFocused {
				return keymap.ContextParentEpicFocused
			}
			// Check if epic tasks section is focused
			if modal.TaskSectionFocused {
				return keymap.ContextEpicTasks
			}
			// Check if blocked-by section is focused
			if modal.BlockedBySectionFocused {
				return keymap.ContextBlockedByFocused
			}
			// Check if blocks section is focused
			if modal.BlocksSectionFocused {
				return keymap.ContextBlocksFocused
			}
		}
		return keymap.ContextModal
	}
	if m.SearchMode {
		return keymap.ContextSearch
	}
	return keymap.ContextMain
}

// handleFormUpdate handles all messages when form is open
func (m Model) handleFormUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle our custom key bindings first
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case keyMsg.Type == tea.KeyCtrlS:
			return m.executeCommand(keymap.CmdFormSubmit)
		case keyMsg.Type == tea.KeyEsc:
			return m.executeCommand(keymap.CmdFormCancel)
		case keyMsg.Type == tea.KeyCtrlX:
			return m.executeCommand(keymap.CmdFormToggleExtend)
		case keyMsg.Type == tea.KeyCtrlO:
			return m.executeCommand(keymap.CmdFormOpenEditor)
		}
	}

	// Handle editor finished message
	if editorMsg, ok := msg.(EditorFinishedMsg); ok {
		return m.handleEditorFinished(editorMsg)
	}

	// Handle window resize
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.Width = sizeMsg.Width
		m.Height = sizeMsg.Height
		m.updatePanelBounds()
	}

	// Forward message to huh form
	form, cmd := m.FormState.Form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.FormState.Form = f
	}

	// Check if form completed (user pressed enter on last field)
	if m.FormState.Form.State == huh.StateCompleted {
		return m.executeCommand(keymap.CmdFormSubmit)
	}

	return m, cmd
}

// handleKey processes key input using the centralized keymap registry
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ctx := m.currentContext()

	// Search mode: forward most keys to textinput for cursor support
	if ctx == keymap.ContextSearch {
		// Special case: ? triggers help even in search mode
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '?' {
			return m.executeCommand(keymap.CmdToggleHelp)
		}

		// Check if this key is bound to a search command (escape, enter, ctrl+u)
		if cmd, found := m.Keymap.Lookup(msg, ctx); found {
			return m.executeCommand(cmd)
		}

		// Forward all other keys to the textinput (handles cursor, typing, etc.)
		var inputCmd tea.Cmd
		m.SearchInput, inputCmd = m.SearchInput.Update(msg)

		// Sync SearchQuery with input value
		newQuery := m.SearchInput.Value()
		if newQuery != m.SearchQuery {
			m.SearchQuery = newQuery
			return m, tea.Batch(inputCmd, m.fetchData())
		}
		return m, inputCmd
	}

	// Look up command from keymap
	cmd, found := m.Keymap.Lookup(msg, ctx)
	if !found {
		return m, nil
	}

	// Execute command
	return m.executeCommand(cmd)
}

// executeCommand executes a keymap command and returns the updated model and any tea.Cmd
func (m Model) executeCommand(cmd keymap.Command) (tea.Model, tea.Cmd) {
	switch cmd {
	// Global commands
	case keymap.CmdQuit:
		return m, tea.Quit

	case keymap.CmdToggleHelp:
		// Show TDQ help when in search mode, regular help otherwise
		if m.SearchMode {
			m.ShowTDQHelp = !m.ShowTDQHelp
			m.HelpOpen = false
		} else {
			m.HelpOpen = !m.HelpOpen
			m.ShowTDQHelp = false
			if m.HelpOpen {
				// Initialize scroll position and calculate total lines
				m.HelpScroll = 0
				helpText := m.Keymap.GenerateHelp()
				m.HelpTotalLines = strings.Count(helpText, "\n") + 1
			}
		}
		return m, nil

	case keymap.CmdRefresh:
		if modal := m.CurrentModal(); modal != nil {
			return m, tea.Batch(m.fetchData(), m.fetchIssueDetails(modal.IssueID))
		}
		if m.HandoffsOpen {
			return m, m.fetchHandoffs()
		}
		if m.StatsOpen {
			return m, m.fetchStats()
		}
		return m, m.fetchData()

	// Panel navigation (main context)
	case keymap.CmdNextPanel:
		m.ActivePanel = (m.ActivePanel + 1) % 3
		m.clampCursor(m.ActivePanel)
		m.ensureCursorVisible(m.ActivePanel)
		return m, nil

	case keymap.CmdPrevPanel:
		m.ActivePanel = (m.ActivePanel + 2) % 3
		m.clampCursor(m.ActivePanel)
		m.ensureCursorVisible(m.ActivePanel)
		return m, nil

	// Cursor movement
	case keymap.CmdCursorDown, keymap.CmdScrollDown:
		if m.HelpOpen {
			m.HelpScroll++
			m.clampHelpScroll()
			return m, nil
		}
		if modal := m.CurrentModal(); modal != nil {
			if modal.ParentEpicFocused {
				// Unfocus parent epic, move past epic zone so next j scrolls
				modal.ParentEpicFocused = false
				modal.Scroll = 1
			} else if modal.TaskSectionFocused {
				// Move epic task cursor, or transition to scroll at end
				if modal.EpicTasksCursor < len(modal.EpicTasks)-1 {
					modal.EpicTasksCursor++
				} else {
					// At last task, try to scroll. If can scroll, unfocus tasks first.
					maxScroll := m.modalMaxScroll(modal)
					if modal.Scroll < maxScroll {
						modal.TaskSectionFocused = false
						modal.Scroll++
					}
					// If can't scroll, stay at last task
				}
			} else if modal.BlockedBySectionFocused {
				// Move blocked-by cursor within bounds
				activeBlockers := filterActiveBlockers(modal.BlockedBy)
				if modal.BlockedByCursor < len(activeBlockers)-1 {
					modal.BlockedByCursor++
				}
				// At last item, stay there
			} else if modal.BlocksSectionFocused {
				// Move blocks cursor within bounds
				if modal.BlocksCursor < len(modal.Blocks)-1 {
					modal.BlocksCursor++
				}
				// At last item, stay there
			} else if modal.Scroll == 0 && modal.ParentEpic != nil {
				// At top with parent epic, focus it first before scrolling
				modal.ParentEpicFocused = true
			} else {
				// Scroll down, clamped to max
				maxScroll := m.modalMaxScroll(modal)
				if modal.Scroll < maxScroll {
					modal.Scroll++
				}
			}
		} else if m.HandoffsOpen {
			if m.HandoffsCursor < len(m.HandoffsData)-1 {
				m.HandoffsCursor++
			}
		} else if m.StatsOpen {
			m.StatsScroll++
		} else {
			m.moveCursor(1)
		}
		return m, nil

	case keymap.CmdCursorUp, keymap.CmdScrollUp:
		if m.HelpOpen {
			m.HelpScroll--
			m.clampHelpScroll()
			return m, nil
		}
		if modal := m.CurrentModal(); modal != nil {
			if modal.ParentEpicFocused {
				// Already at top, stay focused on epic
			} else if modal.TaskSectionFocused {
				// Move epic task cursor
				if modal.EpicTasksCursor > 0 {
					modal.EpicTasksCursor--
				}
				// At first task, stay there (user can use Tab to unfocus)
			} else if modal.BlockedBySectionFocused {
				// Move blocked-by cursor
				if modal.BlockedByCursor > 0 {
					modal.BlockedByCursor--
				}
				// At first item, stay there
			} else if modal.BlocksSectionFocused {
				// Move blocks cursor
				if modal.BlocksCursor > 0 {
					modal.BlocksCursor--
				}
				// At first item, stay there
			} else if modal.Scroll == 0 && modal.ParentEpic != nil {
				// At top of scroll with parent epic, focus it
				modal.ParentEpicFocused = true
			} else if modal.Scroll > 0 {
				modal.Scroll--
			}
		} else if m.HandoffsOpen {
			if m.HandoffsCursor > 0 {
				m.HandoffsCursor--
			}
		} else if m.StatsOpen {
			if m.StatsScroll > 0 {
				m.StatsScroll--
			}
		} else {
			m.moveCursor(-1)
		}
		return m, nil

	case keymap.CmdCursorTop:
		if m.HelpOpen {
			m.HelpScroll = 0
			return m, nil
		}
		if modal := m.CurrentModal(); modal != nil {
			modal.Scroll = 0
		} else if m.HandoffsOpen {
			m.HandoffsCursor = 0
			m.HandoffsScroll = 0
		} else if m.StatsOpen {
			m.StatsScroll = 0
		} else {
			m.Cursor[m.ActivePanel] = 0
			m.saveSelectedID(m.ActivePanel)
			m.ensureCursorVisible(m.ActivePanel)
		}
		return m, nil

	case keymap.CmdCursorBottom:
		if m.HelpOpen {
			m.HelpScroll = m.helpMaxScroll()
			return m, nil
		}
		if modal := m.CurrentModal(); modal != nil {
			modal.Scroll = m.modalMaxScroll(modal)
		} else if m.HandoffsOpen {
			if len(m.HandoffsData) > 0 {
				m.HandoffsCursor = len(m.HandoffsData) - 1
			}
		} else if m.StatsOpen {
			m.StatsScroll = 9999 // Will be clamped by view
		} else {
			count := m.rowCount(m.ActivePanel)
			if count > 0 {
				m.Cursor[m.ActivePanel] = count - 1
				m.saveSelectedID(m.ActivePanel)
				m.ensureCursorVisible(m.ActivePanel)
			}
		}
		return m, nil

	case keymap.CmdHalfPageDown:
		pageSize := m.visibleHeightForPanel(m.ActivePanel) / 2
		if pageSize < 1 {
			pageSize = 5
		}
		if m.HelpOpen {
			pageSize = m.helpVisibleHeight() / 2
			if pageSize < 1 {
				pageSize = 5
			}
			m.HelpScroll += pageSize
			m.clampHelpScroll()
			return m, nil
		}
		if modal := m.CurrentModal(); modal != nil {
			maxScroll := m.modalMaxScroll(modal)
			modal.Scroll += pageSize
			if modal.Scroll > maxScroll {
				modal.Scroll = maxScroll
			}
		} else if m.HandoffsOpen {
			m.HandoffsCursor += pageSize
			if m.HandoffsCursor >= len(m.HandoffsData) {
				m.HandoffsCursor = len(m.HandoffsData) - 1
			}
			if m.HandoffsCursor < 0 {
				m.HandoffsCursor = 0
			}
		} else if m.StatsOpen {
			m.StatsScroll += pageSize
		} else {
			for i := 0; i < pageSize; i++ {
				m.moveCursor(1)
			}
		}
		return m, nil

	case keymap.CmdHalfPageUp:
		pageSize := m.visibleHeightForPanel(m.ActivePanel) / 2
		if pageSize < 1 {
			pageSize = 5
		}
		if m.HelpOpen {
			pageSize = m.helpVisibleHeight() / 2
			if pageSize < 1 {
				pageSize = 5
			}
			m.HelpScroll -= pageSize
			m.clampHelpScroll()
			return m, nil
		}
		if modal := m.CurrentModal(); modal != nil {
			modal.Scroll -= pageSize
			if modal.Scroll < 0 {
				modal.Scroll = 0
			}
		} else if m.HandoffsOpen {
			m.HandoffsCursor -= pageSize
			if m.HandoffsCursor < 0 {
				m.HandoffsCursor = 0
			}
		} else if m.StatsOpen {
			m.StatsScroll -= pageSize
			if m.StatsScroll < 0 {
				m.StatsScroll = 0
			}
		} else {
			for i := 0; i < pageSize; i++ {
				m.moveCursor(-1)
			}
		}
		return m, nil

	case keymap.CmdFullPageDown:
		pageSize := m.visibleHeightForPanel(m.ActivePanel)
		if pageSize < 1 {
			pageSize = 10
		}
		if m.HelpOpen {
			pageSize = m.helpVisibleHeight()
			if pageSize < 1 {
				pageSize = 10
			}
			m.HelpScroll += pageSize
			m.clampHelpScroll()
			return m, nil
		}
		if modal := m.CurrentModal(); modal != nil {
			maxScroll := m.modalMaxScroll(modal)
			modal.Scroll += pageSize
			if modal.Scroll > maxScroll {
				modal.Scroll = maxScroll
			}
		} else if m.StatsOpen {
			m.StatsScroll += pageSize
		} else {
			for i := 0; i < pageSize; i++ {
				m.moveCursor(1)
			}
		}
		return m, nil

	case keymap.CmdFullPageUp:
		pageSize := m.visibleHeightForPanel(m.ActivePanel)
		if pageSize < 1 {
			pageSize = 10
		}
		if m.HelpOpen {
			pageSize = m.helpVisibleHeight()
			if pageSize < 1 {
				pageSize = 10
			}
			m.HelpScroll -= pageSize
			m.clampHelpScroll()
			return m, nil
		}
		if modal := m.CurrentModal(); modal != nil {
			modal.Scroll -= pageSize
			if modal.Scroll < 0 {
				modal.Scroll = 0
			}
		} else if m.StatsOpen {
			m.StatsScroll -= pageSize
			if m.StatsScroll < 0 {
				m.StatsScroll = 0
			}
		} else {
			for i := 0; i < pageSize; i++ {
				m.moveCursor(-1)
			}
		}
		return m, nil

	// Modal navigation
	case keymap.CmdNavigatePrev:
		// Check if epic tasks are focused - navigate within epic
		if modal := m.CurrentModal(); modal != nil && modal.TaskSectionFocused {
			return m.navigateEpicTask(-1)
		}
		return m.navigateModal(-1)

	case keymap.CmdNavigateNext:
		// Check if epic tasks are focused - navigate within epic
		if modal := m.CurrentModal(); modal != nil && modal.TaskSectionFocused {
			return m.navigateEpicTask(1)
		}
		return m.navigateModal(1)

	case keymap.CmdClose:
		if m.ModalOpen() {
			m.closeModal()
		} else if m.HandoffsOpen {
			m.closeHandoffsModal()
		} else if m.StatsOpen {
			m.closeStatsModal()
		}
		return m, nil

	// Actions
	case keymap.CmdOpenDetails:
		if m.HandoffsOpen {
			return m.openIssueFromHandoffs()
		}
		return m.openModal()

	case keymap.CmdOpenStats:
		return m.openStatsModal()

	case keymap.CmdOpenHandoffs:
		return m.openHandoffsModal()

	case keymap.CmdSearch:
		m.SearchMode = true
		m.SearchQuery = ""
		m.SearchInput.SetValue("")
		m.SearchInput.Focus()
		m.updatePanelBounds() // Recalc bounds for search bar
		return m, m.SearchInput.Cursor.BlinkCmd()

	case keymap.CmdToggleClosed:
		m.IncludeClosed = !m.IncludeClosed
		return m, m.fetchData()

	case keymap.CmdCycleSortMode:
		m.SortMode = (m.SortMode + 1) % 3
		oldQuery := m.SearchQuery
		m.SearchQuery = updateQuerySort(m.SearchQuery, m.SortMode)
		// Recalc bounds if search bar visibility changed
		if (oldQuery == "") != (m.SearchQuery == "") {
			m.updatePanelBounds()
		}
		return m, m.fetchData()

	case keymap.CmdCycleTypeFilter:
		m.TypeFilterMode = (m.TypeFilterMode + 1) % 6 // 6 modes: none + 5 types
		oldQuery := m.SearchQuery
		m.SearchQuery = updateQueryType(m.SearchQuery, m.TypeFilterMode)
		// Recalc bounds if search bar visibility changed
		if (oldQuery == "") != (m.SearchQuery == "") {
			m.updatePanelBounds()
		}
		if m.TypeFilterMode == TypeFilterNone {
			m.StatusMessage = "Type filter: all"
		} else {
			m.StatusMessage = "Type filter: " + m.TypeFilterMode.String()
		}
		return m, tea.Batch(m.fetchData(), tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		}))

	case keymap.CmdMarkForReview:
		// Mark for review works from modal, TaskList, or CurrentWork panel
		if m.ModalOpen() {
			return m.markForReview()
		}
		if m.ActivePanel == PanelCurrentWork || m.ActivePanel == PanelTaskList {
			return m.markForReview()
		}
		return m, m.fetchData()

	case keymap.CmdApprove:
		if m.ActivePanel == PanelTaskList {
			return m.approveIssue()
		}
		return m, nil

	case keymap.CmdDelete:
		return m.confirmDelete()

	case keymap.CmdCloseIssue:
		return m.confirmClose()

	case keymap.CmdReopenIssue:
		return m.reopenIssue()

	// Search commands
	case keymap.CmdSearchConfirm:
		m.SearchMode = false
		m.ShowTDQHelp = false
		m.SearchInput.Blur()
		m.updatePanelBounds() // Recalc bounds after search bar closes
		return m, nil

	case keymap.CmdSearchCancel:
		// If TDQ help is open, close it but stay in search mode
		if m.ShowTDQHelp {
			m.ShowTDQHelp = false
			return m, nil
		}
		// Otherwise exit search mode entirely
		m.SearchMode = false
		m.SearchQuery = ""
		m.SearchInput.SetValue("")
		m.SearchInput.Blur()
		m.updatePanelBounds() // Recalc bounds after search bar closes
		return m, m.fetchData()

	case keymap.CmdSearchClear:
		if m.SearchQuery == "" {
			return m, nil // Nothing to clear
		}
		m.SearchQuery = ""
		m.SearchInput.SetValue("")
		// Recalc bounds since search bar disappears when query is empty
		if !m.SearchMode {
			m.updatePanelBounds()
		}
		return m, m.fetchData()

	// Confirmation commands
	case keymap.CmdConfirm:
		if m.CloseConfirmOpen {
			return m.executeCloseWithReason()
		}
		if m.ConfirmOpen && m.ConfirmAction == "delete" {
			return m.executeDelete()
		}
		return m, nil

	case keymap.CmdCancel:
		if m.CloseConfirmOpen {
			m.CloseConfirmOpen = false
			m.CloseConfirmIssueID = ""
			m.CloseConfirmTitle = ""
			return m, nil
		}
		if m.ConfirmOpen {
			m.ConfirmOpen = false
		}
		return m, nil

	// Section navigation - Tab cycles through focusable sections (top-to-bottom visual order)
	case keymap.CmdFocusTaskSection:
		if modal := m.CurrentModal(); modal != nil {
			// Determine available sections
			hasParentEpic := modal.ParentEpic != nil
			hasEpicTasks := modal.Issue != nil && modal.Issue.Type == models.TypeEpic && len(modal.EpicTasks) > 0
			activeBlockers := filterActiveBlockers(modal.BlockedBy)
			hasBlockedBy := len(activeBlockers) > 0
			hasBlocks := len(modal.Blocks) > 0

			// Cycle through sections in top-to-bottom order:
			// scroll -> parent-epic -> epic-tasks -> blocked-by -> blocks -> scroll
			if modal.ParentEpicFocused {
				modal.ParentEpicFocused = false
				if hasEpicTasks {
					modal.TaskSectionFocused = true
					modal.EpicTasksCursor = 0
				} else if hasBlockedBy {
					modal.BlockedBySectionFocused = true
					modal.BlockedByCursor = 0
				} else if hasBlocks {
					modal.BlocksSectionFocused = true
					modal.BlocksCursor = 0
				}
				// else: back to scroll mode (all false)
			} else if modal.TaskSectionFocused {
				modal.TaskSectionFocused = false
				if hasBlockedBy {
					modal.BlockedBySectionFocused = true
					modal.BlockedByCursor = 0
				} else if hasBlocks {
					modal.BlocksSectionFocused = true
					modal.BlocksCursor = 0
				}
				// else: back to scroll mode (all false)
			} else if modal.BlockedBySectionFocused {
				modal.BlockedBySectionFocused = false
				if hasBlocks {
					modal.BlocksSectionFocused = true
					modal.BlocksCursor = 0
				}
				// else: back to scroll mode (all false)
			} else if modal.BlocksSectionFocused {
				modal.BlocksSectionFocused = false
				// back to scroll mode (all false)
			} else {
				// Currently in scroll mode - focus first available section
				if hasParentEpic {
					modal.ParentEpicFocused = true
				} else if hasEpicTasks {
					modal.TaskSectionFocused = true
					modal.EpicTasksCursor = 0
				} else if hasBlockedBy {
					modal.BlockedBySectionFocused = true
					modal.BlockedByCursor = 0
				} else if hasBlocks {
					modal.BlocksSectionFocused = true
					modal.BlocksCursor = 0
				}
				// else: no sections to focus, stay in scroll mode
			}
		}
		return m, nil

	case keymap.CmdOpenEpicTask:
		if modal := m.CurrentModal(); modal != nil && modal.TaskSectionFocused {
			if modal.EpicTasksCursor < len(modal.EpicTasks) {
				taskID := modal.EpicTasks[modal.EpicTasksCursor].ID
				// Don't reset TaskSectionFocused - preserve parent modal state for when we return
				// Set navigation scope to epic's children for l/r navigation
				return m.pushModalWithScope(taskID, m.ModalSourcePanel(), modal.EpicTasks)
			}
		}
		return m, nil

	case keymap.CmdOpenParentEpic:
		if modal := m.CurrentModal(); modal != nil && modal.ParentEpic != nil {
			modal.ParentEpicFocused = false // Unfocus before pushing
			return m.pushModal(modal.ParentEpic.ID, m.ModalSourcePanel())
		}
		return m, nil

	case keymap.CmdOpenBlockedByIssue:
		if modal := m.CurrentModal(); modal != nil && modal.BlockedBySectionFocused {
			activeBlockers := filterActiveBlockers(modal.BlockedBy)
			if modal.BlockedByCursor < len(activeBlockers) {
				modal.BlockedBySectionFocused = false // Unfocus before pushing
				return m.pushModal(activeBlockers[modal.BlockedByCursor].ID, m.ModalSourcePanel())
			}
		}
		return m, nil

	case keymap.CmdOpenBlocksIssue:
		if modal := m.CurrentModal(); modal != nil && modal.BlocksSectionFocused {
			if modal.BlocksCursor < len(modal.Blocks) {
				modal.BlocksSectionFocused = false // Unfocus before pushing
				return m.pushModal(modal.Blocks[modal.BlocksCursor].ID, m.ModalSourcePanel())
			}
		}
		return m, nil

	case keymap.CmdCopyToClipboard:
		return m.copyCurrentIssueToClipboard()

	case keymap.CmdCopyIDToClipboard:
		return m.copyIssueIDToClipboard()

	// Form commands
	case keymap.CmdNewIssue:
		return m.openNewIssueForm()

	case keymap.CmdEditIssue:
		return m.openEditIssueForm()

	case keymap.CmdFormSubmit:
		return m.submitForm()

	case keymap.CmdFormCancel:
		m.closeForm()
		return m, nil

	case keymap.CmdFormToggleExtend:
		if m.FormState != nil {
			m.FormState.ToggleExtended()
		}
		return m, nil

	case keymap.CmdFormOpenEditor:
		return m.openExternalEditor()
	}

	return m, nil
}
