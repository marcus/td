package serverdb

import (
	"testing"
	"time"
)

func TestUpdateProjectEventCount(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("ec@test.com")
	p, _ := db.CreateProject("proj", "", u.ID)

	now := time.Now().UTC()
	if err := db.UpdateProjectEventCount(p.ID, 5, now); err != nil {
		t.Fatalf("update event count: %v", err)
	}

	count, lastAt, err := db.GetProjectEventCount(p.ID)
	if err != nil {
		t.Fatalf("get event count: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected 5, got %d", count)
	}
	if lastAt == nil {
		t.Fatal("expected last_event_at to be set")
	}
}

func TestUpdateProjectEventCount_Accumulates(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("acc@test.com")
	p, _ := db.CreateProject("proj", "", u.ID)

	t1 := time.Now().UTC()
	_ = db.UpdateProjectEventCount(p.ID, 3, t1)

	t2 := t1.Add(time.Minute)
	_ = db.UpdateProjectEventCount(p.ID, 7, t2)

	count, lastAt, err := db.GetProjectEventCount(p.ID)
	if err != nil {
		t.Fatalf("get event count: %v", err)
	}
	if count != 10 {
		t.Fatalf("expected 10, got %d", count)
	}
	if lastAt == nil {
		t.Fatal("expected last_event_at to be set")
	}
	// last_event_at should be the second timestamp
	if lastAt.Before(t1) {
		t.Fatalf("last_event_at %v should be >= %v", lastAt, t1)
	}
}

func TestGetProjectEventCount_NotFound(t *testing.T) {
	db := newTestDB(t)
	_, _, err := db.GetProjectEventCount("p_nonexistent")
	if err == nil {
		t.Fatal("expected error for missing project")
	}
}

func TestGetProjectEventCount_Defaults(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("def@test.com")
	p, _ := db.CreateProject("proj", "", u.ID)

	count, lastAt, err := db.GetProjectEventCount(p.ID)
	if err != nil {
		t.Fatalf("get event count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
	if lastAt != nil {
		t.Fatalf("expected nil last_event_at, got %v", lastAt)
	}
}

func TestGetProject_IncludesEventCount(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("gp@test.com")
	p, _ := db.CreateProject("proj", "", u.ID)

	now := time.Now().UTC()
	_ = db.UpdateProjectEventCount(p.ID, 12, now)

	got, err := db.GetProject(p.ID, false)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if got.EventCount != 12 {
		t.Fatalf("expected event_count 12, got %d", got.EventCount)
	}
	if got.LastEventAt == nil {
		t.Fatal("expected last_event_at to be set on project")
	}
}

func TestListProjectsForUser_IncludesEventCount(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("lp@test.com")
	p1, _ := db.CreateProject("p1", "", u.ID)
	p2, _ := db.CreateProject("p2", "", u.ID)

	now := time.Now().UTC()
	_ = db.UpdateProjectEventCount(p1.ID, 5, now)
	_ = db.UpdateProjectEventCount(p2.ID, 20, now)

	projects, err := db.ListProjectsForUser(u.ID)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	counts := map[string]int{}
	for _, p := range projects {
		counts[p.Name] = p.EventCount
	}
	if counts["p1"] != 5 {
		t.Fatalf("p1 event_count: expected 5, got %d", counts["p1"])
	}
	if counts["p2"] != 20 {
		t.Fatalf("p2 event_count: expected 20, got %d", counts["p2"])
	}
}
