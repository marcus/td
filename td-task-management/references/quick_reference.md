# td Quick Reference

## Common Commands

### Getting Started
- `td usage --new-session -q` - Start a new agent context with compact current state
- `td usage` - See current state with workflow guidance
- `td init` - Initialize td in a new project

### Single-Issue Workflow
- `td start <id>` - Begin work on an issue
- `td unstart <id>` - Revert to open (undo accidental start)
- `td log "message"` - Track progress
- `td log --decision "chose X because Y"` - Log a decision
- `td log --blocker "stuck on X"` - Log a blocker
- `td handoff <id> --done "..." --remaining "..."` - Capture state for another context
- `td review <id>` - Submit for review
- `td approve <id> --reason "..."` - Approve an independent review
- `td approve <id> --self-review --reason "..."` - Record a trusted-mode self-review
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
- `td create "title" --description-file body.md --acceptance-file acceptance.md` - Create with rich markdown safely
- `cat body.md | td update <id> --append --description-file -` - Append rich markdown from stdin
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

**Sessions** - Every terminal/context gets an auto ID so implementation and review remain attributable.

**Work Sessions (ws)** - Optional container for grouping related issues. Useful for agents handling multiple issues.

**Handoffs** - Use `--done`, `--remaining`, `--decision`, and `--uncertain` when another context will continue the work.
