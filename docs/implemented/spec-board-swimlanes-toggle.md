# Board Swimlanes View Toggle

## Overview

Add a toggle between two view modes when viewing a board in `td monitor`:

1. **Swimlanes view** (DEFAULT): Board issues grouped by status categories (Reviewable, Needs Rework, Ready, Blocked, Closed), sorted within each lane by the board's sort order
2. **Backlog view**: Flat list with manual position ordering (current behavior)

Both views show only issues matching the board's TDQ query. The `b` key toggles between views.

---

## Key Behaviors

| Aspect      | Swimlanes View                                          | Backlog View                            |
| ----------- | ------------------------------------------------------- | --------------------------------------- |
| Layout      | Grouped by status category                              | Flat ordered list                       |
| Sort order  | Within each lane: board sort (priority/created/updated) | By explicit position, then unpositioned |
| J/K reorder | Reorders within current lane                            | Reorders in full list                   |
| Panel title | `BOARD: Sprint 1 [swimlanes] (12)`                      | `BOARD: Sprint 1 [backlog] (12)`        |
| Default     | Yes                                                     | No                                      |

---

## Persistence

The selected view mode (swimlanes/backlog) is persisted per-board in the database. When re-entering a board, the last-used view mode is restored.

### Schema Change (Migration v11)

```sql
ALTER TABLE boards ADD COLUMN view_mode TEXT NOT NULL DEFAULT 'swimlanes';
```

Valid values: `'swimlanes'`, `'backlog'`

---

## Type Changes

**File**: `pkg/monitor/types.go`

```go
// BoardViewMode represents the display mode within a board
type BoardViewMode int

const (
    BoardViewSwimlanes BoardViewMode = iota // Default: grouped by status
    BoardViewBacklog                        // Flat list with position ordering
)

// String returns the display name for the view mode
func (v BoardViewMode) String() string {
    switch v {
    case BoardViewBacklog:
        return "backlog"
    default:
        return "swimlanes"
    }
}
```

Add to `BoardMode` struct:

```go
type BoardMode struct {
    // ... existing fields ...
    ViewMode         BoardViewMode   // Current view mode (swimlanes/backlog)
    SwimlaneCursor   int             // Cursor position in swimlanes view
    SwimlaneScroll   int             // Scroll offset in swimlanes view
    SwimlaneRows     []TaskListRow   // Flattened rows for swimlanes view
    SwimlaneData     TaskListData    // Categorized data for swimlanes view
}
```

---

## Model Changes

**File**: `internal/models/models.go`

Add `ViewMode` field to Board struct:

```go
type Board struct {
    // ... existing fields ...
    ViewMode string `json:"view_mode"` // "swimlanes" or "backlog"
}
```

---

## Database Changes

**File**: `internal/db/schema.go`

Add migration v11 to add `view_mode` column to boards table.

**File**: `internal/db/db.go`

- Update `CreateBoard` to set default view_mode = "swimlanes"
- Update `GetBoard`, `GetBoardByName`, `ListBoards` to include view_mode
- Add `UpdateBoardViewMode(boardID string, viewMode string) error`

---

## Data Layer Changes

**File**: `pkg/monitor/data.go`

Add function to categorize board issues for swimlanes view:

```go
// CategorizeBoardIssues takes board issues and groups them by status category
// for the swimlanes view. Issues are sorted within each category by the
// board's sort order (not by position).
func CategorizeBoardIssues(issues []models.BoardIssueView, sortMode SortMode) TaskListData {
    // Group issues by status into categories:
    // - Reviewable: in_review status
    // - NeedsRework: in_progress with reviewer_session set (was rejected)
    // - Ready: open status
    // - Blocked: blocked status
    // - Closed: closed status

    // Sort within each category by sortMode (priority/created/updated)
    // Return TaskListData with populated slices
}
```

---

## View Changes

**File**: `pkg/monitor/view.go`

### Update `renderTaskListPanel()`

```go
func (m Model) renderTaskListPanel(height int) string {
    if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
        switch m.BoardMode.ViewMode {
        case BoardViewSwimlanes:
            return m.renderBoardSwimlanesView(height)
        case BoardViewBacklog:
            return m.renderTaskListBoardView(height)
        }
    }
    // ... existing categorized view code ...
}
```

### Add `renderBoardSwimlanesView()`

```go
// renderBoardSwimlanesView renders board issues grouped by status category
func (m Model) renderBoardSwimlanesView(height int) string {
    // Very similar to main branch's categorized task list rendering
    // Uses m.BoardMode.SwimlaneRows instead of m.TaskListRows
    // Uses m.BoardMode.SwimlaneCursor and SwimlaneScroll
    // Panel title: "BOARD: <name> [swimlanes] (<count>)"
    // Reuses formatCategoryHeader() and formatCategoryTag()
}
```

### Update `renderTaskListBoardView()`

Update panel title to include view mode indicator:

```go
panelTitle = fmt.Sprintf("BOARD: %s [backlog] (%d)", boardName, totalRows)
```

---

## Keyboard Handling

**File**: `pkg/monitor/keymap/bindings.go`

Add command:

```go
CmdToggleBoardView Command = "toggle_board_view"
```

Add binding in board context:

```go
{Key: "v", Command: CmdToggleBoardView, Description: "Toggle swimlanes/backlog view"},
```

**File**: `pkg/monitor/actions.go`

Add handler:

```go
func (m *Model) toggleBoardViewMode() tea.Cmd {
    if m.BoardMode.Board == nil {
        return nil
    }

    // Toggle view mode
    if m.BoardMode.ViewMode == BoardViewSwimlanes {
        m.BoardMode.ViewMode = BoardViewBacklog
    } else {
        m.BoardMode.ViewMode = BoardViewSwimlanes
    }

    // Try to preserve selected issue across view switch
    selectedID := m.getSelectedBoardIssueID()

    // Rebuild rows for new view if needed
    if m.BoardMode.ViewMode == BoardViewSwimlanes {
        m.buildBoardSwimlaneRows()
        m.restoreBoardSwimlaneCursor(selectedID)
    } else {
        m.restoreBoardBacklogCursor(selectedID)
    }

    // Persist view mode to database
    return m.persistBoardViewMode()
}
```

---

## J/K Reordering in Swimlanes

When in swimlanes view, J/K (Shift+j/k) reorders the issue within its current status lane:

1. Find the current issue's lane (category)
2. Find adjacent issues in the same lane
3. Swap positions with the adjacent issue
4. If at lane boundary, do nothing (don't cross lanes)

**File**: `pkg/monitor/commands.go`

Update `moveIssueInBoard()`:

```go
func (m *Model) moveIssueInBoard(direction int) tea.Cmd {
    if m.BoardMode.ViewMode == BoardViewSwimlanes {
        return m.moveIssueInSwimlane(direction)
    }
    return m.moveIssueInBacklog(direction)
}

func (m *Model) moveIssueInSwimlane(direction int) tea.Cmd {
    // Get current issue and its category
    // Find adjacent issue in same category
    // Swap their positions
    // Rebuild swimlane rows
}
```

---

## Cursor Management

Each view mode maintains separate cursor and scroll state:

- **Backlog view**: `BoardMode.Cursor`, `BoardMode.ScrollOffset`
- **Swimlanes view**: `BoardMode.SwimlaneCursor`, `BoardMode.SwimlaneScroll`

When toggling views:

1. Save current issue ID
2. Switch view mode
3. Find same issue in new view, set cursor there
4. If not found, reset cursor to 0

---

## Files to Modify

| File                             | Changes                                           |
| -------------------------------- | ------------------------------------------------- |
| `internal/db/schema.go`          | Migration v11: add view_mode column               |
| `internal/db/db.go`              | UpdateBoardViewMode, include view_mode in queries |
| `internal/models/models.go`      | Add ViewMode field to Board                       |
| `pkg/monitor/types.go`           | Add BoardViewMode enum, extend BoardMode struct   |
| `pkg/monitor/model.go`           | Add swimlane row building, cursor management      |
| `pkg/monitor/data.go`            | Add CategorizeBoardIssues function                |
| `pkg/monitor/view.go`            | Add renderBoardSwimlanesView, update routing      |
| `pkg/monitor/keymap/bindings.go` | Add CmdToggleBoardView, bind 'b' key              |
| `pkg/monitor/actions.go`         | Add toggleBoardViewMode handler                   |
| `pkg/monitor/commands.go`        | Update moveIssueInBoard for swimlane support      |
| `pkg/monitor/keymap/help.go`     | Update help text                                  |

---

## Implementation Order

1. **Schema** (`internal/db/schema.go`): Migration v11
2. **Models** (`internal/models/models.go`): Add ViewMode to Board
3. **Database** (`internal/db/db.go`): CRUD for view_mode
4. **Types** (`pkg/monitor/types.go`): Add BoardViewMode, extend BoardMode
5. **Data** (`pkg/monitor/data.go`): Add CategorizeBoardIssues
6. **Model** (`pkg/monitor/model.go`): Swimlane row building, cursor management
7. **View** (`pkg/monitor/view.go`): Add renderBoardSwimlanesView, update routing
8. **Keymap** (`pkg/monitor/keymap/bindings.go`): Add command and binding
9. **Actions** (`pkg/monitor/actions.go`): Add toggle handler
10. **Commands** (`pkg/monitor/commands.go`): Swimlane reordering
11. **Help** (`pkg/monitor/keymap/help.go`): Update documentation

---

## Verification

```bash
# Build and test
go build -o td . && go test ./...

# Create a board with issues
td board create "Sprint 1" -q 'sprint = "Sprint 1"'
td board add "Sprint 1" td-xxx td-yyy td-zzz

# Launch monitor and enter board
td monitor
# Press B, select "Sprint 1"

# Verify swimlanes is default
# - Issues should be grouped by status (REVIEWABLE, READY, BLOCKED, etc.)
# - Panel title shows "[swimlanes]"

# Toggle to backlog
# Press b
# - Issues should be in flat list with position numbers
# - Panel title shows "[backlog]"

# Toggle back to swimlanes
# Press b
# - Back to grouped view

# Test J/K reordering in swimlanes
# Navigate to an issue, press K (Shift+k)
# - Issue should move up within its lane only

# Test J/K reordering in backlog
# Press b to switch to backlog
# Press K - issue moves up in full list

# Exit and re-enter board
# Press Esc, then B, select same board
# - Should restore last view mode (backlog if that's what was used)

# Verify status filter works in both views
# Press c to toggle closed visibility
# Press F to cycle status filters
```
