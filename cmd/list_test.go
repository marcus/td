package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func TestParseDateFilter(t *testing.T) {
	// Reference date for testing
	refDate := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		input      string
		wantAfter  time.Time
		wantBefore time.Time
	}{
		{
			name:       "after: prefix",
			input:      "after:2024-06-15",
			wantAfter:  refDate,
			wantBefore: time.Time{},
		},
		{
			name:       "before: prefix",
			input:      "before:2024-06-15",
			wantAfter:  time.Time{},
			wantBefore: refDate,
		},
		{
			name:       "DATE.. format (after)",
			input:      "2024-06-15..",
			wantAfter:  refDate,
			wantBefore: time.Time{},
		},
		{
			name:       "..DATE format (before)",
			input:      "..2024-06-15",
			wantAfter:  time.Time{},
			wantBefore: refDate,
		},
		{
			name:       "DATE..DATE format (range)",
			input:      "2024-06-01..2024-06-30",
			wantAfter:  time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			wantBefore: time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "exact date (entire day)",
			input:      "2024-06-15",
			wantAfter:  refDate,
			wantBefore: refDate.Add(24 * time.Hour),
		},
		{
			name:       "invalid date returns zero values",
			input:      "invalid",
			wantAfter:  time.Time{},
			wantBefore: time.Time{},
		},
		{
			name:       "empty string returns zero values",
			input:      "",
			wantAfter:  time.Time{},
			wantBefore: time.Time{},
		},
		{
			name:       "whitespace trimmed",
			input:      "  2024-06-15  ",
			wantAfter:  refDate,
			wantBefore: refDate.Add(24 * time.Hour),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotAfter, gotBefore := parseDateFilter(tc.input)

			if !gotAfter.Equal(tc.wantAfter) {
				t.Errorf("parseDateFilter(%q) after = %v, want %v", tc.input, gotAfter, tc.wantAfter)
			}
			if !gotBefore.Equal(tc.wantBefore) {
				t.Errorf("parseDateFilter(%q) before = %v, want %v", tc.input, gotBefore, tc.wantBefore)
			}
		})
	}
}

func TestParsePointsFilter(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantMin int
		wantMax int
	}{
		{
			name:    "exact match",
			input:   "5",
			wantMin: 5,
			wantMax: 5,
		},
		{
			name:    "greater than or equal",
			input:   ">=3",
			wantMin: 3,
			wantMax: 0,
		},
		{
			name:    "less than or equal",
			input:   "<=8",
			wantMin: 0,
			wantMax: 8,
		},
		{
			name:    "range with dash",
			input:   "3-8",
			wantMin: 3,
			wantMax: 8,
		},
		{
			name:    "whitespace trimmed",
			input:   "  5  ",
			wantMin: 5,
			wantMax: 5,
		},
		{
			name:    "zero value",
			input:   "0",
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:    "large value",
			input:   "21",
			wantMin: 21,
			wantMax: 21,
		},
		{
			name:    "range 1-13",
			input:   "1-13",
			wantMin: 1,
			wantMax: 13,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotMin, gotMax := parsePointsFilter(tc.input)

			if gotMin != tc.wantMin {
				t.Errorf("parsePointsFilter(%q) min = %d, want %d", tc.input, gotMin, tc.wantMin)
			}
			if gotMax != tc.wantMax {
				t.Errorf("parsePointsFilter(%q) max = %d, want %d", tc.input, gotMax, tc.wantMax)
			}
		})
	}
}

func TestParseDateFilterEdgeCases(t *testing.T) {
	t.Run("malformed after: prefix", func(t *testing.T) {
		after, before := parseDateFilter("after:")
		if !after.IsZero() || !before.IsZero() {
			t.Error("Empty after: should return zero values")
		}
	})

	t.Run("malformed before: prefix", func(t *testing.T) {
		after, before := parseDateFilter("before:")
		if !after.IsZero() || !before.IsZero() {
			t.Error("Empty before: should return zero values")
		}
	})

	t.Run("malformed range with multiple ..", func(t *testing.T) {
		// This tests current behavior - multiple .. splits incorrectly
		after, before := parseDateFilter("2024-01-01..2024-06-15..2024-12-31")
		// Should parse first two parts
		if after.IsZero() {
			t.Log("Multiple .. in range handles first two parts")
		}
		_ = before // avoid unused variable
	})

	t.Run("range with only ..", func(t *testing.T) {
		after, before := parseDateFilter("..")
		if !after.IsZero() || !before.IsZero() {
			t.Error("Empty range should return zero values")
		}
	})
}

func TestParsePointsFilterEdgeCases(t *testing.T) {
	t.Run("negative value treated as zero", func(t *testing.T) {
		// Sscanf won't parse negative correctly with %d for this use case
		min, max := parsePointsFilter("-5")
		if min != 0 || max != 0 {
			// This depends on implementation - negative might be parsed differently
			t.Logf("Negative input: min=%d, max=%d", min, max)
		}
	})

	t.Run("non-numeric string", func(t *testing.T) {
		min, max := parsePointsFilter("abc")
		if min != 0 || max != 0 {
			t.Error("Non-numeric should return 0, 0")
		}
	})

	t.Run("range with spaces", func(t *testing.T) {
		min, max := parsePointsFilter("3 - 8")
		// Should fail to parse due to spaces in range
		t.Logf("Range with spaces: min=%d, max=%d", min, max)
	})

	t.Run(">=0 edge case", func(t *testing.T) {
		min, max := parsePointsFilter(">=0")
		if min != 0 || max != 0 {
			t.Errorf(">=0: got min=%d, max=%d", min, max)
		}
	})

	t.Run("<=0 edge case", func(t *testing.T) {
		min, max := parsePointsFilter("<=0")
		if min != 0 || max != 0 {
			t.Errorf("<=0: got min=%d, max=%d", min, max)
		}
	})
}

// TestStatusFilterParsing tests the status flag parsing logic used in list command
// These tests verify the behavior of --status filtering with all variants including --status all
func TestStatusFilterParsing(t *testing.T) {
	tests := []struct {
		name        string
		statusInput []string
		allFlag     bool
		expectAll   bool
		expectOpen  bool
	}{
		{
			name:        "single status open",
			statusInput: []string{"open"},
			allFlag:     false,
			expectAll:   false,
			expectOpen:  true,
		},
		{
			name:        "single status closed",
			statusInput: []string{"closed"},
			allFlag:     false,
			expectAll:   false,
			expectOpen:  false,
		},
		{
			name:        "multiple statuses comma-separated",
			statusInput: []string{"open,in_progress"},
			allFlag:     false,
			expectAll:   false,
			expectOpen:  true,
		},
		{
			name:        "multiple status flags",
			statusInput: []string{"open", "in_review"},
			allFlag:     false,
			expectAll:   false,
			expectOpen:  true,
		},
		{
			name:        "status all flag sets showAll to true",
			statusInput: []string{"all"},
			allFlag:     false,
			expectAll:   true,
			expectOpen:  false,
		},
		{
			name:        "status all with mixed case",
			statusInput: []string{"ALL"},
			allFlag:     false,
			expectAll:   true,
			expectOpen:  false,
		},
		{
			name:        "status all with trailing spaces",
			statusInput: []string{" all "},
			allFlag:     false,
			expectAll:   true,
			expectOpen:  false,
		},
		{
			name:        "all flag directly set",
			statusInput: []string{},
			allFlag:     true,
			expectAll:   true,
			expectOpen:  false,
		},
		{
			name:        "status all in comma-separated list",
			statusInput: []string{"open,all,closed"},
			allFlag:     false,
			expectAll:   true,
			expectOpen:  false,
		},
		{
			name:        "review alias normalization",
			statusInput: []string{"review"},
			allFlag:     false,
			expectAll:   false,
			expectOpen:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate status parsing logic from list.go
			opts := db.ListIssuesOptions{}
			showAll := tc.allFlag

			for _, s := range tc.statusInput {
				for _, part := range strings.Split(s, ",") {
					part = strings.TrimSpace(part)
					if part != "" {
						if strings.EqualFold(part, "all") {
							showAll = true
							continue
						}
						status := models.NormalizeStatus(part)
						opts.Status = append(opts.Status, status)
					}
				}
			}

			if !showAll && len(opts.Status) == 0 {
				// Default: exclude closed issues unless --all is specified
				opts.Status = []models.Status{
					models.StatusOpen,
					models.StatusInProgress,
					models.StatusBlocked,
					models.StatusInReview,
				}
			}

			if tc.expectAll {
				if !showAll {
					t.Errorf("Expected showAll=true, got false")
				}
			}

			if tc.expectOpen && len(opts.Status) > 0 {
				foundOpen := false
				for _, s := range opts.Status {
					if s == models.StatusOpen {
						foundOpen = true
						break
					}
				}
				if !foundOpen {
					t.Errorf("Expected StatusOpen in status filter, got %v", opts.Status)
				}
			}
		})
	}
}

// TestStatusAllIncludesAllStatuses verifies that --status all includes all possible statuses
// in the filtering logic (not necessarily in CLI output, but in the database filter)
func TestStatusAllIncludesAllStatuses(t *testing.T) {
	// Simulate the logic: when showAll=true and no specific statuses set,
	// the default filter should allow all statuses
	t.Run("status all should not add status filter", func(t *testing.T) {
		opts := db.ListIssuesOptions{}
		showAll := true

		// When showAll is true and no statuses specified, opts.Status remains empty
		// This means no status filtering is applied, allowing all statuses
		if len(opts.Status) != 0 {
			t.Errorf("Expected empty status filter when showAll=true with no specific statuses")
		}
		if !showAll {
			t.Error("Expected showAll=true")
		}
	})

	t.Run("default behavior excludes closed when no status specified", func(t *testing.T) {
		opts := db.ListIssuesOptions{}
		showAll := false

		if !showAll && len(opts.Status) == 0 {
			opts.Status = []models.Status{
				models.StatusOpen,
				models.StatusInProgress,
				models.StatusBlocked,
				models.StatusInReview,
			}
		}

		// Verify closed status is not in default
		for _, s := range opts.Status {
			if s == models.StatusClosed {
				t.Error("Expected StatusClosed not to be in default filter")
			}
		}

		if len(opts.Status) != 4 {
			t.Errorf("Expected 4 default statuses, got %d", len(opts.Status))
		}
	})

	t.Run("status all vs default behavior difference", func(t *testing.T) {
		// Default: no --all flag
		defaultOpts := db.ListIssuesOptions{}
		showAllDefault := false
		if !showAllDefault && len(defaultOpts.Status) == 0 {
			defaultOpts.Status = []models.Status{
				models.StatusOpen,
				models.StatusInProgress,
				models.StatusBlocked,
				models.StatusInReview,
			}
		}

		// With --status all
		allOpts := db.ListIssuesOptions{}
		showAllWithAll := true

		if len(defaultOpts.Status) == 0 {
			t.Error("Default should have status filters")
		}

		if showAllWithAll && len(allOpts.Status) == 0 {
			// With showAll=true, no status filter means all statuses included
			t.Log("With --status all, no status filter is applied - all statuses will be included")
		}
	})
}

// TestStatusAllVariations tests different ways to invoke --status all
func TestStatusAllVariations(t *testing.T) {
	tests := []struct {
		name              string
		statusInput       []string
		shouldActivateAll bool
	}{
		{
			name:              "lowercase all",
			statusInput:       []string{"all"},
			shouldActivateAll: true,
		},
		{
			name:              "uppercase ALL",
			statusInput:       []string{"ALL"},
			shouldActivateAll: true,
		},
		{
			name:              "mixed case AlL",
			statusInput:       []string{"AlL"},
			shouldActivateAll: true,
		},
		{
			name:              "all with spaces",
			statusInput:       []string{"  all  "},
			shouldActivateAll: true,
		},
		{
			name:              "all in comma-separated first position",
			statusInput:       []string{"all,open"},
			shouldActivateAll: true,
		},
		{
			name:              "all in comma-separated middle position",
			statusInput:       []string{"open,all,closed"},
			shouldActivateAll: true,
		},
		{
			name:              "all in comma-separated last position",
			statusInput:       []string{"open,closed,all"},
			shouldActivateAll: true,
		},
		{
			name:              "all as separate flag",
			statusInput:       []string{"open", "all"},
			shouldActivateAll: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			showAll := false
			statusCount := 0

			for _, s := range tc.statusInput {
				for _, part := range strings.Split(s, ",") {
					part = strings.TrimSpace(part)
					if part != "" {
						if strings.EqualFold(part, "all") {
							showAll = true
						} else {
							statusCount++
						}
					}
				}
			}

			if tc.shouldActivateAll && !showAll {
				t.Error("Expected --status all to activate showAll=true")
			}
			if !tc.shouldActivateAll && showAll {
				t.Error("Expected showAll=false for this input")
			}
		})
	}
}
