package query

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// Regression coverage for td-951238: date comparisons silently returned wrong
// results. `created < <date>` matched nothing and `created > <date>` matched
// everything, because the comparison fell through to a numeric path that turned
// the timestamp into Unix seconds and the date literal into 0.

// dateIssues builds one issue per day from 2026-05-01 to 2026-05-20 at noon
// local time, so counts either side of a boundary are known exactly.
func dateIssues(t *testing.T) []models.Issue {
	t.Helper()
	issues := make([]models.Issue, 0, 20)
	for day := 1; day <= 20; day++ {
		ts := time.Date(2026, 5, day, 12, 0, 0, 0, time.Local)
		issues = append(issues, models.Issue{
			ID:        fmt.Sprintf("td-%03d", day),
			Title:     fmt.Sprintf("day %d", day),
			Status:    models.StatusOpen,
			Type:      models.TypeTask,
			Priority:  models.PriorityP2,
			CreatedAt: ts,
			UpdatedAt: ts,
		})
	}
	return issues
}

// dateSource serves a fixed issue set to the query engine.
type dateSource struct {
	issues []models.Issue
}

func (s *dateSource) ListIssues(opts db.ListIssuesOptions) ([]models.Issue, error) {
	out := append([]models.Issue(nil), s.issues...)
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

func (s *dateSource) GetIssue(id string) (*models.Issue, error) {
	for i := range s.issues {
		if s.issues[i].ID == id {
			return &s.issues[i], nil
		}
	}
	return nil, fmt.Errorf("issue %s not found", id)
}

func (s *dateSource) GetLogs(string, int) ([]models.Log, error) { return nil, nil }

func (s *dateSource) GetComments(string) ([]models.Comment, error) { return nil, nil }

func (s *dateSource) GetLatestHandoff(string) (*models.Handoff, error) { return nil, nil }

func (s *dateSource) GetLinkedFiles(string) ([]models.IssueFile, error) { return nil, nil }

func (s *dateSource) GetDependencies(string) ([]string, error) { return nil, nil }

func (s *dateSource) GetRejectedInProgressIssueIDs() (map[string]bool, error) { return nil, nil }

func (s *dateSource) GetIssuesWithOpenDeps() (map[string]bool, error) { return nil, nil }

// TestDateComparisonCounts pins the counts on both sides of a boundary.
// 2026-05-09 has exactly one issue on it, so `<` and `<=` (and `>` and `>=`)
// must differ by that one issue, and each pair must partition the whole set.
func TestDateComparisonCounts(t *testing.T) {
	src := &dateSource{issues: dateIssues(t)}

	tests := []struct {
		query string
		want  int
	}{
		{"created < 2026-05-09", 8},   // days 1-8
		{"created <= 2026-05-09", 9},  // days 1-9, the 9th included
		{"created > 2026-05-09", 11},  // days 10-20, the 9th excluded
		{"created >= 2026-05-09", 12}, // days 9-20
		{"created = 2026-05-09", 1},
		{"created != 2026-05-09", 19},
		{"updated < 2026-05-09", 8},
		{"created >= 2026-05-01", 20},
		{"created < 2026-05-01", 0},
		{"created > 2026-05-20", 0},
		{"created >= 2026-05-15 AND created <= 2026-05-17", 3},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got, err := Execute(src, tt.query, "", ExecuteOptions{})
			if err != nil {
				t.Fatalf("Execute(%q) error = %v", tt.query, err)
			}
			if len(got) != tt.want {
				t.Errorf("Execute(%q) = %d issues, want %d", tt.query, len(got), tt.want)
			}
		})
	}
}

// TestDateComparisonPartitions is the property the bug violated: complementary
// predicates must cover the set exactly once, never zero times or twice.
func TestDateComparisonPartitions(t *testing.T) {
	src := &dateSource{issues: dateIssues(t)}
	const total = 20

	pairs := [][2]string{
		{"created < 2026-05-09", "created >= 2026-05-09"},
		{"created <= 2026-05-09", "created > 2026-05-09"},
		{"created < 2026-05-01", "created >= 2026-05-01"},
	}

	for _, pair := range pairs {
		lo, err := Execute(src, pair[0], "", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute(%q) error = %v", pair[0], err)
		}
		hi, err := Execute(src, pair[1], "", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute(%q) error = %v", pair[1], err)
		}
		if len(lo)+len(hi) != total {
			t.Errorf("%q (%d) + %q (%d) = %d, want %d",
				pair[0], len(lo), pair[1], len(hi), len(lo)+len(hi), total)
		}
	}
}

// TestDateComparisonGranularity pins the semantics directly: a bare date names a
// calendar day, an hour offset names an instant.
func TestDateComparisonGranularity(t *testing.T) {
	now := time.Date(2026, 5, 9, 18, 0, 0, 0, time.Local)
	noon := time.Date(2026, 5, 9, 12, 0, 0, 0, time.Local)
	issue := models.Issue{ID: "td-001", CreatedAt: noon, UpdatedAt: noon}

	tests := []struct {
		query string
		want  bool
	}{
		// Day granularity: the whole of the 9th is "equal to" 2026-05-09.
		{"created = 2026-05-09", true},
		{"created <= 2026-05-09", true},
		{"created >= 2026-05-09", true},
		{"created < 2026-05-09", false},
		{"created > 2026-05-09", false},
		// Instant granularity: noon is more than 3h before 18:00, less than 12h.
		{"created < -3h", true},
		{"created > -3h", false},
		{"created > -12h", true},
		// Relative day keywords resolve against Now.
		{"created = today", true},
		{"created = yesterday", false},
		{"created >= -7d", true},
		{"created < -7d", false},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			q, err := Parse(tt.query)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.query, err)
			}
			if errs := q.Validate(); len(errs) > 0 {
				t.Fatalf("Validate(%q) error = %v", tt.query, errs[0])
			}
			ev := NewEvaluator(&EvalContext{Now: now}, q)
			matcher, err := ev.ToMatcher()
			if err != nil {
				t.Fatalf("ToMatcher(%q) error = %v", tt.query, err)
			}
			if got := matcher(issue); got != tt.want {
				t.Errorf("%q matched %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

// TestDateComparisonNullClosed pins NULL semantics: an issue that was never
// closed matches no ordering comparison on closed, in either direction.
func TestDateComparisonNullClosed(t *testing.T) {
	closedAt := time.Date(2026, 5, 10, 9, 0, 0, 0, time.Local)
	issues := []models.Issue{
		{ID: "td-001", Title: "open", Status: models.StatusOpen,
			CreatedAt: time.Date(2026, 5, 1, 9, 0, 0, 0, time.Local)},
		{ID: "td-002", Title: "closed", Status: models.StatusClosed,
			CreatedAt: time.Date(2026, 5, 1, 9, 0, 0, 0, time.Local),
			ClosedAt:  &closedAt},
	}
	src := &dateSource{issues: issues}

	tests := []struct {
		query string
		want  int
	}{
		{"closed < 2026-05-20", 1},
		{"closed > 2026-05-01", 1},
		{"closed >= 2026-05-20", 0},
		{"closed <= 2026-05-01", 0},
		{"closed = 2026-05-10", 1},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got, err := Execute(src, tt.query, "", ExecuteOptions{})
			if err != nil {
				t.Fatalf("Execute(%q) error = %v", tt.query, err)
			}
			if len(got) != tt.want {
				t.Errorf("Execute(%q) = %d issues, want %d", tt.query, len(got), tt.want)
			}
		})
	}
}

// TestDateComparisonErrors: a predicate that cannot be evaluated must say so
// rather than return an empty result set that reads as "no matches".
func TestDateComparisonErrors(t *testing.T) {
	src := &dateSource{issues: dateIssues(t)}

	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{"non-date value", "created < banana", "date field"},
		{"quoted non-date", `created < "not a date"`, "date field"},
		{"number value", "created > 5", "date field"},
		{"contains on date", "created ~ 2026", "not supported"},
		{"malformed date", "created < 2026-13-45", "date field"},
		{"ordering on cross-entity", "log.timestamp < 2026-05-09", "not supported"},
		{"ordering on handoff", "handoff.timestamp >= today", "not supported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Execute(src, tt.query, "", ExecuteOptions{})
			if err == nil {
				t.Fatalf("Execute(%q) returned no error; an unusable predicate must not look like an empty result", tt.query)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Execute(%q) error = %q, want it to mention %q", tt.query, err, tt.wantErr)
			}
		})
	}
}

// TestExecuteReportsTruncation: a limited result set must be distinguishable
// from a complete one.
func TestExecuteReportsTruncation(t *testing.T) {
	src := &dateSource{issues: dateIssues(t)}

	res, err := ExecuteDetailed(src, "created >= 2026-05-01", "", ExecuteOptions{Limit: 5})
	if err != nil {
		t.Fatalf("ExecuteDetailed() error = %v", err)
	}
	if len(res.Issues) != 5 {
		t.Errorf("returned %d issues, want 5", len(res.Issues))
	}
	if res.Matched != 20 {
		t.Errorf("Matched = %d, want 20", res.Matched)
	}
	if !res.Truncated {
		t.Error("Truncated = false, want true when the limit dropped matches")
	}

	res, err = ExecuteDetailed(src, "created >= 2026-05-01", "", ExecuteOptions{Limit: 0})
	if err != nil {
		t.Fatalf("ExecuteDetailed() error = %v", err)
	}
	if len(res.Issues) != 20 || res.Truncated {
		t.Errorf("limit 0 returned %d issues (truncated=%v), want 20 and false", len(res.Issues), res.Truncated)
	}

	// The pre-filter scan cap is reported too: it means matches were never seen.
	res, err = ExecuteDetailed(src, "created >= 2026-05-01", "", ExecuteOptions{MaxResults: 10})
	if err != nil {
		t.Fatalf("ExecuteDetailed() error = %v", err)
	}
	if !res.ScanLimited {
		t.Error("ScanLimited = false, want true when the scan cap was hit")
	}
}

// TestNoteDateComparison covers the note evaluator, which shares the same
// comparison helpers and was broken the same way.
func TestNoteDateComparison(t *testing.T) {
	notes := []models.Note{
		{ID: "n1", Title: "early", CreatedAt: time.Date(2026, 5, 1, 9, 0, 0, 0, time.Local)},
		{ID: "n2", Title: "late", CreatedAt: time.Date(2026, 5, 20, 9, 0, 0, 0, time.Local)},
	}

	tests := []struct {
		query string
		want  int
	}{
		{"created < 2026-05-09", 1},
		{"created > 2026-05-09", 1},
		{"created >= 2026-05-01", 2},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			q, err := Parse(tt.query)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.query, err)
			}
			matcher, err := noteNodeToMatcher(q.Root, &EvalContext{Now: time.Now()})
			if err != nil {
				t.Fatalf("noteNodeToMatcher(%q) error = %v", tt.query, err)
			}
			count := 0
			for _, n := range notes {
				if matcher(n) {
					count++
				}
			}
			if count != tt.want {
				t.Errorf("%q matched %d notes, want %d", tt.query, count, tt.want)
			}
		})
	}
}
