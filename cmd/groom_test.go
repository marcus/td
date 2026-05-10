package cmd

import (
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func TestCheckMissingDescription(t *testing.T) {
	tests := []struct {
		name  string
		desc  string
		found bool
	}{
		{"empty", "", true},
		{"whitespace", "   ", true},
		{"present", "A real description", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &models.Issue{ID: "td-abc", Title: "Test", Description: tt.desc}
			f := checkMissingDescription(issue)
			if tt.found && f == nil {
				t.Error("expected finding, got nil")
			}
			if !tt.found && f != nil {
				t.Errorf("expected nil, got %+v", f)
			}
		})
	}
}

func TestCheckMissingAcceptance(t *testing.T) {
	issue := &models.Issue{ID: "td-abc", Title: "Test", Acceptance: ""}
	f := checkMissingAcceptance(issue)
	if f == nil {
		t.Fatal("expected finding for missing acceptance")
	}
	if f.Check != "acceptance" {
		t.Errorf("expected check=acceptance, got %s", f.Check)
	}

	issue.Acceptance = "Given/When/Then"
	f = checkMissingAcceptance(issue)
	if f != nil {
		t.Error("expected nil for issue with acceptance")
	}
}

func TestCheckUnestimatedPoints(t *testing.T) {
	issue := &models.Issue{ID: "td-abc", Title: "Test", Points: 0}
	f := checkUnestimatedPoints(issue)
	if f == nil {
		t.Fatal("expected finding for zero points")
	}

	issue.Points = 3
	f = checkUnestimatedPoints(issue)
	if f != nil {
		t.Error("expected nil for estimated issue")
	}
}

func TestCheckVagueTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		found bool
	}{
		{"short", "Fix bug", true},
		{"borderline", "14 chars here!", true},  // 14 < 15
		{"exact", "15 chars here!!", false},      // 15 chars
		{"long", "A detailed title for the issue", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &models.Issue{ID: "td-abc", Title: tt.title}
			f := checkVagueTitle(issue)
			if tt.found && f == nil {
				t.Error("expected finding, got nil")
			}
			if !tt.found && f != nil {
				t.Errorf("expected nil, got %+v", f)
			}
		})
	}
}

func TestCheckStale(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		status    models.Status
		updatedAt time.Time
		found     bool
	}{
		{"fresh open", models.StatusOpen, now.Add(-24 * time.Hour), false},
		{"stale open", models.StatusOpen, now.Add(-35 * 24 * time.Hour), true},
		{"fresh in_progress", models.StatusInProgress, now.Add(-3 * 24 * time.Hour), false},
		{"stale in_progress", models.StatusInProgress, now.Add(-10 * 24 * time.Hour), true},
		{"in_review not stale", models.StatusInReview, now.Add(-60 * 24 * time.Hour), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &models.Issue{ID: "td-abc", Title: "Test issue title here", Status: tt.status, UpdatedAt: tt.updatedAt}
			f := checkStale(issue, now)
			if tt.found && f == nil {
				t.Error("expected finding, got nil")
			}
			if !tt.found && f != nil {
				t.Errorf("expected nil, got %+v", f)
			}
		})
	}
}

func TestParseStatuses(t *testing.T) {
	statuses := parseStatuses("open,in_progress")
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
	if statuses[0] != models.StatusOpen || statuses[1] != models.StatusInProgress {
		t.Errorf("unexpected statuses: %v", statuses)
	}
}

func TestParseChecks(t *testing.T) {
	checks := parseChecks("")
	if checks != nil {
		t.Error("empty string should return nil")
	}

	checks = parseChecks("description,points")
	if !checks["description"] || !checks["points"] {
		t.Error("expected description and points enabled")
	}
	if checks["stale"] {
		t.Error("stale should not be enabled")
	}
}

func TestCheckEnabled(t *testing.T) {
	if !checkEnabled(nil, "anything") {
		t.Error("nil map should enable all checks")
	}

	m := map[string]bool{"description": true}
	if !checkEnabled(m, "description") {
		t.Error("should be enabled")
	}
	if checkEnabled(m, "points") {
		t.Error("should not be enabled")
	}
}
