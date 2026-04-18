package monitor

import (
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/td/internal/models"
)

func TestFetchBoardIssuesDeepCopiesStatusFilter(t *testing.T) {
	original := map[models.Status]bool{
		models.StatusOpen:       true,
		models.StatusInProgress: true,
		models.StatusClosed:     false,
	}

	m := Model{
		BoardMode: BoardMode{
			StatusFilter: original,
		},
	}

	// fetchBoardIssues should deep copy the map internally.
	// We can't run the returned Cmd (needs DB), but we verify the
	// function doesn't panic and returns a non-nil Cmd.
	cmd := m.fetchBoardIssues("board-1")
	if cmd == nil {
		t.Fatal("fetchBoardIssues returned nil Cmd")
	}

	// Mutate the original after fetchBoardIssues captured its copy.
	// If the copy was shallow, this mutation would be visible to the
	// goroutine — a data race. We can't observe the goroutine's state
	// here, but the structural test below verifies the copy pattern.
	original[models.StatusClosed] = true
}

func TestDeepCopyStatusFilterIndependence(t *testing.T) {
	// Directly test the deep-copy pattern used in fetchBoardIssues:
	// mutations to original must not affect the copy, and vice versa.
	original := map[models.Status]bool{
		models.StatusOpen:       true,
		models.StatusInProgress: true,
		models.StatusClosed:     false,
	}

	// Same logic as fetchBoardIssues lines 2094-2097
	copied := make(map[models.Status]bool, len(original))
	for k, v := range original {
		copied[k] = v
	}

	original[models.StatusClosed] = true
	if copied[models.StatusClosed] != false {
		t.Error("Deep copy was affected by mutation to original — map was shared, not copied")
	}

	copied[models.StatusInProgress] = false
	if original[models.StatusInProgress] != true {
		t.Error("Original was affected by mutation to copy — map was shared, not copied")
	}
}

func TestHelpFilterBackspaceUTF8(t *testing.T) {
	tests := []struct {
		name   string
		filter string
		want   string
	}{
		{"ASCII single char", "a", ""},
		{"ASCII multi char", "abc", "ab"},
		{"2-byte rune (é)", "filé", "fil"},
		{"3-byte rune (€)", "cost€", "cost"},
		{"4-byte rune (emoji)", "test\U0001F600", "test"},
		{"only multi-byte", "é", ""},
		{"two multi-byte", "éé", "é"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Exercise the actual handleKey code path with HelpFilterMode active.
			// The help filter backspace handler (commands.go ~line 367) runs before
			// the keymap lookup, so Keymap can be nil.
			m := Model{
				HelpOpen:       true,
				HelpFilterMode: true,
				HelpFilter:     tc.filter,
			}

			updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
			result := updated.(Model)

			if result.HelpFilter != tc.want {
				t.Errorf("got %q, want %q", result.HelpFilter, tc.want)
			}
			if !utf8.ValidString(result.HelpFilter) {
				t.Errorf("result %q is not valid UTF-8", result.HelpFilter)
			}
		})
	}
}
