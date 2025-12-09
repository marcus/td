# td CLI Specification

## Overview

A minimalist local task and session management CLI designed for AI-assisted development workflows. Optimized for **session continuity**—capturing working state so new context windows can resume where previous ones stopped.

Backed by a per-project SQLite database. Initialize with `td init` to create the `.todos` environment.

### Design Principles

1. **Session-aware**: Tracks who worked on what, enabling review workflows
2. **Handoff-native**: Structured state capture, not just status flags
3. **Agent-friendly**: Machine-readable output, composable commands
4. **Local-first**: No external dependencies, git-friendly exports

---

## Issue Lifecycle

Issues follow an enforced workflow where **implementers cannot close their own work**:

```
                    ┌──────────────────────────────────────┐
                    │                                      │
                    ▼                                      │
┌──────┐  start  ┌─────────────┐  review  ┌───────────┐   │
│ open │ ──────▶ │ in_progress │ ───────▶ │ in_review │   │
└──────┘         └─────────────┘          └───────────┘   │
                        │                    │      │     │
                        │ block              │      │     │
                        ▼                    │      │     │
                   ┌─────────┐               │      │     │
                   │ blocked │───────────────┘      │     │
                   └─────────┘     review           │     │
                                                    │     │
                              approve ┌─────────────┘     │
                                      │                   │
                                      ▼         reject    │
                                 ┌────────┐ ──────────────┘
                                 │ closed │
                                 └────────┘
```

**Key constraint**: The session that implements (calls `td start`, `td log`, `td handoff`) cannot approve. A different session must review and close.

---

## Global Help

```
td help [command]
td --help
```

---

## Initialization

### `td init`

Creates the local `.todos` directory and SQLite database. Automatically adds `.todos/` to `.gitignore` if in a git repository.

```bash
td init
# Output:
# INITIALIZED .todos/
# Added .todos/ to .gitignore
# Session: ses_a1b2c3
```

Each `td init` or new terminal session generates a session ID used for tracking implementer vs reviewer.

---

## Session Identity

### `td whoami`

Show current session identity.

```bash
td whoami
# Output:
# SESSION: ses_a1b2c3
# STARTED: 2025-01-15T10:30:00Z
# ISSUES TOUCHED: td-5q, td-6r
```

### `td session [name]`

Optionally name/tag the current session for easier identification.

```bash
td session "claude-impl-oauth"
# Output: SESSION NAMED ses_a1b2c3 "claude-impl-oauth"

td whoami
# Output:
# SESSION: ses_a1b2c3 (claude-impl-oauth)
# ...
```

---

## Issue Management Commands

### `td create [title] [flags]`

Create a new issue.

**Flags:**

```txt
  --acceptance string    Acceptance criteria
  --blocks string        Issues this blocks (e.g. td-43da)
  --depends-on string    Issues this depends on (e.g. td-42ad)
  --description string   Description text
  --labels string        Comma-separated labels (e.g. frontend,urgent)
  --parent string        Parent issue ID reference (e.g. td-4daf)
  --points int           Story points (Fibonacci: 1,2,3,5,8,13,21)
  --priority string      Priority: P0 (critical), P1 (high), P2 (medium/default), P3 (low), P4 (none)
  --title string         Issue title (optional when [title] positional is used)
  --type string          Issue type (bug, feature, task, epic, chore)
```

**Examples:**

```bash
td create "Fix login bug" --type bug --priority P1 --labels auth,urgent
td create "OAuth integration" --depends-on td-42ad --blocks td-43da
td create --title "Refactor API" --points 5 --type task
```

### `td show [issue-id]`

Display full details of an issue, including session history and handoff state.

**Output options:**

```txt
  --long        Detailed multi-line output (default)
  --short       Compact summary
  --json        Machine-readable JSON representation
```

**Example output:**

```bash
td show td-5q
# Output:
# td-5q: Implement OAuth flow
# Status: in_review
# Type: feature | Priority: P1 | Points: 5
# Labels: auth, backend
#
# CURRENT HANDOFF (ses_a1b2c3, 2h ago):
#   Done:
#     - OAuth callback route implemented
#     - Token storage working
#   Remaining:
#     - Error handling for token refresh
#     - Unit tests
#   Decisions:
#     - Using JWT (not opaque tokens)
#   Uncertain:
#     - Refresh token rotation policy
#
# SESSION LOG:
#   [10:30] Started by ses_a1b2c3
#   [10:45] "Set up OAuth callback route"
#   [11:15] "Token storage implemented, tested manually"
#   [12:00] Submitted for review
#
# AWAITING REVIEW - requires different session to approve/reject
```

### `td update [issue-id...] [flags]`

Update one or more fields on existing issues.

**Flags:**

```txt
  --acceptance string    New acceptance criteria
  --blocks string        Replace blocked issues
  --depends-on string    Replace dependency issues  
  --description string   New description
  --labels string        Replace labels (comma-separated)
  --parent string        New parent issue ID
  --points int           New story points
  --priority string      New priority (P0-P4)
  --title string         New title
  --type string          New type (bug, feature, task, epic, chore)
```

Note: Status changes use dedicated commands (`start`, `block`, `review`, `approve`, `reject`), not `--status` flag.

```bash
td update td-5q --labels auth,urgent,backend
# Output: UPDATED td-5q
```

### `td delete [issue-id...]`

Soft-delete one or more issues.

```bash
td delete td-5q td-6r
# Output:
# DELETED td-5q
# DELETED td-6r
```

### `td restore [issue-id...]`

Restore one or more soft-deleted issues. Use `td deleted` to see recoverable issues.

```bash
td restore td-5q
# Output: RESTORED td-5q
```

---

## Work Session Commands

These commands track the working state of issues and enforce the review workflow.

### `td start [issue-id] [--reason "text"] [--force]`

Begin work on an issue. Records current session as implementer and captures git state.

```bash
td start td-5q --reason "Starting OAuth implementation"
# Output:
# STARTED td-5q (session: ses_a1b2c3)
# Git: abc1234 (main) clean
```

If working tree is dirty:

```bash
td start td-5q
# Output:
# STARTED td-5q (session: ses_a1b2c3)
# Git: abc1234 (main) 2 modified, 1 untracked
# Warning: Starting with uncommitted changes
```

- Cannot start blocked issues without `--force`
- Sets status to `in_progress`
- Records session ID as implementer
- Records git commit SHA, branch, and dirty file count

### `td log "message"`

Append a log entry to the currently focused issue. Low-friction progress tracking during a session.

```bash
td log "Set up OAuth callback route"
# Output: LOGGED td-5q

td log "Token storage working, tested manually"
# Output: LOGGED td-5q

td log --blocker "Unsure about refresh token expiry handling"
# Output: LOGGED td-5q [blocker]
```

**Structured reasoning traces** for capturing debugging/exploration process:

```bash
td log --hypothesis "Token refresh failing due to clock skew"
# Output: LOGGED td-5q [hypothesis]

td log --tried "Added 30s buffer to expiry check"
# Output: LOGGED td-5q [tried]

td log --result "Fixed for local, need to verify prod"
# Output: LOGGED td-5q [result]
```

**Flags:**

```txt
  --blocker     Mark this log entry as a blocker/uncertainty
  --decision    Mark as a decision made
  --hypothesis  Mark as a hypothesis being tested
  --tried       Mark as an approach that was attempted
  --result      Mark as the outcome of a hypothesis/attempt
  --issue       Specify issue ID (default: focused issue)
```

Logs are timestamped and attached to the current session. The `hypothesis → tried → result` pattern captures debugging reasoning for future sessions.

### `td handoff [issue-id] [flags]`

Capture structured working state. Required before `td review` or when stopping work. Automatically captures git state.

```bash
td handoff td-5q << EOF
done:
  - OAuth callback route implemented
  - Token storage in SQLite
remaining:
  - Error handling for expired tokens
  - Unit tests
  - Refresh token flow
decisions:
  - Using JWT (not opaque tokens) for stateless verification
  - 1 hour token expiry
uncertain:
  - Should refresh tokens be one-time use?
EOF
# Output:
# HANDOFF RECORDED td-5q
# Git: def5678 (feature/oauth) +3 commits since start
# Changed: 4 files (+156 -23)
```

**Alternative flag syntax:**

```bash
td handoff td-5q \
  --done "OAuth callback route" \
  --done "Token storage" \
  --remaining "Error handling" \
  --remaining "Unit tests" \
  --decision "Using JWT tokens" \
  --uncertain "Refresh token rotation?"
```

**Flags:**

```txt
  --done string        Completed item (repeatable)
  --remaining string   Remaining item (repeatable)
  --decision string    Decision made (repeatable)
  --uncertain string   Uncertainty/question (repeatable)
```

Handoff state is versioned—each handoff creates a snapshot, previous handoffs are preserved in history.

Git state is captured automatically:
- Current commit SHA and branch
- Commits since `td start`
- Changed files with line counts (staged + unstaged)
- Uncommitted changes warning if working tree is dirty

### `td review [issue-id] [--reason "text"]`

Submit issue for review. **Requires handoff first.**

```bash
td review td-5q --reason "Ready for review, all acceptance criteria met"
# Output: REVIEW REQUESTED td-5q (session: ses_a1b2c3)
```

- Fails if no handoff exists
- Sets status to `in_review`
- Records implementer session (blocks that session from approving)

### `td approve [issue-id] [--reason "text"]`

Approve and close an issue. **Must be different session than implementer.**

```bash
td approve td-5q --reason "Verified OAuth flow works, tests pass"
# Output: APPROVED td-5q (reviewer: ses_x7y8z9)
```

- Fails if current session is the implementer
- Sets status to `closed`
- Records reviewer session

### `td reject [issue-id] [--reason "text"]`

Reject and return to `in_progress`. Issue stays assigned to original implementer.

```bash
td reject td-5q --reason "Token refresh not handling network errors"
# Output: REJECTED td-5q → in_progress
```

- Returns to `in_progress` status
- Adds rejection reason to log
- Original implementer can address and re-submit

### `td block [issue-id...] [--reason "text"]`

Mark issue(s) as blocked.

```bash
td block td-5q --reason "Waiting on API design review"
# Output: BLOCKED td-5q
```

### `td reopen [issue-id...] [--reason "text"]`

Reopen closed issues. Requires new review cycle.

```bash
td reopen td-5q --reason "Regression found in production"
# Output: REOPENED td-5q → open
```

---

## File Linking

Associate files with issues for context tracking and change detection.

### `td link [issue-id] [file-pattern] [flags]`

Link files to an issue with an optional role.

```bash
td link td-5q src/auth/*.go --role implementation
# Output: LINKED 3 files to td-5q

td link td-5q docs/oauth.md --role reference
# Output: LINKED 1 file to td-5q

td link td-5q internal/tokens/ --role implementation
# Output: LINKED 5 files to td-5q
```

**Flags:**

```txt
  --role string    File role: implementation, test, reference, config (default: implementation)
  --recursive      Include subdirectories (default for directories)
```

### `td unlink [issue-id] [file-pattern]`

Remove file associations.

```bash
td unlink td-5q src/auth/deprecated.go
# Output: UNLINKED 1 file from td-5q
```

### `td files [issue-id]`

List linked files with change status since issue was started.

```bash
td files td-5q
# Output:
# td-5q: Implement OAuth flow
# Started: abc1234 (2h ago)
#
# IMPLEMENTATION:
#   src/auth/oauth.go        [modified]  +45 -12
#   src/auth/tokens.go       [modified]  +23 -0
#   src/auth/middleware.go   [unchanged]
#
# TEST:
#   src/auth/oauth_test.go   [new]       +120
#
# REFERENCE:
#   docs/oauth.md            [unchanged]
#
# UNTRACKED CHANGES:
#   src/auth/util.go         [modified]  +8 -2  (not linked)
```

**Flags:**

```txt
  --json          Machine-readable output
  --changed       Only show files with changes
```

Linked files are tracked with their SHA at link time. `td files` compares against current state to show drift.

---

## Focus Mode

Track current working issue across commands.

### `td focus [issue-id]`

Set the current working issue.

```bash
td focus td-5q
# Output: FOCUSED td-5q
```

### `td unfocus`

Clear focus.

```bash
td unfocus
# Output: UNFOCUSED
```

### `td current`

Show focused issue, active work, and pending reviews.

```bash
td current
# Output:
# FOCUSED: td-5q  Implement OAuth flow  P1  5pts  [in_progress]
#
# IN PROGRESS (this session):
#   td-5q  Implement OAuth flow       P1  5pts
#
# AWAITING YOUR REVIEW:
#   td-3a  Fix login redirect         P2  3pts  (impl: ses_d4e5f6)
#   td-7b  Add rate limiting          P1  5pts  (impl: ses_g7h8i9)
```

**Flags:**

```txt
  --json        Machine-readable JSON output
```

---

## Work Sessions

Work sessions provide an alternative entry point for agents working across multiple issues. Issues remain the source of truth; sessions collect work and fan out to issues on handoff.

```
┌─────────────────────────────────────────────────┐
│  Work Session (agent entry point)               │
│  - Collects logs, progress, decisions           │
│  - Tags multiple issues as relevant             │
└───────────────┬─────────────────────────────────┘
                │ fans out on handoff
                ▼
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│  Issue td-5q    │  │  Issue td-6r    │  │  Issue td-7s    │
│  - handoff      │  │  - handoff      │  │  - handoff      │
│  - logs         │  │  - logs         │  │  - logs         │
└─────────────────┘  └─────────────────┘  └─────────────────┘
```

### `td ws start [name]`

Start a named work session. Aliases: `td session start`, `td worksession start`.

```bash
td ws start "Auth layer implementation"
# Output:
# WORK SESSION STARTED: ws-7f3a
# Name: Auth layer implementation
#
# Tag issues with: td ws tag <issue-ids>
# Log progress with: td ws log "message"
```

Automatically runs `td start` on tagged issues when they're added.

### `td ws tag [issue-ids...]`

Associate issues with the current work session.

```bash
td ws tag td-5q
# Output: TAGGED td-5q → ws-7f3a
# STARTED td-5q (session: ses_a1b2c3)

# Discover related work mid-session
td ws tag td-6r td-7s
# Output:
# TAGGED td-6r → ws-7f3a
# STARTED td-6r (session: ses_a1b2c3)
# TAGGED td-7s → ws-7f3a
# STARTED td-7s (session: ses_a1b2c3)
```

### `td ws untag [issue-ids...]`

Remove issues from work session (doesn't change issue status).

```bash
td ws untag td-7s
# Output: UNTAGGED td-7s from ws-7f3a
```

### `td ws log "message" [flags]`

Log to the work session. Entry is attached to the session AND all tagged issues.

```bash
td ws log "Discovered OAuth and session mgmt share token storage"
# Output: LOGGED ws-7f3a → td-5q, td-6r

td ws log --hypothesis "Token refresh failing due to shared state"
# Output: LOGGED ws-7f3a [hypothesis] → td-5q, td-6r
```

**Flags:** Same as `td log` (`--blocker`, `--decision`, `--hypothesis`, `--tried`, `--result`)

To log to a specific issue only:

```bash
td ws log "OAuth-specific: Added Google provider" --only td-5q
# Output: LOGGED ws-7f3a → td-5q
```

### `td ws current`

Show current work session state.

```bash
td ws current
# Output:
# WORK SESSION: ws-7f3a "Auth layer implementation"
# Started: 2h ago
# Git: abc1234 → def5678 (+5 commits)
#
# TAGGED ISSUES:
#   td-5q  Implement OAuth flow      [in_progress]  P1  5pts
#   td-6r  Session management        [in_progress]  P2  3pts
#
# SESSION LOG (last 5):
#   [10:30] Started session
#   [10:35] Tagged td-5q, td-6r
#   [10:45] "Discovered shared token storage"
#   [11:00] [hypothesis] "Token refresh failing due to shared state"
#   [11:30] [result] "Confirmed - extracted shared TokenStore"
#
# FILES CHANGED:
#   internal/auth/oauth.go      +45 -12
#   internal/auth/session.go    +23 -8
#   internal/auth/tokens.go     +89 (new)
```

**Flags:**

```txt
  --json        Machine-readable output
```

### `td ws handoff [flags]`

End work session and generate handoffs for all tagged issues.

```bash
td ws handoff << EOF
done:
  - Unified token storage for OAuth and sessions
  - Shared TokenStore middleware extracted
  - OAuth callback working
remaining:
  - OAuth refresh flow (td-5q)
  - Session expiry handling (td-6r)
decisions:
  - Single token store, not per-auth-method
  - JWT for OAuth, opaque for sessions
uncertain:
  - Token rotation policy across both systems
EOF
# Output:
# HANDOFF RECORDED ws-7f3a
# Generated handoffs:
#   td-5q: done=3, remaining=1
#   td-6r: done=2, remaining=1
#
# Cross-references added to both issues.
# Work session ended.
```

**Issue-specific remaining items:** Tag items with `(td-xxx)` to route them to specific issues. Untagged items go to all.

**Flags:**

```bash
td ws handoff --continue   # Record handoff but keep session open
td ws handoff --review     # Handoff and submit all tagged issues for review
```

### `td ws end`

End work session without handoff (for abandoning work).

```bash
td ws end
# Output:
# WARNING: No handoff recorded for ws-7f3a
# Tagged issues remain in_progress: td-5q, td-6r
# WORK SESSION ENDED
```

### `td ws list`

List recent work sessions.

```bash
td ws list
# Output:
# ws-7f3a  "Auth layer implementation"     2h ago   td-5q,td-6r    [active]
# ws-6e2b  "API refactor"                  1d ago   td-3a,td-4b    [completed]
# ws-5d1a  "Bug fixes"                     3d ago   td-1x          [completed]
```

### `td ws show [session-id]`

Show details of a past work session.

```bash
td ws show ws-6e2b
# Output:
# WORK SESSION: ws-6e2b "API refactor"
# Duration: 3h (1d ago)
# Git: aaa1111 → bbb2222 (+8 commits)
#
# TAGGED ISSUES:
#   td-3a  Refactor endpoints    [closed]  ✓
#   td-4b  Update API docs       [closed]  ✓
#
# HANDOFF SUMMARY:
#   Done: REST endpoints consolidated, OpenAPI spec updated
#   Decisions: Kept v1 compat, added deprecation warnings
#
# Full log: td ws show ws-6e2b --full
```

---

## Listing and Search

### `td list [filters] [flags]`

List issues matching given filters.

**Aliases:** `td ls`

**Filters:**

```txt
  -i, --id stringArray         Filter by specific issue IDs
  -l, --labels stringArray     Filter by labels
  -s, --status stringArray     Status filter (open, in_progress, blocked, in_review, closed)
  -t, --type stringArray       Issue type filter
  -p, --priority string        Priority filter (P0, P1-P4, <=P2, >=P1)
      --points string          Story points filter (1,2,3,5,8,13,21, >=8, <=5)
      --created string         Created date or range
      --updated string         Updated date or range
      --closed string          Closed date or range
  -q, --search string          Search title, description, logs
      --implementer string     Filter by implementer session
      --reviewer string        Filter by reviewer session
      --reviewable             Show issues current session can review
```

**Output:**

```txt
  --long        Detailed multi-line output
  --short       One-line compact output (default)
  --json        Machine-readable JSON output
```

**Sorting / Paging:**

```txt
  --sort string       Sort by: priority,points,created,updated,status,id,title
  -r, --reverse       Reverse sort order
  -n, --limit int     Limit results (default 50)
```

### `td reviewable`

Alias for `td list --status in_review --reviewable`. Shows issues awaiting review that current session can review.

```bash
td reviewable
# Output:
# td-3a  Fix login redirect    P2  3pts  feature  (impl: ses_d4e5f6)
# td-7b  Add rate limiting     P1  5pts  task     (impl: ses_g7h8i9)
```

### `td blocked`

Alias for `td list --status blocked`.

### `td ready`

Alias for `td list --status open --sort priority`.

### `td next`

Show highest-priority open issue.

```bash
td next
# Output:
# td-5q  [P1]  Implement OAuth flow  5pts  feature
#
# Run `td start td-5q` to begin working on this issue.
```

---

## Dependency Graph

### `td blocked-by [issue-id]`

Show what issues are waiting on this issue (directly or transitively).

```bash
td blocked-by td-5q
# Output:
# td-5q: Implement OAuth flow [in_progress]
# └── blocks:
#     td-8c: Add protected routes [open]
#     └── blocks:
#         td-9d: User dashboard [open]
#         td-10e: Admin panel [open]
#
# 3 issues blocked (1 direct, 2 transitive)
```

**Flags:**

```txt
  --direct      Only show direct dependencies (no transitive)
  --json        Machine-readable output
```

### `td depends-on [issue-id]`

Show what issues this issue depends on.

```bash
td depends-on td-8c
# Output:
# td-8c: Add protected routes [open]
# └── depends on:
#     td-5q: Implement OAuth flow [in_progress]
#     td-2a: Session middleware [closed] ✓
#
# 1 blocking, 1 resolved
```

### `td critical-path`

Show the sequence of issues that unblocks the most work.

```bash
td critical-path
# Output:
# CRITICAL PATH (unblocks 12 issues):
#
# 1. td-5q  Implement OAuth flow       [in_progress]  P1  5pts
#    └─▶ unblocks 3
# 2. td-8c  Add protected routes       [open]         P1  3pts
#    └─▶ unblocks 2
# 3. td-9d  User dashboard             [open]         P2  8pts
#    └─▶ unblocks 4
#
# BOTTLENECKS (blocking most issues):
#   td-5q: 6 issues waiting (direct + transitive)
#   td-3a: 4 issues waiting
#   td-8c: 3 issues waiting
```

**Flags:**

```txt
  --limit int   Max issues to show (default: 10)
  --json        Machine-readable output
```

---

### `td search [query] [filters] [flags]`

Full-text search across title, description, logs, and handoff content.

```bash
td search "token refresh"
td search "OAuth" --status in_progress
```

### `td deleted`

Show soft-deleted issues for recovery with `td restore`.

**Flags:**

```txt
  --json        Machine-readable JSON output
```

---

## Context and Handoff Commands

### `td context [issue-id]`

Generate contextual summary for resuming work. If no issue specified, uses focused issue.

```bash
td context td-5q
# Output:
# CONTEXT: td-5q "Implement OAuth flow"
#
# LATEST HANDOFF (ses_a1b2c3, 2h ago):
#   Done: OAuth callback, token storage
#   Remaining: Error handling, tests, refresh flow
#   Decisions: JWT tokens, 1h expiry
#   Uncertain: Refresh token rotation
#
# FILES TOUCHED:
#   routes/auth.go
#   db/tokens.go
#   db/migrations/003_tokens.sql
#
# SESSION LOG (last 5):
#   [10:45] Set up OAuth callback route
#   [11:15] Token storage implemented
#   [11:30] BLOCKER: Unsure about refresh token expiry
#   [11:45] Decision: 1 hour expiry, revisit if issues
#   [12:00] Submitted for review
#
# BLOCKED BY: nothing
# BLOCKS: td-8c "Add protected routes"
```

**Flags:**

```txt
  --full        Include complete session history
  --json        Machine-readable output
```

### `td resume [issue-id]`

Shortcut: show context and set focus.

```bash
td resume td-5q
# Output: [context output]
# FOCUSED td-5q
```

---

## AI Integration

### `td usage`

Generate optimized context block for AI agents. Includes:

1. Current session identity and active work session (if any)
2. Focused issue with handoff state
3. Issues awaiting review (that this session can review)
4. High-priority open issues
5. Command reference

```bash
td usage
# Output:
# You have access to `td`, a local task management CLI.
#
# CURRENT SESSION: ses_a1b2c3
# WORK SESSION: ws-7f3a "Auth layer implementation" (2 issues tagged)
#
# FOCUSED ISSUE: td-5q "Implement OAuth flow" [in_progress]
#   Last handoff (2h ago):
#     Done: OAuth callback route, token storage
#     Remaining: Error handling, tests
#     Uncertain: Refresh token rotation policy
#   Files: routes/auth.go, db/tokens.go
#
# AWAITING YOUR REVIEW (2 issues):
#   td-3a "Fix login redirect" P2 - impl by different session
#   td-7b "Add rate limiting" P1 - impl by different session
#
# READY TO START (3 issues):
#   td-9d "Add logout endpoint" P1 feature
#   td-2e "Update auth docs" P3 task
#
# WORKFLOWS:
#
#   Single-issue:
#     1. `td start <id>` to begin work
#     2. `td log "message"` to track progress
#     3. `td handoff <id>` to capture state (REQUIRED)
#     4. `td review <id>` to submit for review
#
#   Multi-issue (recommended for agents):
#     1. `td ws start "name"` to begin work session
#     2. `td ws tag <ids>` to associate issues as you discover them
#     3. `td ws log "message"` to track progress (fans out to all tagged)
#     4. `td ws handoff` to capture state and end session
#
# KEY COMMANDS:
#   td current              What you're working on
#   td ws current           Current work session state
#   td context <id>         Full context for resuming
#   td next                 Highest priority open issue
#   td critical-path        What unblocks the most work
#   td reviewable           Issues you can review
#   td ws log "msg"         Track progress (multi-issue)
#   td ws handoff           Capture state, end session
#   td approve/reject <id>  Complete review
#
# IMPORTANT: You cannot approve issues you implemented.
# Use `td handoff` or `td ws handoff` before stopping work.
```

**Flags:**

```txt
  --compact     Shorter output for smaller context windows
  --json        Machine-readable output
```

---

## Hierarchy & Relationships

### `td tree [issue-id]`

Visualize parent/child relationships.

```bash
td tree td-1a
# Output:
# td-1a Epic: User Authentication
# ├── td-2b Feature: Login flow [closed] ✓
# │   ├── td-3c Task: OAuth integration [in_review] ⧗
# │   └── td-4d Task: Password reset [open]
# └── td-5e Feature: Session management [in_progress] ●
```

**Flags:**

```txt
  --depth int     Max depth (default: unlimited)
  --json          Machine-readable output
```

---

## Comments

### `td comments [issue-id]`

List comments for an issue.

### `td comments add [issue-id] "text"`

Add a comment.

```bash
td comments add td-5q "Completed OAuth provider integration"
# Output: COMMENT ADDED td-5q
```

Supports file input and heredoc:

```bash
td comments add td-5q < notes.txt
td comments add td-5q << EOF
Multi-line comment here.
EOF
```

---

## Project Info

### `td info` (alias: `td stats`)

Show database statistics and project overview.

```bash
td info
# Output:
# Project: my-app
# Database: .todos/issues.db
# Current Session: ses_a1b2c3
#
# Issues: 47 total
#   Open:        20
#   In Progress:  3
#   Blocked:      2
#   In Review:    4
#   Closed:      18
#
# Review Queue:
#   Awaiting review: 4
#   You can review:  2
#
# By Type:
#   bug:      8
#   feature: 15
#   task:    20
#   epic:     4
```

---

## System Commands

### `td upgrade`

Update td and run migrations.

### `td version`

Show version.

### `td export [flags]`

Export database.

```txt
  --format string    json (default) or md
  --output string    File path (default: stdout)
  --all              Include closed/deleted
```

### `td import [file] [flags]`

Import issues.

```txt
  --format string    json (default) or md
  --dry-run          Preview changes
  --force            Overwrite existing
```

---

## JSON Output

Most read commands support `--json`. Mutation commands emit stable text:

```txt
CREATED td-ab12
STARTED td-ab12 (session: ses_a1b2c3)
LOGGED td-ab12
HANDOFF RECORDED td-ab12
REVIEW REQUESTED td-ab12 (session: ses_a1b2c3)
APPROVED td-ab12 (reviewer: ses_x7y8z9)
REJECTED td-ab12 → in_progress
BLOCKED td-ab12
UPDATED td-ab12
DELETED td-ab12
RESTORED td-ab12
```

**Example JSON (td show --json):**

```json
{
  "id": "td-5q",
  "title": "Implement OAuth flow",
  "status": "in_review",
  "type": "feature",
  "priority": "P1",
  "points": 5,
  "labels": ["auth", "backend"],
  "implementer_session": "ses_a1b2c3",
  "reviewer_session": null,
  "handoff": {
    "timestamp": "2025-01-15T12:00:00Z",
    "session": "ses_a1b2c3",
    "done": ["OAuth callback route", "Token storage"],
    "remaining": ["Error handling", "Unit tests"],
    "decisions": ["Using JWT tokens", "1 hour expiry"],
    "uncertain": ["Refresh token rotation policy"],
    "files": ["routes/auth.go", "db/tokens.go"]
  },
  "logs": [
    {"timestamp": "2025-01-15T10:45:00Z", "message": "Set up OAuth callback route", "type": "progress"},
    {"timestamp": "2025-01-15T11:15:00Z", "message": "Token storage implemented", "type": "progress"},
    {"timestamp": "2025-01-15T11:30:00Z", "message": "Unsure about refresh token expiry", "type": "blocker"}
  ],
  "created_at": "2025-01-15T10:00:00Z",
  "updated_at": "2025-01-15T12:00:00Z"
}
```

---

## Error Handling

```
ERROR: issue not found: td-xyz1
ERROR: cannot approve own implementation: td-5q (implemented by current session)
ERROR: handoff required before review: td-5q
ERROR: cannot start blocked issue: td-5q (use --force to override)
```

With `--json`:

```json
{
  "error": {
    "code": "cannot_self_approve",
    "message": "cannot approve own implementation: td-5q"
  }
}
```

Error codes: `not_found`, `invalid_input`, `conflict`, `cannot_self_approve`, `handoff_required`, `database_error`

---

## Filter Syntax Reference

### Date Ranges

Date filters (`--created`, `--updated`, `--closed`) support multiple formats:

```bash
# After a date
td list --created "2025-01-01.."
td list --created "after:2025-01-01"

# Before a date
td list --created "..2025-12-31"
td list --created "before:2025-12-31"

# Date range (inclusive)
td list --created "2025-01-01..2025-12-31"

# Specific date (entire day)
td list --created "2025-01-15"
```

### Priority Ranges

```bash
td list --priority P1           # Exact match
td list --priority "<=P2"       # P0, P1, P2
td list --priority ">=P1"       # P1, P2, P3, P4
```

### Points Ranges

```bash
td list --points 5              # Exact match
td list --points ">=8"          # 8, 13, 21
td list --points "<=5"          # 1, 2, 3, 5
td list --points "1-5"          # 1, 2, 3, 5
```

---

## Issue Fields Reference

| Field | Description | Values |
|-------|-------------|--------|
| `status` | Issue state | `open`, `in_progress`, `blocked`, `in_review`, `closed` |
| `type` | Issue category | `bug`, `feature`, `task`, `epic`, `chore` |
| `priority` | Urgency level | `P0` (critical), `P1` (high), `P2` (medium), `P3` (low), `P4` (none) |
| `points` | Complexity estimate | Fibonacci: `1`, `2`, `3`, `5`, `8`, `13`, `21` |
| `labels` | Tags | Comma-separated strings |
| `parent` | Hierarchy | Issue ID reference |
| `blocks` | Dependency | Issue IDs this blocks |
| `depends_on` | Dependency | Issue IDs this depends on |
| `acceptance` | Criteria | Acceptance criteria text |
| `description` | Details | Description text |
| `implementer_session` | Tracking | Session that worked on issue |
| `reviewer_session` | Tracking | Session that reviewed issue |

---

## Implementation Notes

* **Session tracking**: Session ID generated per terminal session, stored in `.todos/session`. Can be named with `td session`.
* **Handoff versioning**: Each `td handoff` creates a new snapshot. Previous handoffs preserved in `handoff_history` table.
* **Review enforcement**: `implementer_session` column on issues. `td approve` checks `current_session != implementer_session`.
* **Logs**: Append-only `logs` table with `issue_id`, `session_id`, `work_session_id`, `timestamp`, `message`, `type` (progress/blocker/decision/hypothesis/tried/result).
* **Focus state**: `.todos/config.json`
* **Issue IDs**: Hash-based, 4-6 characters.
* **Work session IDs**: `ws-` prefix, 4 characters hash.
* **File linking**: `issue_files` table with `issue_id`, `file_path`, `role`, `linked_sha`, `linked_at`. SHA compared against current for change detection.
* **Git state**: `git_snapshots` table with `issue_id`, `event` (start/handoff), `commit_sha`, `branch`, `dirty_files`, `timestamp`.
* **Work sessions**: `work_sessions` table with `id`, `name`, `session_id`, `started_at`, `ended_at`, `start_sha`, `end_sha`. Junction table `work_session_issues` links sessions to issues with `tagged_at`.

---

## Quick Reference

| Action | Command |
|--------|---------|
| Start work | `td start td-5q` |
| Log progress | `td log "message"` |
| Log blocker | `td log --blocker "stuck on X"` |
| Log hypothesis | `td log --hypothesis "might be X"` |
| Log attempt | `td log --tried "tried Y"` |
| Log result | `td log --result "Y worked"` |
| Capture state | `td handoff td-5q` |
| Submit for review | `td review td-5q` |
| Approve (reviewer) | `td approve td-5q` |
| Reject (reviewer) | `td reject td-5q` |
| See reviewable | `td reviewable` |
| Resume work | `td resume td-5q` |
| Get context | `td context td-5q` |
| AI context | `td usage` |
| What's next | `td next` |
| Current focus | `td current` |
| List issues | `td list` or `td ls` |
| Link files | `td link td-5q src/*.go` |
| View linked files | `td files td-5q` |
| What's blocked by this | `td blocked-by td-5q` |
| What this depends on | `td depends-on td-5q` |
| Critical path | `td critical-path` |
| Start work session | `td ws start "name"` |
| Tag issue to session | `td ws tag td-5q` |
| Log to session | `td ws log "message"` |
| View session state | `td ws current` |
| End session + handoff | `td ws handoff` |
| List work sessions | `td ws list` |
| Delete issue | `td delete td-5q` |
| Restore deleted | `td restore td-5q` |
| View deleted | `td deleted` |

---

## Tech Stack

**Language:** Go

**Core Libraries:**
- `github.com/spf13/cobra` — CLI framework
- `modernc.org/sqlite` — Pure Go SQLite
- `github.com/charmbracelet/lipgloss` — Terminal styling
- `github.com/spf13/viper` — Config management

**Project Structure:**
```
td/
├── cmd/           # Cobra commands
├── internal/
│   ├── db/        # SQLite operations
│   ├── models/    # Issue, Log, Handoff structs
│   ├── session/   # Session ID management
│   └── output/    # JSON/text formatters
├── main.go
└── go.mod
```