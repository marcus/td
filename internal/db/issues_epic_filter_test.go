package db

import (
	"strings"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestEpicFilterWithStatusAndNormalizedID(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	epic := &models.Issue{
		Title: "Epic for combined filters",
		Type:  models.TypeEpic,
	}
	if err := db.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue epic failed: %v", err)
	}

	openChild := &models.Issue{
		Title:    "Open child",
		ParentID: epic.ID,
		Status:   models.StatusOpen,
	}
	inProgressChild := &models.Issue{
		Title:    "In progress child",
		ParentID: epic.ID,
		Status:   models.StatusInProgress,
	}
	inReviewChild := &models.Issue{
		Title:    "In review child",
		ParentID: epic.ID,
		Status:   models.StatusInReview,
	}

	for _, issue := range []*models.Issue{openChild, inProgressChild, inReviewChild} {
		if err := db.CreateIssue(issue); err != nil {
			t.Fatalf("CreateIssue child failed: %v", err)
		}
	}

	tests := []struct {
		name       string
		epicID     string
		statuses   []models.Status
		wantIDs    []string
		notWantIDs []string
	}{
		{
			name:       "full epic id with single status",
			epicID:     epic.ID,
			statuses:   []models.Status{models.StatusInReview},
			wantIDs:    []string{inReviewChild.ID},
			notWantIDs: []string{openChild.ID, inProgressChild.ID},
		},
		{
			name:       "bare epic id with multiple statuses",
			epicID:     strings.TrimPrefix(epic.ID, "td-"),
			statuses:   []models.Status{models.StatusOpen, models.StatusInProgress},
			wantIDs:    []string{openChild.ID, inProgressChild.ID},
			notWantIDs: []string{inReviewChild.ID},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results, err := db.ListIssues(ListIssuesOptions{
				EpicID: tc.epicID,
				Status: tc.statuses,
			})
			if err != nil {
				t.Fatalf("ListIssues failed: %v", err)
			}

			found := make(map[string]bool, len(results))
			for _, issue := range results {
				found[issue.ID] = true
			}

			if len(results) != len(tc.wantIDs) {
				t.Fatalf("expected %d results, got %d", len(tc.wantIDs), len(results))
			}
			for _, wantID := range tc.wantIDs {
				if !found[wantID] {
					t.Fatalf("missing expected issue %s in results %#v", wantID, found)
				}
			}
			for _, notWantID := range tc.notWantIDs {
				if found[notWantID] {
					t.Fatalf("unexpected issue %s in results %#v", notWantID, found)
				}
			}
		})
	}
}
