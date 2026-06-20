package serverdb

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

var (
	nonAlphanumRe     = regexp.MustCompile(`[^a-z0-9]+`)
	leadingTrailingRe = regexp.MustCompile(`^-+|-+$`)
)

// slugify converts a free-form project name into a URL-safe kebab-case slug.
// This matches td-watch's algorithm exactly:
//   - Lowercase and trim.
//   - Collapse any run of characters that aren't ASCII alphanumeric to a single '-'.
//   - Strip leading/trailing hyphens.
//   - Return ” for empty or whitespace-only input.
func slugify(name string) string {
	trimmed := strings.TrimSpace(strings.ToLower(name))
	if trimmed == "" {
		return ""
	}
	s := nonAlphanumRe.ReplaceAllString(trimmed, "-")
	s = leadingTrailingRe.ReplaceAllString(s, "")
	return s
}

// uniqueSlug generates a slug from base that is unique in the projects table.
// excludeProjectID is the ID of the project being updated (so it can keep its
// own slug); pass "" when creating a new project.
// If base is empty, the caller should pass the project ID as base instead.
// Tries base, then base-2, base-3, ... until a free slot is found.
func uniqueSlug(db *sql.DB, base, excludeProjectID string) (string, error) {
	candidate := base
	for i := 2; ; i++ {
		var id string
		err := db.QueryRow(
			`SELECT id FROM projects WHERE slug = ? AND id != ?`,
			candidate, excludeProjectID,
		).Scan(&id)
		if err == sql.ErrNoRows {
			// candidate is free
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("check slug uniqueness: %w", err)
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

// uniqueSlugTx is the same as uniqueSlug but operates inside a transaction.
func uniqueSlugTx(tx *sql.Tx, base, excludeProjectID string) (string, error) {
	candidate := base
	for i := 2; ; i++ {
		var id string
		err := tx.QueryRow(
			`SELECT id FROM projects WHERE slug = ? AND id != ?`,
			candidate, excludeProjectID,
		).Scan(&id)
		if err == sql.ErrNoRows {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("check slug uniqueness: %w", err)
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

// slugBase returns the slug base for the given name and id.
// If slugify(name) is non-empty, returns that; otherwise returns id.
func slugBase(name, id string) string {
	s := slugify(name)
	if s == "" {
		return id
	}
	return s
}
