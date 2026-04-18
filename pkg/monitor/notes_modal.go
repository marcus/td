package monitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/pkg/monitor/modal"
	"github.com/marcus/td/pkg/monitor/mouse"
)

// --- Notes Modal State ---

// NotesState holds the state for the notes modal system.
type NotesState struct {
	// List state
	Notes       []models.Note
	ListCursor  int
	ShowArchived bool

	// Detail state
	DetailNote    *models.Note
	DetailRender  string // Pre-rendered markdown content

	// Edit state
	Editing       bool
	Creating      bool
	EditTitle     *textinput.Model
	EditContent   *textarea.Model
	EditNoteID    string // ID of note being edited (empty for create)

	// Delete confirmation
	DeleteConfirm bool
	DeleteNoteID  string
}

// NotesDataMsg carries fetched notes data for the notes modal.
type NotesDataMsg struct {
	Notes []models.Note
	Error error
}

// NoteDetailMsg carries a single note for the detail view.
type NoteDetailMsg struct {
	Note  *models.Note
	Error error
}

// NoteMarkdownRenderedMsg carries pre-rendered markdown for a note.
type NoteMarkdownRenderedMsg struct {
	NoteID string
	Render string
}

// NoteSavedMsg carries the result of creating/updating a note.
type NoteSavedMsg struct {
	Note  *models.Note
	IsNew bool
	Error error
}

// NoteDeletedMsg carries the result of deleting a note.
type NoteDeletedMsg struct {
	NoteID string
	Error  error
}

// NotePinToggledMsg carries the result of toggling pin status.
type NotePinToggledMsg struct {
	NoteID string
	Pinned bool
	Error  error
}

// NoteArchivedMsg carries the result of archiving/unarchiving.
type NoteArchivedMsg struct {
	NoteID   string
	Archived bool
	Error    error
}

// --- Open/Close ---

// openNotesModal opens the notes list modal and fetches notes data.
func (m Model) openNotesModal() (tea.Model, tea.Cmd) {
	m.NotesOpen = true
	m.NotesState = &NotesState{}
	m.NotesMouseHandler = mouse.NewHandler()

	return m, m.fetchNotes()
}

// closeNotesModal closes the notes modal and clears state.
func (m *Model) closeNotesModal() {
	m.NotesOpen = false
	m.NotesState = nil
	m.NotesModal = nil
	m.NotesMouseHandler = nil
}

// --- Data Fetching ---

func (m Model) fetchNotes() tea.Cmd {
	database := m.DB
	return func() tea.Msg {
		archived := false
		notes, err := database.ListNotes(db.ListNotesOptions{
			Archived: &archived,
			Limit:    100,
		})
		return NotesDataMsg{Notes: notes, Error: err}
	}
}

func (m Model) fetchNotesWithArchived() tea.Cmd {
	database := m.DB
	return func() tea.Msg {
		notes, err := database.ListNotes(db.ListNotesOptions{
			Limit: 100,
		})
		return NotesDataMsg{Notes: notes, Error: err}
	}
}

func (m Model) renderNoteMarkdownAsync(noteID, content string) tea.Cmd {
	width := m.modalContentWidth()
	theme := m.MarkdownTheme
	return func() tea.Msg {
		return NoteMarkdownRenderedMsg{
			NoteID: noteID,
			Render: preRenderMarkdown(content, width, theme),
		}
	}
}

// --- Modal Builders ---

// createNotesListModal builds the declarative modal for the notes list.
func (m *Model) createNotesListModal() *modal.Modal {
	ns := m.NotesState
	if ns == nil {
		return nil
	}

	modalWidth := m.Width * 80 / 100
	if modalWidth > 100 {
		modalWidth = 100
	}
	if modalWidth < 50 {
		modalWidth = 50
	}

	archivedLabel := "Show Archived"
	if ns.ShowArchived {
		archivedLabel = "Hide Archived"
	}

	title := fmt.Sprintf("Notes (%d)", len(ns.Notes))
	md := modal.New(title,
		modal.WithWidth(modalWidth),
		modal.WithVariant(modal.VariantInfo),
		modal.WithHints(false),
	)

	// Build list items from notes
	items := make([]modal.ListItem, 0, len(ns.Notes))
	contentWidth := modalWidth - 6 // border + padding + cursor
	for i, note := range ns.Notes {
		label := formatNoteListItem(note, contentWidth)
		items = append(items, modal.ListItem{
			ID:    fmt.Sprintf("note-%d", i),
			Label: label,
		})
	}

	if len(items) == 0 {
		md.AddSection(modal.Text(subtleStyle.Render("No notes yet. Press c to create one.")))
	} else {
		maxVisible := (m.Height * 60 / 100) - 6 // Leave room for buttons
		if maxVisible < 5 {
			maxVisible = 5
		}
		md.AddSection(modal.List("notes-list", items, &ns.ListCursor, modal.WithMaxVisible(maxVisible)))
	}

	md.AddSection(modal.Spacer())
	md.AddSection(modal.Buttons(
		modal.Btn(" New ", "create"),
		modal.Btn(" "+archivedLabel+" ", "toggle-archived"),
		modal.Btn(" Close ", "close"),
	))

	return md
}

// createNoteDetailModal builds the declarative modal for viewing a note.
func (m *Model) createNoteDetailModal() *modal.Modal {
	ns := m.NotesState
	if ns == nil || ns.DetailNote == nil {
		return nil
	}

	note := ns.DetailNote
	modalWidth := m.Width * 80 / 100
	if modalWidth > 100 {
		modalWidth = 100
	}
	if modalWidth < 50 {
		modalWidth = 50
	}

	md := modal.New(note.Title,
		modal.WithWidth(modalWidth),
		modal.WithVariant(modal.VariantDefault),
		modal.WithHints(false),
	)

	// Note metadata
	meta := formatNoteMeta(note)
	md.AddSection(modal.Text(meta))
	md.AddSection(modal.Spacer())

	// Rendered markdown content
	content := ns.DetailRender
	if content == "" {
		content = note.Content
	}
	if content == "" {
		content = subtleStyle.Render("(empty)")
	}
	md.AddSection(modal.Custom(
		func(contentWidth int, focusID, hoverID string) modal.RenderedSection {
			return modal.RenderedSection{Content: content}
		},
		nil,
	))
	md.AddSection(modal.Spacer())

	// Action buttons
	pinLabel := " Pin "
	if note.Pinned {
		pinLabel = " Unpin "
	}
	archiveLabel := " Archive "
	if note.Archived {
		archiveLabel = " Unarchive "
	}
	md.AddSection(modal.Buttons(
		modal.Btn(" Edit ", "edit"),
		modal.Btn(pinLabel, "toggle-pin"),
		modal.Btn(archiveLabel, "toggle-archive"),
		modal.Btn(" Delete ", "delete", modal.BtnDanger()),
		modal.Btn(" Back ", "back"),
	))

	return md
}

// createNoteEditModal builds the declarative modal for creating/editing a note.
func (m *Model) createNoteEditModal() *modal.Modal {
	ns := m.NotesState
	if ns == nil {
		return nil
	}

	modalWidth := m.Width * 80 / 100
	if modalWidth > 100 {
		modalWidth = 100
	}
	if modalWidth < 50 {
		modalWidth = 50
	}

	title := "New Note"
	if ns.Editing {
		title = "Edit Note"
	}

	md := modal.New(title,
		modal.WithWidth(modalWidth),
		modal.WithVariant(modal.VariantDefault),
		modal.WithHints(false),
	)

	md.AddSection(modal.InputWithLabel("edit-title", "Title", ns.EditTitle,
		modal.WithSubmitOnEnter(false),
	))
	md.AddSection(modal.Spacer())

	taHeight := m.Height*50/100 - 10
	if taHeight < 5 {
		taHeight = 5
	}
	md.AddSection(modal.TextareaWithLabel("edit-content", "Content (markdown)", ns.EditContent, taHeight))
	md.AddSection(modal.Spacer())
	md.AddSection(modal.Buttons(
		modal.Btn(" Save ", "save", modal.BtnPrimary()),
		modal.Btn(" Cancel ", "cancel-edit"),
	))

	return md
}

// createNoteDeleteConfirmModal builds a delete confirmation modal.
func (m *Model) createNoteDeleteConfirmModal() *modal.Modal {
	md := modal.New("Delete Note?",
		modal.WithWidth(50),
		modal.WithVariant(modal.VariantDanger),
		modal.WithHints(false),
	)

	md.AddSection(modal.Text("Are you sure you want to delete this note? This cannot be undone."))
	md.AddSection(modal.Spacer())
	md.AddSection(modal.Buttons(
		modal.Btn(" Delete ", "confirm-delete", modal.BtnDanger()),
		modal.Btn(" Cancel ", "cancel-delete"),
	))

	return md
}

// --- Action Handlers ---

// handleNotesAction handles actions from the notes list modal.
func (m Model) handleNotesAction(action string) (tea.Model, tea.Cmd) {
	ns := m.NotesState
	if ns == nil {
		return m, nil
	}

	// Handle delete confirmation sub-modal
	if ns.DeleteConfirm {
		switch action {
		case "confirm-delete":
			noteID := ns.DeleteNoteID
			ns.DeleteConfirm = false
			ns.DeleteNoteID = ""
			database := m.DB
			return m, func() tea.Msg {
				err := database.DeleteNote(noteID)
				return NoteDeletedMsg{NoteID: noteID, Error: err}
			}
		case "cancel-delete", "cancel":
			ns.DeleteConfirm = false
			ns.DeleteNoteID = ""
			// Rebuild appropriate modal
			if ns.DetailNote != nil {
				m.NotesModal = m.createNoteDetailModal()
			} else {
				m.NotesModal = m.createNotesListModal()
			}
			if m.NotesModal != nil {
				m.NotesModal.Reset()
			}
			return m, nil
		}
		return m, nil
	}

	// Handle edit modal actions
	if ns.Editing || ns.Creating {
		switch action {
		case "save":
			return m.saveNote()
		case "cancel-edit", "cancel":
			return m.cancelNoteEdit()
		}
		return m, nil
	}

	// Handle detail modal actions
	if ns.DetailNote != nil {
		switch action {
		case "edit":
			return m.openNoteEditor(ns.DetailNote)
		case "toggle-pin":
			return m.toggleNotePin(ns.DetailNote)
		case "toggle-archive":
			return m.toggleNoteArchive(ns.DetailNote)
		case "delete":
			ns.DeleteConfirm = true
			ns.DeleteNoteID = ns.DetailNote.ID
			m.NotesModal = m.createNoteDeleteConfirmModal()
			if m.NotesModal != nil {
				m.NotesModal.Reset()
			}
			return m, nil
		case "back", "cancel":
			ns.DetailNote = nil
			ns.DetailRender = ""
			m.NotesModal = m.createNotesListModal()
			if m.NotesModal != nil {
				m.NotesModal.Reset()
			}
			return m, nil
		}
		return m, nil
	}

	// Handle list modal actions
	switch action {
	case "create":
		return m.openNoteCreator()
	case "toggle-archived":
		ns.ShowArchived = !ns.ShowArchived
		if ns.ShowArchived {
			return m, m.fetchNotesWithArchived()
		}
		return m, m.fetchNotes()
	case "close", "cancel":
		m.closeNotesModal()
		return m, nil
	default:
		// List item selection - open note detail
		if strings.HasPrefix(action, "note-") {
			idx := ns.ListCursor
			if idx >= 0 && idx < len(ns.Notes) {
				note := ns.Notes[idx]
				ns.DetailNote = &note
				ns.DetailRender = ""
				m.NotesModal = m.createNoteDetailModal()
				if m.NotesModal != nil {
					m.NotesModal.Reset()
				}
				return m, m.renderNoteMarkdownAsync(note.ID, note.Content)
			}
		}
	}

	return m, nil
}

// --- Note Operations ---

func (m Model) openNoteCreator() (tea.Model, tea.Cmd) {
	ns := m.NotesState
	if ns == nil {
		return m, nil
	}

	ti := textinput.New()
	ti.Placeholder = "Note title"
	ti.CharLimit = 200
	ti.Width = 40
	ti.Focus()

	ta := textarea.New()
	ta.Placeholder = "Write your note here (markdown supported)..."
	ta.CharLimit = 10000

	ns.Creating = true
	ns.Editing = false
	ns.EditTitle = &ti
	ns.EditContent = &ta
	ns.EditNoteID = ""

	m.NotesModal = m.createNoteEditModal()
	if m.NotesModal != nil {
		m.NotesModal.Reset()
		m.NotesModal.SetFocus("edit-title")
	}
	return m, ti.Focus()
}

func (m Model) openNoteEditor(note *models.Note) (tea.Model, tea.Cmd) {
	ns := m.NotesState
	if ns == nil || note == nil {
		return m, nil
	}

	ti := textinput.New()
	ti.SetValue(note.Title)
	ti.CharLimit = 200
	ti.Width = 40
	ti.Focus()

	ta := textarea.New()
	ta.SetValue(note.Content)
	ta.CharLimit = 10000

	ns.Editing = true
	ns.Creating = false
	ns.EditTitle = &ti
	ns.EditContent = &ta
	ns.EditNoteID = note.ID

	m.NotesModal = m.createNoteEditModal()
	if m.NotesModal != nil {
		m.NotesModal.Reset()
		m.NotesModal.SetFocus("edit-title")
	}
	return m, ti.Focus()
}

func (m Model) saveNote() (tea.Model, tea.Cmd) {
	ns := m.NotesState
	if ns == nil || ns.EditTitle == nil || ns.EditContent == nil {
		return m, nil
	}

	title := strings.TrimSpace(ns.EditTitle.Value())
	content := ns.EditContent.Value()

	if title == "" {
		m.StatusMessage = "Note title cannot be empty"
		m.StatusIsError = true
		return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} })
	}

	database := m.DB
	isNew := ns.Creating

	if isNew {
		return m, func() tea.Msg {
			note, err := database.CreateNote(title, content)
			return NoteSavedMsg{Note: note, IsNew: true, Error: err}
		}
	}

	noteID := ns.EditNoteID
	return m, func() tea.Msg {
		note, err := database.UpdateNote(noteID, title, content)
		return NoteSavedMsg{Note: note, IsNew: false, Error: err}
	}
}

func (m Model) cancelNoteEdit() (tea.Model, tea.Cmd) {
	ns := m.NotesState
	if ns == nil {
		return m, nil
	}

	ns.Editing = false
	ns.Creating = false
	ns.EditTitle = nil
	ns.EditContent = nil
	ns.EditNoteID = ""

	// Go back to detail or list
	if ns.DetailNote != nil {
		m.NotesModal = m.createNoteDetailModal()
	} else {
		m.NotesModal = m.createNotesListModal()
	}
	if m.NotesModal != nil {
		m.NotesModal.Reset()
	}
	return m, nil
}

func (m Model) toggleNotePin(note *models.Note) (tea.Model, tea.Cmd) {
	database := m.DB
	noteID := note.ID
	wasPinned := note.Pinned
	return m, func() tea.Msg {
		var err error
		if wasPinned {
			err = database.UnpinNote(noteID)
		} else {
			err = database.PinNote(noteID)
		}
		return NotePinToggledMsg{NoteID: noteID, Pinned: !wasPinned, Error: err}
	}
}

func (m Model) toggleNoteArchive(note *models.Note) (tea.Model, tea.Cmd) {
	database := m.DB
	noteID := note.ID
	wasArchived := note.Archived
	return m, func() tea.Msg {
		var err error
		if wasArchived {
			err = database.UnarchiveNote(noteID)
		} else {
			err = database.ArchiveNote(noteID)
		}
		return NoteArchivedMsg{NoteID: noteID, Archived: !wasArchived, Error: err}
	}
}

// --- Formatting Helpers ---

func formatNoteListItem(note models.Note, width int) string {
	var parts []string

	// Pin indicator
	if note.Pinned {
		parts = append(parts, titleStyle.Render("*"))
	}

	// Title
	titleText := note.Title
	if note.Archived {
		titleText = subtleStyle.Render(titleText + " (archived)")
	} else {
		titleText = titleStyle.Render(titleText)
	}
	parts = append(parts, titleText)

	// Relative time
	age := formatNoteAge(note.UpdatedAt)
	parts = append(parts, subtleStyle.Render(age))

	return strings.Join(parts, " ")
}

func formatNoteMeta(note *models.Note) string {
	var parts []string

	if note.Pinned {
		parts = append(parts, titleStyle.Render("Pinned"))
	}
	if note.Archived {
		parts = append(parts, subtleStyle.Render("Archived"))
	}

	age := formatNoteAge(note.UpdatedAt)
	created := formatNoteAge(note.CreatedAt)

	parts = append(parts, subtleStyle.Render(fmt.Sprintf("Updated %s", age)))
	if created != age {
		parts = append(parts, subtleStyle.Render(fmt.Sprintf("Created %s", created)))
	}

	return strings.Join(parts, "  ")
}

func formatNoteAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	default:
		return t.Format("Jan 2")
	}
}

// --- View Rendering ---

// renderNotesModal renders the notes modal (delegates to declarative modal or loading).
func (m Model) renderNotesModal() string {
	if m.NotesModal != nil && m.NotesMouseHandler != nil {
		return m.NotesModal.Render(m.Width, m.Height, m.NotesMouseHandler)
	}

	// Loading state
	modalWidth := m.Width * 80 / 100
	if modalWidth > 100 {
		modalWidth = 100
	}
	if modalWidth < 50 {
		modalWidth = 50
	}

	content := subtleStyle.Render("Loading notes...")
	return m.wrapSimpleModal("Notes", content, modalWidth)
}

// wrapSimpleModal wraps content in a basic modal frame (used for loading/error states).
func (m Model) wrapSimpleModal(title, content string, width int) string {
	modalHeight := m.Height * 60 / 100
	if modalHeight < 10 {
		modalHeight = 10
	}
	if modalHeight > 30 {
		modalHeight = 30
	}

	style := modalBorderStyle.Width(width - 2).Height(modalHeight - 2)
	titleLine := titleStyle.Render(title)
	body := titleLine + "\n\n" + content

	return style.Render(body)
}
