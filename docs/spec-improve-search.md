# Task Search Improvements Plan

## Problem

Search doesn't find issues reliably. User reports a closed issue with "Gracefully" in the title doesn't appear when searching for "gracefully".

## Current Implementation

`internal/db/db.go:537-541` - Simple `LIKE '%query%'` on title and description only, sorted by priority.

## Scope: Add ID Search + Relevance Ranking

### Step 1: Add ID to Search Filter

**File**: `internal/db/db.go` (line 537-541)

```go
// Current
query += " AND (title LIKE ? OR description LIKE ?)"
searchPattern := "%" + opts.Search + "%"
args = append(args, searchPattern, searchPattern)

// New
query += " AND (id LIKE ? OR title LIKE ? OR description LIKE ?)"
searchPattern := "%" + opts.Search + "%"
args = append(args, searchPattern, searchPattern, searchPattern)
```

### Step 2: Add SearchResult Type

**File**: `internal/db/db.go`

```go
type SearchResult struct {
    Issue      models.Issue
    Score      int    // Higher = better match
    MatchField string // Primary field that matched
}
```

### Step 3: Implement Relevance Scoring

**File**: `internal/db/db.go`

New function `SearchIssuesRanked`:

1. Query all matching issues (ID, title, or description LIKE pattern)
2. Score each result:
   - **100**: Exact ID match (case-insensitive)
   - **90**: ID contains query
   - **80**: Title exact match
   - **70**: Title starts with query
   - **60**: Title contains query
   - **40**: Description contains query
   - **20**: Labels contain query
3. Sort by score DESC, then priority ASC
4. Return `[]SearchResult`

### Step 4: Update Search Command

**File**: `cmd/search.go`

- Use `SearchIssuesRanked` instead of `SearchIssues`
- Add `--show-score` flag to display relevance scores
- Default output unchanged (just better ordering)

### Step 5: Update Monitor Search

**File**: `pkg/monitor/data.go`

Update `fetchTaskList` to use ranked search, maintaining existing category grouping but with better ordering within categories.

### Step 6: Add Tests

**File**: `internal/db/db_test.go`

```go
func TestSearchIssuesRanked(t *testing.T) {
    // Test ID match scores higher than title match
    // Test title match scores higher than description
    // Test case-insensitive matching
    // Test closed issues included when status filter allows
}
```

## Files to Modify

1. `internal/db/db.go` - SearchResult type, SearchIssuesRanked function, update ListIssues
2. `cmd/search.go` - Use ranked search, add --show-score flag
3. `pkg/monitor/data.go` - Integrate ranked search
4. `internal/db/db_test.go` - Tests
