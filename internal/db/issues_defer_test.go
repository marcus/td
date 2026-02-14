package db

import (
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func strPtr(s string) *string {
	return &s
}

func TestListIssues_ExcludeDeferred(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// 1. Normal issue (no defer_until)
	normal := &models.Issue{Title: "normal issue"}
	if err := database.CreateIssue(normal); err != nil {
		t.Fatalf("CreateIssue normal: %v", err)
	}

	// 2. Deferred until far future
	futureDeferred := &models.Issue{Title: "future deferred", DeferUntil: strPtr("2099-01-01")}
	if err := database.CreateIssue(futureDeferred); err != nil {
		t.Fatalf("CreateIssue futureDeferred: %v", err)
	}

	// 3. Deferred until past (already surfaced)
	pastDeferred := &models.Issue{Title: "past deferred", DeferUntil: strPtr("2020-01-01")}
	if err := database.CreateIssue(pastDeferred); err != nil {
		t.Fatalf("CreateIssue pastDeferred: %v", err)
	}

	issues, err := database.ListIssues(ListIssuesOptions{ExcludeDeferred: true})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}

	ids := map[string]bool{}
	for _, iss := range issues {
		ids[iss.ID] = true
	}
	if !ids[normal.ID] {
		t.Errorf("expected normal issue %s in results", normal.ID)
	}
	if !ids[pastDeferred.ID] {
		t.Errorf("expected past deferred issue %s in results", pastDeferred.ID)
	}
	if ids[futureDeferred.ID] {
		t.Errorf("future deferred issue %s should be excluded", futureDeferred.ID)
	}
}

func TestListIssues_DeferredOnly(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// 1. Normal issue (no defer_until)
	normal := &models.Issue{Title: "normal issue"}
	if err := database.CreateIssue(normal); err != nil {
		t.Fatalf("CreateIssue normal: %v", err)
	}

	// 2. Deferred until far future
	futureDeferred := &models.Issue{Title: "future deferred", DeferUntil: strPtr("2099-01-01")}
	if err := database.CreateIssue(futureDeferred); err != nil {
		t.Fatalf("CreateIssue futureDeferred: %v", err)
	}

	// 3. Deferred until past (already surfaced)
	pastDeferred := &models.Issue{Title: "past deferred", DeferUntil: strPtr("2020-01-01")}
	if err := database.CreateIssue(pastDeferred); err != nil {
		t.Fatalf("CreateIssue pastDeferred: %v", err)
	}

	issues, err := database.ListIssues(ListIssuesOptions{DeferredOnly: true})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].ID != futureDeferred.ID {
		t.Errorf("expected issue %s, got %s", futureDeferred.ID, issues[0].ID)
	}
}

func TestListIssues_OverdueOnly(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// 1. Due in past, status open
	overdue := &models.Issue{Title: "overdue open", DueDate: strPtr("2020-01-01")}
	if err := database.CreateIssue(overdue); err != nil {
		t.Fatalf("CreateIssue overdue: %v", err)
	}

	// 2. Due in future, status open
	futureDue := &models.Issue{Title: "future due", DueDate: strPtr("2099-01-01")}
	if err := database.CreateIssue(futureDue); err != nil {
		t.Fatalf("CreateIssue futureDue: %v", err)
	}

	// 3. Due in past, status closed
	now := time.Now()
	closedOverdue := &models.Issue{
		Title:    "closed overdue",
		DueDate:  strPtr("2020-01-01"),
		Status:   models.StatusClosed,
		ClosedAt: &now,
	}
	if err := database.CreateIssue(closedOverdue); err != nil {
		t.Fatalf("CreateIssue closedOverdue: %v", err)
	}

	issues, err := database.ListIssues(ListIssuesOptions{OverdueOnly: true})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].ID != overdue.ID {
		t.Errorf("expected issue %s, got %s", overdue.ID, issues[0].ID)
	}
}

func TestListIssues_SurfacingOnly(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// 1. defer_until in past, defer_count=2 (surfacing: was deferred, now visible)
	surfacing := &models.Issue{Title: "surfacing", DeferUntil: strPtr("2020-01-01"), DeferCount: 2}
	if err := database.CreateIssue(surfacing); err != nil {
		t.Fatalf("CreateIssue surfacing: %v", err)
	}

	// 2. defer_until in past, defer_count=0 (not surfacing: never actually deferred)
	notDeferred := &models.Issue{Title: "not deferred", DeferUntil: strPtr("2020-01-01"), DeferCount: 0}
	if err := database.CreateIssue(notDeferred); err != nil {
		t.Fatalf("CreateIssue notDeferred: %v", err)
	}

	// 3. defer_until in future, defer_count=1 (still deferred, not surfacing yet)
	stillDeferred := &models.Issue{Title: "still deferred", DeferUntil: strPtr("2099-01-01"), DeferCount: 1}
	if err := database.CreateIssue(stillDeferred); err != nil {
		t.Fatalf("CreateIssue stillDeferred: %v", err)
	}

	issues, err := database.ListIssues(ListIssuesOptions{SurfacingOnly: true})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].ID != surfacing.ID {
		t.Errorf("expected issue %s, got %s", surfacing.ID, issues[0].ID)
	}
}

func TestListIssues_DueSoonDays(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	in10Days := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	// 1. Due tomorrow
	dueTomorrow := &models.Issue{Title: "due tomorrow", DueDate: &tomorrow}
	if err := database.CreateIssue(dueTomorrow); err != nil {
		t.Fatalf("CreateIssue dueTomorrow: %v", err)
	}

	// 2. Due in 10 days
	dueIn10 := &models.Issue{Title: "due in 10 days", DueDate: &in10Days}
	if err := database.CreateIssue(dueIn10); err != nil {
		t.Fatalf("CreateIssue dueIn10: %v", err)
	}

	// 3. Due yesterday (already past)
	dueYesterday := &models.Issue{Title: "due yesterday", DueDate: &yesterday}
	if err := database.CreateIssue(dueYesterday); err != nil {
		t.Fatalf("CreateIssue dueYesterday: %v", err)
	}

	issues, err := database.ListIssues(ListIssuesOptions{DueSoonDays: 3})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].ID != dueTomorrow.ID {
		t.Errorf("expected issue %s, got %s", dueTomorrow.ID, issues[0].ID)
	}
}

func TestListIssues_DefaultExcludesDeferred(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// 1. Normal issue
	normal := &models.Issue{Title: "normal issue"}
	if err := database.CreateIssue(normal); err != nil {
		t.Fatalf("CreateIssue normal: %v", err)
	}

	// 2. Deferred to far future
	deferred := &models.Issue{Title: "deferred issue", DeferUntil: strPtr("2099-01-01")}
	if err := database.CreateIssue(deferred); err != nil {
		t.Fatalf("CreateIssue deferred: %v", err)
	}

	// With ExcludeDeferred (as cmd/list.go sets) -- should hide deferred
	filtered, err := database.ListIssues(ListIssuesOptions{ExcludeDeferred: true})
	if err != nil {
		t.Fatalf("ListIssues ExcludeDeferred: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("ExcludeDeferred: expected 1 issue, got %d", len(filtered))
	}
	if filtered[0].ID != normal.ID {
		t.Errorf("ExcludeDeferred: expected %s, got %s", normal.ID, filtered[0].ID)
	}

	// With zero-value opts (no temporal flags) -- should include all
	all, err := database.ListIssues(ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues default: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("default: expected 2 issues, got %d", len(all))
	}
}
