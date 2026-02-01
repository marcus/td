package query

import (
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func TestNewEvalContext(t *testing.T) {
	ctx := NewEvalContext("ses_123")

	if ctx.CurrentSession != "ses_123" {
		t.Errorf("CurrentSession = %q, want ses_123", ctx.CurrentSession)
	}
	if ctx.Now.IsZero() {
		t.Error("Now should not be zero")
	}
}

func TestToMatcher(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		issue   models.Issue
		matches bool
	}{
		{
			name:  "status equals open",
			query: "status = open",
			issue: models.Issue{
				ID:     "td-001",
				Status: models.StatusOpen,
			},
			matches: true,
		},
		{
			name:  "status not matches",
			query: "status = open",
			issue: models.Issue{
				ID:     "td-002",
				Status: models.StatusClosed,
			},
			matches: false,
		},
		{
			name:  "title contains",
			query: `title ~ "auth"`,
			issue: models.Issue{
				ID:    "td-003",
				Title: "Fix authentication bug",
			},
			matches: true,
		},
		{
			name:  "title not contains",
			query: `title ~ "auth"`,
			issue: models.Issue{
				ID:    "td-004",
				Title: "Update readme",
			},
			matches: false,
		},
		{
			name:  "priority comparison",
			query: "priority = P1",
			issue: models.Issue{
				ID:       "td-005",
				Priority: models.PriorityP1,
			},
			matches: true,
		},
		{
			name:  "type equals",
			query: "type = bug",
			issue: models.Issue{
				ID:   "td-006",
				Type: models.TypeBug,
			},
			matches: true,
		},
		{
			name:  "AND expression",
			query: "status = open AND priority = P1",
			issue: models.Issue{
				ID:       "td-007",
				Status:   models.StatusOpen,
				Priority: models.PriorityP1,
			},
			matches: true,
		},
		{
			name:  "AND expression partial fail",
			query: "status = open AND priority = P1",
			issue: models.Issue{
				ID:       "td-008",
				Status:   models.StatusOpen,
				Priority: models.PriorityP2,
			},
			matches: false,
		},
		{
			name:  "OR expression",
			query: "status = open OR status = in_progress",
			issue: models.Issue{
				ID:     "td-009",
				Status: models.StatusInProgress,
			},
			matches: true,
		},
		{
			name:  "NOT expression",
			query: "NOT status = closed",
			issue: models.Issue{
				ID:     "td-010",
				Status: models.StatusOpen,
			},
			matches: true,
		},
		{
			name:  "is function",
			query: "is(open)",
			issue: models.Issue{
				ID:     "td-011",
				Status: models.StatusOpen,
			},
			matches: true,
		},
		{
			name:  "sprint equals",
			query: `sprint = "v1.0"`,
			issue: models.Issue{
				ID:     "td-013",
				Sprint: "v1.0",
			},
			matches: true,
		},
		{
			name:  "sprint not equals",
			query: `sprint = "v1.0"`,
			issue: models.Issue{
				ID:     "td-014",
				Sprint: "v2.0",
			},
			matches: false,
		},
		{
			name:  "sprint contains",
			query: `sprint ~ "v1"`,
			issue: models.Issue{
				ID:     "td-015",
				Sprint: "v1.0-beta",
			},
			matches: true,
		},
		{
			name:  "sprint empty check",
			query: `sprint = ""`,
			issue: models.Issue{
				ID:     "td-016",
				Sprint: "",
			},
			matches: true,
		},
		{
			name:  "empty query matches all",
			query: "",
			issue: models.Issue{
				ID: "td-012",
			},
			matches: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := Parse(tt.query)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			ctx := NewEvalContext("ses_test")
			eval := NewEvaluator(ctx, query)

			matcher, err := eval.ToMatcher()
			if err != nil {
				t.Fatalf("ToMatcher error: %v", err)
			}

			got := matcher(tt.issue)
			if got != tt.matches {
				t.Errorf("matcher(%v) = %v, want %v", tt.issue.ID, got, tt.matches)
			}
		})
	}
}

func TestCaseInsensitiveEnumMatching(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		issue   models.Issue
		matches bool
	}{
		{
			name:  "lowercase priority exact match",
			query: "priority = p1",
			issue: models.Issue{
				ID:       "td-ci-01",
				Priority: models.PriorityP1,
			},
			matches: true,
		},
		{
			name:  "lowercase priority no match",
			query: "priority = p0",
			issue: models.Issue{
				ID:       "td-ci-02",
				Priority: models.PriorityP2,
			},
			matches: false,
		},
		{
			name:  "lowercase priority ordinal lte matches",
			query: "priority <= p1",
			issue: models.Issue{
				ID:       "td-ci-03",
				Priority: models.PriorityP0,
			},
			matches: true,
		},
		{
			name:  "lowercase priority ordinal lte excludes",
			query: "priority <= p1",
			issue: models.Issue{
				ID:       "td-ci-04",
				Priority: models.PriorityP3,
			},
			matches: false,
		},
		{
			name:  "uppercase status matches",
			query: "status = OPEN",
			issue: models.Issue{
				ID:     "td-ci-05",
				Status: models.StatusOpen,
			},
			matches: true,
		},
		{
			name:  "is function mixed case",
			query: "is(Open)",
			issue: models.Issue{
				ID:     "td-ci-06",
				Status: models.StatusOpen,
			},
			matches: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := Parse(tt.query)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			// Validate normalizes enum values to canonical form
			if errs := query.Validate(); len(errs) > 0 {
				t.Fatalf("validation errors: %v", errs)
			}

			ctx := NewEvalContext("ses_test")
			eval := NewEvaluator(ctx, query)

			matcher, err := eval.ToMatcher()
			if err != nil {
				t.Fatalf("ToMatcher error: %v", err)
			}

			got := matcher(tt.issue)
			if got != tt.matches {
				t.Errorf("matcher(%v) = %v, want %v", tt.issue.ID, got, tt.matches)
			}
		})
	}
}

func TestHasCrossEntityConditions(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		hasCross bool
	}{
		{
			name:     "simple field",
			query:    "status = open",
			hasCross: false,
		},
		{
			name:     "log field",
			query:    `log.message ~ "fix"`,
			hasCross: true,
		},
		{
			name:     "comment field",
			query:    `comment.text ~ "review"`,
			hasCross: true,
		},
		{
			name:     "handoff field",
			query:    `handoff.done ~ "completed"`,
			hasCross: true,
		},
		{
			name:     "file field",
			query:    `file.path ~ "main.go"`,
			hasCross: true,
		},
		{
			name:     "blocks function",
			query:    "blocks(td-001)",
			hasCross: true,
		},
		{
			name:     "blocked_by function",
			query:    "blocked_by(td-002)",
			hasCross: true,
		},
		{
			name:     "mixed - has cross entity",
			query:    `status = open AND log.message ~ "fix"`,
			hasCross: true,
		},
		{
			name:     "empty query",
			query:    "",
			hasCross: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := Parse(tt.query)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			ctx := NewEvalContext("ses_test")
			eval := NewEvaluator(ctx, query)

			got := eval.HasCrossEntityConditions()
			if got != tt.hasCross {
				t.Errorf("HasCrossEntityConditions() = %v, want %v", got, tt.hasCross)
			}
		})
	}
}

func TestMatchValue(t *testing.T) {
	ctx := &EvalContext{
		CurrentSession: "ses_abc",
		Now:            time.Now(),
	}

	tests := []struct {
		name     string
		field    string
		operator string
		value    interface{}
		matches  bool
	}{
		{
			name:     "equals match",
			field:    "open",
			operator: OpEq,
			value:    "open",
			matches:  true,
		},
		{
			name:     "equals case insensitive",
			field:    "OPEN",
			operator: OpEq,
			value:    "open",
			matches:  true,
		},
		{
			name:     "not equals",
			field:    "open",
			operator: OpNeq,
			value:    "closed",
			matches:  true,
		},
		{
			name:     "contains",
			field:    "this is a test message",
			operator: OpContains,
			value:    "test",
			matches:  true,
		},
		{
			name:     "contains case insensitive",
			field:    "This is a TEST message",
			operator: OpContains,
			value:    "test",
			matches:  true,
		},
		{
			name:     "not contains",
			field:    "hello world",
			operator: OpNotContains,
			value:    "foo",
			matches:  true,
		},
		{
			name:     "special value @me",
			field:    "ses_abc",
			operator: OpEq,
			value:    &SpecialValue{Type: "me"},
			matches:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchValue(tt.field, tt.operator, tt.value, ctx)
			if got != tt.matches {
				t.Errorf("matchValue(%q, %q, %v) = %v, want %v", tt.field, tt.operator, tt.value, got, tt.matches)
			}
		})
	}
}

func TestMatchLogs(t *testing.T) {
	ctx := &EvalContext{CurrentSession: "ses_test"}
	logs := []models.Log{
		{Message: "Fixed the auth bug", Type: models.LogTypeProgress, SessionID: "ses_001"},
		{Message: "Added tests", Type: models.LogTypeDecision, SessionID: "ses_002"},
	}

	tests := []struct {
		name    string
		filter  crossEntityFilter
		matches bool
	}{
		{
			name: "match message contains",
			filter: crossEntityFilter{
				entity:   "log",
				field:    "message",
				operator: OpContains,
				value:    "auth",
			},
			matches: true,
		},
		{
			name: "no match message",
			filter: crossEntityFilter{
				entity:   "log",
				field:    "message",
				operator: OpContains,
				value:    "deploy",
			},
			matches: false,
		},
		{
			name: "match session",
			filter: crossEntityFilter{
				entity:   "log",
				field:    "session",
				operator: OpEq,
				value:    "ses_001",
			},
			matches: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchLogs(logs, tt.filter, ctx)
			if got != tt.matches {
				t.Errorf("matchLogs() = %v, want %v", got, tt.matches)
			}
		})
	}
}

func TestMatchHandoff(t *testing.T) {
	ctx := &EvalContext{CurrentSession: "ses_test"}
	handoff := &models.Handoff{
		Done:      []string{"implemented feature", "added tests"},
		Remaining: []string{"deploy to prod", "update docs"},
		Decisions: []string{"use JWT for auth"},
		Uncertain: []string{"performance impact"},
	}

	tests := []struct {
		name    string
		filter  crossEntityFilter
		matches bool
	}{
		{
			name: "match done",
			filter: crossEntityFilter{
				entity:   "handoff",
				field:    "done",
				operator: OpContains,
				value:    "feature",
			},
			matches: true,
		},
		{
			name: "match remaining",
			filter: crossEntityFilter{
				entity:   "handoff",
				field:    "remaining",
				operator: OpContains,
				value:    "deploy",
			},
			matches: true,
		},
		{
			name: "match decisions",
			filter: crossEntityFilter{
				entity:   "handoff",
				field:    "decisions",
				operator: OpContains,
				value:    "JWT",
			},
			matches: true,
		},
		{
			name: "no match",
			filter: crossEntityFilter{
				entity:   "handoff",
				field:    "done",
				operator: OpContains,
				value:    "nothing here",
			},
			matches: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchHandoff(handoff, tt.filter, ctx)
			if got != tt.matches {
				t.Errorf("matchHandoff() = %v, want %v", got, tt.matches)
			}
		})
	}
}

func TestMatchFiles(t *testing.T) {
	ctx := &EvalContext{CurrentSession: "ses_test"}
	files := []models.IssueFile{
		{FilePath: "cmd/main.go", Role: models.FileRoleImplementation},
		{FilePath: "pkg/auth/handler.go", Role: models.FileRoleReference},
	}

	tests := []struct {
		name    string
		filter  crossEntityFilter
		matches bool
	}{
		{
			name: "match path",
			filter: crossEntityFilter{
				entity:   "file",
				field:    "path",
				operator: OpContains,
				value:    "main.go",
			},
			matches: true,
		},
		{
			name: "match role",
			filter: crossEntityFilter{
				entity:   "file",
				field:    "role",
				operator: OpEq,
				value:    "implementation",
			},
			matches: true,
		},
		{
			name: "no match path",
			filter: crossEntityFilter{
				entity:   "file",
				field:    "path",
				operator: OpContains,
				value:    "test.go",
			},
			matches: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchFiles(files, tt.filter, ctx)
			if got != tt.matches {
				t.Errorf("matchFiles() = %v, want %v", got, tt.matches)
			}
		})
	}
}

func TestDescendantOfIsCrossEntity(t *testing.T) {
	query, err := Parse("descendant_of(td-epic)")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	ctx := NewEvalContext("ses_test")
	eval := NewEvaluator(ctx, query)

	if !eval.HasCrossEntityConditions() {
		t.Error("descendant_of should be a cross-entity condition")
	}
}

func TestExecuteConstants(t *testing.T) {
	// Verify memory limit constants are reasonable
	if DefaultMaxResults <= 0 {
		t.Error("DefaultMaxResults must be positive")
	}
	if DefaultMaxResults < 1000 {
		t.Errorf("DefaultMaxResults=%d is too small for practical use", DefaultMaxResults)
	}

	if MaxDescendantDepth <= 0 {
		t.Error("MaxDescendantDepth must be positive")
	}
	if MaxDescendantDepth < 10 {
		t.Errorf("MaxDescendantDepth=%d is too small for practical use", MaxDescendantDepth)
	}
}

func TestExtractCrossEntityConditions(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		expectCount int
	}{
		{
			name:        "no cross entity",
			query:       "status = open",
			expectCount: 0,
		},
		{
			name:        "single log field",
			query:       `log.message ~ "fix"`,
			expectCount: 1,
		},
		{
			name:        "multiple cross entity",
			query:       `log.message ~ "fix" AND comment.text ~ "review"`,
			expectCount: 2,
		},
		{
			name:        "function call",
			query:       "blocks(td-001)",
			expectCount: 1,
		},
		{
			name:        "descendant_of function",
			query:       "descendant_of(td-epic)",
			expectCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := Parse(tt.query)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			if query.Root == nil {
				if tt.expectCount != 0 {
					t.Errorf("expected %d filters, got 0 (nil root)", tt.expectCount)
				}
				return
			}

			count := countCrossEntityConditions(query.Root)
			if count != tt.expectCount {
				t.Errorf("countCrossEntityConditions() returned %d, want %d", count, tt.expectCount)
			}
		})
	}
}

func TestFunctionArgEnumNormalization(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		issue   models.Issue
		matches bool
	}{
		{
			name:  "any() with mixed-case status values",
			query: "any(status, OPEN, Closed)",
			issue: models.Issue{
				ID:     "td-fn-01",
				Status: models.StatusOpen,
			},
			matches: true,
		},
		{
			name:  "any() with lowercase priority values",
			query: "any(priority, p0, p1)",
			issue: models.Issue{
				ID:       "td-fn-02",
				Priority: models.PriorityP1,
			},
			matches: true,
		},
		{
			name:  "any() with lowercase priority no match",
			query: "any(priority, p0, p1)",
			issue: models.Issue{
				ID:       "td-fn-03",
				Priority: models.PriorityP3,
			},
			matches: false,
		},
		{
			name:  "none() with mixed-case status",
			query: "none(status, CLOSED, blocked)",
			issue: models.Issue{
				ID:     "td-fn-04",
				Status: models.StatusOpen,
			},
			matches: true,
		},
		{
			name:  "none() excludes matching mixed-case",
			query: "none(status, OPEN, blocked)",
			issue: models.Issue{
				ID:     "td-fn-05",
				Status: models.StatusOpen,
			},
			matches: false,
		},
		{
			name:  "is() with mixed case",
			query: "is(IN_PROGRESS)",
			issue: models.Issue{
				ID:     "td-fn-06",
				Status: models.StatusInProgress,
			},
			matches: true,
		},
		{
			name:  "all() with mixed-case labels matches",
			query: "all(status, OPEN)",
			issue: models.Issue{
				ID:     "td-fn-07",
				Status: models.StatusOpen,
			},
			matches: true,
		},
		{
			name:  "all() with mixed-case no match",
			query: "all(status, CLOSED)",
			issue: models.Issue{
				ID:     "td-fn-08",
				Status: models.StatusOpen,
			},
			matches: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := Parse(tt.query)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if errs := query.Validate(); len(errs) > 0 {
				t.Fatalf("validation errors: %v", errs)
			}

			ctx := NewEvalContext("ses_test")
			eval := NewEvaluator(ctx, query)

			matcher, err := eval.ToMatcher()
			if err != nil {
				t.Fatalf("ToMatcher error: %v", err)
			}

			got := matcher(tt.issue)
			if got != tt.matches {
				t.Errorf("matcher(%v) = %v, want %v", tt.issue.ID, got, tt.matches)
			}
		})
	}
}

func TestIsPriorityCaseInsensitive(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"P0", true},
		{"P4", true},
		{"p0", true},
		{"p3", true},
		{"P5", false},
		{"p5", false},
		{"X1", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isPriority(tt.input); got != tt.expected {
				t.Errorf("isPriority(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestComparePriorityCaseInsensitive(t *testing.T) {
	tests := []struct {
		name     string
		a, b, op string
		expected bool
	}{
		{"uppercase lt", "P0", "P2", OpLt, true},
		{"lowercase lt", "p0", "p2", OpLt, true},
		{"mixed case lt", "p0", "P2", OpLt, true},
		{"lowercase gte", "p2", "p1", OpGte, true},
		{"lowercase eq via lte+gte", "p1", "p1", OpLte, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := comparePriority(tt.a, tt.b, tt.op); got != tt.expected {
				t.Errorf("comparePriority(%q, %q, %q) = %v, want %v", tt.a, tt.b, tt.op, got, tt.expected)
			}
		})
	}
}
