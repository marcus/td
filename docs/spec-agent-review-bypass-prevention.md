# Spec: Prevent Agents from Bypassing Review Workflow

## Problem Summary

Agents can bypass the review workflow through several mechanisms:

1. **Create → Close bypass**: `td create "task"` leaves `ImplementerSession` empty, so the old self-close check passes
2. **Unstart → Restart bypass**: `td unstart` clears `ImplementerSession`, then different agent starts, original can approve
3. **Historical session bypass**: Agent A starts, A unstarts, B starts → A could approve (not tracked in history)

## Solution: CreatorSession + SessionHistory

Track who created each issue AND all sessions that ever touched it.

### Data Model Changes

#### 1. CreatorSession field on Issue
```go
CreatorSession string `json:"creator_session,omitempty"`
```
Set at creation time, immutable after.

#### 2. IssueSessionHistory table
```sql
CREATE TABLE issue_session_history (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    action TEXT NOT NULL,  -- 'created', 'started', 'unstarted', 'reviewed'
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Close Check Logic

```go
wasInvolved, _ := database.WasSessionInvolved(issueID, sess.ID)
isCreator := issue.CreatorSession == sess.ID
isImplementer := issue.ImplementerSession == sess.ID
hasOtherImplementer := issue.ImplementerSession != "" && !isImplementer

// Can close if: never involved OR (only created AND someone else implemented)
canClose := !wasInvolved || (isCreator && hasOtherImplementer)

if !canClose {
    // Blocked - require --self-close-exception
}
```

### Approve Check Logic

```go
wasInvolved, _ := database.WasSessionInvolved(issueID, sess.ID)

// Cannot approve if session was ever involved (unless minor)
if wasInvolved && !issue.Minor {
    // Blocked
}
```

### Session Actions Recorded

| Action | When | Effect |
|--------|------|--------|
| `created` | `td create` | Recorded in history, CreatorSession set |
| `started` | `td start` | Recorded in history |
| `unstarted` | `td unstart` | Recorded in history (critical for bypass prevention) |
| `reviewed` | `td review`/`td approve` | Recorded in history |

### Backward Compatibility

- Existing issues: empty `CreatorSession`, no history entries
- Fall back to current behavior (only check `ImplementerSession`)
- New issues get full protection

## Files Modified

| File | Changes |
|------|---------|
| `internal/models/models.go` | Added `CreatorSession`, `IssueSessionHistory`, `IssueSessionAction` |
| `internal/db/schema.go` | Migrations 6 & 7 for column and table |
| `internal/db/db.go` | Added `RecordSessionAction`, `WasSessionInvolved`, `GetSessionHistory` |
| `cmd/create.go` | Set CreatorSession, record "created" action |
| `cmd/start.go` | Record "started" action |
| `cmd/unstart.go` | Record "unstarted" action |
| `cmd/review.go` | Updated close/approve checks to use history |

## Policy Summary

| Scenario | Allowed? |
|----------|----------|
| Creator closes (no one implemented) | ❌ Blocked |
| Creator closes (other implemented) | ✅ Allowed |
| Implementer closes | ❌ Blocked |
| Session that unstarted approves | ❌ Blocked |
| Creator approves | ❌ Blocked |
| Minor task self-approve | ✅ Allowed |
| Unrelated session approves | ✅ Allowed |
