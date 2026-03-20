# td Quick Reference

## Common Commands

### Getting Started
- `td usage` - See current state, pending reviews, and next steps
- `td usage -q` - Compact view (use after first read)
- `td init` - Initialize td in a new project

### Single-Issue Workflow
- `td start <id>` - Begin work on an issue
- `td unstart <id>` - Revert to open (undo accidental start)
- `td log "message"` - Track progress
- `td log --decision "chose X because Y"` - Log a decision
- `td log --blocker "stuck on X"` - Log a blocker
- `td handoff <id> --done "..." --remaining "..."` - Capture state before context ends
- `td review <id>` - Submit for review
- `td approve <id>` - Approve (different session only)
- `td reject <id> --reason "..."` - Reject back to author

### Multi-Issue Workflow
- `td ws start "name"` - Start a work session for multiple issues
- `td ws tag <id1> <id2>` - Associate issues with work session (auto-starts open issues)
- `td ws tag --no-start <id>` - Associate without starting
- `td ws log "message"` - Log to all tagged issues
- `td ws handoff` - Capture state and end session
- `td ws current` - See current work session state

### Issue Management
- `td create "title" --type feature --priority P1` - Create issue
- `td list` - List all issues
- `td list --status in_progress` - Filter by status
- `td show <id>` - View issue details
- `td next` - Highest priority open issue
- `td critical-path` - What unblocks the most work
- `td reviewable` - Issues you can review
- `td due <id> <date>` - Set a due date on an issue
- `td due <id> --clear` - Remove due date
- `td defer <id> <date>` - Defer an issue until a future date
- `td defer <id> --clear` - Remove deferral
- `td close <id>` - Close without review
- `td reopen <id>` - Reopen a closed issue
- `td restore <id>` - Restore a soft-deleted issue

### Notes
- `td note add "title"` - Create a new note
- `td note list` - List notes
- `td note show <id>` - Display a note
- `td note edit <id>` - Edit a note
- `td note delete <id>` - Delete a note (soft-delete)
- `td note pin <id>` - Pin a note
- `td note unpin <id>` - Unpin a note
- `td note archive <id>` - Archive a note
- `td note unarchive <id>` - Unarchive a note

### Boards
- `td board create "name"` - Create a new board
- `td board list` - List all boards
- `td board show <name>` - Show issues in a board
- `td board edit <name>` - Edit a board's name or query
- `td board delete <name>` - Delete a board
- `td board move <issue> <position>` - Set an issue's position on a board
- `td board unposition <issue>` - Remove an issue's explicit position

### Dependencies
- `td dep add <issue> <depends-on>` - Add a dependency
- `td dep rm <issue> <depends-on>` - Remove a dependency
- `td dep <issue>` - Show what an issue depends on
- `td dep <issue> --blocking` - Show what depends on an issue
- `td depends-on <id>` - Show what an issue depends on (alias)
- `td blocked-by <id>` - Show what issues are waiting on this issue

### Work Session Subcommands
- `td ws start "name"` - Start a work session
- `td ws tag <id1> <id2>` - Associate issues (auto-starts open issues)
- `td ws tag --no-start <id>` - Associate without starting
- `td ws untag <id>` - Remove issue from work session
- `td ws log "message"` - Log to all tagged issues
- `td ws handoff` - End session with handoffs for all tagged issues
- `td ws end` - End work session without handoff
- `td ws current` - Show current work session state
- `td ws show <id>` - Show details of a past work session
- `td ws list` - List recent work sessions

### File Tracking
- `td link <id> <files...>` - Track files with an issue
- `td files <id>` - Show file changes (modified, new, deleted, unchanged)

### Session & Focus
- `td session --new "name"` - Force new named session
- `td focus <id>` - Set the current working issue
- `td unfocus` - Clear focus
- `td resume <id>` - Show context and set focus
- `td context <id>` - Full context for resuming
- `td whoami` - Show current session identity

### Utilities
- `td monitor` - Live TUI dashboard
- `td undo` - Undo last action
- `td block <id>` - Mark issue as blocked
- `td unblock <id>` - Unblock issue back to open
- `td delete <id>` - Soft-delete an issue
- `td deleted` - Show soft-deleted issues
- `td errors` - View failed td command attempts
- `td errors --since 24h` - Errors from last 24 hours
- `td search "query"` - Full-text search across issues
- `td query "TDQ expression"` - Search with TDQ query language
- `td tree` - Visualize parent/child relationships
- `td export` - Export database

## Issue Statuses

```
open → in_progress → in_review → closed
         |              |
         v              | (reject)
     blocked -----------+
```

## Key Concepts

**Sessions** - Every terminal/context gets an auto ID. Session that starts work ≠ session that reviews.

**Work Sessions (ws)** - Optional container for grouping related issues. Useful for agents handling multiple issues.

**Handoffs** - Critical for agent handoffs. Use `--done`, `--remaining`, `--decision`, `--uncertain` to pass structured state.
