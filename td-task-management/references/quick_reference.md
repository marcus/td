# td Quick Reference

## Common Commands

### Getting Started
- `td usage` - See current state, pending reviews, and next steps
- `td usage -q` - Compact view (use after first read)
- `td init` - Initialize td in a new project
- `td status` - Dashboard: session, focus, reviews, blocked, ready issues
- `td info` - Database statistics and project overview
- `td whoami` - Show current session identity

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
- `td ws untag <id>` - Remove issue from work session
- `td ws log "message"` - Log to all tagged issues
- `td ws handoff` - Capture state and end session
- `td ws end` - End work session without handoff
- `td ws current` - See current work session state
- `td ws list` - List recent work sessions
- `td ws show [session-id]` - Show details of a past work session

### Issue Management
- `td create "title" --type feature --priority P1` - Create issue
- `td update <id> --title "..." --priority P2` - Update issue fields
- `td list` - List all issues
- `td list --status in_progress` - Filter by status
- `td show <id>` - View issue details
- `td next` - Highest priority open issue
- `td critical-path` - What unblocks the most work
- `td reviewable` - Issues you can review
- `td blocked` - List blocked issues
- `td in-review` - List issues currently in review
- `td ready` - Open issues sorted by priority
- `td search "query"` - Full-text search across issues
- `td query "TDQ expression"` - Search with TDQ query language

### Focus
- `td focus <id>` - Set current working issue
- `td unfocus` - Clear focus

### Due Dates & Deferral
- `td due <id> <date>` - Set a due date on an issue
- `td defer <id> <date>` - Defer an issue until a future date

### Status Changes
- `td block <id>` - Mark issue as blocked
- `td unblock <id>` - Unblock issue back to open
- `td reopen <id>` - Reopen a closed issue
- `td delete <id>` - Soft-delete an issue
- `td restore <id>` - Restore a soft-deleted issue
- `td deleted` - Show soft-deleted issues

### Notes
- `td note add "title"` - Create a new note
- `td note list` - List notes
- `td note show <id>` - Display a note
- `td note edit <id>` - Edit a note
- `td note delete <id>` - Soft-delete a note
- `td note pin <id>` - Pin a note
- `td note unpin <id>` - Unpin a note
- `td note archive <id>` - Archive a note
- `td note unarchive <id>` - Unarchive a note

### Boards
- `td board list` - List all boards
- `td board create "name"` - Create a new board
- `td board show <board>` - Show issues in a board
- `td board edit <board>` - Edit a board's name or query
- `td board delete <board>` - Delete a board
- `td board move <board> <id> <position>` - Set issue position on a board
- `td board unposition <board> <id>` - Remove explicit position

### Dependencies
- `td dep add <id> <depends-on>...` - Add dependencies (issue depends on others)
- `td dep rm <id> <depends-on>` - Remove a dependency
- `td depends-on <id>` - Show what an issue depends on
- `td blocked-by <id>` - Show what issues are waiting on this issue
- `td critical-path` - Sequence of issues that unblocks the most work

### Epics & Tasks
- `td epic create "title"` - Create a new epic
- `td epic list` - List all epics
- `td task create "title"` - Create a new task
- `td task list` - List all tasks
- `td tree <id>` - Visualize parent/child relationships

### Comments
- `td comment <id> "text"` - Add a comment to an issue
- `td comments list <id>` - List comments for an issue

### File Tracking
- `td link <id> <files...>` - Track files with an issue
- `td unlink <id> <file>` - Remove file association
- `td files <id>` - Show file changes (modified, new, deleted, unchanged)

### Sessions
- `td session --new "name"` - Force new named session
- `td sessions` - List all sessions (branch + agent scoped)
- `td check-handoff` - Check if handoff is needed before exiting

### Diagnostics & Utilities
- `td context <id>` - Full context for resuming
- `td monitor` - Live TUI dashboard
- `td undo` - Undo last action
- `td last` - Show the last action performed
- `td workflow` - Show issue status workflow diagram
- `td doctor` - Run diagnostic checks for sync setup
- `td errors` - View failed td command attempts
- `td stats` - Usage statistics and error counts
- `td security` - View security exception log
- `td version` - Show version info

### Configuration
- `td config list` - List all config values
- `td config get <key>` - Get a config value
- `td config set <key> <value>` - Set a config value
- `td config associate <dir> <target>` - Associate directory with a td project
- `td config dissociate <dir>` - Remove directory association

### Feature Flags
- `td feature list` - List feature flags and their state
- `td feature get <name>` - Get a feature flag
- `td feature set <name> <true|false>` - Set a feature flag
- `td feature unset <name>` - Remove a local override

### Sync
- `td sync` - Push and pull changes with remote server
- `td sync --push` - Push only
- `td sync --pull` - Pull only
- `td sync --status` - Check sync state
- `td sync init` - Interactive guided sync setup
- `td tail` - Show recent sync activity
- `td conflicts` - Show recent sync conflicts

### Sync Project Management
- `td sync-project create "name"` - Create remote project
- `td sync-project join [name]` - Join a project interactively
- `td sync-project link <id>` - Link local project to remote (scripting)
- `td sync-project unlink` - Unlink local project
- `td sync-project list` - List remote projects
- `td sync-project members` - List project members
- `td sync-project invite <email> [role]` - Invite a user
- `td sync-project kick <user-id>` - Remove a member
- `td sync-project role <user-id> <role>` - Change member role

### Auth
- `td auth login` - Log in to sync server
- `td auth logout` - Log out
- `td auth status` - Show authentication status

### Import/Export
- `td export` - Export database
- `td import` - Import issues

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

**Notes** - Freeform documents not tied to a specific issue. Pin important notes, archive old ones.

**Boards** - Custom views for organizing issues. Each board has a query filter and manual positioning.

**Dependencies** - Track which issues block others. `td critical-path` finds the sequence that unblocks the most work.
