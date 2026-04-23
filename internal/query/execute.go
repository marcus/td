package query

import (
	"fmt"
	"strings"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

const (
	// DefaultMaxResults limits in-memory filtering to prevent OOM
	DefaultMaxResults = 10000
	// MaxDescendantDepth prevents infinite recursion in descendant_of
	MaxDescendantDepth = 100
)

// ExecuteOptions contains options for query execution
type ExecuteOptions struct {
	Limit      int
	SortBy     string
	SortDesc   bool
	MaxResults int // Max issues to process in-memory (0 = DefaultMaxResults)
}

// Execute parses and executes a TDQ query
func Execute(database QuerySource, queryStr string, sessionID string, opts ExecuteOptions) ([]models.Issue, error) {
	// Parse the query
	query, err := Parse(queryStr)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	// Validate the query
	if errs := query.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("validation error: %v", errs[0])
	}

	// Set memory limits
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = DefaultMaxResults
	}

	// Determine sort options: query sort clause takes precedence over opts
	sortBy := opts.SortBy
	sortDesc := opts.SortDesc
	if query.Sort != nil {
		sortBy = query.Sort.Field
		sortDesc = query.Sort.Descending
	}

	// Create evaluation context
	ctx := NewEvalContext(sessionID)
	evaluator := NewEvaluator(ctx, query)

	// Check if we need cross-entity queries
	hasCrossEntity := evaluator.HasCrossEntityConditions()

	// Fetch issues with a limit to prevent OOM
	// We fetch more than maxResults to allow for filtering, but cap it
	fetchOpts := db.ListIssuesOptions{
		SortBy:   sortBy,
		SortDesc: sortDesc,
		Limit:    maxResults, // Cap fetch to prevent loading entire DB
	}
	issues, err := database.ListIssues(fetchOpts)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	var filtered []models.Issue
	if hasCrossEntity {
		// When cross-entity conditions exist, use the AST-walking evaluator
		// which handles both cross-entity and regular fields with correct boolean logic
		filtered, err = applyCrossEntityFilters(database, issues, query, ctx)
		if err != nil {
			return nil, fmt.Errorf("cross-entity filter error: %w", err)
		}
	} else {
		// Pure regular-field queries: use in-memory matcher (faster, no DB lookups)
		matcher, err := evaluator.ToMatcher()
		if err != nil {
			return nil, fmt.Errorf("matcher error: %w", err)
		}
		for _, issue := range issues {
			if matcher(issue) {
				filtered = append(filtered, issue)
			}
		}
	}

	// Apply limit after filtering
	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}

	return filtered, nil
}

func applyCrossEntityFilters(database QuerySource, issues []models.Issue, query *Query, ctx *EvalContext) ([]models.Issue, error) {
	if query.Root == nil {
		return issues, nil
	}

	// Pre-fetch bulk data for efficiency
	prefetch, err := prefetchCrossEntityData(database, query.Root)
	if err != nil {
		return nil, err
	}

	// Build a per-issue matcher that walks the AST and respects OR/AND/NOT
	var result []models.Issue
	for _, issue := range issues {
		match, err := evalCrossEntityNode(database, issue, query.Root, ctx, prefetch)
		if err != nil {
			return nil, err
		}
		if match {
			result = append(result, issue)
		}
	}
	return result, nil
}

// crossEntityPrefetch holds pre-fetched bulk data to avoid per-issue queries
type crossEntityPrefetch struct {
	reworkIDs        map[string]bool
	issuesWithOpenDeps map[string]bool
}

// prefetchCrossEntityData walks the AST to find what bulk data needs pre-fetching
func prefetchCrossEntityData(database QuerySource, n Node) (*crossEntityPrefetch, error) {
	p := &crossEntityPrefetch{}
	needs := collectFunctionNames(n)
	var err error
	if needs["rework"] {
		p.reworkIDs, err = database.GetRejectedInProgressIssueIDs()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch rework IDs: %w", err)
		}
	}
	if needs["is_ready"] || needs["has_open_deps"] {
		p.issuesWithOpenDeps, err = database.GetIssuesWithOpenDeps()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch dependency data: %w", err)
		}
	}
	return p, nil
}

func collectFunctionNames(n Node) map[string]bool {
	names := make(map[string]bool)
	switch node := n.(type) {
	case *BinaryExpr:
		for k, v := range collectFunctionNames(node.Left) {
			names[k] = v
		}
		for k, v := range collectFunctionNames(node.Right) {
			names[k] = v
		}
	case *UnaryExpr:
		for k, v := range collectFunctionNames(node.Expr) {
			names[k] = v
		}
	case *FunctionCall:
		names[node.Name] = true
	}
	return names
}

// evalCrossEntityNode recursively evaluates the AST for a single issue,
// respecting AND/OR/NOT boolean operators for all conditions.
// When hasMixedOR is true, regular field conditions are also evaluated here
// (not just passed through) so that OR between cross-entity and regular fields works correctly.
func evalCrossEntityNode(database QuerySource, issue models.Issue, n Node, ctx *EvalContext, pf *crossEntityPrefetch) (bool, error) {
	switch node := n.(type) {
	case *BinaryExpr:
		left, err := evalCrossEntityNode(database, issue, node.Left, ctx, pf)
		if err != nil {
			return false, err
		}
		// Short-circuit for AND/OR
		if node.Op == OpAnd && !left {
			return false, nil
		}
		if node.Op == OpOr && left {
			return true, nil
		}
		right, err := evalCrossEntityNode(database, issue, node.Right, ctx, pf)
		if err != nil {
			return false, err
		}
		if node.Op == OpAnd {
			return right, nil
		}
		return right, nil

	case *UnaryExpr:
		inner, err := evalCrossEntityNode(database, issue, node.Expr, ctx, pf)
		if err != nil {
			return false, err
		}
		return !inner, nil

	case *FieldExpr:
		filter := fieldExprToFilter(node, false)
		if filter != nil {
			return applyCrossEntityFilter(database, issue, *filter, ctx, pf.reworkIDs, pf.issuesWithOpenDeps)
		}
		// Regular field - evaluate in-memory
		evaluator := NewEvaluator(ctx, &Query{})
		matcher, err := evaluator.fieldExprToMatcher(node)
		if err != nil {
			return true, nil
		}
		return matcher(issue), nil

	case *FunctionCall:
		filter := functionCallToFilter(node, false)
		if filter != nil {
			return applyCrossEntityFilter(database, issue, *filter, ctx, pf.reworkIDs, pf.issuesWithOpenDeps)
		}
		// Regular function - evaluate in-memory
		evaluator := NewEvaluator(ctx, &Query{})
		matcher, err := evaluator.functionToMatcher(node)
		if err != nil {
			return true, nil
		}
		return matcher(issue), nil

	case *TextSearch:
		evaluator := NewEvaluator(ctx, &Query{})
		matcher, err := evaluator.nodeToMatcher(node)
		if err != nil {
			return true, nil
		}
		return matcher(issue), nil

	default:
		return true, nil
	}
}

// fieldExprToFilter converts a FieldExpr to a crossEntityFilter if it's a cross-entity field.
// Returns nil for non-cross-entity fields.
func fieldExprToFilter(node *FieldExpr, negated bool) *crossEntityFilter {
	if node.Field == "epic" {
		return &crossEntityFilter{
			entity:   "epic",
			field:    "id",
			operator: node.Operator,
			value:    node.Value,
			negated:  negated,
		}
	}
	parts := strings.Split(node.Field, ".")
	if len(parts) > 1 {
		prefix := parts[0]
		if prefix == "log" || prefix == "comment" || prefix == "handoff" || prefix == "file" || prefix == "epic" {
			return &crossEntityFilter{
				entity:   prefix,
				field:    parts[1],
				operator: node.Operator,
				value:    node.Value,
				negated:  negated,
			}
		}
	}
	return nil
}

// functionCallToFilter converts a FunctionCall to a crossEntityFilter if it's a cross-entity function.
// Returns nil for non-cross-entity functions.
func functionCallToFilter(node *FunctionCall, negated bool) *crossEntityFilter {
	if node.Name == "blocks" || node.Name == "blocked_by" || node.Name == "linked_to" || node.Name == "descendant_of" || node.Name == "rework" || node.Name == "is_ready" || node.Name == "has_open_deps" {
		return &crossEntityFilter{
			entity:   "function",
			field:    node.Name,
			operator: "",
			value:    node.Args,
			negated:  negated,
		}
	}
	return nil
}

// countCrossEntityConditions counts the number of cross-entity leaf conditions in the AST.
// Used by tests to verify cross-entity condition detection.
func countCrossEntityConditions(n Node) int {
	count := 0
	switch node := n.(type) {
	case *BinaryExpr:
		count += countCrossEntityConditions(node.Left)
		count += countCrossEntityConditions(node.Right)
	case *UnaryExpr:
		count += countCrossEntityConditions(node.Expr)
	case *FieldExpr:
		if fieldExprToFilter(node, false) != nil {
			count++
		}
	case *FunctionCall:
		if functionCallToFilter(node, false) != nil {
			count++
		}
	}
	return count
}

type crossEntityFilter struct {
	entity   string // log, comment, handoff, file, dep, epic
	field    string // message, type, text, etc.
	operator string
	value    interface{}
	negated  bool // true if wrapped in NOT (unused in new AST-walk approach)
}

func applyCrossEntityFilter(database QuerySource, issue models.Issue, filter crossEntityFilter, ctx *EvalContext, reworkIDs, issuesWithOpenDeps map[string]bool) (bool, error) {
	switch filter.entity {
	case "log":
		logs, err := database.GetLogs(issue.ID, 0) // 0 = no limit
		if err != nil {
			return false, err
		}
		return matchLogs(logs, filter, ctx), nil

	case "comment":
		comments, err := database.GetComments(issue.ID)
		if err != nil {
			return false, err
		}
		return matchComments(comments, filter, ctx), nil

	case "handoff":
		handoff, err := database.GetLatestHandoff(issue.ID)
		if err != nil {
			// No handoff = no match for handoff queries
			return false, nil
		}
		if handoff == nil {
			return false, nil
		}
		return matchHandoff(handoff, filter, ctx), nil

	case "file":
		files, err := database.GetLinkedFiles(issue.ID)
		if err != nil {
			return false, err
		}
		return matchFiles(files, filter, ctx), nil

	case "epic":
		return matchEpicAncestor(database, issue, filter, ctx)

	case "function":
		return applyFunctionFilter(database, issue, filter, reworkIDs, issuesWithOpenDeps)

	default:
		return true, nil
	}
}

func matchLogs(logs []models.Log, filter crossEntityFilter, ctx *EvalContext) bool {
	for _, log := range logs {
		var fieldValue string
		switch filter.field {
		case "message":
			fieldValue = log.Message
		case "type":
			fieldValue = string(log.Type)
		case "session":
			fieldValue = log.SessionID
		default:
			continue
		}

		if matchValue(fieldValue, filter.operator, filter.value, ctx) {
			return true
		}
	}
	return false
}

func matchComments(comments []models.Comment, filter crossEntityFilter, ctx *EvalContext) bool {
	for _, comment := range comments {
		var fieldValue string
		switch filter.field {
		case "text":
			fieldValue = comment.Text
		case "session":
			fieldValue = comment.SessionID
		default:
			continue
		}

		if matchValue(fieldValue, filter.operator, filter.value, ctx) {
			return true
		}
	}
	return false
}

func matchHandoff(handoff *models.Handoff, filter crossEntityFilter, ctx *EvalContext) bool {
	var fieldValue string
	switch filter.field {
	case "done":
		fieldValue = strings.Join(handoff.Done, " ")
	case "remaining":
		fieldValue = strings.Join(handoff.Remaining, " ")
	case "decisions":
		fieldValue = strings.Join(handoff.Decisions, " ")
	case "uncertain":
		fieldValue = strings.Join(handoff.Uncertain, " ")
	default:
		return false
	}

	return matchValue(fieldValue, filter.operator, filter.value, ctx)
}

func matchFiles(files []models.IssueFile, filter crossEntityFilter, ctx *EvalContext) bool {
	for _, file := range files {
		var fieldValue string
		switch filter.field {
		case "path":
			fieldValue = file.FilePath
		case "role":
			fieldValue = string(file.Role)
		default:
			continue
		}

		if matchValue(fieldValue, filter.operator, filter.value, ctx) {
			return true
		}
	}
	return false
}

// matchEpicAncestor traverses up the parent chain to find an epic ancestor
// and checks if the epic's field matches the filter condition.
// Returns (true, nil) if an epic ancestor matches, (false, nil) if no match or no epic.
func matchEpicAncestor(database QuerySource, issue models.Issue, filter crossEntityFilter, ctx *EvalContext) (bool, error) {
	// Traverse up the parent chain looking for an epic
	current := issue.ParentID
	visited := make(map[string]bool)
	depth := 0

	for current != "" && !visited[current] && depth < MaxDescendantDepth {
		visited[current] = true
		depth++

		parent, err := database.GetIssue(current)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				break
			}
			return false, fmt.Errorf("matchEpicAncestor: failed to get parent %s: %w", current, err)
		}

		// Check if this ancestor is an epic
		if parent.Type == models.TypeEpic {
			// Found an epic - check if it matches the filter
			var fieldValue string
			switch filter.field {
			case "id":
				fieldValue = parent.ID
			case "labels":
				fieldValue = strings.Join(parent.Labels, ",")
			case "title":
				fieldValue = parent.Title
			case "status":
				fieldValue = string(parent.Status)
			case "priority":
				fieldValue = string(parent.Priority)
			default:
				// Unknown field - no match
				return false, nil
			}

			if matchValue(fieldValue, filter.operator, filter.value, ctx) {
				return true, nil
			}
			// Continue up the chain - there might be nested epics
		}

		current = parent.ParentID
	}

	// No matching epic found
	return false, nil
}

func matchValue(fieldValue, operator string, value interface{}, ctx *EvalContext) bool {
	// Resolve special values
	strValue := fmt.Sprintf("%v", value)
	if sv, ok := value.(*SpecialValue); ok {
		if sv.Type == "me" {
			strValue = ctx.CurrentSession
		}
	}

	switch operator {
	case OpEq:
		return strings.EqualFold(fieldValue, strValue)
	case OpNeq:
		return !strings.EqualFold(fieldValue, strValue)
	case OpContains:
		return strings.Contains(strings.ToLower(fieldValue), strings.ToLower(strValue))
	case OpNotContains:
		return !strings.Contains(strings.ToLower(fieldValue), strings.ToLower(strValue))
	default:
		return false
	}
}

func applyFunctionFilter(database QuerySource, issue models.Issue, filter crossEntityFilter, reworkIDs, issuesWithOpenDeps map[string]bool) (bool, error) {
	// Handle no-arg functions first
	switch filter.field {
	case "rework":
		return reworkIDs[issue.ID], nil
	case "is_ready":
		// is_ready() returns true if the issue has NO open dependencies
		return !issuesWithOpenDeps[issue.ID], nil
	case "has_open_deps":
		// has_open_deps() returns true if the issue has at least one open dependency
		return issuesWithOpenDeps[issue.ID], nil
	}

	// Functions that require arguments
	args, ok := filter.value.([]interface{})
	if !ok || len(args) == 0 {
		return false, nil
	}

	targetID := fmt.Sprintf("%v", args[0])

	switch filter.field {
	case "blocks":
		// Check if this issue blocks the target (i.e., target depends on this issue)
		deps, err := database.GetDependencies(targetID)
		if err != nil {
			return false, err
		}
		for _, depID := range deps {
			if depID == issue.ID {
				return true, nil
			}
		}
		return false, nil

	case "blocked_by":
		// Check if this issue is blocked by the target (i.e., this issue depends on target)
		deps, err := database.GetDependencies(issue.ID)
		if err != nil {
			return false, err
		}
		for _, depID := range deps {
			if depID == targetID {
				return true, nil
			}
		}
		return false, nil

	case "linked_to":
		// Check if this issue is linked to the file
		files, err := database.GetLinkedFiles(issue.ID)
		if err != nil {
			return false, err
		}
		for _, file := range files {
			if strings.Contains(file.FilePath, targetID) {
				return true, nil
			}
		}
		return false, nil

	case "descendant_of":
		// Check if this issue is a descendant of the target (recursive parent check)
		current := issue.ParentID
		visited := make(map[string]bool)
		depth := 0
		for current != "" && !visited[current] && depth < MaxDescendantDepth {
			if current == targetID {
				return true, nil
			}
			visited[current] = true
			depth++
			parent, err := database.GetIssue(current)
			if err != nil {
				// "not found" is expected at end of chain - treat as no match
				if strings.Contains(err.Error(), "not found") {
					break
				}
				// Actual DB errors should be returned
				return false, fmt.Errorf("descendant_of: failed to get parent %s: %w", current, err)
			}
			current = parent.ParentID
		}
		if depth >= MaxDescendantDepth {
			return false, fmt.Errorf("descendant_of: max depth %d exceeded (possible cycle)", MaxDescendantDepth)
		}
		return false, nil

	default:
		return false, nil
	}
}

// QuickSearch performs a simple text search (backward compatible with existing search)
func QuickSearch(database *db.DB, text string, sessionID string, limit int) ([]models.Issue, error) {
	// Use the ranked search for simple text queries
	opts := db.ListIssuesOptions{
		Search: text,
		Limit:  limit,
	}
	results, err := database.SearchIssuesRanked(text, opts)
	if err != nil {
		return nil, err
	}

	// Extract issues from search results
	issues := make([]models.Issue, len(results))
	for i, r := range results {
		issues[i] = r.Issue
	}
	return issues, nil
}
