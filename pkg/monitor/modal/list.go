package modal

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ListItem represents an item in a list section.
type ListItem struct {
	ID    string // Unique identifier for this item
	Label string // Display text
	Data  any    // Optional associated data
}

// ListOption is a functional option for List sections.
type ListOption func(*listSection)

// listSection renders a scrollable list of items.
type listSection struct {
	id           string
	items        []ListItem
	selectedIdx  *int // Pointer to allow external control
	maxVisible   int  // Maximum number of visible items
	scrollOffset int  // Current scroll position
}

// List creates a list section with selectable items.
// selectedIdx is a pointer to the currently selected index (can be nil for no selection).
func List(id string, items []ListItem, selectedIdx *int, opts ...ListOption) Section {
	s := &listSection{
		id:          id,
		items:       items,
		selectedIdx: selectedIdx,
		maxVisible:  5, // Default
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithMaxVisible sets the maximum number of visible items.
func WithMaxVisible(n int) ListOption {
	return func(s *listSection) {
		if n > 0 {
			s.maxVisible = n
		}
	}
}

func (s *listSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	if len(s.items) == 0 {
		return RenderedSection{Content: MutedText.Render("(no items)")}
	}

	// Determine visible range
	visibleCount := min(s.maxVisible, len(s.items))
	selectedIdx := 0
	if s.selectedIdx != nil {
		selectedIdx = *s.selectedIdx
	}

	// Adjust scroll to keep selection visible
	if selectedIdx < s.scrollOffset {
		s.scrollOffset = selectedIdx
	} else if selectedIdx >= s.scrollOffset+visibleCount {
		s.scrollOffset = selectedIdx - visibleCount + 1
	}

	// Clamp scroll offset
	maxScroll := max(0, len(s.items)-visibleCount)
	s.scrollOffset = clamp(s.scrollOffset, 0, maxScroll)

	// Check if the list itself is focused (for styling the selected item)
	listIsFocused := focusID == s.id

	var sb strings.Builder
	totalHeight := 0

	for i := 0; i < visibleCount; i++ {
		itemIdx := s.scrollOffset + i
		if itemIdx >= len(s.items) {
			break
		}

		item := s.items[itemIdx]
		isSelected := s.selectedIdx != nil && *s.selectedIdx == itemIdx
		isHovered := item.ID == hoverID

		// Determine style - show focused style only when list is focused AND item is selected
		var style = ListItemNormal
		if isSelected && listIsFocused {
			style = ListItemFocused
		} else if isSelected {
			style = ListItemSelected // Selected but list not focused
		} else if isHovered {
			style = ListItemSelected
		}

		// Render cursor
		cursor := "  "
		if isSelected {
			cursor = ListCursor.Render("> ")
		}

		// Render item
		line := cursor + style.Render(item.Label)
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(line)
		totalHeight++
	}

	// Show scroll indicators if needed
	content := sb.String()
	hasTopIndicator := s.scrollOffset > 0
	if hasTopIndicator {
		content = MutedText.Render("↑ more above") + "\n" + content
		totalHeight++
	}
	if s.scrollOffset+visibleCount < len(s.items) {
		content = content + "\n" + MutedText.Render("↓ more below")
		totalHeight++
	}

	// Register the LIST ITSELF as a single focusable (not each item)
	// This makes Tab move to the next section instead of cycling through items
	// Individual items are still clickable via mouse hit regions
	focusables := []FocusableInfo{{
		ID:      s.id, // Use the list's ID, not individual item IDs
		OffsetX: 0,
		OffsetY: 0,
		Width:   contentWidth,
		Height:  totalHeight,
	}}

	return RenderedSection{
		Content:    content,
		Focusables: focusables,
	}
}

func (s *listSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	// Check if the list itself is focused (not individual items)
	if focusID != s.id {
		return "", nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return "", nil
	}

	if s.selectedIdx == nil {
		return "", nil
	}

	switch keyMsg.String() {
	case "up", "k":
		if *s.selectedIdx > 0 {
			*s.selectedIdx--
		}
		return "", nil

	case "down", "j":
		if *s.selectedIdx < len(s.items)-1 {
			*s.selectedIdx++
		}
		return "", nil

	case "enter":
		// Return the selected item's ID as the action
		if *s.selectedIdx >= 0 && *s.selectedIdx < len(s.items) {
			return s.items[*s.selectedIdx].ID, nil
		}
		return "", nil

	case "home":
		*s.selectedIdx = 0
		return "", nil

	case "end":
		*s.selectedIdx = len(s.items) - 1
		return "", nil
	}

	return "", nil
}
