# Spec: Prevent Agents from Bypassing Review Workflow

> **See also** [spec-delegated-review-closure.md](./spec-delegated-review-closure.md).
> The implementation-history tracking introduced here (`CreatorSession`, `issue_session_history`, `WasSessionImplementationInvolved`) is the foundation that the delegated-review policy builds on. Delegated review reuses these checks to define reviewer independence, while allowing any involved session to perform the close once an independent review has been recorded.

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

---

## Agent-Scoped Sessions (td-7302eba7)

### Problem: PPID Instability

Each Bash tool invocation runs in a separate subprocess with different PPID. This undermined bypass prevention because the same agent could create, start, and approve an issue across multiple commands since each had a different session ID.

### Solution: Agent Ancestry Detection

Walk up the process tree to find the parent agent process (e.g., `claude`, `cursor`, `codex`). Use its PID as a stable session identifier.

```
Terminal
  └─ zsh (4200)
       └─ claude (15261)  ← STABLE IDENTIFIER
            └─ zsh (per-command)
                 └─ td command
```

### Session Path Structure

Sessions are now scoped by **branch + agent**:

```
.todos/sessions/
  main/
    claude-code_15261.json    # Claude Code session A
    claude-code_83769.json    # Claude Code session B (different agent)
    cursor_45678.json         # Cursor session
  feature-x/
    claude-code_15261.json    # Same agent, different branch
```

### Session File Format

```json
{
  "id": "ses_a8f773",
  "branch": "main",
  "agent_type": "claude-code",
  "agent_pid": 15261,
  "context_id": "proc:ppid=59609",
  "started_at": "2026-01-06T14:00:43Z",
  "last_activity": "2026-01-06T14:47:16Z"
}
```

### Agent Detection Priority

1. **TD_SESSION_ID** - Explicit override (most reliable)
2. **CURSOR_AGENT** - Cursor IDE env var
3. **Process ancestry** - Walk tree for known agents: `claude`, `cursor`, `codex`, `windsurf`, `zed`, `aider`, `copilot`, `gemini`
4. **Terminal session** - TERM_SESSION_ID, TMUX_PANE, etc.
5. **Fallback** - Unknown agent

### Key Behaviors

| Scenario | Session Behavior |
|----------|------------------|
| Same agent, multiple commands | Same session (stable PID) |
| `/clear` in conversation | Same session (process doesn't restart) |
| Exit agent, restart | New session (new PID) |
| `td usage --new-session` | New session (explicit rotation) |
| Two agents, same branch | Different sessions (different PIDs) |
| Same agent, branch switch | Same session follows (same PID) |

### Files Modified

| File | Changes |
|------|---------|
| `internal/session/agent_fingerprint.go` | New file: process tree walking, agent detection |
| `internal/session/session.go` | Added AgentType/AgentPID fields, agent-scoped paths |
| `cmd/system.go` | Updated display to show agent info |
| `cmd/context.go` | Updated session display |

### Bypass Prevention Scenarios

| Scenario | Old Behavior | New Behavior |
|----------|-------------|--------------|
| Agent A creates, A implements, A approves | ❌ Could bypass via PPID changes | ✅ Blocked (same agent PID) |
| Agent A implements, /clear, A approves | ❌ Could bypass | ✅ Blocked (same agent PID) |
| Agent A implements, Agent B approves | ✅ Allowed | ✅ Allowed (different PIDs) |
