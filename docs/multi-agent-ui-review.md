# Multi-Agent UI Review & Improvement Plan

> Review of `td` codebase on the `boards` branch to identify issues with multi-agent workflows and UI clarifications needed for users managing multiple sessions.

## Current Architecture Strengths

The codebase has solid foundations for multi-agent workflows:

1. **Session Isolation** (`internal/session/session.go:103-107`): Sessions are scoped by branch + agent fingerprint (type + PID), stored as `.todos/sessions/<branch>/<agent_type>_<pid>.json`

2. **Work Locking**: Issues have `ImplementerSession` field - only that session can modify in-progress issues

3. **Audit Trail**: All actions logged with session ID attribution

4. **Active Sessions Tracking**: Footer shows `[X active]` count for sessions with recent activity

---

## Identified Problems for Multi-Agent Scenarios

### 1. Active Sessions Display is Too Minimal

**Location**: `pkg/monitor/view.go:1797-1799`

```go
if len(m.ActiveSessions) > 0 {
    sessionsIndicator = activeSessionStyle.Render(fmt.Sprintf(" %d active ", len(m.ActiveSessions)))
}
```

**Problem**: Only shows count, not which sessions or what they're working on. A user can't tell if the "2 active" includes their own session or which agents are involved.

### 2. Session ID Truncation Loses Agent Info

**Location**: `pkg/monitor/view.go:2096-2101`

```go
func truncateSession(sessionID string) string {
    if len(sessionID) <= 10 {
        return sessionID
    }
    return sessionID[:10]
}
```

**Problem**: Session IDs are `ses_XXXXXX` (10 chars) so they fit, but there's no agent type shown. Users see `Impl: ses_a1b2c3` but don't know if that's `claude-code` or `cursor`.

### 3. No "My Session" vs "Other Session" Distinction

**Location**: `pkg/monitor/view.go:2006-2008` (formatIssueCompact)

**Problem**: In-progress issues show implementer session but there's no visual distinction between issues owned by the current session vs other agents. A user running the monitor can't quickly identify which work is "theirs".

### 4. Current Work Panel Doesn't Group by Agent

**Location**: `pkg/monitor/view.go:171-290`

**Problem**: IN PROGRESS section lists all in-progress issues without grouping or highlighting by ownership. With multiple agents, this gets confusing.

### 5. Handoff Notification is Passive

**Location**: `pkg/monitor/data.go:366-383`

**Problem**: Handoffs are fetched but require polling. Agent B must refresh to see a handoff from Agent A - there's no proactive alert showing "YOU have a handoff waiting".

### 6. Activity Feed Doesn't Distinguish Session Ownership

**Location**: `pkg/monitor/view.go:2030-2069` (formatActivityItem)

**Problem**: Activity shows session ID but all sessions look the same. No visual cue for "this is my activity" vs "this is another agent's activity".

### 7. Board Last-Viewed is Global, Not Per-Session

**Location**: `pkg/monitor/model.go:262-275`

**Problem**: When an agent opens a board, `last_viewed_at` updates globally. If two agents use different boards, they'll "fight" over which board auto-restores on startup.

### 8. Git Worktree Support Missing

**Location**: `internal/session/session.go:110-124`

**Problem**: Sessions are branch-scoped but not worktree-scoped. If a user has multiple git worktrees on the same branch, different agents in different worktrees could share sessions unintentionally.

---

## Recommended Improvements

### Phase 1: UI Visibility (High Priority)

| Improvement | Description |
|------------|-------------|
| **Enhanced Active Sessions** | Change `[2 active]` to `[2 active: claude-code, cursor]` showing agent types |
| **Agent Type in Session Display** | Show `ses_a1b2c3 [claude-code]` instead of just `ses_a1b2c3` |
| **"My Session" Highlighting** | Visual indicator (color/icon) for issues owned by current session |
| **Session Info in Footer** | Add "Session: ses_xxx [agent-type]" to footer |

### Phase 2: Handoff Improvements (Medium Priority)

| Improvement | Description |
|------------|-------------|
| **Directed Handoff Alerts** | Show "HANDOFF FOR YOU" vs "HANDOFF BY YOU" distinction |
| **Handoff Target Session** | Allow handoffs to specify target session |
| **Pending Handoff Filter** | TDQ function `handoff.for(@me)` to find handoffs awaiting your attention |

### Phase 3: Current Work Panel Enhancements (Medium Priority)

| Improvement | Description |
|------------|-------------|
| **Group by Agent** | Optional grouping of in-progress by implementer session |
| **"My Work" Section** | Separate "MY IN PROGRESS" from "OTHER AGENTS' WORK" |
| **Quick Agent Filter** | Filter current work to show only your session's issues |

### Phase 4: Git Worktree Support (For Future)

| Improvement | Description |
|------------|-------------|
| **Worktree Detection** | Detect if running in a git worktree |
| **Worktree-Scoped Sessions** | Include worktree path in session file naming |
| **Cross-Worktree Visibility** | Allow viewing issues from other worktrees |

---

## Implementation Priority

### Quick Wins (can implement immediately)

- Show agent type alongside session IDs
- Add "My Session" indicator to footer
- Color-code "my" vs "other" activities

### Medium Effort (require more changes)

- Enhanced active sessions display with agent list
- "My Work" vs "Other Work" sections in Current Work panel
- Handoff direction awareness

### Larger Changes (architectural)

- Git worktree support in session identity
- Per-session board preferences
- Proactive handoff notifications

---

## Technical Details

### Session File Structure

Current path format:
```
.todos/sessions/<branch>/<agent_type>_<pid>.json
```

Example:
```
.todos/sessions/main/claude-code_15261.json
.todos/sessions/main/cursor_42789.json
```

### Session Data Model

```go
type Session struct {
    ID                string    `json:"id"`                  // ses_XXXXXX
    Name              string    `json:"name,omitempty"`      // User-provided name
    Branch            string    `json:"branch,omitempty"`    // Git branch
    AgentType         string    `json:"agent_type,omitempty"`// claude-code, cursor, etc.
    AgentPID          int       `json:"agent_pid,omitempty"` // Parent agent PID
    ContextID         string    `json:"context_id,omitempty"`// Audit only
    PreviousSessionID string    `json:"previous_session_id,omitempty"`
    StartedAt         time.Time `json:"started_at"`
    LastActivity      time.Time `json:"last_activity,omitempty"`
}
```

### Supported Agent Types

- `claude-code` - Claude Code CLI
- `cursor` - Cursor IDE
- `codex` - OpenAI Codex
- `windsurf` - Windsurf
- `zed` - Zed editor
- `aider` - Aider
- `copilot` - GitHub Copilot
- `gemini` - Google Gemini
- `terminal` - Plain terminal
- `unknown` - Undetected agent

### Active Sessions Query

Currently fetches sessions with activity in last 5 minutes:
```go
func fetchActiveSessions(database *db.DB) []string {
    since := time.Now().Add(-5 * time.Minute)
    sessions, err := database.GetActiveSessions(since)
    // Returns session IDs only - no agent type info
}
```

**Improvement needed**: Return session objects with agent type, not just IDs.

---

## Related Files

- `internal/session/session.go` - Session management
- `internal/session/agent_fingerprint.go` - Agent detection
- `pkg/monitor/model.go` - Monitor state model
- `pkg/monitor/view.go` - Monitor rendering
- `pkg/monitor/data.go` - Data fetching
- `docs/implemented/proposal-session-identity.md` - Session design doc
