package monitor

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/query"
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
	if m.BoardPickerOpen {
		return keymap.ContextBoardPicker
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
	if m.BoardMode.Active {
		return keymap.ContextBoard
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

		if m.FormState != nil {
			moveToButtons := func(focus int) (tea.Model, tea.Cmd) {
				m.FormState.ButtonFocus = focus
				m.FormState.ButtonHover = 0
				return m, nil
			}

			switch keyMsg.Type {
			case tea.KeyTab:
				if m.FormState.ButtonFocus >= 0 {
					switch m.FormState.ButtonFocus {
					case formButtonFocusSubmit:
						return moveToButtons(formButtonFocusCancel)
					case formButtonFocusCancel:
						return moveToButtons(formButtonFocusForm)
					default:
						return moveToButtons(formButtonFocusSubmit)
					}
				}
				if m.FormState.focusedFieldKey() == m.FormState.lastFieldKey() {
					return moveToButtons(formButtonFocusSubmit)
				}
			case tea.KeyShiftTab:
				if m.FormState.ButtonFocus >= 0 {
					switch m.FormState.ButtonFocus {
					case formButtonFocusCancel:
						return moveToButtons(formButtonFocusSubmit)
					case formButtonFocusSubmit:
						return moveToButtons(formButtonFocusForm)
					default:
						return moveToButtons(formButtonFocusCancel)
					}
				}
				if m.FormState.focusedFieldKey() == m.FormState.firstFieldKey() {
					return moveToButtons(formButtonFocusCancel)
				}
			case tea.KeyEnter:
				switch m.FormState.ButtonFocus {
				case formButtonFocusSubmit:
					return m.executeCommand(keymap.CmdFormSubmit)
				case formButtonFocusCancel:
					return m.executeCommand(keymap.CmdFormCancel)
				}
			}

			if m.FormState.ButtonFocus >= 0 {
				return m, nil
			}
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
		} else if m.BoardPickerOpen {
			if m.BoardPickerCursor < len(m.AllBoards)-1 {
				m.BoardPickerCursor++
			}
		} else if m.BoardMode.Active {
			if m.BoardMode.Cursor < len(m.BoardMode.Issues)-1 {
				m.BoardMode.Cursor++
				m.ensureBoardCursorVisible()
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
		} else if m.BoardPickerOpen {
			if m.BoardPickerCursor > 0 {
				m.BoardPickerCursor--
			}
		} else if m.BoardMode.Active {
			if m.BoardMode.Cursor > 0 {
				m.BoardMode.Cursor--
				m.ensureBoardCursorVisible()
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
		} else if m.BoardPickerOpen {
			m.BoardPickerCursor = 0
		} else if m.BoardMode.Active {
			m.BoardMode.Cursor = 0
			m.BoardMode.ScrollOffset = 0
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
		} else if m.BoardPickerOpen {
			if len(m.AllBoards) > 0 {
				m.BoardPickerCursor = len(m.AllBoards) - 1
			}
		} else if m.BoardMode.Active {
			if len(m.BoardMode.Issues) > 0 {
				m.BoardMode.Cursor = len(m.BoardMode.Issues) - 1
				m.ensureBoardCursorVisible()
			}
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
		} else if m.BoardMode.Active {
			m.BoardMode.Cursor += pageSize
			if m.BoardMode.Cursor >= len(m.BoardMode.Issues) {
				m.BoardMode.Cursor = len(m.BoardMode.Issues) - 1
			}
			if m.BoardMode.Cursor < 0 {
				m.BoardMode.Cursor = 0
			}
			m.ensureBoardCursorVisible()
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
		} else if m.BoardMode.Active {
			m.BoardMode.Cursor -= pageSize
			if m.BoardMode.Cursor < 0 {
				m.BoardMode.Cursor = 0
			}
			m.ensureBoardCursorVisible()
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
		if m.BoardMode.Active {
			return m.openIssueFromBoard()
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

	// Button navigation for confirmation dialogs
	case keymap.CmdNextButton:
		if m.CloseConfirmOpen {
			// Cycle: input(0) -> confirm(1) -> cancel(2) -> input(0)
			m.CloseConfirmButtonFocus = (m.CloseConfirmButtonFocus + 1) % 3
			if m.CloseConfirmButtonFocus == 0 {
				m.CloseConfirmInput.Focus()
			} else {
				m.CloseConfirmInput.Blur()
			}
			return m, nil
		}
		if m.ConfirmOpen {
			// Cycle: yes(0) -> no(1) -> yes(0)
			m.ConfirmButtonFocus = (m.ConfirmButtonFocus + 1) % 2
			return m, nil
		}
		return m, nil

	case keymap.CmdPrevButton:
		if m.CloseConfirmOpen {
			// Reverse cycle: input(0) <- confirm(1) <- cancel(2) <- input(0)
			m.CloseConfirmButtonFocus = (m.CloseConfirmButtonFocus + 2) % 3
			if m.CloseConfirmButtonFocus == 0 {
				m.CloseConfirmInput.Focus()
			} else {
				m.CloseConfirmInput.Blur()
			}
			return m, nil
		}
		if m.ConfirmOpen {
			// Reverse cycle: yes(0) <- no(1) <- yes(0)
			m.ConfirmButtonFocus = (m.ConfirmButtonFocus + 1) % 2
			return m, nil
		}
		return m, nil

	case keymap.CmdSelect:
		// Execute focused button in confirmation dialogs
		if m.CloseConfirmOpen {
			switch m.CloseConfirmButtonFocus {
			case 0: // Input focused - confirm (same as Enter in input)
				return m.executeCloseWithReason()
			case 1: // Confirm button focused
				return m.executeCloseWithReason()
			case 2: // Cancel button focused
				m.CloseConfirmOpen = false
				m.CloseConfirmIssueID = ""
				m.CloseConfirmTitle = ""
				return m, nil
			}
		}
		if m.ConfirmOpen {
			switch m.ConfirmButtonFocus {
			case 0: // Yes button focused
				return m.executeDelete()
			case 1: // No button focused
				m.ConfirmOpen = false
				return m, nil
			}
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

	// Board commands
	case keymap.CmdOpenBoardPicker:
		return m.openBoardPicker()

	case keymap.CmdSelectBoard:
		return m.selectBoard()

	case keymap.CmdCloseBoardPicker:
		m.BoardPickerOpen = false
		return m, nil

	// Board mode commands
	case keymap.CmdExitBoardMode:
		return m.exitBoardMode()

	case keymap.CmdToggleBoardClosed:
		return m.toggleBoardClosed()

	case keymap.CmdCycleBoardStatusFilter:
		return m.cycleBoardStatusFilter()

	case keymap.CmdMoveIssueUp:
		return m.moveIssueInBoard(-1)

	case keymap.CmdMoveIssueDown:
		return m.moveIssueInBoard(1)
	}

	return m, nil
}

// openBoardPicker opens the board picker modal
func (m Model) openBoardPicker() (Model, tea.Cmd) {
	m.BoardPickerOpen = true
	m.BoardPickerCursor = 0
	return m, m.fetchBoards()
}

// selectBoard selects the currently highlighted board and activates board mode
func (m Model) selectBoard() (Model, tea.Cmd) {
	if !m.BoardPickerOpen || len(m.AllBoards) == 0 {
		return m, nil
	}
	if m.BoardPickerCursor >= len(m.AllBoards) {
		return m, nil
	}

	board := m.AllBoards[m.BoardPickerCursor]
	m.BoardMode.Active = true
	m.BoardMode.Board = &board
	m.BoardMode.Cursor = 0
	m.BoardMode.ScrollOffset = 0
	if m.BoardMode.StatusFilter == nil {
		m.BoardMode.StatusFilter = DefaultBoardStatusFilter()
	}
	m.BoardPickerOpen = false

	// Update last viewed
	if err := m.DB.UpdateBoardLastViewed(board.ID); err != nil {
		m.StatusMessage = "Error: " + err.Error()
		m.StatusIsError = true
	}

	return m, m.fetchBoardIssues(board.ID)
}

// openIssueFromBoard opens the issue modal for the currently selected board issue
func (m Model) openIssueFromBoard() (tea.Model, tea.Cmd) {
	if !m.BoardMode.Active || len(m.BoardMode.Issues) == 0 {
		return m, nil
	}
	if m.BoardMode.Cursor < 0 || m.BoardMode.Cursor >= len(m.BoardMode.Issues) {
		return m, nil
	}

	issueID := m.BoardMode.Issues[m.BoardMode.Cursor].Issue.ID
	return m.pushModal(issueID, PanelTaskList) // Use TaskList as source panel for board mode
}

// exitBoardMode switches back to the All Issues board
func (m Model) exitBoardMode() (Model, tea.Cmd) {
	// Get the built-in All Issues board
	board, err := m.DB.GetBoard("bd-all-issues")
	if err != nil {
		m.StatusMessage = "Error: " + err.Error()
		m.StatusIsError = true
		return m, nil
	}

	m.BoardMode.Active = true
	m.BoardMode.Board = board
	m.BoardMode.Cursor = 0
	m.BoardMode.ScrollOffset = 0
	if m.BoardMode.StatusFilter == nil {
		m.BoardMode.StatusFilter = DefaultBoardStatusFilter()
	}

	// Update last viewed
	if err := m.DB.UpdateBoardLastViewed(board.ID); err != nil {
		m.StatusMessage = "Error: " + err.Error()
		m.StatusIsError = true
	}

	return m, m.fetchBoardIssues(board.ID)
}

// toggleBoardClosed toggles the closed status in the board status filter
func (m Model) toggleBoardClosed() (Model, tea.Cmd) {
	if !m.BoardMode.Active || m.BoardMode.Board == nil {
		return m, nil
	}

	if m.BoardMode.StatusFilter == nil {
		m.BoardMode.StatusFilter = DefaultBoardStatusFilter()
	}
	m.BoardMode.StatusFilter[models.StatusClosed] = !m.BoardMode.StatusFilter[models.StatusClosed]

	if m.BoardMode.StatusFilter[models.StatusClosed] {
		m.StatusMessage = "Showing closed issues"
	} else {
		m.StatusMessage = "Hiding closed issues"
	}

	return m, tea.Batch(
		m.fetchBoardIssues(m.BoardMode.Board.ID),
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} }),
	)
}

// cycleBoardStatusFilter cycles through status filter presets
func (m Model) cycleBoardStatusFilter() (Model, tea.Cmd) {
	if !m.BoardMode.Active || m.BoardMode.Board == nil {
		return m, nil
	}

	// Cycle to next preset
	m.BoardStatusPreset = (m.BoardStatusPreset + 1) % 7 // 7 presets
	m.BoardMode.StatusFilter = m.BoardStatusPreset.ToFilter()

	m.StatusMessage = "Filter: " + m.BoardStatusPreset.Name()

	return m, tea.Batch(
		m.fetchBoardIssues(m.BoardMode.Board.ID),
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} }),
	)
}

// moveIssueInBoard moves the current issue up or down in the board
func (m Model) moveIssueInBoard(direction int) (Model, tea.Cmd) {
	if !m.BoardMode.Active || m.BoardMode.Board == nil {
		return m, nil
	}
	if len(m.BoardMode.Issues) == 0 {
		return m, nil
	}

	cursor := m.BoardMode.Cursor
	if cursor < 0 || cursor >= len(m.BoardMode.Issues) {
		return m, nil
	}

	currentIssue := m.BoardMode.Issues[cursor]

	// Determine target index
	targetIdx := cursor + direction
	if targetIdx < 0 || targetIdx >= len(m.BoardMode.Issues) {
		return m, nil // Can't move beyond bounds
	}

	targetIssue := m.BoardMode.Issues[targetIdx]

	// Both must be positioned for swap, or we need to position the current issue
	if currentIssue.HasPosition && targetIssue.HasPosition {
		// Both positioned - swap positions
		if err := m.DB.SwapIssuePositions(m.BoardMode.Board.ID, currentIssue.Issue.ID, targetIssue.Issue.ID); err != nil {
			m.StatusMessage = "Error: " + err.Error()
			m.StatusIsError = true
			return m, nil
		}
		m.BoardMode.Cursor = targetIdx
	} else if !currentIssue.HasPosition {
		// Current issue is unpositioned - insert at target position
		var insertPos int
		if targetIssue.HasPosition {
			if direction < 0 {
				// Moving up: insert just above target
				insertPos = targetIssue.Position
			} else {
				// Moving down: insert just below target
				insertPos = targetIssue.Position + 1
			}
		} else {
			// Target also unpositioned, find nearest positioned neighbor or use 1
			insertPos = 1
			for i := targetIdx; i >= 0; i-- {
				if m.BoardMode.Issues[i].HasPosition {
					if direction < 0 {
						insertPos = m.BoardMode.Issues[i].Position
					} else {
						insertPos = m.BoardMode.Issues[i].Position + 1
					}
					break
				}
			}
		}
		if err := m.DB.SetIssuePosition(m.BoardMode.Board.ID, currentIssue.Issue.ID, insertPos); err != nil {
			m.StatusMessage = "Error: " + err.Error()
			m.StatusIsError = true
			return m, nil
		}
		m.BoardMode.Cursor = targetIdx
	} else {
		// Current is positioned but target is not - can't swap with unpositioned
		return m, nil
	}

	return m, m.fetchBoardIssues(m.BoardMode.Board.ID)
}

// fetchBoards returns a command that fetches all boards
func (m Model) fetchBoards() tea.Cmd {
	return func() tea.Msg {
		boards, err := m.DB.ListBoards()
		return BoardsDataMsg{Boards: boards, Error: err}
	}
}

// fetchBoardIssues returns a command that fetches issues for a board
func (m Model) fetchBoardIssues(boardID string) tea.Cmd {
	return func() tea.Msg {
		// Get the board to check if it has a query
		board, err := m.DB.GetBoard(boardID)
		if err != nil {
			return BoardIssuesMsg{BoardID: boardID, Error: err}
		}

		var issues []models.BoardIssueView
		if board.Query != "" {
			// Execute TDQ query, then apply positions
			queryResults, err := query.Execute(m.DB, board.Query, m.SessionID, query.ExecuteOptions{})
			if err != nil {
				return BoardIssuesMsg{BoardID: boardID, Error: err}
			}
			issues, err = m.DB.ApplyBoardPositions(boardID, queryResults)
			if err != nil {
				return BoardIssuesMsg{BoardID: boardID, Error: err}
			}
		} else {
			// Empty query - use GetBoardIssues which handles this case
			issues, err = m.DB.GetBoardIssues(boardID, m.SessionID, nil)
			if err != nil {
				return BoardIssuesMsg{BoardID: boardID, Error: err}
			}
		}

		return BoardIssuesMsg{BoardID: boardID, Issues: issues}
	}
}
