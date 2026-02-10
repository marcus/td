package monitor

import (
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestFilterAutofillItems_EmptyQuery(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "First item", Type: models.TypeTask},
		{ID: "td-bbb", Title: "Second item", Type: models.TypeEpic},
	}
	result := filterAutofillItems("", items)
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestFilterAutofillItems_ByID(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa111", Title: "First item", Type: models.TypeTask},
		{ID: "td-bbb222", Title: "Second item", Type: models.TypeEpic},
		{ID: "td-ccc333", Title: "Third item", Type: models.TypeTask},
	}
	result := filterAutofillItems("bbb", items)
	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}
	if result[0].ID != "td-bbb222" {
		t.Errorf("expected td-bbb222, got %s", result[0].ID)
	}
}

func TestFilterAutofillItems_ByTitle(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "Add authentication", Type: models.TypeTask},
		{ID: "td-bbb", Title: "Fix login bug", Type: models.TypeBug},
		{ID: "td-ccc", Title: "Auth refactor", Type: models.TypeFeature},
	}
	result := filterAutofillItems("auth", items)
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestFilterAutofillItems_CaseInsensitive(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "IMPORTANT Task", Type: models.TypeTask},
	}
	result := filterAutofillItems("important", items)
	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}
}

func TestFilterAutofillItems_NoMatch(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "First item", Type: models.TypeTask},
		{ID: "td-bbb", Title: "Second item", Type: models.TypeEpic},
	}
	result := filterAutofillItems("nonexistent", items)
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}
}

func TestFilterAutofillItems_TDPrefix(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-abc123", Title: "Task one", Type: models.TypeTask},
		{ID: "td-def456", Title: "Task two", Type: models.TypeTask},
	}
	result := filterAutofillItems("td-abc", items)
	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}
	if result[0].ID != "td-abc123" {
		t.Errorf("expected td-abc123, got %s", result[0].ID)
	}
}

func TestFilterAutofillItems_FuzzyMatch(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "Add authentication module", Type: models.TypeTask},
		{ID: "td-bbb", Title: "Fix login bug", Type: models.TypeBug},
		{ID: "td-ccc", Title: "Auth token handler", Type: models.TypeFeature},
	}
	// "athn" should fuzzy-match items containing a-t-h-n in sequence
	result := filterAutofillItems("athn", items)
	if len(result) < 2 {
		t.Fatalf("expected at least 2 fuzzy matches for 'athn', got %d", len(result))
	}
	// "Auth token handler" should rank highest: "a-t-h" are consecutive in "Auth"
	if result[0].ID != "td-ccc" {
		t.Errorf("expected td-ccc as best match, got %s", result[0].ID)
	}
	// "Fix login bug" should not match (no a-t-h-n sequence)
	for _, item := range result {
		if item.ID == "td-bbb" {
			t.Error("expected td-bbb to not match 'athn'")
		}
	}
}

func TestFilterAutofillItems_FuzzyRanking(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "Something else entirely", Type: models.TypeTask},
		{ID: "td-bbb", Title: "Fix database migration", Type: models.TypeBug},
		{ID: "td-abc123", Title: "Task runner", Type: models.TypeTask},
	}
	// "abc" should rank td-abc123 highest (exact ID substring match)
	result := filterAutofillItems("abc", items)
	if len(result) == 0 {
		t.Fatal("expected at least 1 match for 'abc'")
	}
	if result[0].ID != "td-abc123" {
		t.Errorf("expected td-abc123 as best match, got %s", result[0].ID)
	}
}

func TestCurrentDepToken_SingleValue(t *testing.T) {
	result := currentDepToken("td-aaa")
	if result != "td-aaa" {
		t.Errorf("expected 'td-aaa', got '%s'", result)
	}
}

func TestCurrentDepToken_MultipleValues(t *testing.T) {
	result := currentDepToken("td-aaa, td-bbb, partial")
	if result != "partial" {
		t.Errorf("expected 'partial', got '%s'", result)
	}
}

func TestCurrentDepToken_TrailingComma(t *testing.T) {
	result := currentDepToken("td-aaa, td-bbb, ")
	if result != "" {
		t.Errorf("expected empty string, got '%s'", result)
	}
}

func TestCurrentDepToken_Empty(t *testing.T) {
	result := currentDepToken("")
	if result != "" {
		t.Errorf("expected empty string, got '%s'", result)
	}
}

func TestCurrentDepToken_WithSpaces(t *testing.T) {
	result := currentDepToken("td-aaa,  td-bbb ,  td-c")
	if result != "td-c" {
		t.Errorf("expected 'td-c', got '%s'", result)
	}
}

func TestExcludeIDs_RemovesExisting(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "First"},
		{ID: "td-bbb", Title: "Second"},
		{ID: "td-ccc", Title: "Third"},
	}
	result := excludeIDs(items, []string{"td-bbb"})
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
	for _, item := range result {
		if item.ID == "td-bbb" {
			t.Error("expected td-bbb to be excluded")
		}
	}
}

func TestExcludeIDs_EmptyExcluded(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "First"},
		{ID: "td-bbb", Title: "Second"},
	}
	result := excludeIDs(items, nil)
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestExcludeIDs_AllExcluded(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "First"},
		{ID: "td-bbb", Title: "Second"},
	}
	result := excludeIDs(items, []string{"td-aaa", "td-bbb"})
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}
}

func TestExcludeIDs_TrimSpaces(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "First"},
		{ID: "td-bbb", Title: "Second"},
	}
	result := excludeIDs(items, []string{" td-aaa "})
	if len(result) != 1 {
		t.Errorf("expected 1 item, got %d", len(result))
	}
	if result[0].ID != "td-bbb" {
		t.Errorf("expected td-bbb, got %s", result[0].ID)
	}
}

func TestIsExactAutofillID_Match(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "First"},
		{ID: "td-bbb", Title: "Second"},
	}
	if !isExactAutofillID("td-aaa", items) {
		t.Error("expected true for exact ID match")
	}
}

func TestIsExactAutofillID_NoMatch(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "First"},
	}
	if isExactAutofillID("td-bbb", items) {
		t.Error("expected false for non-matching ID")
	}
}

func TestIsExactAutofillID_Empty(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa", Title: "First"},
	}
	if isExactAutofillID("", items) {
		t.Error("expected false for empty query")
	}
}

func TestIsExactAutofillID_Partial(t *testing.T) {
	items := []AutofillItem{
		{ID: "td-aaa123", Title: "First"},
	}
	if isExactAutofillID("td-aaa", items) {
		t.Error("expected false for partial ID match")
	}
}
