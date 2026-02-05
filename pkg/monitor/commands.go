package monitor

import (
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/marcus/td/internal/agent"
	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/query"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
	"github.com/marcus/td/pkg/monitor/keymap"
	"github.com/marcus/td/pkg/monitor/mouse"
)

// currentContext returns the keymap context based on current UI state
func (m Model) currentContext() keymap.Context {
	if m.SyncPromptOpen {
		return keymap.ContextSyncPrompt
	}
	if m.GettingStartedOpen {
		return keymap.ContextGettingStarted
	}
	if m.HelpOpen {
		return keymap.ContextHelp
	}
	if m.CloseConfirmOpen {
		return keymap.ContextCloseConfirm
	}
	if m.ConfirmOpen {
		return keymap.ContextConfirm
	}
	if m.BoardEditorOpen {
		return keymap.ContextBoardEditor
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
	if m.ShowTDQHelp {
		return keymap.ContextTDQHelp
	}
	// Search mode takes priority - it's an overlay that captures input
	if m.SearchMode {
		return keymap.ContextSearch
	}
	// Modal takes priority over board mode - ESC should close modal, not exit board
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
	// Board mode context when Task List is active and in board mode
	if m.ActivePanel == PanelTaskList && m.TaskListMode == TaskListModeBoard {
		return keymap.ContextBoard
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
				// When moving away from form fields, blur the focused field
				if focus != formButtonFocusForm && m.FormState.ButtonFocus == formButtonFocusForm {
					if field := m.FormState.Form.GetFocusedField(); field != nil {
						field.Blur()
					}
				}
				// When moving back to form fields, focus the appropriate field
				if focus == formButtonFocusForm && m.FormState.ButtonFocus != formButtonFocusForm {
					if field := m.FormState.Form.GetFocusedField(); field != nil {
						field.Focus()
					}
				}
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
		modalWidth, _ := m.formModalDimensions()
		formWidth := modalWidth - 4
		m.FormState.Width = formWidth
		m.FormState.Form.WithWidth(formWidth)
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

	// Sync Prompt modal: let declarative modal handle keys first
	if m.SyncPromptOpen && m.SyncPromptModal != nil {
		action, cmd := m.SyncPromptModal.HandleKey(msg)
		if action != "" {
			return m, m.handleSyncPromptAction(action)
		}
		if cmd != nil {
			return m, cmd
		}
		// Fall through to keymap for esc, etc.
	}

	// Getting Started modal: let declarative modal handle keys first
	if m.GettingStartedOpen && m.GettingStartedModal != nil {
		action, cmd := m.GettingStartedModal.HandleKey(msg)
		if action != "" {
			return m.handleGettingStartedAction(action)
		}
		if cmd != nil {
			return m, cmd
		}
		// Fall through to keymap for esc, q, I, etc.
	}

	// TDQ Help modal: let declarative modal handle keys first
	if m.ShowTDQHelp && m.TDQHelpModal != nil {
		action, cmd := m.TDQHelpModal.HandleKey(msg)
		if action != "" {
			return m.handleTDQHelpAction(action)
		}
		if cmd != nil {
			return m, cmd
		}
		// Fall through to keymap for esc, etc.
	}

	// Stats modal: let declarative modal handle keys first (when data is ready)
	if m.StatsOpen && m.StatsModal != nil && !m.StatsLoading && m.StatsError == nil {
		action, cmd := m.StatsModal.HandleKey(msg)
		if action != "" {
			return m.handleStatsAction(action)
		}
		if cmd != nil {
			return m, cmd
		}
		// Fall through to keymap for esc, scroll keys, etc.
	}

	// Handoffs modal: let declarative modal handle keys first (when data is ready)
	if m.HandoffsOpen && m.HandoffsModal != nil && !m.HandoffsLoading && m.HandoffsError == nil && len(m.HandoffsData) > 0 {
		action, cmd := m.HandoffsModal.HandleKey(msg)
		if action != "" {
			return m.handleHandoffsAction(action)
		}
		if cmd != nil {
			return m, cmd
		}
		// NOTE: j/k/up/down/home/end navigation is handled by the keymap below, not by the
		// list section. The list's Update modifies *selectedIdx which points to a stale
		// model instance (due to value receiver semantics). We let the keymap handlers
		// update m.HandoffsCursor on the current copy instead.
		// Fall through to keymap for navigation, ctrl+d, G, g g, r (refresh), etc.
	}

	// Board editor modal: let declarative modal handle keys first
	if m.BoardEditorOpen && m.BoardEditorModal != nil {
		// Delete confirmation sub-modal gets special handling
		if m.BoardEditorDeleteConfirm {
			action, cmd := m.BoardEditorModal.HandleKey(msg)
			if action != "" {
				return m.handleBoardEditorAction(action)
			}
			if cmd != nil {
				return m, cmd
			}
			// Handle esc to cancel delete
			if msg.Type == tea.KeyEsc {
				return m.handleBoardEditorAction("delete-cancel")
			}
			return m, nil
		}

		// For info mode, just let modal handle keys
		if m.BoardEditorMode == "info" {
			action, cmd := m.BoardEditorModal.HandleKey(msg)
			if action != "" {
				return m.handleBoardEditorAction(action)
			}
			if cmd != nil {
				return m, cmd
			}
			if msg.Type == tea.KeyEsc {
				return m.handleBoardEditorAction("cancel")
			}
			return m, nil
		}

		// Edit/Create mode: shared pointers let modal handle all keys.
		// Inputs are stored as *textinput.Model / *textarea.Model on the Model
		// struct. Bubbletea copies the pointer (not the data), so the modal's
		// sections and all Model copies reference the same underlying instance.
		// No manual forwarding or Focus/Blur sync needed.

		// Intercept Ctrl+S (modal doesn't know this shortcut)
		if msg.Type == tea.KeyCtrlS {
			return m.handleBoardEditorAction("save")
		}

		prevQuery := ""
		if m.BoardEditorQueryInput != nil {
			prevQuery = m.BoardEditorQueryInput.Value()
		}

		action, cmd := m.BoardEditorModal.HandleKey(msg)
		if action != "" {
			return m.handleBoardEditorAction(action)
		}

		// Check if query changed for live preview debounce
		if m.BoardEditorQueryInput != nil {
			newQuery := m.BoardEditorQueryInput.Value()
			if newQuery != prevQuery && newQuery != "" {
				return m, tea.Batch(cmd, m.boardEditorDebouncedPreview(newQuery))
			}
		}
		return m, cmd
	}

	// Board picker modal: let declarative modal handle keys first (when data is ready)
	if m.BoardPickerOpen && m.BoardPickerModal != nil && len(m.AllBoards) > 0 {
		action, cmd := m.BoardPickerModal.HandleKey(msg)
		if action != "" {
			return m.handleBoardPickerAction(action)
		}
		if cmd != nil {
			return m, cmd
		}
		// NOTE: j/k/up/down/home/end navigation is handled by the keymap below, not by the
		// list section. The list's Update modifies *selectedIdx which points to a stale
		// model instance (due to value receiver semantics). We let the keymap handlers
		// update m.BoardPickerCursor on the current copy instead.
		// Fall through to keymap for navigation, esc, etc.
	}

	// Delete confirmation modal: let declarative modal handle keys first
	if m.ConfirmOpen && m.DeleteConfirmModal != nil && m.DeleteConfirmMouseHandler != nil {
		// Handle Y/N quick keys directly (modal doesn't know about these)
		key := msg.String()
		switch key {
		case "y", "Y":
			return m.handleDeleteConfirmAction("yes")
		case "n", "N":
			return m.handleDeleteConfirmAction("no")
		}

		// Let declarative modal handle tab/shift+tab/enter/esc
		action, cmd := m.DeleteConfirmModal.HandleKey(msg)
		if action != "" {
			return m.handleDeleteConfirmAction(action)
		}
		if cmd != nil {
			return m, cmd
		}
		// Fall through to keymap only for unhandled keys
	}

	// Close confirmation modal: let declarative modal handle keys
	if m.CloseConfirmOpen && m.CloseConfirmModal != nil && m.CloseConfirmMouseHandler != nil {
		// Let declarative modal handle tab/shift+tab/enter/esc and input
		action, cmd := m.CloseConfirmModal.HandleKey(msg)
		if action != "" {
			return m.handleCloseConfirmAction(action)
		}
		if cmd != nil {
			return m, cmd
		}
		// Consume keys that the modal handles internally (focus cycling, input)
		// to prevent double-handling by keymap
		key := msg.String()
		switch key {
		case "tab", "shift+tab", "enter", "up", "down", "left", "right", "home", "end", "backspace", "delete":
			return m, nil // Key was handled by modal
		}
		// Also consume regular character keys for the input field
		if msg.Type == tea.KeyRunes {
			return m, nil
		}
		// Fall through to keymap only for unhandled keys (like esc)
	}

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
			cmds := []tea.Cmd{inputCmd, m.fetchData()}
			// Also refresh board issues if in board mode
			if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
				cmds = append(cmds, m.fetchBoardIssues(m.BoardMode.Board.ID))
			}
			return m, tea.Batch(cmds...)
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
			if m.ShowTDQHelp {
				m.closeTDQHelpModal()
			} else {
				m = m.openTDQHelpModal()
			}
			m.HelpOpen = false
		} else {
			m.HelpOpen = !m.HelpOpen
			m.closeTDQHelpModal()
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
		} else if m.TaskListMode == TaskListModeBoard && m.ActivePanel == PanelTaskList {
			if m.BoardMode.ViewMode == BoardViewSwimlanes {
				if m.BoardMode.SwimlaneCursor < len(m.BoardMode.SwimlaneRows)-1 {
					m.BoardMode.SwimlaneCursor++
					m.ensureSwimlaneCursorVisible()
				}
			} else {
				if m.BoardMode.Cursor < len(m.BoardMode.Issues)-1 {
					m.BoardMode.Cursor++
					m.ensureBoardCursorVisible()
				}
			}
		} else if m.HandoffsOpen {
			if m.HandoffsCursor < len(m.HandoffsData)-1 {
				m.HandoffsCursor++
			}
		} else if m.StatsOpen {
			// Use declarative modal scroll when available
			if m.StatsModal != nil && !m.StatsLoading && m.StatsError == nil {
				m.StatsModal.Scroll(1)
			} else {
				m.StatsScroll++
			}
		} else if m.ShowTDQHelp && m.TDQHelpModal != nil {
			m.TDQHelpModal.Scroll(1)
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
		} else if m.TaskListMode == TaskListModeBoard && m.ActivePanel == PanelTaskList {
			if m.BoardMode.ViewMode == BoardViewSwimlanes {
				if m.BoardMode.SwimlaneCursor > 0 {
					m.BoardMode.SwimlaneCursor--
					m.ensureSwimlaneCursorVisible()
				}
			} else {
				if m.BoardMode.Cursor > 0 {
					m.BoardMode.Cursor--
					m.ensureBoardCursorVisible()
				}
			}
		} else if m.HandoffsOpen {
			if m.HandoffsCursor > 0 {
				m.HandoffsCursor--
			}
		} else if m.StatsOpen {
			// Use declarative modal scroll when available
			if m.StatsModal != nil && !m.StatsLoading && m.StatsError == nil {
				m.StatsModal.Scroll(-1)
			} else if m.StatsScroll > 0 {
				m.StatsScroll--
			}
		} else if m.ShowTDQHelp && m.TDQHelpModal != nil {
			m.TDQHelpModal.Scroll(-1)
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
		} else if m.TaskListMode == TaskListModeBoard && m.ActivePanel == PanelTaskList {
			if m.BoardMode.ViewMode == BoardViewSwimlanes {
				m.BoardMode.SwimlaneCursor = 0
				m.BoardMode.SwimlaneScroll = 0
			} else {
				m.BoardMode.Cursor = 0
				m.BoardMode.ScrollOffset = 0
			}
		} else if m.HandoffsOpen {
			m.HandoffsCursor = 0
			m.HandoffsScroll = 0
		} else if m.StatsOpen {
			// Use declarative modal scroll when available
			if m.StatsModal != nil && !m.StatsLoading && m.StatsError == nil {
				m.StatsModal.SetScrollOffset(0)
			} else {
				m.StatsScroll = 0
			}
		} else if m.ShowTDQHelp && m.TDQHelpModal != nil {
			m.TDQHelpModal.SetScrollOffset(0)
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
		} else if m.TaskListMode == TaskListModeBoard && m.ActivePanel == PanelTaskList {
			if m.BoardMode.ViewMode == BoardViewSwimlanes {
				if len(m.BoardMode.SwimlaneRows) > 0 {
					m.BoardMode.SwimlaneCursor = len(m.BoardMode.SwimlaneRows) - 1
					m.ensureSwimlaneCursorVisible()
				}
			} else {
				if len(m.BoardMode.Issues) > 0 {
					m.BoardMode.Cursor = len(m.BoardMode.Issues) - 1
					m.ensureBoardCursorVisible()
				}
			}
		} else if m.HandoffsOpen {
			if len(m.HandoffsData) > 0 {
				m.HandoffsCursor = len(m.HandoffsData) - 1
			}
		} else if m.StatsOpen {
			// Use declarative modal scroll when available
			if m.StatsModal != nil && !m.StatsLoading && m.StatsError == nil {
				m.StatsModal.SetScrollOffset(9999) // Will be clamped during render
			} else {
				m.StatsScroll = 9999 // Will be clamped by view
			}
		} else if m.ShowTDQHelp && m.TDQHelpModal != nil {
			m.TDQHelpModal.SetScrollOffset(9999) // Will be clamped during render
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
		} else if m.TaskListMode == TaskListModeBoard && m.ActivePanel == PanelTaskList {
			if m.BoardMode.ViewMode == BoardViewSwimlanes {
				m.BoardMode.SwimlaneCursor += pageSize
				if m.BoardMode.SwimlaneCursor >= len(m.BoardMode.SwimlaneRows) {
					m.BoardMode.SwimlaneCursor = len(m.BoardMode.SwimlaneRows) - 1
				}
				if m.BoardMode.SwimlaneCursor < 0 {
					m.BoardMode.SwimlaneCursor = 0
				}
			} else {
				m.BoardMode.Cursor += pageSize
				if m.BoardMode.Cursor >= len(m.BoardMode.Issues) {
					m.BoardMode.Cursor = len(m.BoardMode.Issues) - 1
				}
				if m.BoardMode.Cursor < 0 {
					m.BoardMode.Cursor = 0
				}
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
			// Use declarative modal scroll when available
			if m.StatsModal != nil && !m.StatsLoading && m.StatsError == nil {
				m.StatsModal.Scroll(pageSize)
			} else {
				m.StatsScroll += pageSize
			}
		} else if m.ShowTDQHelp && m.TDQHelpModal != nil {
			m.TDQHelpModal.Scroll(pageSize)
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
		} else if m.TaskListMode == TaskListModeBoard && m.ActivePanel == PanelTaskList {
			if m.BoardMode.ViewMode == BoardViewSwimlanes {
				m.BoardMode.SwimlaneCursor -= pageSize
				if m.BoardMode.SwimlaneCursor < 0 {
					m.BoardMode.SwimlaneCursor = 0
				}
			} else {
				m.BoardMode.Cursor -= pageSize
				if m.BoardMode.Cursor < 0 {
					m.BoardMode.Cursor = 0
				}
			}
			m.ensureBoardCursorVisible()
		} else if m.HandoffsOpen {
			m.HandoffsCursor -= pageSize
			if m.HandoffsCursor < 0 {
				m.HandoffsCursor = 0
			}
		} else if m.StatsOpen {
			// Use declarative modal scroll when available
			if m.StatsModal != nil && !m.StatsLoading && m.StatsError == nil {
				m.StatsModal.Scroll(-pageSize)
			} else {
				m.StatsScroll -= pageSize
				if m.StatsScroll < 0 {
					m.StatsScroll = 0
				}
			}
		} else if m.ShowTDQHelp && m.TDQHelpModal != nil {
			m.TDQHelpModal.Scroll(-pageSize)
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
			// Use declarative modal scroll when available
			if m.StatsModal != nil && !m.StatsLoading && m.StatsError == nil {
				m.StatsModal.Scroll(pageSize)
			} else {
				m.StatsScroll += pageSize
			}
		} else if m.ShowTDQHelp && m.TDQHelpModal != nil {
			m.TDQHelpModal.Scroll(pageSize)
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
			// Use declarative modal scroll when available
			if m.StatsModal != nil && !m.StatsLoading && m.StatsError == nil {
				m.StatsModal.Scroll(-pageSize)
			} else {
				m.StatsScroll -= pageSize
				if m.StatsScroll < 0 {
					m.StatsScroll = 0
				}
			}
		} else if m.ShowTDQHelp && m.TDQHelpModal != nil {
			m.TDQHelpModal.Scroll(-pageSize)
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
		} else if m.ShowTDQHelp {
			m.closeTDQHelpModal()
		}
		return m, nil

	// Actions
	case keymap.CmdOpenDetails:
		if m.HandoffsOpen {
			return m.openIssueFromHandoffs()
		}
		if m.TaskListMode == TaskListModeBoard && m.ActivePanel == PanelTaskList {
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
		m.updatePanelBounds() // Recalc bounds for search bar
		return m, m.SearchInput.Focus()

	case keymap.CmdToggleClosed:
		m.IncludeClosed = !m.IncludeClosed
		return m, tea.Batch(m.fetchData(), m.saveFilterState())

	case keymap.CmdCycleSortMode:
		m.SortMode = (m.SortMode + 1) % 3
		oldQuery := m.SearchQuery
		m.SearchQuery = updateQuerySort(m.SearchQuery, m.SortMode)
		// Recalc bounds if search bar visibility changed
		if (oldQuery == "") != (m.SearchQuery == "") {
			m.updatePanelBounds()
		}
		// In board mode, also refresh board issues to apply new sort
		if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
			return m, tea.Batch(m.fetchData(), m.fetchBoardIssues(m.BoardMode.Board.ID), m.saveFilterState())
		}
		return m, tea.Batch(m.fetchData(), m.saveFilterState())

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
		cmds := []tea.Cmd{
			m.fetchData(),
			m.saveFilterState(),
			tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} }),
		}
		// In board mode, also refresh board issues to apply type filter
		if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
			cmds = append(cmds, m.fetchBoardIssues(m.BoardMode.Board.ID))
		}
		return m, tea.Batch(cmds...)

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
		m.closeTDQHelpModal()
		m.SearchInput.Blur()
		m.updatePanelBounds() // Recalc bounds after search bar closes
		// Focus TaskList panel with cursor on first result
		m.ActivePanel = PanelTaskList
		m.Cursor[PanelTaskList] = 0
		m.ScrollOffset[PanelTaskList] = 0
		return m, m.saveFilterState()

	case keymap.CmdSearchCancel:
		// If TDQ help is open, close it but stay in search mode
		if m.ShowTDQHelp {
			m.closeTDQHelpModal()
			return m, nil
		}
		// Otherwise exit search mode entirely
		m.SearchMode = false
		m.SearchQuery = ""
		m.SearchInput.SetValue("")
		m.SearchInput.Blur()
		m.updatePanelBounds() // Recalc bounds after search bar closes
		// Refresh appropriate data based on mode
		if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
			return m, tea.Batch(m.fetchBoardIssues(m.BoardMode.Board.ID), m.saveFilterState())
		}
		return m, tea.Batch(m.fetchData(), m.saveFilterState())

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
		// Refresh appropriate data based on mode
		if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
			return m, tea.Batch(m.fetchBoardIssues(m.BoardMode.Board.ID), m.saveFilterState())
		}
		return m, tea.Batch(m.fetchData(), m.saveFilterState())

	// Confirmation commands
	case keymap.CmdConfirm:
		if m.CloseConfirmOpen {
			return m.executeCloseWithReason()
		}
		// Delete confirmation is now handled by declarative modal in handleKey
		// This is a fallback for legacy button focus state
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
		// Delete confirmation is now handled by declarative modal in handleKey
		// This is a fallback
		if m.ConfirmOpen {
			m.closeDeleteConfirmModal()
		}
		return m, nil

	// Button navigation for confirmation dialogs
	// Note: Both delete and close confirmation modals now handle tab cycling
	// internally through their declarative modal HandleKey() methods
	case keymap.CmdNextButton:
		return m, nil

	case keymap.CmdPrevButton:
		return m, nil

	case keymap.CmdSelect:
		// Confirmation dialog enter handling is now done by declarative modals
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

	case keymap.CmdSendToWorktree:
		return m.sendToWorktree()

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

	// Board editor commands
	case keymap.CmdEditBoard:
		return m.openBoardEditor()
	case keymap.CmdNewBoard:
		return m.openBoardEditorCreate()
	case keymap.CmdBoardEditorSave:
		return m.handleBoardEditorAction("save")
	case keymap.CmdBoardEditorCancel:
		return m.handleBoardEditorAction("cancel")
	case keymap.CmdBoardEditorDelete:
		return m.handleBoardEditorAction("delete")

	// Board commands
	case keymap.CmdOpenBoardPicker:
		return m.openBoardPicker()

	case keymap.CmdSelectBoard:
		return m.selectBoard()

	case keymap.CmdCloseBoardPicker:
		m.closeBoardPickerModal()
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

	case keymap.CmdMoveIssueToTop:
		return m.moveIssueToTop()

	case keymap.CmdMoveIssueToBottom:
		return m.moveIssueToBottom()

	case keymap.CmdToggleBoardView:
		return m.toggleBoardView()

	// Getting started commands
	case keymap.CmdOpenGettingStarted:
		return m.openGettingStarted()

	case keymap.CmdInstallInstructions:
		return m.installAgentInstructions()
	}

	return m, nil
}

// openBoardPicker opens the board picker modal
func (m Model) openBoardPicker() (Model, tea.Cmd) {
	return m.openBoardPickerModal()
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
	m.TaskListMode = TaskListModeBoard
	m.ActivePanel = PanelTaskList // Focus the Task List panel
	m.BoardMode.Board = &board
	m.BoardMode.Cursor = 0
	m.BoardMode.ScrollOffset = 0
	m.BoardMode.SwimlaneCursor = 0
	m.BoardMode.SwimlaneScroll = 0
	m.BoardMode.ViewMode = BoardViewModeFromString(board.ViewMode)
	if m.BoardMode.StatusFilter == nil {
		m.BoardMode.StatusFilter = DefaultBoardStatusFilter()
	}
	m.closeBoardPickerModal()

	// Update last viewed (skip if DB not initialized, e.g., in tests)
	if m.DB != nil {
		if err := m.DB.UpdateBoardLastViewed(board.ID); err != nil {
			m.StatusMessage = "Error: " + err.Error()
			m.StatusIsError = true
		}
	}

	return m, m.fetchBoardIssues(board.ID)
}

// openIssueFromBoard opens the issue modal for the currently selected board issue
func (m Model) openIssueFromBoard() (tea.Model, tea.Cmd) {
	if m.TaskListMode != TaskListModeBoard {
		return m, nil
	}

	var issueID string
	if m.BoardMode.ViewMode == BoardViewSwimlanes {
		// Swimlanes view: get issue from SwimlaneRows
		if len(m.BoardMode.SwimlaneRows) == 0 {
			return m, nil
		}
		if m.BoardMode.SwimlaneCursor < 0 || m.BoardMode.SwimlaneCursor >= len(m.BoardMode.SwimlaneRows) {
			return m, nil
		}
		issueID = m.BoardMode.SwimlaneRows[m.BoardMode.SwimlaneCursor].Issue.ID
	} else {
		// Backlog view: get issue from Issues
		if len(m.BoardMode.Issues) == 0 {
			return m, nil
		}
		if m.BoardMode.Cursor < 0 || m.BoardMode.Cursor >= len(m.BoardMode.Issues) {
			return m, nil
		}
		issueID = m.BoardMode.Issues[m.BoardMode.Cursor].Issue.ID
	}
	return m.pushModal(issueID, PanelTaskList) // Use TaskList as source panel for board mode
}

// exitBoardMode returns to the categorized Task List view, but first clears
// any active search/sort/type filters. Only exits if no filters are active.
func (m Model) exitBoardMode() (Model, tea.Cmd) {
	// If there's an active search query or non-default sort/type filter, clear it first
	hasSearchQuery := m.SearchQuery != ""
	hasNonDefaultSort := m.SortMode != SortByPriority
	hasTypeFilter := m.TypeFilterMode != TypeFilterNone

	if hasSearchQuery || hasNonDefaultSort || hasTypeFilter {
		// Clear filters instead of exiting
		m.SearchQuery = ""
		m.SortMode = SortByPriority
		m.TypeFilterMode = TypeFilterNone
		m.updatePanelBounds()
		m.StatusMessage = "Filters cleared"
		// Refresh board issues with cleared filters
		if m.BoardMode.Board != nil {
			return m, tea.Batch(
				m.fetchBoardIssues(m.BoardMode.Board.ID),
				m.saveFilterState(),
				tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} }),
			)
		}
		return m, tea.Batch(m.saveFilterState(), tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} }))
	}

	// No filters active, exit board mode
	m.TaskListMode = TaskListModeCategorized
	m.BoardMode.Board = nil
	m.BoardMode.Issues = nil
	m.BoardMode.Cursor = 0
	m.BoardMode.ScrollOffset = 0
	m.BoardMode.SwimlaneCursor = 0
	m.BoardMode.SwimlaneScroll = 0
	m.BoardMode.SwimlaneData = TaskListData{}
	m.BoardMode.SwimlaneRows = nil
	return m, m.fetchData()
}

// toggleBoardClosed toggles the closed status in the board status filter
func (m Model) toggleBoardClosed() (Model, tea.Cmd) {
	if m.TaskListMode != TaskListModeBoard || m.BoardMode.Board == nil {
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
	if m.TaskListMode != TaskListModeBoard || m.BoardMode.Board == nil {
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

// toggleBoardView toggles between swimlanes and backlog view modes
func (m Model) toggleBoardView() (Model, tea.Cmd) {
	if m.TaskListMode != TaskListModeBoard || m.BoardMode.Board == nil {
		return m, nil
	}

	// Get currently selected issue ID before toggling
	var selectedID string
	if m.BoardMode.ViewMode == BoardViewSwimlanes {
		if m.BoardMode.SwimlaneCursor >= 0 && m.BoardMode.SwimlaneCursor < len(m.BoardMode.SwimlaneRows) {
			selectedID = m.BoardMode.SwimlaneRows[m.BoardMode.SwimlaneCursor].Issue.ID
		}
	} else {
		if m.BoardMode.Cursor >= 0 && m.BoardMode.Cursor < len(m.BoardMode.Issues) {
			selectedID = m.BoardMode.Issues[m.BoardMode.Cursor].Issue.ID
		}
	}

	// Toggle view mode
	if m.BoardMode.ViewMode == BoardViewSwimlanes {
		m.BoardMode.ViewMode = BoardViewBacklog
		m.StatusMessage = "Switched to backlog view"
	} else {
		m.BoardMode.ViewMode = BoardViewSwimlanes
		m.StatusMessage = "Switched to swimlanes view"
	}

	// Try to preserve selection by finding the same issue in the new view
	if selectedID != "" {
		if m.BoardMode.ViewMode == BoardViewSwimlanes {
			// Find issue in swimlane rows
			for i, row := range m.BoardMode.SwimlaneRows {
				if row.Issue.ID == selectedID {
					m.BoardMode.SwimlaneCursor = i
					break
				}
			}
		} else {
			// Find issue in backlog issues
			for i, biv := range m.BoardMode.Issues {
				if biv.Issue.ID == selectedID {
					m.BoardMode.Cursor = i
					break
				}
			}
		}
	}

	// Persist view mode to database
	viewModeStr := m.BoardMode.ViewMode.String()
	if err := m.DB.UpdateBoardViewMode(m.BoardMode.Board.ID, viewModeStr); err != nil {
		// Non-fatal, just show error
		m.StatusMessage = "View switched (save failed: " + err.Error() + ")"
		m.StatusIsError = true
	}

	// Update the board struct too for consistency
	m.BoardMode.Board.ViewMode = viewModeStr

	return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} })
}

// moveIssueInBoard moves the current issue up or down in the board
func (m Model) moveIssueInBoard(direction int) (Model, tea.Cmd) {
	if m.TaskListMode != TaskListModeBoard || m.BoardMode.Board == nil {
		return m, nil
	}

	if m.BoardMode.ViewMode == BoardViewSwimlanes {
		return m.moveIssueInSwimlane(direction)
	}
	return m.moveIssueInBacklog(direction)
}

// moveIssueInBacklog moves the current issue up or down in the backlog view
func (m Model) moveIssueInBacklog(direction int) (Model, tea.Cmd) {
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

	// Position-on-demand: ensure both issues are positioned before swapping
	// First, position target if needed
	if !targetIssue.HasPosition {
		var targetPos int
		if currentIssue.HasPosition {
			// Place target relative to current using sparse gap
			if direction < 0 {
				targetPos = currentIssue.Position - db.PositionGap
			} else {
				targetPos = currentIssue.Position + db.PositionGap
			}
		} else {
			// Neither positioned - assign first sparse key
			targetPos = db.PositionGap
		}
		if err := m.DB.SetIssuePositionLogged(m.BoardMode.Board.ID, targetIssue.Issue.ID, targetPos, m.SessionID); err != nil {
			m.StatusMessage = "Error: " + err.Error()
			m.StatusIsError = true
			return m, nil
		}
		m.BoardMode.Issues[targetIdx].HasPosition = true
		m.BoardMode.Issues[targetIdx].Position = targetPos
		targetIssue = m.BoardMode.Issues[targetIdx] // Refresh local variable
	}

	// Now handle current issue
	if !currentIssue.HasPosition {
		// Current is unpositioned - insert relative to target using sparse gap
		var insertPos int
		if direction < 0 {
			insertPos = targetIssue.Position - db.PositionGap
		} else {
			insertPos = targetIssue.Position + db.PositionGap
		}
		if err := m.DB.SetIssuePositionLogged(m.BoardMode.Board.ID, currentIssue.Issue.ID, insertPos, m.SessionID); err != nil {
			m.StatusMessage = "Error: " + err.Error()
			m.StatusIsError = true
			return m, nil
		}
		// Track the issue we want selected after refresh (positions change sort order)
		m.BoardMode.PendingSelectionID = currentIssue.Issue.ID
	} else {
		// Both now positioned - swap positions
		curPos := currentIssue.Position
		tgtPos := targetIssue.Position
		if err := m.DB.SwapIssuePositions(m.BoardMode.Board.ID, currentIssue.Issue.ID, targetIssue.Issue.ID); err != nil {
			m.StatusMessage = "Error: " + err.Error()
			m.StatusIsError = true
			return m, nil
		}
		// Log both sides of the swap (positions are exchanged)
		m.DB.SetIssuePositionLogged(m.BoardMode.Board.ID, currentIssue.Issue.ID, tgtPos, m.SessionID)
		m.DB.SetIssuePositionLogged(m.BoardMode.Board.ID, targetIssue.Issue.ID, curPos, m.SessionID)
		// Track the issue we want selected after refresh
		m.BoardMode.PendingSelectionID = currentIssue.Issue.ID
	}

	return m, m.fetchBoardIssues(m.BoardMode.Board.ID)
}

// moveIssueInSwimlane moves the current issue up or down within its swimlane (category)
func (m Model) moveIssueInSwimlane(direction int) (Model, tea.Cmd) {
	if len(m.BoardMode.SwimlaneRows) == 0 {
		return m, nil
	}

	cursor := m.BoardMode.SwimlaneCursor
	if cursor < 0 || cursor >= len(m.BoardMode.SwimlaneRows) {
		return m, nil
	}

	currentRow := m.BoardMode.SwimlaneRows[cursor]
	currentCategory := currentRow.Category

	// Find target index within the same category
	targetIdx := cursor + direction
	if targetIdx < 0 || targetIdx >= len(m.BoardMode.SwimlaneRows) {
		return m, nil // Can't move beyond bounds
	}

	targetRow := m.BoardMode.SwimlaneRows[targetIdx]

	// Only allow moves within the same category
	if targetRow.Category != currentCategory {
		return m, nil // Can't cross lane boundaries
	}

	// Find the BoardIssueView for both issues to get position info
	var currentBIV, targetBIV *models.BoardIssueView
	for i := range m.BoardMode.Issues {
		if m.BoardMode.Issues[i].Issue.ID == currentRow.Issue.ID {
			currentBIV = &m.BoardMode.Issues[i]
		}
		if m.BoardMode.Issues[i].Issue.ID == targetRow.Issue.ID {
			targetBIV = &m.BoardMode.Issues[i]
		}
	}

	if currentBIV == nil || targetBIV == nil {
		return m, nil
	}

	// Position-on-demand: ensure both issues are positioned before swapping
	// First, position target if needed
	if !targetBIV.HasPosition {
		var targetPos int
		if currentBIV.HasPosition {
			// Place target relative to current using sparse gap
			if direction < 0 {
				targetPos = currentBIV.Position - db.PositionGap
			} else {
				targetPos = currentBIV.Position + db.PositionGap
			}
		} else {
			// Neither positioned - assign first sparse key
			targetPos = db.PositionGap
		}
		if err := m.DB.SetIssuePositionLogged(m.BoardMode.Board.ID, targetBIV.Issue.ID, targetPos, m.SessionID); err != nil {
			m.StatusMessage = "Error: " + err.Error()
			m.StatusIsError = true
			return m, nil
		}
		targetBIV.HasPosition = true
		targetBIV.Position = targetPos
	}

	// Now handle current issue
	if !currentBIV.HasPosition {
		// Current is unpositioned - insert relative to target using sparse gap
		var insertPos int
		if direction < 0 {
			insertPos = targetBIV.Position - db.PositionGap
		} else {
			insertPos = targetBIV.Position + db.PositionGap
		}
		if err := m.DB.SetIssuePositionLogged(m.BoardMode.Board.ID, currentBIV.Issue.ID, insertPos, m.SessionID); err != nil {
			m.StatusMessage = "Error: " + err.Error()
			m.StatusIsError = true
			return m, nil
		}
		// Track the issue we want selected after refresh (positions change sort order)
		m.BoardMode.PendingSelectionID = currentBIV.Issue.ID
	} else {
		// Both now positioned - swap positions
		curPos := currentBIV.Position
		tgtPos := targetBIV.Position
		if err := m.DB.SwapIssuePositions(m.BoardMode.Board.ID, currentBIV.Issue.ID, targetBIV.Issue.ID); err != nil {
			m.StatusMessage = "Error: " + err.Error()
			m.StatusIsError = true
			return m, nil
		}
		// Log both sides of the swap (positions are exchanged)
		m.DB.SetIssuePositionLogged(m.BoardMode.Board.ID, currentBIV.Issue.ID, tgtPos, m.SessionID)
		m.DB.SetIssuePositionLogged(m.BoardMode.Board.ID, targetBIV.Issue.ID, curPos, m.SessionID)
		// Track the issue we want selected after refresh
		m.BoardMode.PendingSelectionID = currentBIV.Issue.ID
	}

	return m, m.fetchBoardIssues(m.BoardMode.Board.ID)
}

// moveIssueToTop moves the selected issue to position 1 (top of positioned issues).
// Works in both swimlanes and backlog views. For swimlanes, only moves within same category.
func (m Model) moveIssueToTop() (Model, tea.Cmd) {
	if m.TaskListMode != TaskListModeBoard || m.BoardMode.Board == nil {
		return m, nil
	}

	boardID := m.BoardMode.Board.ID

	// Get selected issue based on view mode
	var issueID string
	if m.BoardMode.ViewMode == BoardViewSwimlanes {
		if len(m.BoardMode.SwimlaneRows) == 0 {
			return m, nil
		}
		cursor := m.BoardMode.SwimlaneCursor
		if cursor < 0 || cursor >= len(m.BoardMode.SwimlaneRows) {
			return m, nil
		}
		currentRow := m.BoardMode.SwimlaneRows[cursor]
		issueID = currentRow.Issue.ID

		// Check if already at top of category
		categoryStart := m.findCategoryStart(cursor)
		if cursor == categoryStart {
			return m, nil // Already at top of category
		}
	} else {
		// Backlog view
		if len(m.BoardMode.Issues) == 0 {
			return m, nil
		}
		if m.BoardMode.Cursor < 0 || m.BoardMode.Cursor >= len(m.BoardMode.Issues) {
			return m, nil
		}
		if m.BoardMode.Cursor == 0 {
			return m, nil // Already at top
		}
		issueID = m.BoardMode.Issues[m.BoardMode.Cursor].Issue.ID
	}

	// Compute a sort key below the current minimum
	positions, err := m.DB.GetBoardIssuePositions(boardID)
	if err != nil {
		m.StatusMessage = "Error: " + err.Error()
		m.StatusIsError = true
		return m, nil
	}
	var newPos int
	if len(positions) == 0 {
		newPos = db.PositionGap
	} else {
		// Check if issue is already at min
		if positions[0].IssueID == issueID {
			return m, nil
		}
		newPos = positions[0].Position - db.PositionGap
	}
	if err := m.DB.SetIssuePositionLogged(boardID, issueID, newPos, m.SessionID); err != nil {
		m.StatusMessage = "Error: " + err.Error()
		m.StatusIsError = true
		return m, nil
	}

	m.BoardMode.PendingSelectionID = issueID
	return m, m.fetchBoardIssues(boardID)
}

// moveIssueToBottom moves the selected issue to the end of positioned issues.
// Works in both swimlanes and backlog views. For swimlanes, only moves within same category.
func (m Model) moveIssueToBottom() (Model, tea.Cmd) {
	if m.TaskListMode != TaskListModeBoard || m.BoardMode.Board == nil {
		return m, nil
	}

	boardID := m.BoardMode.Board.ID

	// Get selected issue based on view mode
	var issueID string
	if m.BoardMode.ViewMode == BoardViewSwimlanes {
		if len(m.BoardMode.SwimlaneRows) == 0 {
			return m, nil
		}
		cursor := m.BoardMode.SwimlaneCursor
		if cursor < 0 || cursor >= len(m.BoardMode.SwimlaneRows) {
			return m, nil
		}
		currentRow := m.BoardMode.SwimlaneRows[cursor]
		issueID = currentRow.Issue.ID

		// Check if already at bottom of category
		categoryEnd := m.findCategoryEnd(cursor)
		if cursor == categoryEnd {
			return m, nil // Already at bottom of category
		}
	} else {
		// Backlog view
		if len(m.BoardMode.Issues) == 0 {
			return m, nil
		}
		if m.BoardMode.Cursor < 0 || m.BoardMode.Cursor >= len(m.BoardMode.Issues) {
			return m, nil
		}
		if m.BoardMode.Cursor == len(m.BoardMode.Issues)-1 {
			return m, nil // Already at bottom
		}
		issueID = m.BoardMode.Issues[m.BoardMode.Cursor].Issue.ID
	}

	// Get max position and place after it with a sparse gap
	maxPos, err := m.DB.GetMaxBoardPosition(boardID)
	if err != nil {
		m.StatusMessage = "Error: " + err.Error()
		m.StatusIsError = true
		return m, nil
	}

	var newPos int
	if maxPos == 0 {
		newPos = db.PositionGap
	} else {
		newPos = maxPos + db.PositionGap
	}
	if err := m.DB.SetIssuePositionLogged(boardID, issueID, newPos, m.SessionID); err != nil {
		m.StatusMessage = "Error: " + err.Error()
		m.StatusIsError = true
		return m, nil
	}

	m.BoardMode.PendingSelectionID = issueID
	return m, m.fetchBoardIssues(boardID)
}

// findCategoryStart finds the index of the first issue in the current category
func (m Model) findCategoryStart(cursor int) int {
	if cursor < 0 || cursor >= len(m.BoardMode.SwimlaneRows) {
		return cursor
	}
	currentCategory := m.BoardMode.SwimlaneRows[cursor].Category
	for i := cursor - 1; i >= 0; i-- {
		if m.BoardMode.SwimlaneRows[i].Category != currentCategory {
			return i + 1
		}
	}
	return 0
}

// findCategoryEnd finds the index of the last issue in the current category
func (m Model) findCategoryEnd(cursor int) int {
	if cursor < 0 || cursor >= len(m.BoardMode.SwimlaneRows) {
		return cursor
	}
	currentCategory := m.BoardMode.SwimlaneRows[cursor].Category
	for i := cursor + 1; i < len(m.BoardMode.SwimlaneRows); i++ {
		if m.BoardMode.SwimlaneRows[i].Category != currentCategory {
			return i - 1
		}
	}
	return len(m.BoardMode.SwimlaneRows) - 1
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
	// Capture status filter at call time (closure captures by value)
	statusFilter := m.BoardMode.StatusFilter
	if statusFilter == nil {
		statusFilter = DefaultBoardStatusFilter()
	}

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
			// Filter by status (query.Execute doesn't filter by status)
			var filtered []models.Issue
			for _, issue := range queryResults {
				if statusFilter[issue.Status] {
					filtered = append(filtered, issue)
				}
			}
			issues, err = m.DB.ApplyBoardPositions(boardID, filtered)
			if err != nil {
				return BoardIssuesMsg{BoardID: boardID, Error: err}
			}
		} else {
			// Empty query - use GetBoardIssues with status filter
			statusSlice := StatusFilterMapToSlice(statusFilter)
			issues, err = m.DB.GetBoardIssues(boardID, m.SessionID, statusSlice)
			if err != nil {
				return BoardIssuesMsg{BoardID: boardID, Error: err}
			}
		}

		// Pre-compute rejected IDs to avoid synchronous DB query in Update handler
		rejectedIDs, _ := m.DB.GetRejectedInProgressIssueIDs()
		if rejectedIDs == nil {
			rejectedIDs = make(map[string]bool)
		}

		return BoardIssuesMsg{BoardID: boardID, Issues: issues, RejectedIDs: rejectedIDs}
	}
}

// saveFilterState returns a command that persists the current filter state to config
func (m Model) saveFilterState() tea.Cmd {
	return func() tea.Msg {
		state := &config.FilterState{
			SearchQuery:   m.SearchQuery,
			SortMode:      m.SortMode.String(),
			TypeFilter:    m.TypeFilterMode.String(),
			IncludeClosed: m.IncludeClosed,
		}
		// Fire and forget - errors are not critical
		_ = config.SetFilterState(m.BaseDir, state)
		return nil
	}
}

// openGettingStarted opens the getting started modal
func (m Model) openGettingStarted() (Model, tea.Cmd) {
	m.GettingStartedOpen = true
	m.GettingStartedModal = m.createGettingStartedModal()
	m.GettingStartedModal.Reset()
	m.GettingStartedMouseHandler = mouse.NewHandler()
	return m, nil
}

// handleGettingStartedAction handles actions from the getting started modal
func (m Model) handleGettingStartedAction(action string) (Model, tea.Cmd) {
	switch action {
	case "install":
		return m.installAgentInstructions()
	case "close", "cancel":
		m.GettingStartedOpen = false
		m.GettingStartedModal = nil
		m.GettingStartedMouseHandler = nil
		if m.IsFirstRunInit {
			m.IsFirstRunInit = false
			return m, checkSyncPrompt(m.BaseDir)
		}
		return m, nil
	}
	return m, nil
}

// checkSyncPrompt returns a tea.Cmd that checks if the user is authenticated
// and has remote projects, returning SyncPromptDataMsg if so.
func checkSyncPrompt(baseDir string) tea.Cmd {
	return func() tea.Msg {
		if !features.IsEnabled(baseDir, features.SyncMonitorPrompt.Name) {
			return nil
		}
		if !syncconfig.IsAuthenticated() {
			return nil // no auth, skip silently
		}
		serverURL := syncconfig.GetServerURL()
		apiKey := syncconfig.GetAPIKey()
		deviceID, err := syncconfig.GetDeviceID()
		if err != nil {
			slog.Debug("sync prompt: device id failed", "err", err)
			return nil
		}
		client := syncclient.New(serverURL, apiKey, deviceID)
		projects, err := client.ListProjects()
		if err != nil {
			slog.Debug("sync prompt: list projects failed", "err", err)
			return nil
		}
		return SyncPromptDataMsg{Projects: projects}
	}
}

// handleStatsAction handles actions from the stats modal
func (m Model) handleStatsAction(action string) (Model, tea.Cmd) {
	switch action {
	case "close", "cancel":
		m.closeStatsModal()
		return m, nil
	}
	return m, nil
}

// handleTDQHelpAction handles actions from the TDQ help modal
func (m Model) handleTDQHelpAction(action string) (Model, tea.Cmd) {
	switch action {
	case "close", "cancel":
		m.closeTDQHelpModal()
		return m, nil
	}
	return m, nil
}

// handleHandoffsAction handles actions from the handoffs modal
func (m Model) handleHandoffsAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "open":
		// Open the selected handoff's issue
		return m.openIssueFromHandoffs()
	case "close", "cancel":
		m.closeHandoffsModal()
		return m, nil
	default:
		// Check if action is a list item selection (handoff-N format)
		if len(action) > 8 && action[:8] == "handoff-" {
			// List item clicked - open the issue
			return m.openIssueFromHandoffs()
		}
	}
	return m, nil
}

// handleBoardPickerAction handles actions from the board picker modal
func (m Model) handleBoardPickerAction(action string) (Model, tea.Cmd) {
	switch action {
	case "select":
		// Select the currently highlighted board
		return m.selectBoard()
	case "cancel":
		m.closeBoardPickerModal()
		return m, nil
	default:
		// Check if action is a list item selection (board-N format)
		if len(action) > 6 && action[:6] == "board-" {
			// List item clicked - select the board
			return m.selectBoard()
		}
	}
	return m, nil
}

// installAgentInstructions installs td instructions to the agent file
func (m Model) installAgentInstructions() (Model, tea.Cmd) {
	return m, m.doInstallInstructions()
}

// doInstallInstructions returns a command that installs agent instructions
func (m Model) doInstallInstructions() tea.Cmd {
	return func() tea.Msg {
		targetPath := m.AgentFilePath
		if targetPath == "" {
			// No existing file - use preferred file (AGENTS.md)
			targetPath = agent.PreferredAgentFile(m.BaseDir)
		}

		err := agent.InstallInstructions(targetPath)
		if err != nil {
			return InstallInstructionsResultMsg{
				Success: false,
				Message: "Failed: " + err.Error(),
			}
		}
		return InstallInstructionsResultMsg{
			Success: true,
			Message: "Added td instructions to " + filepath.Base(targetPath),
		}
	}
}

// checkFirstRun returns a command that checks if this is a first-time run
func (m Model) checkFirstRun() tea.Cmd {
	return func() tea.Msg {
		agentPath := agent.DetectAgentFile(m.BaseDir)
		hasTD := false
		if agentPath != "" {
			hasTD = agent.HasTDInstructions(agentPath)
		}

		return FirstRunCheckMsg{
			IsFirstRun:      !hasTD, // Show modal if no instructions found
			AgentFilePath:   agentPath,
			HasInstructions: hasTD,
		}
	}
}
