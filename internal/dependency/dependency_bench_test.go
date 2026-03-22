package dependency

import (
	"fmt"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func benchSetupDB(b *testing.B) (*db.DB, func()) {
	b.Helper()
	dir := b.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		b.Fatalf("Initialize failed: %v", err)
	}
	return database, func() { database.Close() }
}

func benchCreateIssue(b *testing.B, database *db.DB, title string) *models.Issue {
	b.Helper()
	issue := &models.Issue{Title: title, Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2}
	if err := database.CreateIssue(issue); err != nil {
		b.Fatalf("CreateIssue failed: %v", err)
	}
	return issue
}

func BenchmarkGetDependencies(b *testing.B) {
	database, cleanup := benchSetupDB(b)
	defer cleanup()

	issue := benchCreateIssue(b, database, "Main Issue")
	for i := 0; i < 20; i++ {
		dep := benchCreateIssue(b, database, fmt.Sprintf("Dep %d", i))
		if err := database.AddDependency(issue.ID, dep.ID, "depends_on"); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := GetDependencies(database, issue.ID)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetDependents(b *testing.B) {
	database, cleanup := benchSetupDB(b)
	defer cleanup()

	blocker := benchCreateIssue(b, database, "Blocker")
	for i := 0; i < 20; i++ {
		dep := benchCreateIssue(b, database, fmt.Sprintf("Dependent %d", i))
		if err := database.AddDependency(dep.ID, blocker.ID, "depends_on"); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := GetDependents(database, blocker.ID)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetTransitiveBlocked(b *testing.B) {
	database, cleanup := benchSetupDB(b)
	defer cleanup()

	// Chain: A -> B1..B5 -> C1..C5 (each Bi blocks all Cj)
	root := benchCreateIssue(b, database, "Root")
	var midIssues []*models.Issue
	for i := 0; i < 5; i++ {
		mid := benchCreateIssue(b, database, fmt.Sprintf("Mid %d", i))
		database.AddDependency(mid.ID, root.ID, "depends_on")
		midIssues = append(midIssues, mid)
	}
	for i := 0; i < 5; i++ {
		leaf := benchCreateIssue(b, database, fmt.Sprintf("Leaf %d", i))
		for _, mid := range midIssues {
			database.AddDependency(leaf.ID, mid.ID, "depends_on")
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetTransitiveBlocked(database, root.ID, make(map[string]bool))
	}
}
