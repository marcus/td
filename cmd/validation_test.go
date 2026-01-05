package cmd

import (
	"strings"
	"testing"
)

func TestValidateIssueID(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		wantError bool
	}{
		{
			name:      "valid ID",
			id:        "td-abc123",
			wantError: false,
		},
		{
			name:      "empty string",
			id:        "",
			wantError: true,
		},
		{
			name:      "whitespace only",
			id:        "   ",
			wantError: true,
		},
		{
			name:      "tabs only",
			id:        "\t\t",
			wantError: true,
		},
		{
			name:      "newlines only",
			id:        "\n\n",
			wantError: true,
		},
		{
			name:      "ID with surrounding whitespace",
			id:        "  td-valid  ",
			wantError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateIssueID(tc.id, "show <issue-id>")
			if tc.wantError && err == nil {
				t.Errorf("ValidateIssueID(%q) expected error, got nil", tc.id)
			}
			if !tc.wantError && err != nil {
				t.Errorf("ValidateIssueID(%q) unexpected error: %v", tc.id, err)
			}
		})
	}
}

func TestValidateIssueIDErrorMessage(t *testing.T) {
	err := ValidateIssueID("", "show <issue-id>")
	if err == nil {
		t.Fatal("Expected error for empty ID")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "issue ID required") {
		t.Errorf("Error message should contain 'issue ID required': %s", errMsg)
	}
	if !strings.Contains(errMsg, "td show <issue-id>") {
		t.Errorf("Error message should contain usage hint: %s", errMsg)
	}
}

func TestValidateIssueIDs(t *testing.T) {
	tests := []struct {
		name      string
		ids       []string
		wantError bool
	}{
		{
			name:      "all valid IDs",
			ids:       []string{"td-abc", "td-def", "td-ghi"},
			wantError: false,
		},
		{
			name:      "one empty ID",
			ids:       []string{"td-abc", "", "td-ghi"},
			wantError: true,
		},
		{
			name:      "first ID empty",
			ids:       []string{"", "td-def"},
			wantError: true,
		},
		{
			name:      "last ID whitespace",
			ids:       []string{"td-abc", "   "},
			wantError: true,
		},
		{
			name:      "empty slice",
			ids:       []string{},
			wantError: false,
		},
		{
			name:      "single valid ID",
			ids:       []string{"td-single"},
			wantError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateIssueIDs(tc.ids, "show <issue-id>")
			if tc.wantError && err == nil {
				t.Errorf("ValidateIssueIDs(%v) expected error, got nil", tc.ids)
			}
			if !tc.wantError && err != nil {
				t.Errorf("ValidateIssueIDs(%v) unexpected error: %v", tc.ids, err)
			}
		})
	}
}
