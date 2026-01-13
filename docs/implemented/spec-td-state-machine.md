# Issue Status State Machine Implementation Plan

## Overview

Replace scattered status transition logic with a centralized state machine in `internal/workflow/`. Start with a **liberal configuration** (all current transitions allowed) with hooks for future stricter guards.

## Current State

- 5 statuses: `open`, `in_progress`, `blocked`, `in_review`, `closed`
- Transitions scattered across: `cmd/start.go`, `cmd/unstart.go`, `cmd/review.go`, `cmd/block.go`
- No centralized validation - each command directly sets `issue.Status`
- Action logging exists via `LogAction()` for undo support

## Implementation

### Phase 1: Create `internal/workflow/` Package

**New files:**

```
internal/workflow/
    workflow.go      # Core StateMachine type and Execute method
    transitions.go   # Transition definitions (from→to mappings)
    guards.go        # Guard interface and implementations
    errors.go        # TransitionError type
```

**Core types (`workflow.go`):**

```go
type TransitionMode int
const (
    ModeLiberal   TransitionMode = iota  // Guards disabled (default)
    ModeAdvisory                         // Guards warn but allow
    ModeStrict                           // Guards block transitions
)

type Guard interface {
    Name() string
    Check(ctx *TransitionContext) GuardResult
}

type TransitionContext struct {
    Issue      *models.Issue
    FromStatus models.Status
    ToStatus   models.Status
    SessionID  string
    Force      bool
}

type StateMachine struct {
    transitions map[models.Status]map[models.Status]*Transition
    mode        TransitionMode
}

func (sm *StateMachine) CanTransition(ctx *TransitionContext) (bool, []GuardResult)
func (sm *StateMachine) Validate(ctx *TransitionContext) error  // Returns error if invalid
```

**Transition definitions (`transitions.go`):**

| From        | To          | Guards (future)        |
| ----------- | ----------- | ---------------------- |
| open        | in_progress | -                      |
| open        | blocked     | -                      |
| open        | in_review   | -                      |
| open        | closed      | -                      |
| in_progress | open        | -                      |
| in_progress | blocked     | -                      |
| in_progress | in_review   | -                      |
| in_progress | closed      | -                      |
| blocked     | open        | -                      |
| blocked     | in_progress | BlockedGuard           |
| blocked     | closed      | -                      |
| in_review   | open        | -                      |
| in_review   | in_progress | -                      |
| in_review   | closed      | DifferentReviewerGuard |
| closed      | open        | -                      |

### Phase 2: Initial Guards (Advisory Only)

```go
// BlockedGuard - warns when starting blocked issue without --force
type BlockedGuard struct{}

// DifferentReviewerGuard - warns on self-approval (except minor tasks)
type DifferentReviewerGuard struct{}

// EpicChildrenGuard - warns when closing epic with open children
type EpicChildrenGuard struct{}
```

### Phase 3: Integration with Commands

**Pattern for each command:**

```go
// Before (current - cmd/start.go:88)
issue.Status = models.StatusInProgress

// After
sm := workflow.DefaultMachine()  // Returns ModeLiberal
ctx := &workflow.TransitionContext{
    Issue:      issue,
    FromStatus: issue.Status,
    ToStatus:   models.StatusInProgress,
    SessionID:  sess.ID,
    Force:      force,
}
if err := sm.Validate(ctx); err != nil {
    return err
}
issue.Status = models.StatusInProgress
```

**Commands to update (in order):**

1. `cmd/start.go` - open/blocked → in_progress
2. `cmd/unstart.go` - in_progress → open
3. `cmd/block.go` - \* → blocked, blocked → open, closed → open
4. `cmd/review.go` - \* → in_review, in_review → closed, in_review → in_progress
5. `pkg/monitor/actions.go` - TUI status changes

### Phase 4: Optional Future Enhancements

- Add `Resolution` field to Issue (completed, abandoned, duplicate, wont_fix)
- Config option to switch between Liberal/Advisory/Strict modes
- Workflow hooks for cascade logic (currently in `db.CascadeUpParentStatus`)

## Files to Modify

| File                               | Changes                                              |
| ---------------------------------- | ---------------------------------------------------- |
| `internal/workflow/workflow.go`    | NEW - core state machine                             |
| `internal/workflow/transitions.go` | NEW - transition definitions                         |
| `internal/workflow/guards.go`      | NEW - guard implementations                          |
| `internal/workflow/errors.go`      | NEW - TransitionError                                |
| `cmd/start.go`                     | Add Validate() call before status change             |
| `cmd/unstart.go`                   | Add Validate() call                                  |
| `cmd/block.go`                     | Add Validate() calls for block/unblock/reopen        |
| `cmd/review.go`                    | Add Validate() calls for review/approve/reject/close |
| `pkg/monitor/actions.go`           | Add Validate() calls for TUI actions                 |

## Verification

1. Run existing tests: `go test ./...`
2. Manual test each transition:
   ```bash
   td create "test" --type task
   td start <id>           # open → in_progress
   td unstart <id>         # in_progress → open
   td block <id>           # open → blocked
   td unblock <id>         # blocked → open
   td start <id> && td review <id>  # → in_review
   td reject <id>          # in_review → in_progress
   td review <id> && td approve <id>  # → closed
   td reopen <id>          # closed → open
   ```
3. Verify undo still works: `td undo`
4. Verify monitor TUI transitions work: `td monitor`
