package db

import (
	"sort"
	"strings"

	"github.com/marcus/td/internal/models"
)

// SearchResult holds an issue with relevance scoring for ranked search
type SearchResult struct {
	Issue      models.Issue
	Score      int    // Higher = better match (0-100)
	MatchField string // Primary field that matched: 'id', 'title', 'description', 'labels'
}

// SearchIssues performs full-text search across issues
func (db *DB) SearchIssues(query string, opts ListIssuesOptions) ([]models.Issue, error) {
	opts.Search = query
	return db.ListIssues(opts)
}

// SearchIssuesRanked performs search with relevance scoring.
// Scope is the issue's own text plus the work logged against it: logs and
// handoff content. Matches found only in that activity rank below matches in
// the issue's title or description.
func (db *DB) SearchIssuesRanked(query string, opts ListIssuesOptions) ([]SearchResult, error) {
	opts.SearchActivity = true
	issues, err := db.SearchIssues(query, opts)
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)
	results := make([]SearchResult, 0, len(issues))

	for _, issue := range issues {
		score := 0
		matchField := ""

		idLower := strings.ToLower(issue.ID)
		titleLower := strings.ToLower(issue.Title)
		descLower := strings.ToLower(issue.Description)
		labelsLower := strings.ToLower(strings.Join(issue.Labels, ","))

		// Score by match quality (highest wins)
		if strings.EqualFold(issue.ID, query) {
			score = 100
			matchField = "id"
		} else if strings.Contains(idLower, queryLower) {
			score = 90
			matchField = "id"
		} else if strings.EqualFold(issue.Title, query) {
			score = 80
			matchField = "title"
		} else if strings.HasPrefix(titleLower, queryLower) {
			score = 70
			matchField = "title"
		} else if strings.Contains(titleLower, queryLower) {
			score = 60
			matchField = "title"
		} else if strings.Contains(descLower, queryLower) {
			score = 40
			matchField = "description"
		} else if strings.Contains(labelsLower, queryLower) {
			score = 20
			matchField = "labels"
		} else {
			// The row came back, so the match is in the issue's activity.
			// Name which one so results say what was actually searched.
			score = 10
			matchField = db.activityMatchField(issue.ID, query)
		}

		results = append(results, SearchResult{
			Issue:      issue,
			Score:      score,
			MatchField: matchField,
		})
	}

	// Sort by score DESC, then by priority ASC
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Issue.Priority < results[j].Issue.Priority
	})

	return results, nil
}

// activityMatchField reports whether a match came from a log or a handoff.
// Returns "" when neither hit, which means the caller filtered the row in for
// some other reason.
func (db *DB) activityMatchField(issueID, query string) string {
	pattern := "%" + query + "%"

	var found int
	err := db.conn.QueryRow(
		`SELECT 1 FROM logs WHERE issue_id = ? AND message LIKE ? LIMIT 1`,
		issueID, pattern).Scan(&found)
	if err == nil {
		return "log"
	}

	err = db.conn.QueryRow(
		`SELECT 1 FROM handoffs WHERE issue_id = ? AND `+handoffSearchExpr+` LIMIT 1`,
		issueID, pattern, pattern, pattern, pattern).Scan(&found)
	if err == nil {
		return "handoff"
	}

	return ""
}
