package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
)

// EvalContext provides context for query evaluation
type EvalContext struct {
	CurrentSession string    // for @me resolution
	Now            time.Time // for relative date calculation
}

// NewEvalContext creates a new evaluation context
func NewEvalContext(sessionID string) *EvalContext {
	return &EvalContext{
		CurrentSession: sessionID,
		Now:            time.Now(),
	}
}

// escapeSQLWildcards escapes SQL LIKE pattern wildcards (% and _) in user input
// to prevent unintended pattern matching
func escapeSQLWildcards(s string) string {
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

// QueryResult contains the result of query evaluation
type QueryResult struct {
	Issues []models.Issue

	// For cross-entity queries, we may need to filter after fetching
	CrossEntityFilter func(issue models.Issue, logs []LogEntry, comments []CommentEntry, handoffs []HandoffEntry, files []FileEntry) bool
}

// LogEntry represents a log entry for cross-entity filtering
type LogEntry struct {
	Message   string
	Type      string
	Timestamp time.Time
	Session   string
}

// CommentEntry represents a comment for cross-entity filtering
type CommentEntry struct {
	Text    string
	Created time.Time
	Session string
}

// HandoffEntry represents a handoff for cross-entity filtering
type HandoffEntry struct {
	Done      string
	Remaining string
	Decisions string
	Uncertain string
	Timestamp time.Time
}

// FileEntry represents a linked file for cross-entity filtering
type FileEntry struct {
	Path string
	Role string
}

// SQLCondition represents a SQL WHERE clause fragment
type SQLCondition struct {
	Clause string
	Args   []interface{}
}

// Evaluator converts a Query AST to SQL conditions and in-memory filters
type Evaluator struct {
	ctx   *EvalContext
	query *Query
}

// NewEvaluator creates a new query evaluator
func NewEvaluator(ctx *EvalContext, query *Query) *Evaluator {
	return &Evaluator{ctx: ctx, query: query}
}

// ToSQLConditions converts the query to SQL WHERE clauses
// Returns conditions that can be pushed to the database
func (e *Evaluator) ToSQLConditions() ([]SQLCondition, error) {
	if e.query.Root == nil {
		return nil, nil
	}
	return e.nodeToSQL(e.query.Root)
}

// ToMatcher returns a function that matches issues in memory
// Used for complex conditions that can't be expressed in SQL
func (e *Evaluator) ToMatcher() (func(models.Issue) bool, error) {
	if e.query.Root == nil {
		return func(models.Issue) bool { return true }, nil
	}
	return e.nodeToMatcher(e.query.Root)
}

// HasCrossEntityConditions checks if the query has cross-entity conditions
func (e *Evaluator) HasCrossEntityConditions() bool {
	if e.query.Root == nil {
		return false
	}
	return e.hasCrossEntity(e.query.Root)
}

func (e *Evaluator) hasCrossEntity(n Node) bool {
	switch node := n.(type) {
	case *BinaryExpr:
		return e.hasCrossEntity(node.Left) || e.hasCrossEntity(node.Right)
	case *UnaryExpr:
		return e.hasCrossEntity(node.Expr)
	case *FieldExpr:
		parts := strings.Split(node.Field, ".")
		if len(parts) > 1 {
			prefix := parts[0]
			return prefix == "log" || prefix == "comment" || prefix == "handoff" || prefix == "file" || prefix == "dep"
		}
		return false
	case *FunctionCall:
		return node.Name == "blocks" || node.Name == "blocked_by" || node.Name == "linked_to" || node.Name == "descendant_of" || node.Name == "rework"
	default:
		return false
	}
}

func (e *Evaluator) nodeToSQL(n Node) ([]SQLCondition, error) {
	switch node := n.(type) {
	case *BinaryExpr:
		return e.binaryExprToSQL(node)
	case *UnaryExpr:
		return e.unaryExprToSQL(node)
	case *FieldExpr:
		return e.fieldExprToSQL(node)
	case *FunctionCall:
		return e.functionToSQL(node)
	case *TextSearch:
		return e.textSearchToSQL(node)
	default:
		return nil, fmt.Errorf("unsupported node type: %T", n)
	}
}

func (e *Evaluator) binaryExprToSQL(node *BinaryExpr) ([]SQLCondition, error) {
	leftConds, err := e.nodeToSQL(node.Left)
	if err != nil {
		return nil, err
	}
	rightConds, err := e.nodeToSQL(node.Right)
	if err != nil {
		return nil, err
	}

	if len(leftConds) == 0 && len(rightConds) == 0 {
		return nil, nil
	}
	if len(leftConds) == 0 {
		return rightConds, nil
	}
	if len(rightConds) == 0 {
		return leftConds, nil
	}

	// Combine with AND/OR
	leftClause := e.combineConditions(leftConds, "AND")
	rightClause := e.combineConditions(rightConds, "AND")

	combined := SQLCondition{
		Clause: fmt.Sprintf("(%s %s %s)", leftClause.Clause, node.Op, rightClause.Clause),
		Args:   append(leftClause.Args, rightClause.Args...),
	}
	return []SQLCondition{combined}, nil
}

func (e *Evaluator) unaryExprToSQL(node *UnaryExpr) ([]SQLCondition, error) {
	conds, err := e.nodeToSQL(node.Expr)
	if err != nil {
		return nil, err
	}
	if len(conds) == 0 {
		return nil, nil
	}

	combined := e.combineConditions(conds, "AND")
	return []SQLCondition{{
		Clause: fmt.Sprintf("NOT (%s)", combined.Clause),
		Args:   combined.Args,
	}}, nil
}

func (e *Evaluator) fieldExprToSQL(node *FieldExpr) ([]SQLCondition, error) {
	// Cross-entity fields can't be converted to SQL directly
	parts := strings.Split(node.Field, ".")
	if len(parts) > 1 {
		prefix := parts[0]
		if prefix == "log" || prefix == "comment" || prefix == "handoff" || prefix == "file" || prefix == "dep" {
			return nil, nil // Will be handled in-memory
		}
	}

	field := node.Field
	value := e.resolveValue(node.Value)

	// Map field names to database columns
	dbField := e.mapFieldToColumn(field)

	switch node.Operator {
	case OpEq:
		return e.eqCondition(dbField, value)
	case OpNeq:
		return []SQLCondition{{Clause: fmt.Sprintf("%s != ?", dbField), Args: []interface{}{value}}}, nil
	case OpLt:
		return []SQLCondition{{Clause: fmt.Sprintf("%s < ?", dbField), Args: []interface{}{value}}}, nil
	case OpGt:
		return []SQLCondition{{Clause: fmt.Sprintf("%s > ?", dbField), Args: []interface{}{value}}}, nil
	case OpLte:
		return []SQLCondition{{Clause: fmt.Sprintf("%s <= ?", dbField), Args: []interface{}{value}}}, nil
	case OpGte:
		return []SQLCondition{{Clause: fmt.Sprintf("%s >= ?", dbField), Args: []interface{}{value}}}, nil
	case OpContains:
		strVal := fmt.Sprintf("%v", value)
		escapedVal := escapeSQLWildcards(strVal)
		if dbField == "labels" {
			// Special handling for labels (comma-separated)
			// Use ESCAPE clause since we're escaping % and _ with backslash
			return []SQLCondition{{
				Clause: "(labels LIKE ? ESCAPE '\\' OR labels LIKE ? ESCAPE '\\' OR labels LIKE ? ESCAPE '\\' OR labels = ?)",
				Args:   []interface{}{escapedVal + ",%", "%," + escapedVal + ",%", "%," + escapedVal, strVal},
			}}, nil
		}
		return []SQLCondition{{Clause: fmt.Sprintf("%s LIKE ? ESCAPE '\\\\'", dbField), Args: []interface{}{"%" + escapedVal + "%"}}}, nil
	case OpNotContains:
		strVal := fmt.Sprintf("%v", value)
		escapedVal := escapeSQLWildcards(strVal)
		return []SQLCondition{{Clause: fmt.Sprintf("%s NOT LIKE ? ESCAPE '\\\\'", dbField), Args: []interface{}{"%" + escapedVal + "%"}}}, nil
	default:
		return nil, fmt.Errorf("unsupported operator: %s", node.Operator)
	}
}

func (e *Evaluator) eqCondition(field string, value interface{}) ([]SQLCondition, error) {
	// Handle special values
	if sv, ok := value.(*SpecialValue); ok {
		switch sv.Type {
		case "empty":
			return []SQLCondition{{Clause: fmt.Sprintf("(%s IS NULL OR %s = '')", field, field)}}, nil
		case "null":
			return []SQLCondition{{Clause: fmt.Sprintf("%s IS NULL", field)}}, nil
		}
	}
	return []SQLCondition{{Clause: fmt.Sprintf("%s = ?", field), Args: []interface{}{value}}}, nil
}

func (e *Evaluator) functionToSQL(node *FunctionCall) ([]SQLCondition, error) {
	switch node.Name {
	case "has":
		if len(node.Args) < 1 {
			return nil, fmt.Errorf("has() requires 1 argument")
		}
		field := e.mapFieldToColumn(fmt.Sprintf("%v", node.Args[0]))
		return []SQLCondition{{Clause: fmt.Sprintf("(%s IS NOT NULL AND %s != '')", field, field)}}, nil

	case "is":
		if len(node.Args) < 1 {
			return nil, fmt.Errorf("is() requires 1 argument")
		}
		status := fmt.Sprintf("%v", node.Args[0])
		return []SQLCondition{{Clause: "status = ?", Args: []interface{}{status}}}, nil

	case "any":
		if len(node.Args) < 2 {
			return nil, fmt.Errorf("any() requires at least 2 arguments")
		}
		field := e.mapFieldToColumn(fmt.Sprintf("%v", node.Args[0]))
		placeholders := make([]string, len(node.Args)-1)
		args := make([]interface{}, len(node.Args)-1)
		for i := 1; i < len(node.Args); i++ {
			placeholders[i-1] = "?"
			args[i-1] = e.resolveValue(node.Args[i])
		}
		return []SQLCondition{{
			Clause: fmt.Sprintf("%s IN (%s)", field, strings.Join(placeholders, ",")),
			Args:   args,
		}}, nil

	case "child_of":
		if len(node.Args) < 1 {
			return nil, fmt.Errorf("child_of() requires 1 argument")
		}
		parentID := fmt.Sprintf("%v", node.Args[0])
		return []SQLCondition{{Clause: "parent_id = ?", Args: []interface{}{parentID}}}, nil

	case "descendant_of":
		// This requires recursive query, return nil and handle in memory
		return nil, nil

	case "blocks", "blocked_by", "linked_to":
		// These require joins, handle in memory
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown function: %s", node.Name)
	}
}

func (e *Evaluator) textSearchToSQL(node *TextSearch) ([]SQLCondition, error) {
	pattern := "%" + node.Text + "%"
	return []SQLCondition{{
		Clause: "(id LIKE ? OR title LIKE ? OR description LIKE ?)",
		Args:   []interface{}{pattern, pattern, pattern},
	}}, nil
}

func (e *Evaluator) combineConditions(conds []SQLCondition, op string) SQLCondition {
	if len(conds) == 0 {
		return SQLCondition{Clause: "1=1"}
	}
	if len(conds) == 1 {
		return conds[0]
	}

	clauses := make([]string, len(conds))
	var allArgs []interface{}
	for i, c := range conds {
		clauses[i] = c.Clause
		allArgs = append(allArgs, c.Args...)
	}

	return SQLCondition{
		Clause: "(" + strings.Join(clauses, " "+op+" ") + ")",
		Args:   allArgs,
	}
}

func (e *Evaluator) mapFieldToColumn(field string) string {
	switch field {
	case "created":
		return "created_at"
	case "updated":
		return "updated_at"
	case "closed":
		return "closed_at"
	case "parent":
		return "parent_id"
	case "epic":
		return "parent_id"
	case "implementer":
		return "implementer_session"
	case "reviewer":
		return "reviewer_session"
	case "branch":
		return "created_branch"
	default:
		return field
	}
}

func (e *Evaluator) resolveValue(v interface{}) interface{} {
	switch val := v.(type) {
	case *SpecialValue:
		if val.Type == "me" {
			return e.ctx.CurrentSession
		}
		return val
	case *DateValue:
		return e.resolveDate(val)
	default:
		return v
	}
}

func (e *Evaluator) resolveDate(d *DateValue) interface{} {
	if !d.Relative {
		return d.Raw
	}

	now := e.ctx.Now

	switch d.Raw {
	case "today":
		return now.Format("2006-01-02")
	case "yesterday":
		return now.AddDate(0, 0, -1).Format("2006-01-02")
	case "this_week":
		// Start of current week (Monday)
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		return now.AddDate(0, 0, -(weekday - 1)).Format("2006-01-02")
	case "last_week":
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		return now.AddDate(0, 0, -(weekday-1)-7).Format("2006-01-02")
	case "this_month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	case "last_month":
		return time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	default:
		// Parse relative offset like -7d, +3w
		return e.parseRelativeOffset(d.Raw)
	}
}

func (e *Evaluator) parseRelativeOffset(s string) string {
	if len(s) < 2 {
		return s
	}

	sign := 1
	start := 0
	if s[0] == '-' {
		sign = -1
		start = 1
	} else if s[0] == '+' {
		start = 1
	}

	unit := s[len(s)-1]
	numStr := s[start : len(s)-1]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return s
	}

	num *= sign
	now := e.ctx.Now

	switch unit {
	case 'd':
		return now.AddDate(0, 0, num).Format("2006-01-02")
	case 'w':
		return now.AddDate(0, 0, num*7).Format("2006-01-02")
	case 'm':
		return now.AddDate(0, num, 0).Format("2006-01-02")
	case 'h':
		return now.Add(time.Duration(num) * time.Hour).Format("2006-01-02 15:04:05")
	default:
		return s
	}
}

// nodeToMatcher converts a node to an in-memory matcher function
func (e *Evaluator) nodeToMatcher(n Node) (func(models.Issue) bool, error) {
	switch node := n.(type) {
	case *BinaryExpr:
		leftMatcher, err := e.nodeToMatcher(node.Left)
		if err != nil {
			return nil, err
		}
		rightMatcher, err := e.nodeToMatcher(node.Right)
		if err != nil {
			return nil, err
		}
		if node.Op == OpAnd {
			return func(i models.Issue) bool {
				return leftMatcher(i) && rightMatcher(i)
			}, nil
		}
		return func(i models.Issue) bool {
			return leftMatcher(i) || rightMatcher(i)
		}, nil

	case *UnaryExpr:
		matcher, err := e.nodeToMatcher(node.Expr)
		if err != nil {
			return nil, err
		}
		return func(i models.Issue) bool {
			return !matcher(i)
		}, nil

	case *FieldExpr:
		return e.fieldExprToMatcher(node)

	case *FunctionCall:
		return e.functionToMatcher(node)

	case *TextSearch:
		pattern := strings.ToLower(node.Text)
		return func(i models.Issue) bool {
			return strings.Contains(strings.ToLower(i.ID), pattern) ||
				strings.Contains(strings.ToLower(i.Title), pattern) ||
				strings.Contains(strings.ToLower(i.Description), pattern)
		}, nil

	default:
		return nil, fmt.Errorf("unsupported node type for matcher: %T", n)
	}
}

func (e *Evaluator) fieldExprToMatcher(node *FieldExpr) (func(models.Issue) bool, error) {
	field := node.Field
	value := e.resolveValue(node.Value)

	// Get field value getter
	getter := e.getFieldGetter(field)
	if getter == nil {
		return func(models.Issue) bool { return true }, nil
	}

	switch node.Operator {
	case OpEq:
		return func(i models.Issue) bool {
			return e.compareEqual(getter(i), value)
		}, nil
	case OpNeq:
		return func(i models.Issue) bool {
			return !e.compareEqual(getter(i), value)
		}, nil
	case OpContains:
		pattern := strings.ToLower(fmt.Sprintf("%v", value))
		return func(i models.Issue) bool {
			fieldVal := strings.ToLower(fmt.Sprintf("%v", getter(i)))
			return strings.Contains(fieldVal, pattern)
		}, nil
	case OpNotContains:
		pattern := strings.ToLower(fmt.Sprintf("%v", value))
		return func(i models.Issue) bool {
			fieldVal := strings.ToLower(fmt.Sprintf("%v", getter(i)))
			return !strings.Contains(fieldVal, pattern)
		}, nil
	case OpLt, OpGt, OpLte, OpGte:
		return func(i models.Issue) bool {
			return e.compareOrder(getter(i), value, node.Operator)
		}, nil
	default:
		return func(models.Issue) bool { return true }, nil
	}
}

func (e *Evaluator) getFieldGetter(field string) func(models.Issue) interface{} {
	switch field {
	case "id":
		return func(i models.Issue) interface{} { return i.ID }
	case "title":
		return func(i models.Issue) interface{} { return i.Title }
	case "description":
		return func(i models.Issue) interface{} { return i.Description }
	case "status":
		return func(i models.Issue) interface{} { return string(i.Status) }
	case "type":
		return func(i models.Issue) interface{} { return string(i.Type) }
	case "priority":
		return func(i models.Issue) interface{} { return string(i.Priority) }
	case "points":
		return func(i models.Issue) interface{} { return i.Points }
	case "labels":
		return func(i models.Issue) interface{} { return strings.Join(i.Labels, ",") }
	case "parent", "parent_id":
		return func(i models.Issue) interface{} { return i.ParentID }
	case "implementer", "implementer_session":
		return func(i models.Issue) interface{} { return i.ImplementerSession }
	case "reviewer", "reviewer_session":
		return func(i models.Issue) interface{} { return i.ReviewerSession }
	case "branch", "created_branch":
		return func(i models.Issue) interface{} { return i.CreatedBranch }
	case "sprint":
		return func(i models.Issue) interface{} { return i.Sprint }
	case "minor":
		return func(i models.Issue) interface{} { return i.Minor }
	case "created", "created_at":
		return func(i models.Issue) interface{} { return i.CreatedAt }
	case "updated", "updated_at":
		return func(i models.Issue) interface{} { return i.UpdatedAt }
	case "closed", "closed_at":
		return func(i models.Issue) interface{} {
			if i.ClosedAt != nil {
				return *i.ClosedAt
			}
			return nil
		}
	default:
		return nil
	}
}

func (e *Evaluator) compareEqual(a, b interface{}) bool {
	if sv, ok := b.(*SpecialValue); ok {
		switch sv.Type {
		case "empty":
			return a == nil || a == "" || a == 0
		case "null":
			return a == nil
		}
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func (e *Evaluator) compareOrder(a, b interface{}, op string) bool {
	// Handle priority comparison specially
	if priorityA, okA := a.(string); okA {
		if priorityB, okB := b.(string); okB {
			if isPriority(priorityA) && isPriority(priorityB) {
				return comparePriority(priorityA, priorityB, op)
			}
		}
	}

	// Try numeric comparison
	numA := toNumber(a)
	numB := toNumber(b)

	switch op {
	case OpLt:
		return numA < numB
	case OpGt:
		return numA > numB
	case OpLte:
		return numA <= numB
	case OpGte:
		return numA >= numB
	default:
		return false
	}
}

func (e *Evaluator) functionToMatcher(node *FunctionCall) (func(models.Issue) bool, error) {
	switch node.Name {
	case "has":
		if len(node.Args) < 1 {
			return nil, fmt.Errorf("has() requires 1 argument")
		}
		field := fmt.Sprintf("%v", node.Args[0])
		getter := e.getFieldGetter(field)
		if getter == nil {
			return func(models.Issue) bool { return false }, nil
		}
		return func(i models.Issue) bool {
			v := getter(i)
			if v == nil {
				return false
			}
			if s, ok := v.(string); ok {
				return s != ""
			}
			return true
		}, nil

	case "is":
		if len(node.Args) < 1 {
			return nil, fmt.Errorf("is() requires 1 argument")
		}
		status := fmt.Sprintf("%v", node.Args[0])
		return func(i models.Issue) bool {
			return strings.EqualFold(string(i.Status), status)
		}, nil

	case "any":
		if len(node.Args) < 2 {
			return nil, fmt.Errorf("any() requires at least 2 arguments")
		}
		field := fmt.Sprintf("%v", node.Args[0])
		getter := e.getFieldGetter(field)
		if getter == nil {
			return func(models.Issue) bool { return false }, nil
		}
		values := make([]string, len(node.Args)-1)
		for i := 1; i < len(node.Args); i++ {
			values[i-1] = strings.ToLower(fmt.Sprintf("%v", e.resolveValue(node.Args[i])))
		}
		return func(i models.Issue) bool {
			fieldVal := strings.ToLower(fmt.Sprintf("%v", getter(i)))
			for _, v := range values {
				if fieldVal == v {
					return true
				}
			}
			return false
		}, nil

	case "all":
		if len(node.Args) < 2 {
			return nil, fmt.Errorf("all() requires at least 2 arguments")
		}
		field := fmt.Sprintf("%v", node.Args[0])
		getter := e.getFieldGetter(field)
		if getter == nil {
			return func(models.Issue) bool { return false }, nil
		}
		values := make([]string, len(node.Args)-1)
		for i := 1; i < len(node.Args); i++ {
			values[i-1] = strings.ToLower(fmt.Sprintf("%v", e.resolveValue(node.Args[i])))
		}
		return func(i models.Issue) bool {
			fieldVal := strings.ToLower(fmt.Sprintf("%v", getter(i)))
			for _, v := range values {
				if !strings.Contains(fieldVal, v) {
					return false
				}
			}
			return true
		}, nil

	case "none":
		if len(node.Args) < 2 {
			return nil, fmt.Errorf("none() requires at least 2 arguments")
		}
		field := fmt.Sprintf("%v", node.Args[0])
		getter := e.getFieldGetter(field)
		if getter == nil {
			return func(models.Issue) bool { return true }, nil
		}
		values := make([]string, len(node.Args)-1)
		for i := 1; i < len(node.Args); i++ {
			values[i-1] = strings.ToLower(fmt.Sprintf("%v", e.resolveValue(node.Args[i])))
		}
		return func(i models.Issue) bool {
			fieldVal := strings.ToLower(fmt.Sprintf("%v", getter(i)))
			for _, v := range values {
				if fieldVal == v || strings.Contains(fieldVal, v) {
					return false
				}
			}
			return true
		}, nil

	case "child_of":
		if len(node.Args) < 1 {
			return nil, fmt.Errorf("child_of() requires 1 argument")
		}
		parentID := fmt.Sprintf("%v", node.Args[0])
		return func(i models.Issue) bool {
			return i.ParentID == parentID
		}, nil

	case "descendant_of":
		// This requires recursive parent traversal, handled via cross-entity filter
		// Return placeholder that allows issue through (will be filtered in Execute)
		return func(models.Issue) bool { return true }, nil

	case "blocks", "blocked_by", "linked_to", "rework":
		// These require database lookups, handled via cross-entity filter
		return func(models.Issue) bool { return true }, nil

	default:
		return nil, fmt.Errorf("unknown function: %s", node.Name)
	}
}

// Helper functions

func isPriority(s string) bool {
	return regexp.MustCompile(`^P[0-4]$`).MatchString(s)
}

func comparePriority(a, b, op string) bool {
	// P0 is highest priority (lowest number)
	priorityOrder := map[string]int{"P0": 0, "P1": 1, "P2": 2, "P3": 3, "P4": 4}
	orderA := priorityOrder[a]
	orderB := priorityOrder[b]

	switch op {
	case OpLt:
		return orderA < orderB
	case OpGt:
		return orderA > orderB
	case OpLte:
		return orderA <= orderB
	case OpGte:
		return orderA >= orderB
	default:
		return false
	}
}

func toNumber(v interface{}) float64 {
	switch val := v.(type) {
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case float64:
		return val
	case string:
		if n, err := strconv.ParseFloat(val, 64); err == nil {
			return n
		}
	case time.Time:
		return float64(val.Unix())
	}
	return 0
}
