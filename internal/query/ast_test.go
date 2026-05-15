package query

import (
	"strings"
	"testing"
)

func TestBinaryExprString(t *testing.T) {
	expr := &BinaryExpr{
		Op:    OpAnd,
		Left:  &FieldExpr{Field: "status", Operator: OpEq, Value: "open"},
		Right: &FieldExpr{Field: "priority", Operator: OpEq, Value: "P0"},
	}
	got := expr.String()
	want := "(status = open AND priority = P0)"
	if got != want {
		t.Errorf("BinaryExpr.String() = %q, want %q", got, want)
	}
	if expr.nodeType() != "BinaryExpr" {
		t.Errorf("nodeType() = %q", expr.nodeType())
	}
}

func TestUnaryExprString(t *testing.T) {
	expr := &UnaryExpr{
		Op:   OpNot,
		Expr: &FieldExpr{Field: "status", Operator: OpEq, Value: "closed"},
	}
	got := expr.String()
	want := "(NOT status = closed)"
	if got != want {
		t.Errorf("UnaryExpr.String() = %q, want %q", got, want)
	}
	if expr.nodeType() != "UnaryExpr" {
		t.Errorf("nodeType() = %q", expr.nodeType())
	}
}

func TestFieldExprString(t *testing.T) {
	f := &FieldExpr{Field: "title", Operator: OpContains, Value: "auth"}
	if got, want := f.String(), "title ~ auth"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if f.nodeType() != "FieldExpr" {
		t.Errorf("nodeType() = %q", f.nodeType())
	}
}

func TestFunctionCallString(t *testing.T) {
	fn := &FunctionCall{Name: "has", Args: []interface{}{"labels"}}
	if got, want := fn.String(), "has(labels)"; got != want {
		t.Errorf("got %q want %q", got, want)
	}

	fnMulti := &FunctionCall{Name: "any", Args: []interface{}{"labels", "bug", "regression"}}
	if got, want := fnMulti.String(), "any(labels, bug, regression)"; got != want {
		t.Errorf("got %q want %q", got, want)
	}

	if fn.nodeType() != "FunctionCall" {
		t.Errorf("nodeType() = %q", fn.nodeType())
	}
}

func TestTextSearchString(t *testing.T) {
	ts := &TextSearch{Text: "refactor"}
	if got, want := ts.String(), `"refactor"`; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if ts.nodeType() != "TextSearch" {
		t.Errorf("nodeType() = %q", ts.nodeType())
	}
}

func TestDateValueString(t *testing.T) {
	d := &DateValue{Raw: "2024-01-15", Relative: false}
	if d.String() != "2024-01-15" {
		t.Errorf("got %q", d.String())
	}
	rel := &DateValue{Raw: "-7d", Relative: true}
	if rel.String() != "-7d" {
		t.Errorf("got %q", rel.String())
	}
}

func TestSpecialValueString(t *testing.T) {
	cases := map[string]string{
		"me":      "@me",
		"empty":   "EMPTY",
		"null":    "NULL",
		"unknown": "unknown",
	}
	for typ, want := range cases {
		sv := &SpecialValue{Type: typ}
		if got := sv.String(); got != want {
			t.Errorf("SpecialValue{%q}.String() = %q, want %q", typ, got, want)
		}
	}
}

func TestListValueString(t *testing.T) {
	lv := &ListValue{Values: []interface{}{"a", "b", "c"}}
	if got, want := lv.String(), "(a, b, c)"; got != want {
		t.Errorf("got %q want %q", got, want)
	}

	empty := &ListValue{}
	if got, want := empty.String(), "()"; got != want {
		t.Errorf("empty list got %q want %q", got, want)
	}
}

func TestSortClauseString(t *testing.T) {
	asc := &SortClause{Field: "created_at"}
	if got, want := asc.String(), "sort:created_at"; got != want {
		t.Errorf("got %q want %q", got, want)
	}

	desc := &SortClause{Field: "priority", Descending: true}
	if got, want := desc.String(), "sort:-priority"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestQueryString(t *testing.T) {
	q := &Query{
		Root: &FieldExpr{Field: "status", Operator: OpEq, Value: "open"},
		Sort: &SortClause{Field: "created_at", Descending: true},
	}
	got := q.String()
	if !strings.Contains(got, "status = open") {
		t.Errorf("missing root expr: %q", got)
	}
	if !strings.Contains(got, "sort:-created_at") {
		t.Errorf("missing sort clause: %q", got)
	}
}

func TestQueryStringEmpty(t *testing.T) {
	q := &Query{}
	if got := q.String(); got != "" {
		t.Errorf("empty Query.String() = %q, want empty", got)
	}
}

func TestQueryStringOnlySort(t *testing.T) {
	q := &Query{Sort: &SortClause{Field: "title"}}
	if got, want := q.String(), "sort:title"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestKnownFieldsContainsCoreIssueFields(t *testing.T) {
	required := []string{"id", "title", "status", "type", "priority", "created", "updated"}
	for _, f := range required {
		if _, ok := KnownFields[f]; !ok {
			t.Errorf("KnownFields missing %q", f)
		}
	}
}

func TestCrossEntityFieldsHaveExpectedPrefixes(t *testing.T) {
	for _, prefix := range []string{"log", "comment", "handoff", "file", "dep", "note"} {
		sub, ok := CrossEntityFields[prefix]
		if !ok {
			t.Errorf("CrossEntityFields missing %q", prefix)
			continue
		}
		if len(sub) == 0 {
			t.Errorf("CrossEntityFields[%q] is empty", prefix)
		}
		// Each prefix should also be marked as a prefix in KnownFields.
		if KnownFields[prefix] != "prefix" {
			t.Errorf("KnownFields[%q] should be %q, got %q", prefix, "prefix", KnownFields[prefix])
		}
	}
}

func TestEnumValuesPopulated(t *testing.T) {
	for k, vs := range EnumValues {
		if len(vs) == 0 {
			t.Errorf("EnumValues[%q] is empty", k)
		}
	}
}

func TestKnownFunctionsArgRanges(t *testing.T) {
	// Spot-check shape: every function help text begins with its name.
	for name, spec := range KnownFunctions {
		if !strings.HasPrefix(spec.Help, name) {
			t.Errorf("function %q help should start with name: %q", name, spec.Help)
		}
		if spec.MinArgs < 0 {
			t.Errorf("function %q has negative MinArgs %d", name, spec.MinArgs)
		}
		if spec.MaxArgs != -1 && spec.MaxArgs < spec.MinArgs {
			t.Errorf("function %q has MaxArgs %d < MinArgs %d", name, spec.MaxArgs, spec.MinArgs)
		}
	}

	// Specific contracts the parser relies on.
	if KnownFunctions["has"].MinArgs != 1 || KnownFunctions["has"].MaxArgs != 1 {
		t.Errorf("has() should accept exactly one argument")
	}
	if KnownFunctions["any"].MaxArgs != -1 {
		t.Errorf("any() should be variadic (MaxArgs=-1)")
	}
}

func TestSortFieldToColumnHasExpectedKeys(t *testing.T) {
	for _, f := range []string{"created", "updated", "priority", "title", "status"} {
		col, ok := SortFieldToColumn[f]
		if !ok || col == "" {
			t.Errorf("SortFieldToColumn missing %q", f)
		}
	}
}

func TestNoteSortFieldToColumnHasExpectedKeys(t *testing.T) {
	for _, f := range []string{"created", "updated", "title", "pinned", "archived"} {
		col, ok := NoteSortFieldToColumn[f]
		if !ok || col == "" {
			t.Errorf("NoteSortFieldToColumn missing %q", f)
		}
	}
}

func TestOperatorConstantsAreDistinct(t *testing.T) {
	ops := []string{OpEq, OpNeq, OpLt, OpGt, OpLte, OpGte, OpContains, OpNotContains, OpIn, OpNotIn}
	seen := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		if _, dup := seen[op]; dup {
			t.Errorf("duplicate operator constant: %q", op)
		}
		seen[op] = struct{}{}
	}
}
