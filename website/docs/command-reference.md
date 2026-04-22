---
sidebar_position: 11
---

# Command Reference

Complete reference for all `td` commands.

## Core Commands

| Command | Description |
|---------|-------------|
| `td create "title" [flags]` | Create issue. Flags: `--type`, `--priority`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--parent`, `--epic`, `--minor` |
| `td list [flags]` | List issues. Flags: `--status`, `--type`, `--priority`, `--epic` |
| `td show <id>` | Display full issue details |
| `td update <id> [flags]` | Update fields. Flags: `--title`, `--type`, `--priority`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--labels` |
| `td delete <id>` | Soft-delete issue |
| `td restore <id>` | Restore soft-deleted issue |

## Workflow Commands

| Command | Description |
|---------|-------------|
| `td start <id>` | Begin work (status -> in_progress) |
| `td unstart <id>` | Revert to open |
| `td log "message" [flags]` | Log progress. Flags: `--decision`, `--blocker`, `--hypothesis`, `--tried`, `--result` |
| `td handoff <id> [flags]` | Capture state. Flags: `--done`, `--remaining`, `--decision`, `--uncertain` |
| `td review <id>` | Submit for review. Submitting session is recorded as `review_requested_by_session` |
| `td reviewable [--include-approved]` | Show issues you can review; with `--include-approved`, also show reviewed issues you can close |
| `td approve <id> [flags]` | Approve and close, record-only review, or close using a recorded approval. Flags: `--reason`, `--record-only`, `--decision approved\|changes_requested`, `--all` |
| `td reject <id> --reason "..."` | Reject back to open. Supersedes any active approval review |
| `td block <id>` | Mark as blocked |
| `td unblock <id>` | Unblock to open |
| `td close <id>` | Admin close only (duplicates, won't-fix, cleanup). Use `td approve` for reviewed work |
| `td reopen <id>` | Reopen closed issue |
| `td comment <id> "text"` | Add comment |

## Deferral & Due Dates

| Command | Description |
|---------|-------------|
| `td defer <id> <date>` | Defer issue until a future date |
| `td defer <id> --clear` | Remove deferral, make immediately actionable |
| `td due <id> <date>` | Set due date on an issue |
| `td due <id> --clear` | Remove due date |

Date formats: `+7d`, `+2w`, `+1m`, `monday`, `tomorrow`, `next-week`, `next-month`, `2026-03-15`

The `--defer` and `--due` flags are also available on `td create` and `td update`.

**List filters:** `--all` (include deferred), `--deferred`, `--surfacing`, `--overdue`, `--due-soon`

## Review Flag Details

`td approve` operates in three modes under `review_policy_mode=delegated`:

| Invocation | Effect |
|------------|--------|
| `td approve <id>` | Direct reviewer-close: caller must be an eligible reviewer with no active approval recorded |
| `td approve <id> --record-only --reason "..."` | Record an approval review without closing. Caller must be an eligible reviewer |
| `td approve <id> --record-only --decision changes_requested --reason "..."` | Record a non-approving review |
| `td approve <id>` (with existing approval) | Close using a recorded approval. Caller must be an allowed closer (creator, implementer, review-requester, or reviewer-of-record) |

`td reviewable --include-approved` surfaces reviewed issues the current session is allowed to close — useful for orchestrators that delegated review to a sub-agent.

Under `strict` and `balanced` modes, `--record-only` and `--decision` are unavailable; `td approve` performs a review-and-close in one step.

## Agent-Safe Rich Text Input

Use `--description-file` and `--acceptance-file` for markdown-heavy fields so shells do not mangle code fences, quotes, or blank lines. Pass `-` to read the full field from stdin.

```bash
td create "Document sync failure modes" \
  --description-file docs/issue-description.md \
  --acceptance-file docs/issue-acceptance.md

cat docs/acceptance.md | td update td-a1b2 --append --acceptance-file -
```

## Query & Search

| Command | Description |
|---------|-------------|
| `td query "expression"` | TDQ query |
| `td search "keyword"` | Full-text search |
| `td next` | Highest-priority open issue |
| `td ready` | Open issues by priority |
| `td blocked` | List blocked issues |
| `td in-review` | List in-review issues |

## Dependencies

| Command | Description |
|---------|-------------|
| `td dep add <issue> <depends-on>` | Add dependency |
| `td dep rm <issue> <depends-on>` | Remove dependency |
| `td dep <issue>` | Show dependencies |
| `td dep <issue> --blocking` | Show what it blocks |
| `td blocked-by <issue>` | Issues blocked by this |
| `td critical-path` | Optimal unblocking sequence |

## Boards

| Command | Description |
|---------|-------------|
| `td board create "name" --query "..."` | Create board |
| `td board list` | List boards |
| `td board show <board>` | Show board |
| `td board move <board> <id> <pos>` | Position issue |
| `td board edit <board> [flags]` | Edit board |
| `td board delete <board>` | Delete board |

## Epics & Trees

| Command | Description |
|---------|-------------|
| `td epic create "title" [flags]` | Create epic |
| `td epic list` | List epics |
| `td tree <id>` | Show tree |
| `td tree add-child <parent> <child>` | Add child |

## Sessions

| Command | Description |
|---------|-------------|
| `td usage [flags]` | Agent context. Flags: `--new-session`, `-q` |
| `td session [name]` | Name session |
| `td session --new` | Force new session |
| `td status` | Dashboard view |
| `td focus <id>` | Set focus |
| `td unfocus` | Clear focus |
| `td whoami` | Show session identity |

## Work Sessions

| Command | Description |
|---------|-------------|
| `td ws start "name"` | Start work session |
| `td ws tag <ids...>` | Tag issues |
| `td ws log "message"` | Log to all tagged |
| `td ws current` | Show session state |
| `td ws handoff` | Handoff all, end session |

## Files

| Command | Description |
|---------|-------------|
| `td link <id> <files...>` | Link files to issue |
| `td unlink <id> <files...>` | Unlink files |
| `td files <id>` | Show file status |

## System

| Command | Description |
|---------|-------------|
| `td init` | Initialize project |
| `td monitor` | Live TUI dashboard |
| `td undo` | Undo last action |
| `td version` | Show version |
| `td export` | Export database |
| `td import` | Import issues |
| `td stats [subcommand]` | Usage statistics |
