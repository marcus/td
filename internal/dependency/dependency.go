package dependency

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// WouldCreateCycle checks if adding a dependency from issueID to dependsOnID would create a cycle.
// Returns true if adding the dependency would create a circular dependency.
func WouldCreateCycle(database *db.DB, issueID, dependsOnID string) bool {
	visited := make(map[string]bool)
	return hasCyclePath(database, dependsOnID, issueID, visited)
}

// hasCyclePath checks if there's a path from 'from' to 'to' through the dependency graph.
func hasCyclePath(database *db.DB, from, to string, visited map[string]bool) bool {
	if from == to {
		return true
	}
	if visited[from] {
		return false
	}
	visited[from] = true

	deps, _ := database.GetDependencies(from)
	for _, dep := range deps {
		if hasCyclePath(database, dep, to, visited) {
			return true
		}
	}
	return false
}

// Validate checks that a dependency can be added (both issues exist, no cycles, not duplicate).
// Returns nil if valid, or an error describing what went wrong.
func Validate(database *db.DB, issueID, dependsOnID string) error {
	// Verify both issues exist
	_, err := database.GetIssue(issueID)
	if err != nil {
		return fmt.Errorf("issue not found: %s", issueID)
	}

	_, err = database.GetIssue(dependsOnID)
	if err != nil {
		return fmt.Errorf("issue not found: %s", dependsOnID)
	}

	// Check for circular dependency
	if WouldCreateCycle(database, issueID, dependsOnID) {
		return fmt.Errorf("cannot add dependency: would create circular dependency")
	}

	// Check if dependency already exists
	existingDeps, _ := database.GetDependencies(issueID)
	for _, d := range existingDeps {
		if d == dependsOnID {
			return ErrDependencyExists
		}
	}

	return nil
}

// ValidateAndAdd validates the dependency (no cycles, both issues exist, not duplicate) and adds it.
// Returns nil on success, or an error describing what went wrong.
func ValidateAndAdd(database *db.DB, issueID, dependsOnID string) error {
	if err := Validate(database, issueID, dependsOnID); err != nil {
		return err
	}

	// Add the dependency
	if err := database.AddDependency(issueID, dependsOnID, "depends_on"); err != nil {
		return fmt.Errorf("failed to add dependency: %w", err)
	}

	return nil
}

// ErrDependencyExists is returned when trying to add a dependency that already exists.
var ErrDependencyExists = fmt.Errorf("dependency already exists")

// Remove removes a dependency between two issues.
func Remove(database *db.DB, issueID, dependsOnID string) error {
	return database.RemoveDependency(issueID, dependsOnID)
}

// GetDependencies returns issues that issueID depends on.
func GetDependencies(database *db.DB, issueID string) ([]models.Issue, error) {
	depIDs, err := database.GetDependencies(issueID)
	if err != nil {
		return nil, err
	}

	var deps []models.Issue
	for _, depID := range depIDs {
		issue, err := database.GetIssue(depID)
		if err != nil {
			// Skip issues that no longer exist
			continue
		}
		deps = append(deps, *issue)
	}

	return deps, nil
}

// GetDependents returns issues that depend on issueID.
func GetDependents(database *db.DB, issueID string) ([]models.Issue, error) {
	blockedIDs, err := database.GetBlockedBy(issueID)
	if err != nil {
		return nil, err
	}

	var dependents []models.Issue
	for _, id := range blockedIDs {
		issue, err := database.GetIssue(id)
		if err != nil {
			// Skip issues that no longer exist
			continue
		}
		dependents = append(dependents, *issue)
	}

	return dependents, nil
}

// GetTransitiveBlocked returns all unique issues transitively blocked by issueID.
func GetTransitiveBlocked(database *db.DB, issueID string, visited map[string]bool) []string {
	return getTransitiveBlockedFiltered(database, issueID, visited, false)
}

// GetTransitiveBlockedOpen returns all unique open/non-closed issues transitively blocked by issueID.
func GetTransitiveBlockedOpen(database *db.DB, issueID string, visited map[string]bool) []string {
	return getTransitiveBlockedFiltered(database, issueID, visited, true)
}

func getTransitiveBlockedFiltered(database *db.DB, issueID string, visited map[string]bool, excludeClosed bool) []string {
	if visited[issueID] {
		return nil
	}
	visited[issueID] = true

	blocked, _ := database.GetBlockedBy(issueID)
	var all []string

	for _, id := range blocked {
		if visited[id] {
			continue
		}
		if excludeClosed {
			issue, err := database.GetIssue(id)
			if err != nil || issue.Status == models.StatusClosed {
				visited[id] = true
				continue
			}
		}
		all = append(all, id)
		all = append(all, getTransitiveBlockedFiltered(database, id, visited, excludeClosed)...)
	}

	return all
}
