package query

import (
	"fmt"
	"strings"
)

// Node is the interface for all AST nodes
type Node interface {
	String() string
	nodeType() string
}

// BinaryExpr represents a binary expression (AND, OR)
type BinaryExpr struct {
	Op    string // "AND" or "OR"
	Left  Node
	Right Node
}

func (b *BinaryExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", b.Left.String(), b.Op, b.Right.String())
}

func (b *BinaryExpr) nodeType() string { return "BinaryExpr" }

// UnaryExpr represents a unary expression (NOT)
type UnaryExpr struct {
	Op   string // "NOT"
	Expr Node
}

func (u *UnaryExpr) String() string {
	return fmt.Sprintf("(%s %s)", u.Op, u.Expr.String())
}

func (u *UnaryExpr) nodeType() string { return "UnaryExpr" }

// FieldExpr represents a field comparison (field op value)
type FieldExpr struct {
	Field    string      // e.g., "status", "log.message"
	Operator string      // =, !=, ~, !~, <, >, <=, >=
	Value    interface{} // string, int, date value, or special value
}

func (f *FieldExpr) String() string {
	return fmt.Sprintf("%s %s %v", f.Field, f.Operator, f.Value)
}

func (f *FieldExpr) nodeType() string { return "FieldExpr" }

// FunctionCall represents a function call like has(labels), is(open)
type FunctionCall struct {
	Name string
	Args []interface{}
}

func (fn *FunctionCall) String() string {
	args := make([]string, len(fn.Args))
	for i, arg := range fn.Args {
		args[i] = fmt.Sprintf("%v", arg)
	}
	return fmt.Sprintf("%s(%s)", fn.Name, strings.Join(args, ", "))
}

func (fn *FunctionCall) nodeType() string { return "FunctionCall" }

// TextSearch represents a bare text search (searches title, description, id)
type TextSearch struct {
	Text string
}

func (t *TextSearch) String() string {
	return fmt.Sprintf(`"%s"`, t.Text)
}

func (t *TextSearch) nodeType() string { return "TextSearch" }

// DateValue represents a date (absolute or relative)
type DateValue struct {
	Raw      string // original value: "2024-01-15", "-7d", "today", etc.
	Relative bool   // true if relative date
}

func (d *DateValue) String() string {
	return d.Raw
}

// SpecialValue represents special values like @me, EMPTY, NULL
type SpecialValue struct {
	Type string // "me", "empty", "null"
}

func (s *SpecialValue) String() string {
	switch s.Type {
	case "me":
		return "@me"
	case "empty":
		return "EMPTY"
	case "null":
		return "NULL"
	default:
		return s.Type
	}
}

// ListValue represents a list of values for IN operator
type ListValue struct {
	Values []interface{}
}

func (l *ListValue) String() string {
	vals := make([]string, len(l.Values))
	for i, v := range l.Values {
		vals[i] = fmt.Sprintf("%v", v)
	}
	return fmt.Sprintf("(%s)", strings.Join(vals, ", "))
}

// Operator constants
const (
	OpEq          = "="
	OpNeq         = "!="
	OpLt          = "<"
	OpGt          = ">"
	OpLte         = "<="
	OpGte         = ">="
	OpContains    = "~"
	OpNotContains = "!~"
	OpIn          = "IN"
	OpNotIn       = "NOT IN"
)

// Boolean operator constants
const (
	OpAnd = "AND"
	OpOr  = "OR"
	OpNot = "NOT"
)

// Known field names for validation
var KnownFields = map[string]string{
	// Issue fields
	"id":          "string",
	"title":       "string",
	"description": "string",
	"status":      "enum",
	"type":        "enum",
	"priority":    "ordinal",
	"points":      "number",
	"labels":      "string",
	"parent":      "string",
	"epic":        "string",
	"implementer": "string",
	"reviewer":    "string",
	"minor":       "bool",
	"branch":      "string",
	"sprint":      "string",
	"created":     "date",
	"updated":     "date",
	"closed":      "date",

	// Cross-entity prefixes (validated separately)
	"log":     "prefix",
	"comment": "prefix",
	"handoff": "prefix",
	"file":    "prefix",
	"dep":     "prefix",
	"note":    "prefix",
}

// Cross-entity field mappings
var CrossEntityFields = map[string]map[string]string{
	"log": {
		"message":   "string",
		"type":      "enum",
		"timestamp": "date",
		"session":   "string",
	},
	"comment": {
		"text":    "string",
		"created": "date",
		"session": "string",
	},
	"handoff": {
		"done":      "string",
		"remaining": "string",
		"decisions": "string",
		"uncertain": "string",
		"timestamp": "date",
	},
	"file": {
		"path": "string",
		"role": "enum",
	},
	"dep": {
		"blocks":     "string",
		"depends_on": "string",
	},
	"note": {
		"title":    "string",
		"content":  "string",
		"created":  "date",
		"updated":  "date",
		"pinned":   "bool",
		"archived": "bool",
	},
}

// Enum values for validation
var EnumValues = map[string][]string{
	"status":   {"open", "in_progress", "blocked", "in_review", "closed"},
	"type":     {"bug", "feature", "task", "epic", "chore"},
	"priority": {"P0", "P1", "P2", "P3", "P4"},
	"log.type": {"progress", "blocker", "decision", "hypothesis", "tried", "result", "orchestration"},
	"file.role": {"implementation", "test", "reference", "config"},
}

// Known functions
var KnownFunctions = map[string]struct {
	MinArgs int
	MaxArgs int
	Help    string
}{
	"has":           {1, 1, "has(field) - field is not empty"},
	"is":            {1, 1, "is(status) - shorthand for status check"},
	"any":           {2, -1, "any(field, v1, v2, ...) - field matches any value"},
	"all":           {2, -1, "all(field, v1, v2, ...) - field matches all values"},
	"none":          {2, -1, "none(field, v1, v2, ...) - field matches none"},
	"blocks":        {1, 1, "blocks(id) - issues that block the given id"},
	"blocked_by":    {1, 1, "blocked_by(id) - issues blocked by the given id"},
	"child_of":      {1, 1, "child_of(id) - direct children of issue"},
	"descendant_of": {1, 1, "descendant_of(id) - all descendants (recursive)"},
	"linked_to":     {1, 1, "linked_to(path) - issues linked to file path"},
	"rework":        {0, 0, "rework() - issues rejected and awaiting rework"},
	"is_ready":      {0, 0, "is_ready() - issues with no open dependencies"},
	"has_open_deps": {0, 0, "has_open_deps() - issues with open dependencies"},
	"label":         {1, 1, "label(name) - issues with the given label"},
	"labels":        {1, 1, "labels(name) - alias for label()"},
}

// SortClause represents a sort specification
type SortClause struct {
	Field      string // DB column name (e.g., "created_at", "priority")
	Descending bool   // true for descending order
}

func (s *SortClause) String() string {
	if s.Descending {
		return fmt.Sprintf("sort:-%s", s.Field)
	}
	return fmt.Sprintf("sort:%s", s.Field)
}

// SortFieldToColumn maps user-facing sort field names to DB columns
var SortFieldToColumn = map[string]string{
	"created":  "created_at",
	"updated":  "updated_at",
	"closed":   "closed_at",
	"deleted":  "deleted_at",
	"priority": "priority",
	"id":       "id",
	"title":    "title",
	"status":   "status",
	"points":   "points",
	"sprint":   "sprint",
}

// NoteSortFieldToColumn maps user-facing sort field names to DB columns for notes
var NoteSortFieldToColumn = map[string]string{
	"created":  "created_at",
	"updated":  "updated_at",
	"title":    "title",
	"pinned":   "pinned",
	"archived": "archived",
}

// Query represents a parsed TDQ query
type Query struct {
	Root Node
	Raw  string       // original query string
	Sort *SortClause  // optional sort clause
}

func (q *Query) String() string {
	var parts []string
	if q.Root != nil {
		parts = append(parts, q.Root.String())
	}
	if q.Sort != nil {
		parts = append(parts, q.Sort.String())
	}
	return strings.Join(parts, " ")
}
