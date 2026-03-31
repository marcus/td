package db

import (
	"fmt"
	"testing"

	"github.com/marcus/td/internal/models"
)

func benchSetup(b *testing.B) *DB {
	b.Helper()
	dir := b.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		b.Fatalf("Initialize failed: %v", err)
	}
	return db
}

func BenchmarkGetDescendantIssues(b *testing.B) {
	db := benchSetup(b)
	defer db.Close()

	// Create an epic with 50 children
	epic := &models.Issue{Title: "Epic", Type: models.TypeEpic}
	if err := db.CreateIssue(epic); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		child := &models.Issue{Title: fmt.Sprintf("Child %d", i), ParentID: epic.ID, Status: models.StatusOpen}
		if err := db.CreateIssue(child); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetDescendantIssues(epic.ID, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetDescendantIssues_FilteredStatus(b *testing.B) {
	db := benchSetup(b)
	defer db.Close()

	epic := &models.Issue{Title: "Epic", Type: models.TypeEpic}
	if err := db.CreateIssue(epic); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		status := models.StatusOpen
		if i%2 == 0 {
			status = models.StatusClosed
		}
		child := &models.Issue{Title: fmt.Sprintf("Child %d", i), ParentID: epic.ID, Status: status}
		if err := db.CreateIssue(child); err != nil {
			b.Fatal(err)
		}
		if err := db.UpdateIssue(child); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetDescendantIssues(epic.ID, []models.Status{models.StatusOpen})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCascadeUnblockDependents(b *testing.B) {
	db := benchSetup(b)
	defer db.Close()

	blocker := &models.Issue{Title: "Blocker", Status: models.StatusClosed}
	if err := db.CreateIssue(blocker); err != nil {
		b.Fatal(err)
	}

	// Create 20 blocked dependents
	for i := 0; i < 20; i++ {
		dep := &models.Issue{Title: fmt.Sprintf("Blocked %d", i), Status: models.StatusBlocked}
		if err := db.CreateIssue(dep); err != nil {
			b.Fatal(err)
		}
		if err := db.AddDependency(dep.ID, blocker.ID, "depends_on"); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Reset all dependents to blocked status for next iteration
		rows, _ := db.conn.Query(`SELECT id FROM issues WHERE status = 'open' AND title LIKE 'Blocked %'`)
		var ids []string
		for rows.Next() {
			var id string
			rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			db.conn.Exec(`UPDATE issues SET status = 'blocked' WHERE id = ?`, id)
		}
		b.StartTimer()

		db.CascadeUnblockDependents(blocker.ID, "bench-session")
	}
}
