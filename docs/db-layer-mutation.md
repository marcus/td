## Problem
Every synced-entity mutation requires callers to manually snapshot → mutate → snapshot → LogAction. This 4-step protocol is enforced by convention only—forgetting any step silently breaks sync. Already caused real bugs (TUI edits missing PreviousData/NewData).

## Solution
Add `*Logged` variants in the DB layer that atomically handle the full protocol for **all synced entities** (issues, boards, board positions, dependencies, file links). Then migrate all ~45+ call sites.

Note: Work sessions (`TagIssueToWorkSession`/`UntagIssueFromWorkSession`) already have logging baked into the DB layer—they're the reference pattern.

---

## Phase 1: Issues — Add logged variants

**New file: `internal/db/issues_logged.go`**

All functions wrap read + mutate + log in a **single `withWriteLock` call** (critical—the lock is file-based and non-reentrant, so we must inline the SQL rather than calling existing `UpdateIssue`/`LogAction`):

### `CreateIssueLogged(issue *models.Issue, sessionID string) error`
- Inside one `withWriteLock`: generate ID, INSERT issue, INSERT action_log with `ActionCreate` + `NewData`

### `UpdateIssueLogged(issue *models.Issue, sessionID string, actionType ActionType) error`
- Inside one `withWriteLock`: SELECT current row → marshal as `PreviousData`, UPDATE issue, marshal modified issue as `NewData`, INSERT action_log
- Caller modifies the issue struct fields first, then calls this. The function reads the *current DB state* for PreviousData (not the caller's copy), guaranteeing accurate snapshots.

### `DeleteIssueLogged(issueID, sessionID string) error`
- Inside one `withWriteLock`: SELECT current row → marshal as `PreviousData`, UPDATE `deleted_at`/`updated_at`, INSERT action_log with `ActionDelete`

---

## Phase 2: Issues — Migrate CLI call sites

Replace manual snapshot+LogAction patterns with single `*Logged` calls.

**Files to modify:**
- `cmd/create.go` — `CreateIssueLogged` (1 site)
- `cmd/update.go` — `UpdateIssueLogged` (1 site)
- `cmd/start.go` — `UpdateIssueLogged` with `ActionStart` (1 site)
- `cmd/unstart.go` — `UpdateIssueLogged` with `ActionReopen` (1 site)
- `cmd/block.go` — `UpdateIssueLogged` with `ActionBlock` / `ActionReopen` (2 sites)
- `cmd/delete.go` — `DeleteIssueLogged` (1 site)
- `cmd/review.go` — `UpdateIssueLogged` for submit/approve/reject/close (4 sites)

**Pattern change:**
```go
// BEFORE (~8 lines):
prevData, _ := json.Marshal(issue)
issue.Status = models.StatusInReview
database.UpdateIssue(issue)
newData, _ := json.Marshal(issue)
database.LogAction(&models.ActionLog{...PreviousData: string(prevData), NewData: string(newData)...})

// AFTER (~2 lines):
issue.Status = models.StatusInReview
database.UpdateIssueLogged(issue, sess.ID, models.ActionReview)
```

---

## Phase 3: Issues — Migrate TUI call sites

**Files to modify:**
- `pkg/monitor/form_operations.go` — create (1 site) + edit (1 site)
- `pkg/monitor/actions.go`:
  - `markForReview` — primary issue + cascade-down children loop
  - `executeDelete` — 1 site
  - `executeCloseWithReason` — primary issue + cascade-down children loop
  - `approveIssue` — primary issue + cascade-down children loop
  - `reopenIssue` — 1 site

Cascade-down loops call `UpdateIssueLogged` per child instead of manual snapshot+log.

---

## Phase 4: Issues — Migrate DB-layer cascades

**File: `internal/db/issue_relations.go`**

- `CascadeUpParentStatus` — currently reads parent, modifies, calls `UpdateIssue` + `LogAction` separately. Refactor to use an unexported `updateIssueAndLog` helper (same `withWriteLock` inline pattern) to avoid double lock acquisition.
- `CascadeUnblockDependents` — same treatment.

---

## Phase 5: Boards — Add logged variants + migrate

**New file: `internal/db/boards_logged.go`**

Current state: `CreateBoard`, `UpdateBoard`, `DeleteBoard` have no logging. Callers in `cmd/board.go` and `pkg/monitor/board_editor.go` do manual LogAction.

### `CreateBoardLogged(name, queryStr, sessionID string) (*models.Board, error)`
- Inside one `withWriteLock`: validate query, generate ID, INSERT board, INSERT action_log with `ActionBoardCreate` + `NewData`

### `UpdateBoardLogged(board *models.Board, sessionID string) error`
- Inside one `withWriteLock`: SELECT current board → marshal as `PreviousData`, UPDATE board, INSERT action_log with `ActionBoardUpdate`

### `DeleteBoardLogged(boardID, sessionID string) error`
- Inside one `withWriteLock`: SELECT current board → marshal as `PreviousData`, soft-delete positions, DELETE board, INSERT action_log with `ActionBoardDelete`

**Call sites to migrate:**
- `cmd/board.go` — create (1), update (1), delete (1)
- `pkg/monitor/board_editor.go` — create (1), update (1), delete (1)

---

## Phase 6: Board positions — Add logged variants + migrate

**Add to `internal/db/boards_logged.go`**

Current state: `SetIssuePosition`, `RemoveIssuePosition` have no logging. Callers in `cmd/board.go` and `pkg/monitor/commands.go` do manual LogAction.

### `SetIssuePositionLogged(boardID, issueID string, position int, sessionID string) error`
- Inside one `withWriteLock`: UPSERT position, INSERT action_log with `ActionBoardSetPosition` + `NewData`

### `RemoveIssuePositionLogged(boardID, issueID, sessionID string) error`
- Inside one `withWriteLock`: SELECT current position → marshal as `PreviousData`, DELETE position, INSERT action_log with `ActionBoardUnposition`

**Call sites to migrate:**
- `cmd/board.go` — set position (1), remove position (1), respace logging loop
- `pkg/monitor/commands.go` — `logPositionSet` helper and all move operations (~10+ sites that go through the helper)

---

## Phase 7: Dependencies — Add logged variants + migrate

**New file: `internal/db/relations_logged.go`**

Current state: `AddDependency`, `RemoveDependency` have no logging. Callers in `cmd/dependencies.go`, `cmd/create.go`, `cmd/update.go`, `pkg/monitor/form_operations.go` do manual LogAction.

### `AddDependencyLogged(issueID, dependsOnID, relationType, sessionID string) error`
- Inside one `withWriteLock`: INSERT dependency, INSERT action_log with `ActionAddDep` + `NewData` (row data as JSON map)

### `RemoveDependencyLogged(issueID, dependsOnID, sessionID string) error`
- Inside one `withWriteLock`: capture row data, DELETE dependency, INSERT action_log with `ActionRemoveDep` + `PreviousData`

**Call sites to migrate:**
- `cmd/dependencies.go` — add (1), remove (1)
- `cmd/create.go` — add deps (1), add blocks (1)
- `cmd/update.go` — add deps (1), remove deps (1), add blocks (1), remove blocks (1)
- `pkg/monitor/form_operations.go` — add deps on create (1)

---

## Phase 8: File links — Add logged variants + migrate

**Add to `internal/db/relations_logged.go`**

Current state: `LinkFile`, `UnlinkFile` have no logging. Callers in `cmd/link.go` do manual LogAction.

### `LinkFileLogged(issueID, filePath string, role models.FileRole, sha, sessionID string) error`
- Inside one `withWriteLock`: INSERT file link, INSERT action_log with `ActionLinkFile` + `NewData`

### `UnlinkFileLogged(issueID, filePath, sessionID string) error`
- Inside one `withWriteLock`: SELECT current row → marshal as `PreviousData`, DELETE file link, INSERT action_log with `ActionUnlinkFile`

**Call sites to migrate:**
- `cmd/link.go` — link (1), unlink (1)

---

## Phase 9: Testing

- **New test files:**
  - `internal/db/issues_logged_test.go`
  - `internal/db/boards_logged_test.go`
  - `internal/db/relations_logged_test.go`

- **Test coverage per entity:**
  - Logged variant creates both entity row AND action_log entry with correct data
  - PreviousData/NewData snapshots are accurate
  - Unlogged variants do NOT create action_log entries
  - Error paths (entity not found for update/delete) return proper errors

- **Integration verification:**
  - `go build -o td .` — compiles cleanly
  - `go test ./...` — all existing + new tests pass
  - Run sync e2e tests if available

---

## Phase 10: Cleanup + doc comments

- Add warning doc comments to all unlogged variants:
  ```go
  // CreateIssue creates an issue WITHOUT logging to action_log.
  // For local mutations, use CreateIssueLogged instead.
  // This unlogged variant exists for sync receiver applying remote events.
  ```
- Same pattern for `UpdateIssue`, `DeleteIssue`, `CreateBoard`, `UpdateBoard`, `DeleteBoard`, `AddDependency`, `RemoveDependency`, `LinkFile`, `UnlinkFile`, `SetIssuePosition`, `RemoveIssuePosition`

---

## Key design choices
- **Single `withWriteLock`**: Each logged variant does everything (read prev + mutate + log) in one lock acquisition. Inlines SQL rather than calling sub-functions that each lock.
- **Unlogged variants preserved**: Sync receiver needs them to apply remote events without double-logging.
- **Work sessions already done**: `TagIssueToWorkSession`/`UntagIssueFromWorkSession` already inline the logging—no changes needed. They're the reference pattern.
- **ActionType as parameter**: `UpdateIssueLogged` accepts `actionType` since callers know the semantic meaning (review vs close vs approve etc).
- **DB reads PreviousData**: `UpdateIssueLogged` reads current DB state internally for accurate snapshots, rather than trusting caller's copy.

## Files to create
- `internal/db/issues_logged.go`
- `internal/db/issues_logged_test.go`
- `internal/db/boards_logged.go`
- `internal/db/boards_logged_test.go`
- `internal/db/relations_logged.go`
- `internal/db/relations_logged_test.go`

## Files to modify
- `internal/db/issues.go` — add warning doc comments
- `internal/db/boards.go` — add warning doc comments
- `internal/db/issue_relations.go` — add warning doc comments + refactor cascades
- `cmd/create.go`
- `cmd/update.go`
- `cmd/start.go`
- `cmd/unstart.go`
- `cmd/block.go`
- `cmd/delete.go`
- `cmd/review.go`
- `cmd/board.go`
- `cmd/dependencies.go`
- `cmd/link.go`
- `pkg/monitor/form_operations.go`
- `pkg/monitor/actions.go`
- `pkg/monitor/board_editor.go`
- `pkg/monitor/commands.go`

## Verification
1. `go build -o td .` — compiles cleanly
2. `go test ./...` — all tests pass
3. `grep -rn 'LogAction' cmd/ pkg/monitor/` — no remaining manual LogAction calls for synced entities (only non-synced uses like handoff/log remain)
4. Manual test: create/update/delete issue, board, dependency, file link via CLI and TUI — verify action_log entries are correct
5. Run sync chaos test if available
