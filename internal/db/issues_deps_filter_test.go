package db

import (
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestListIssues_ExcludeHasOpenDeps(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	// Create task B (the dependency — starts open)
	taskB := &models.Issue{Title: "task B (dependency)", Status: models.StatusOpen}
	if err := database.CreateIssue(taskB); err != nil {
		t.Fatalf("CreateIssue taskB: %v", err)
	}

	// Create task A that depends on B
	taskA := &models.Issue{Title: "task A (depends on B)", Status: models.StatusOpen}
	if err := database.CreateIssue(taskA); err != nil {
		t.Fatalf("CreateIssue taskA: %v", err)
	}
	if err := database.AddDependency(taskA.ID, taskB.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	// Create task C with no dependencies
	taskC := &models.Issue{Title: "task C (independent)", Status: models.StatusOpen}
	if err := database.CreateIssue(taskC); err != nil {
		t.Fatalf("CreateIssue taskC: %v", err)
	}

	// --- With ExcludeHasOpenDeps: task A should be excluded ---
	issues, err := database.ListIssues(ListIssuesOptions{
		Status:             []models.Status{models.StatusOpen},
		ExcludeHasOpenDeps: true,
	})
	if err != nil {
		t.Fatalf("ListIssues ExcludeHasOpenDeps: %v", err)
	}

	ids := map[string]bool{}
	for _, iss := range issues {
		ids[iss.ID] = true
	}

	if ids[taskA.ID] {
		t.Errorf("task A (%s) should be excluded (has open dependency on B)", taskA.ID)
	}
	if !ids[taskB.ID] {
		t.Errorf("task B (%s) should be included (no dependencies)", taskB.ID)
	}
	if !ids[taskC.ID] {
		t.Errorf("task C (%s) should be included (no dependencies)", taskC.ID)
	}

	// --- Close task B, then task A should appear ---
	taskB.Status = models.StatusClosed
	if err := database.UpdateIssue(taskB); err != nil {
		t.Fatalf("UpdateIssue taskB: %v", err)
	}

	issues2, err := database.ListIssues(ListIssuesOptions{
		Status:             []models.Status{models.StatusOpen},
		ExcludeHasOpenDeps: true,
	})
	if err != nil {
		t.Fatalf("ListIssues after close: %v", err)
	}

	ids2 := map[string]bool{}
	for _, iss := range issues2 {
		ids2[iss.ID] = true
	}

	if !ids2[taskA.ID] {
		t.Errorf("task A (%s) should now be included (dependency B is closed)", taskA.ID)
	}
	if !ids2[taskC.ID] {
		t.Errorf("task C (%s) should still be included", taskC.ID)
	}
}

func TestListIssues_ExcludeHasOpenDeps_DeletedDepIgnored(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	// Create task B (dependency) and soft-delete it
	taskB := &models.Issue{Title: "task B (deleted dep)", Status: models.StatusOpen}
	if err := database.CreateIssue(taskB); err != nil {
		t.Fatalf("CreateIssue taskB: %v", err)
	}
	if err := database.DeleteIssue(taskB.ID); err != nil {
		t.Fatalf("DeleteIssue taskB: %v", err)
	}

	// Create task A that depends on deleted B
	taskA := &models.Issue{Title: "task A", Status: models.StatusOpen}
	if err := database.CreateIssue(taskA); err != nil {
		t.Fatalf("CreateIssue taskA: %v", err)
	}
	if err := database.AddDependency(taskA.ID, taskB.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	// Deleted deps should not block — task A should appear
	issues, err := database.ListIssues(ListIssuesOptions{
		Status:             []models.Status{models.StatusOpen},
		ExcludeHasOpenDeps: true,
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	found := false
	for _, iss := range issues {
		if iss.ID == taskA.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("task A should be included (dependency B is soft-deleted)")
	}
}

func TestListIssues_ExcludeHasOpenDeps_MultipleDeps(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	// Create two dependencies
	dep1 := &models.Issue{Title: "dep1", Status: models.StatusClosed}
	if err := database.CreateIssue(dep1); err != nil {
		t.Fatalf("CreateIssue dep1: %v", err)
	}
	dep2 := &models.Issue{Title: "dep2", Status: models.StatusOpen}
	if err := database.CreateIssue(dep2); err != nil {
		t.Fatalf("CreateIssue dep2: %v", err)
	}

	// Task A depends on both
	taskA := &models.Issue{Title: "task A", Status: models.StatusOpen}
	if err := database.CreateIssue(taskA); err != nil {
		t.Fatalf("CreateIssue taskA: %v", err)
	}
	if err := database.AddDependency(taskA.ID, dep1.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency dep1: %v", err)
	}
	if err := database.AddDependency(taskA.ID, dep2.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency dep2: %v", err)
	}

	// One dep still open — task A should be excluded
	issues, err := database.ListIssues(ListIssuesOptions{
		Status:             []models.Status{models.StatusOpen},
		ExcludeHasOpenDeps: true,
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	for _, iss := range issues {
		if iss.ID == taskA.ID {
			t.Errorf("task A should be excluded (dep2 still open)")
		}
	}

	// Close dep2 — now task A should appear
	dep2.Status = models.StatusClosed
	if err := database.UpdateIssue(dep2); err != nil {
		t.Fatalf("UpdateIssue dep2: %v", err)
	}

	issues2, err := database.ListIssues(ListIssuesOptions{
		Status:             []models.Status{models.StatusOpen},
		ExcludeHasOpenDeps: true,
	})
	if err != nil {
		t.Fatalf("ListIssues after close: %v", err)
	}

	found := false
	for _, iss := range issues2 {
		if iss.ID == taskA.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("task A should now be included (all deps closed)")
	}
}

func TestListIssues_WithoutExcludeHasOpenDeps(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	// Create dependency B (open)
	taskB := &models.Issue{Title: "task B", Status: models.StatusOpen}
	if err := database.CreateIssue(taskB); err != nil {
		t.Fatalf("CreateIssue taskB: %v", err)
	}

	// Create task A depending on B
	taskA := &models.Issue{Title: "task A", Status: models.StatusOpen}
	if err := database.CreateIssue(taskA); err != nil {
		t.Fatalf("CreateIssue taskA: %v", err)
	}
	if err := database.AddDependency(taskA.ID, taskB.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	// Without the flag, task A should still appear
	issues, err := database.ListIssues(ListIssuesOptions{
		Status: []models.Status{models.StatusOpen},
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	found := false
	for _, iss := range issues {
		if iss.ID == taskA.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("task A should be included when ExcludeHasOpenDeps is false")
	}
}
