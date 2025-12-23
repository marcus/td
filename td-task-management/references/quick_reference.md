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

### File Tracking
- `td link <id> <files...>` - Track files with an issue
- `td files <id>` - Show file changes (modified, new, deleted, unchanged)

### Other
- `td context <id>` - Full context for resuming
- `td monitor` - Live dashboard of activity
- `td session --new "name"` - Force new named session
- `td undo` - Undo last action
- `td block <id>` - Mark issue as blocked
- `td delete <id>` - Delete issue

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
