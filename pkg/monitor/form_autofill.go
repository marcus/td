package monitor

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/sahilm/fuzzy"
)

// AutofillState holds state for an autocomplete dropdown attached to a form field.
type AutofillState struct {
	Active   bool           // Whether the dropdown is showing
	FieldKey string         // Which form field this is for ("parent" or "dependencies")
	Filtered []AutofillItem // Filtered by current query
	Idx      int            // Selected index in dropdown
	Query    string         // Current search text (tracked separately from huh field value)
}

// AutofillItem represents a single autocomplete suggestion.
type AutofillItem struct {
	ID    string
	Title string
	Type  models.Type
}

// AutofillResultMsg carries loaded issues for the autocomplete dropdown.
type AutofillResultMsg struct {
	Items []AutofillItem
}

// loadAutofillData fetches all non-closed issues from the database for autocomplete.
func loadAutofillData(database *db.DB) tea.Cmd {
	return func() tea.Msg {
		issues, err := database.ListIssues(db.ListIssuesOptions{
			Status: []models.Status{
				models.StatusOpen,
				models.StatusInProgress,
				models.StatusBlocked,
				models.StatusInReview,
			},
			Limit: 500,
		})
		if err != nil {
			return AutofillResultMsg{Items: nil}
		}

		items := make([]AutofillItem, len(issues))
		for i, issue := range issues {
			items[i] = AutofillItem{
				ID:    issue.ID,
				Title: issue.Title,
				Type:  issue.Type,
			}
		}
		return AutofillResultMsg{Items: items}
	}
}

// autofillSearchSource adapts []AutofillItem for the fuzzy library.
// Each item produces a searchable string of "ID Title" so fuzzy matching
// works across both fields simultaneously.
type autofillSearchSource []AutofillItem

func (s autofillSearchSource) String(i int) string {
	return s[i].ID + " " + s[i].Title
}

func (s autofillSearchSource) Len() int {
	return len(s)
}

// filterAutofillItems uses fzf-style fuzzy matching on ID and Title,
// returning results ranked by match quality (best first).
func filterAutofillItems(query string, items []AutofillItem) []AutofillItem {
	if query == "" {
		return items
	}

	matches := fuzzy.FindFrom(query, autofillSearchSource(items))

	// Sort by score descending (best match first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	result := make([]AutofillItem, len(matches))
	for i, m := range matches {
		result[i] = items[m.Index]
	}
	return result
}

// currentDepToken extracts the text after the last comma for multi-select dependency input.
// For "td-xxx, td-yyy, partial" it returns "partial".
func currentDepToken(deps string) string {
	parts := strings.Split(deps, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

// excludeIDs removes items whose IDs appear in the excluded set.
func excludeIDs(items []AutofillItem, excluded []string) []AutofillItem {
	if len(excluded) == 0 {
		return items
	}

	set := make(map[string]bool, len(excluded))
	for _, id := range excluded {
		set[strings.TrimSpace(id)] = true
	}

	var result []AutofillItem
	for _, item := range items {
		if !set[item.ID] {
			result = append(result, item)
		}
	}
	return result
}

// isExactAutofillID returns true if query exactly matches an item's ID.
// Used to suppress re-activation of the dropdown after selecting an item.
func isExactAutofillID(query string, items []AutofillItem) bool {
	if query == "" {
		return false
	}
	for _, item := range items {
		if item.ID == query {
			return true
		}
	}
	return false
}

// syncAutofillState detects which form field is focused and activates/deactivates
// the autofill dropdown accordingly. Called after every huh form update.
func (m *Model) syncAutofillState() {
	if m.FormState == nil || !m.FormState.ShowExtended {
		if m.FormState != nil {
			m.FormState.Autofill = nil
		}
		return
	}

	focusedKey := m.FormState.focusedFieldKey()

	switch focusedKey {
	case formKeyParent:
		query := m.FormState.Parent
		// Don't show dropdown if value is an exact item ID (user just selected)
		if isExactAutofillID(query, m.FormState.AutofillEpics) {
			m.FormState.Autofill = nil
			return
		}
		if m.FormState.Autofill == nil || m.FormState.Autofill.FieldKey != formKeyParent {
			m.FormState.Autofill = &AutofillState{
				Active:   true,
				FieldKey: formKeyParent,
			}
		}
		if query != m.FormState.Autofill.Query {
			m.FormState.Autofill.Query = query
			m.FormState.Autofill.Filtered = filterAutofillItems(query, m.FormState.AutofillEpics)
			m.FormState.Autofill.Idx = 0
		}

	case formKeyDependencies:
		query := currentDepToken(m.FormState.Dependencies)
		// Don't show dropdown if current token is an exact item ID (user just selected)
		if isExactAutofillID(query, m.FormState.AutofillAll) {
			m.FormState.Autofill = nil
			return
		}
		// Don't re-activate with empty query after selection cleared the state
		if m.FormState.Autofill == nil && query == "" {
			return
		}
		if m.FormState.Autofill == nil || m.FormState.Autofill.FieldKey != formKeyDependencies {
			m.FormState.Autofill = &AutofillState{
				Active:   true,
				FieldKey: formKeyDependencies,
			}
		}
		if query != m.FormState.Autofill.Query {
			m.FormState.Autofill.Query = query
			existing := parseLabels(m.FormState.Dependencies)
			filtered := filterAutofillItems(query, m.FormState.AutofillAll)
			filtered = excludeIDs(filtered, existing)
			m.FormState.Autofill.Filtered = filtered
			m.FormState.Autofill.Idx = 0
		}

	default:
		m.FormState.Autofill = nil
	}
}

// selectAutofillItem handles Enter in the autofill dropdown: populates the field
// value, rebuilds the huh form, and restores focus to the target field.
func (m Model) selectAutofillItem() (tea.Model, tea.Cmd) {
	af := m.FormState.Autofill
	if af == nil || len(af.Filtered) == 0 || af.Idx < 0 || af.Idx >= len(af.Filtered) {
		return m, nil
	}
	selected := af.Filtered[af.Idx]

	if af.FieldKey == formKeyParent {
		m.FormState.Parent = selected.ID
		m.FormState.Autofill = nil
		m.FormState.buildForm()
		// Init form synchronously (sets up fields), discard its cmd to avoid
		// tea.WindowSize() race that snaps the form back to group 0.
		_ = m.FormState.Form.Init()
		// Navigate to details group; only this cmd runs async (target field cursor).
		return m, m.FormState.Form.NextGroup()
	}

	if af.FieldKey == formKeyDependencies {
		// Replace only the current token (text after last comma) with the selected ID
		parts := strings.Split(m.FormState.Dependencies, ",")
		if len(parts) > 0 {
			parts[len(parts)-1] = " " + selected.ID
		} else {
			parts = []string{selected.ID}
		}
		m.FormState.Dependencies = strings.TrimSpace(strings.Join(parts, ",")) + ", "
		// Close dropdown after selection (syncAutofillState won't re-activate
		// because currentDepToken returns "" which triggers the nil guard)
		m.FormState.Autofill = nil
		m.FormState.buildForm()
		// Init form synchronously, discard cmd to avoid tea.WindowSize() race.
		_ = m.FormState.Form.Init()
		_ = m.FormState.Form.NextGroup() // → detailsGroup (discard cmd)
		_ = m.FormState.Form.NextGroup() // → workflowGroup, Minor (discard cmd)
		// Navigate to Dependencies; only this cmd runs async (target field cursor).
		return m, m.FormState.Form.NextField()
	}

	return m, nil
}

// insertDropdownAfterField injects dropdownView into formView by scanning
// for the line containing nextFieldTitle and inserting the dropdown before it.
// Uses ansi.Strip for reliable matching in ANSI-styled output.
func insertDropdownAfterField(formView, dropdownView, nextFieldTitle string) string {
	lines := strings.Split(formView, "\n")
	var result []string
	inserted := false
	for _, line := range lines {
		if !inserted && strings.Contains(ansi.Strip(line), nextFieldTitle) {
			result = append(result, dropdownView)
			inserted = true
		}
		result = append(result, line)
	}
	if !inserted {
		// Fallback: append at end
		result = append(result, dropdownView)
	}
	return strings.Join(result, "\n")
}

// renderFormAutofillDropdown renders the autocomplete dropdown for the active field.
func (m Model) renderFormAutofillDropdown() string {
	if m.FormState == nil || m.FormState.Autofill == nil || !m.FormState.Autofill.Active {
		return ""
	}
	af := m.FormState.Autofill

	if len(af.Filtered) == 0 {
		if af.Query != "" {
			return subtleStyle.Render("  No matching issues")
		}
		if af.FieldKey == formKeyParent && len(m.FormState.AutofillEpics) == 0 {
			return subtleStyle.Render("  No epics found")
		}
		return ""
	}

	maxDropdown := 5
	dropdownCount := len(af.Filtered)
	if dropdownCount > maxDropdown {
		dropdownCount = maxDropdown
	}

	formWidth := m.FormState.Width
	if formWidth <= 0 {
		formWidth = 60
	}

	var lines []string

	// Header hint
	fieldLabel := "epics"
	if af.FieldKey == formKeyDependencies {
		fieldLabel = "issues"
	}
	lines = append(lines, subtleStyle.Render(fmt.Sprintf("  Matching %s:", fieldLabel)))

	for i := 0; i < dropdownCount; i++ {
		item := af.Filtered[i]
		prefix := "  "
		if i == af.Idx {
			prefix = "> "
		}

		title := item.Title
		maxTitle := formWidth - len(item.ID) - 10
		if maxTitle < 10 {
			maxTitle = 10
		}
		if len(title) > maxTitle {
			title = title[:maxTitle-3] + "..."
		}

		line := fmt.Sprintf("%s%s  %s", prefix, item.ID, title)
		if i == af.Idx {
			line = lipgloss.NewStyle().Foreground(primaryColor).Render(line)
		} else {
			line = subtleStyle.Render(line)
		}
		lines = append(lines, line)
	}

	if len(af.Filtered) > maxDropdown {
		lines = append(lines, subtleStyle.Render(
			fmt.Sprintf("  ... and %d more", len(af.Filtered)-maxDropdown)))
	}

	lines = append(lines, subtleStyle.Render("  ↑/↓ navigate  Enter select"))

	return strings.Join(lines, "\n")
}
