# Query-Based Boards Implementation Plan

Transforms boards from manual membership to query-based views (like Jira). Issues appear on boards based on TDQ queries, with optional custom ordering.

## Key Decisions

1. **Query-Based**: Each board has a TDQ query defining which issues appear
2. **Sparse Ordering**: `board_issue_positions` stores only explicit positions; issues without a row are unpositioned. Order is positioned (by `position`), then unpositioned (by query sort or default `priority`, `updated_at`, `id`).
3. **Built-in "All Issues"**: Default board uses empty query (TDQ empty = matches all issues), with closed hidden by default via status filter. Cannot be deleted or have name/query edited.
4. **Sprint Field**: Add `sprint` column to issues for future sprint support

---

## 1. Schema Changes (Migration v10)

**File**: `internal/db/schema.go`

Note: v1 boards were never shipped, so no legacy data migration is expected; `board_issue_positions` should be empty in practice.

```sql
-- Add to boards table
ALTER TABLE boards ADD COLUMN query TEXT NOT NULL DEFAULT '';
ALTER TABLE boards ADD COLUMN is_builtin INTEGER NOT NULL DEFAULT 0;

-- Rename table for semantic clarity (must drop/recreate index)
DROP INDEX IF EXISTS idx_board_issues_position;
ALTER TABLE board_issues RENAME TO board_issue_positions;

-- Recreate index (positions are explicit only; no NULL positions stored)
CREATE UNIQUE INDEX IF NOT EXISTS idx_board_positions_position
    ON board_issue_positions(board_id, position);

-- Add sprint field to issues
ALTER TABLE issues ADD COLUMN sprint TEXT DEFAULT '';

-- Create built-in "All Issues" board (empty query = all issues)
INSERT INTO boards (id, name, query, is_builtin, created_at, updated_at)
VALUES ('bd-all-issues', 'All Issues', '', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(name) DO UPDATE SET
    query = excluded.query,
    is_builtin = 1,
    updated_at = CURRENT_TIMESTAMP;
```

Update `SchemaVersion` from 9 to 10.

---

## 2. Model Changes

**File**: `internal/models/models.go`

Update Board struct:

```go
type Board struct {
    ID           string     `json:"id"`
    Name         string     `json:"name"`
    Query        string     `json:"query"`           // NEW: TDQ query
    IsBuiltin    bool       `json:"is_builtin"`      // NEW: cannot delete
    LastViewedAt *time.Time `json:"last_viewed_at,omitempty"`
    CreatedAt    time.Time  `json:"created_at"`
    UpdatedAt    time.Time  `json:"updated_at"`
}
```

Update BoardIssueView:

```go
type BoardIssueView struct {
    BoardID     string    `json:"board_id"`
    Position    int       `json:"position"`       // Valid only when HasPosition is true
    HasPosition bool      `json:"has_position"`   // NEW: true if explicitly positioned
    Issue       Issue     `json:"issue"`
}
```

Add to Issue struct:

```go
Sprint string `json:"sprint,omitempty"`
```

Add ActionTypes:

```go
ActionBoardUpdate      ActionType = "board_update"
ActionBoardSetPosition ActionType = "board_set_position"
ActionBoardUnposition  ActionType = "board_unposition"
```

---

## 3. Database Functions

**File**: `internal/db/db.go`

### Board CRUD

- `CreateBoard(name, query string) (*Board, error)` - validates query syntax
  - Empty query: allowed and matches all issues (TDQ semantics)
  - Invalid TDQ syntax: returns error, board not created
  - Query validated via `query.Parse()` + `Validate()` before storage
  - If a "no results" board is needed, use an impossible query (e.g., `id = "no-such-id"`)
- `GetBoard(id string) (*Board, error)`
- `GetBoardByName(name string) (*Board, error)`
- `ResolveBoardRef(ref string) (*Board, error)` - accepts name or ID
- `ListBoards() ([]Board, error)` - sorted by last_viewed_at DESC
- `UpdateBoard(board *Board) error` - update name/query
  - Reject updates to `name`/`query` for `is_builtin=1`
  - Validate query via `query.Parse()` + `Validate()`
- `DeleteBoard(id string) error` - fails for is_builtin=1
- `GetLastViewedBoard() (*Board, error)`
- `UpdateBoardLastViewed(boardID string) error`

### Position Functions

- `SetIssuePosition(boardID, issueID string, position int) error`
  - Transactional: remove existing position for issue, then insert at target
  - If target position is occupied, shift positions >= target by +1
- `RemoveIssuePosition(boardID, issueID string) error`
- `GetBoardIssuePositions(boardID string) ([]BoardIssuePosition, error)`
- `SwapIssuePositions(boardID, id1, id2 string) error` - for J/K reordering

### Query Execution

- `GetBoardIssues(boardID, sessionID string, statusFilter []Status) ([]BoardIssueView, error)`
  1. Get board and its query
  2. Execute TDQ query via `query.Execute()`; use query sort if provided, else default to `priority` ASC, `updated_at` DESC, `id` ASC
  3. Apply optional status filter
  4. Get explicit positions from board_issue_positions (ignore rows for issues not in query+filter)
  5. Return: positioned issues (by `position`), then unpositioned (preserving query order). Positions persist even if issues temporarily leave the query.

---

## 4. Query System Update

**File**: `internal/query/ast.go`

Add `sprint` to KnownFields:

```go
"sprint": "string",
```

**File**: `internal/query/evaluator.go`

Add sprint field getter and column mapping.

---

## 5. CLI Commands

**File**: `cmd/board.go` (new file)

```
td board                              # Show help
td board list [--json]                # List all boards
td board create <name> [-q "query"]   # Create board (empty query matches all)
td board delete <board>               # Delete (fails for All Issues)
td board show <board> [--status ...] [--json]  # Show issues in order (default: open/in_progress/blocked/in_review)
td board edit <board> [-q "..."] [-n "..."]  # Edit query/name (fails for All Issues)
td board move <board> <id> <position> # Set explicit position
td board unposition <board> <id>      # Remove explicit position
```

---

## 6. TUI Changes

### New State (`pkg/monitor/types.go`, `pkg/monitor/model.go`)

```go
type BoardMode struct {
    Active       bool
    Board        *models.Board
    Issues       []models.BoardIssueView
    Cursor       int
    ScrollOffset int
    StatusFilter map[models.Status]bool
}

// In Model:
BoardMode          BoardMode
BoardPickerOpen    bool
BoardPickerCursor  int
AllBoards          []models.Board
```

### Keybindings (`pkg/monitor/keymap/`)

| Key     | Context            | Action                              |
| ------- | ------------------ | ----------------------------------- |
| `B`     | Main/Board         | Open board picker                   |
| `j/k`   | Board              | Navigate                            |
| `J/K`   | Board              | Move issue up/down (swap positions) |
| `ctrl+K`| Board              | Move issue to top                   |
| `ctrl+J`| Board              | Move issue to bottom                |
| `c`     | Board              | Toggle closed visibility            |
| `F`     | Board              | Cycle status filter                 |
| `Esc`   | Board              | Exit to All Issues                  |
| `Enter` | Board              | Open issue modal                    |
| `n`     | Picker             | Create new board                    |

### Rendering (`pkg/monitor/view.go`)

- `renderBoardPanel()` - Issues with position indicators
- `renderBoardPicker()` - Modal for board selection
- Panel title: `BOARD: Sprint 1 (12)`

### Commands (`pkg/monitor/commands.go`)

- `openBoardPicker()` - fetch boards, show picker
- `selectBoard()` - activate board, update last_viewed_at
- `exitBoardMode()` - return to All Issues
- `moveIssueInBoard(direction)` - swap with adjacent positioned issue; if current issue is unpositioned, insert it just above/below the nearest positioned neighbor (or at position 1 if none)
- `moveIssueToTop()` - move selected issue to position 1 (top of column/category)
- `moveIssueToBottom()` - move selected issue to max+1 (bottom of positioned issues)
- `toggleBoardClosed()` - toggle closed in status filter (default closed hidden)

### Init (`pkg/monitor/model.go`)

On launch, restore last viewed board via `GetLastViewedBoard()` and initialize status filter to open/in_progress/blocked/in_review (closed false).

---

## 7. Implementation Order

### Phase 1: Database (no UI changes)

1. `internal/db/schema.go` - Migration v10
2. `internal/models/models.go` - Update Board, BoardIssueView, Issue
3. `internal/db/db.go` - Board CRUD + GetBoardIssues
4. `internal/query/` - Add sprint field support

### Phase 2: CLI

5. `cmd/board.go` - All subcommands

### Phase 3: TUI Foundation

6. `pkg/monitor/types.go` - BoardMode, messages
7. `pkg/monitor/model.go` - State fields
8. `pkg/monitor/data.go` - FetchBoards, FetchBoardIssues
9. `pkg/monitor/keymap/` - Contexts, commands, bindings

### Phase 4: TUI Rendering

10. `pkg/monitor/view.go` - Board panel, picker modal
11. `pkg/monitor/commands.go` - Command handlers
12. Context detection for board mode

### Phase 5: Polish

13. Last-viewed persistence in Init()
14. J/K reordering
15. 'c' toggle closed in board mode
16. Help text updates

---

## 8. Files to Modify

| File                             | Changes                                           |
| -------------------------------- | ------------------------------------------------- |
| `internal/db/schema.go`          | Migration v10: query, is_builtin, sprint columns  |
| `internal/db/db.go`              | Board CRUD, GetBoardIssues with query execution   |
| `internal/models/models.go`      | Update Board, BoardIssueView; add Sprint to Issue |
| `internal/query/ast.go`          | Add sprint to KnownFields                         |
| `internal/query/evaluator.go`    | Sprint field getter/column mapping                |
| `cmd/board.go`                   | NEW: All board CLI commands                       |
| `pkg/monitor/types.go`           | BoardMode struct, messages                        |
| `pkg/monitor/model.go`           | Board state fields, Init restoration              |
| `pkg/monitor/data.go`            | FetchBoards, FetchBoardIssues                     |
| `pkg/monitor/view.go`            | renderBoardPanel, renderBoardPicker               |
| `pkg/monitor/keymap/registry.go` | Board contexts and commands                       |
| `pkg/monitor/keymap/bindings.go` | Board keybindings                                 |
| `pkg/monitor/commands.go`        | Board command handlers                            |

---

## 9. Verification

```bash
# Build and test
go build -o td . && go test ./...

# Create boards
td board create "Sprint 1" -q 'sprint = "Sprint 1"'
td board create "High Pri Bugs" -q 'type = bug AND priority <= P1'

# List and show
td board list
td board show "Sprint 1"
td board show bd-all-issues  # All Issues (closed hidden by default)
td board show bd-all-issues --status closed  # Closed only

# Positioning
td board move "Sprint 1" td-abc123 1
td board move "Sprint 1" td-def456 2
td board show "Sprint 1"  # Verify order
td board unposition "Sprint 1" td-abc123

# Edit and delete
td board edit "Sprint 1" -q 'sprint = "Sprint 1" AND status != closed'
td board delete "Sprint 1"
td board delete "All Issues"  # Should error

# TUI
td monitor
# B → select board → j/k navigate → J/K reorder → c toggle closed → Esc exit
# Restart → verify last board restored
```

---

## 10. Key Differences from Original Spec

| Aspect         | Original (spec-issue-boards.md) | New (Query-Based)                          |
| -------------- | ------------------------------- | ------------------------------------------ |
| Membership     | Manual `td board add <id>`      | Automatic via TDQ query                    |
| Default view   | Separate from boards            | Built-in "All Issues" board                |
| Ordering       | All issues positioned           | Sparse: positioned first, rest by query sort/default |
| Sprints        | Not addressed                   | Sprint field on issues, queryable          |
| CLI add/remove | `board add`, `board remove`     | `board move`, `board unposition`           |
