package monitor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/internal/models"
)

func TestIssueDetailStatusStylesUseActionColorMapping(t *testing.T) {
	tests := []struct {
		name      string
		status    models.Status
		colorCode string
	}{
		{name: "open uses start/info color", status: models.StatusOpen, colorCode: "45"},
		{name: "in_progress uses warning fallback", status: models.StatusInProgress, colorCode: "214"},
		{name: "blocked uses danger color", status: models.StatusBlocked, colorCode: "196"},
		{name: "in_review uses review color", status: models.StatusInReview, colorCode: "141"},
		{name: "closed uses success color", status: models.StatusClosed, colorCode: "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			style, ok := issueDetailStatusStyle(tt.status)
			if !ok {
				t.Fatalf("missing issue detail style for status %q", tt.status)
			}
			if got := fmt.Sprint(style.GetForeground()); got != tt.colorCode {
				t.Fatalf("issueDetailStatusStyle(%q) foreground = %q, want %q", tt.status, got, tt.colorCode)
			}
			if !style.GetBold() {
				t.Fatalf("issueDetailStatusStyle(%q) should be bold", tt.status)
			}
			if got := formatIssueDetailStatus(tt.status); !strings.Contains(got, string(tt.status)) {
				t.Fatalf("formatIssueDetailStatus(%q) = %q, want status text", tt.status, got)
			}
		})
	}
}

func TestFormatIssueDetailStatusFallsBackToDefaultFormatting(t *testing.T) {
	status := models.Status("queued")
	if got := formatIssueDetailStatus(status); got != string(status) {
		t.Fatalf("formatIssueDetailStatus(%q) = %q, want %q", status, got, status)
	}
}

func TestRenderModalShowsStatusOnFirstContentLine(t *testing.T) {
	createdAt := time.Date(2026, time.March, 29, 10, 30, 0, 0, time.UTC)
	issue := &models.Issue{
		ID:        "td-123",
		Title:     "Status line lands first",
		Status:    models.StatusOpen,
		Type:      models.TypeTask,
		Priority:  models.PriorityP1,
		CreatedAt: createdAt,
	}

	m := Model{
		Width:  120,
		Height: 40,
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID: issue.ID,
				Issue:   issue,
			},
		},
	}

	lines := modalContentLines(ansi.Strip(m.renderModal()))
	if len(lines) < 3 {
		t.Fatalf("renderModal() produced %d content lines, want at least 3: %q", len(lines), lines)
	}

	if lines[0] != string(models.StatusOpen) {
		t.Fatalf("first content line = %q, want %q", lines[0], models.StatusOpen)
	}
	if lines[1] != issue.ID+" "+issue.Title {
		t.Fatalf("second content line = %q, want title line %q", lines[1], issue.ID+" "+issue.Title)
	}
	if strings.Contains(lines[2], string(models.StatusOpen)) {
		t.Fatalf("metadata line %q should not repeat status text", lines[2])
	}
}

func TestComputeModalSectionLinesAccountsForTopStatusLine(t *testing.T) {
	modal := &ModalEntry{
		IssueID: "td-123",
		Issue: &models.Issue{
			ID:        "td-123",
			Title:     "Status line shifts section starts",
			Status:    models.StatusOpen,
			Type:      models.TypeTask,
			Priority:  models.PriorityP1,
			CreatedAt: time.Date(2026, time.March, 29, 10, 30, 0, 0, time.UTC),
		},
		BlockedBy: []models.Issue{
			{ID: "td-blocker", Title: "Blocker", Status: models.StatusBlocked},
		},
		Blocks: []models.Issue{
			{ID: "td-dependent", Title: "Dependent", Status: models.StatusOpen},
		},
	}

	computeModalSectionLines(modal)

	if modal.BlockedByStartLine != 5 {
		t.Fatalf("BlockedByStartLine = %d, want 5", modal.BlockedByStartLine)
	}
	if modal.BlockedByEndLine != 7 {
		t.Fatalf("BlockedByEndLine = %d, want 7", modal.BlockedByEndLine)
	}
	if modal.BlocksStartLine != 8 {
		t.Fatalf("BlocksStartLine = %d, want 8", modal.BlocksStartLine)
	}
	if modal.BlocksEndLine != 10 {
		t.Fatalf("BlocksEndLine = %d, want 10", modal.BlocksEndLine)
	}
}

func modalContentLines(rendered string) []string {
	rawLines := strings.Split(rendered, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.Trim(line, " │╭╮╰╯─")
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}
