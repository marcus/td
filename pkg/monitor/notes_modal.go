package monitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/pkg/monitor/modal"
)

// NotesState holds the state for the notes modal system.
type NotesState struct {
	Notes        []models.Note
	ListCursor   int
	ShowArchived bool

	DetailNote   *models.Note
	DetailRender string // Pre-rendered markdown content
}

// NoteMarkdownRenderedMsg carries pre-rendered markdown for a note.
type NoteMarkdownRenderedMsg struct {
	NoteID        string
	Render        string
	ThemeRevision uint64
}

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
	md := m.newModal(title, ModalTypeNotes,
		modal.WithWidth(modalWidth),
		modal.WithVariant(modal.VariantInfo),
		modal.WithHints(false),
	)

	items := make([]modal.ListItem, 0, len(ns.Notes))
	contentWidth := modalWidth - 6 // border + padding + cursor
	for i, note := range ns.Notes {
		note := note // retain this list item's value independently of the range
		items = append(items, modal.ListItem{
			ID: fmt.Sprintf("note-%d", i),
			ThemedLabel: func(theme modal.Theme) string {
				return formatNoteListItemWithStyles(note, contentWidth, newMonitorStyles(monitorTheme(theme)))
			},
		})
	}

	if len(items) == 0 {
		md.AddSection(modal.Text("No notes yet. Press c to create one."))
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

	md := m.newModal(note.Title, ModalTypeNotes,
		modal.WithWidth(modalWidth),
		modal.WithVariant(modal.VariantDefault),
		modal.WithHints(false),
	)

	md.AddSection(modal.ThemedCustom(
		func(contentWidth int, focusID, hoverID string, theme modal.Theme) modal.RenderedSection {
			return modal.RenderedSection{Content: formatNoteMetaWithStyles(note, newMonitorStyles(monitorTheme(theme)))}
		},
		nil,
	))
	md.AddSection(modal.Spacer())

	// Read the retained-source render cache on every frame. Capturing the
	// current string here would leave an already-open notes modal showing ANSI
	// from the previous theme after SetTheme regenerates the cache.
	md.AddSection(modal.ThemedCustom(
		func(contentWidth int, focusID, hoverID string, theme modal.Theme) modal.RenderedSection {
			content := ns.DetailRender
			if content == "" {
				content = note.Content
			}
			if content == "" {
				return modal.RenderedSection{Content: newMonitorStyles(monitorTheme(theme)).subtle.Render("(empty)")}
			}
			return modal.RenderedSection{Content: content}
		},
		nil,
	))
	md.AddSection(modal.Spacer())

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

func formatNoteListItemWithStyles(note models.Note, width int, styles monitorStyles) string {
	var parts []string
	if note.Pinned {
		parts = append(parts, styles.title.Render("*"))
	}
	titleText := note.Title
	if note.Archived {
		titleText = styles.subtle.Render(titleText + " (archived)")
	} else {
		titleText = styles.title.Render(titleText)
	}
	parts = append(parts, titleText, styles.subtle.Render(formatNoteAge(note.UpdatedAt)))
	return strings.Join(parts, " ")
}

func formatNoteMetaWithStyles(note *models.Note, styles monitorStyles) string {
	var parts []string
	if note.Pinned {
		parts = append(parts, styles.title.Render("Pinned"))
	}
	if note.Archived {
		parts = append(parts, styles.subtle.Render("Archived"))
	}
	age := formatNoteAge(note.UpdatedAt)
	created := formatNoteAge(note.CreatedAt)
	parts = append(parts, styles.subtle.Render(fmt.Sprintf("Updated %s", age)))
	if created != age {
		parts = append(parts, styles.subtle.Render(fmt.Sprintf("Created %s", created)))
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
		return formatLocalTime(t, "Jan 2")
	}
}
