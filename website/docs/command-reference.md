---
sidebar_position: 13
---

# Command Reference

Complete reference for the `td` CLI, including notes, work sessions, diagnostics, and the feature-gated sync workflow.

> **Note**: `td auth`, `td config`, `td doctor`, `td sync`, and `td sync-project` are registered only when `sync_cli` is enabled for the current process. Use `export TD_ENABLE_FEATURE=sync_cli` in your shell, or prefix one command with `TD_FEATURE_SYNC_CLI=true`. `td feature set sync_cli true` updates project config, but it does not expose these commands by itself.

## Core Issue Commands

| Command | Description |
|---------|-------------|
| `td create "title" [flags]` | Create an issue. Common flags: `--type`, `--priority`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--parent`, `--epic`, `--minor`, `--defer`, `--due` |
| `td list [flags]` | List issues. Common filters: `--status`, `--type`, `--priority`, `--epic`, `--all`, `--deferred`, `--surfacing`, `--overdue`, `--due-soon` |
| `td show <id>` | Display full details for one or more issues |
| `td update <id> [flags]` | Update issue fields, including rich text fields from files or stdin |
| `td delete <id>` | Soft-delete one or more issues |
| `td restore <id>` | Restore one or more soft-deleted issues |
| `td deleted` | List soft-deleted issues |
| `td task create "title"` | Create a task shortcut (`--type task`) |
| `td task list` | List task issues only |
| `td epic create "title"` | Create an epic |
| `td epic list` | List epics |

## Workflow Commands

| Command | Description |
|---------|-------------|
| `td start <id>` | Move issue to `in_progress` |
| `td unstart <id>` | Move issue back to `open` |
| `td block <id>` | Mark one or more issues blocked |
| `td unblock <id>` | Return blocked issues to `open` |
| `td review <id>` | Submit one or more issues for review |
| `td approve <id> [--reason "..."]` | Approve and close issues |
| `td reject <id> --reason "..."` | Reject issues back to `open` |
| `td close <id>` | Close issues directly without review |
| `td reopen <id>` | Reopen closed issues |
| `td handoff <id> [flags]` | Capture done/remaining/decision/uncertain state before ending a session |
| `td resume [id]` | Show context and set focus |
| `td log "message" [flags]` | Add progress, blocker, decision, hypothesis, tried, or result logs |
| `td comment <id> "text"` | Add a comment shortcut |
| `td comments <id>` | List comments for an issue |
| `td comments add <id> "text"` | Add a comment via the subcommand form |
| `td workflow` | Print the issue state workflow summary |
| `td check-handoff` | Exit non-zero when the current focus still needs a handoff |

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
| `td dep add <issue> <depends-on>...` | Add dependencies |
| `td dep rm <issue> <depends-on>` | Remove dependency |
| `td dep <issue>` | Show dependencies |
| `td dep <issue> --blocking` | Show what it blocks |
| `td blocked-by <issue>` | Issues blocked by this |
| `td critical-path` | Optimal unblocking sequence |

## Boards, Trees, And Epics

| Command | Description |
|---------|-------------|
| `td board create "name" --query "..."` | Create board |
| `td board list` | List boards |
| `td board show <board>` | Show board results, optionally filtered by status |
| `td board move <board> <id> <pos>` | Position issue |
| `td board unposition <board> <id>` | Remove a board-specific position override |
| `td board edit <board> [flags]` | Edit board |
| `td board delete <board>` | Delete board |
| `td tree <id>` | Show the issue tree, with optional `--depth` or `--json` |

## Sessions

| Command | Description |
|---------|-------------|
| `td usage [flags]` | Agent context. Flags: `--new-session`, `-q` |
| `td session [name]` | Name session |
| `td session --new` | Force new session |
| `td session list` | List branch- and agent-scoped sessions |
| `td session cleanup` | Remove stale session files |
| `td status` | Dashboard view |
| `td focus <id>` | Set focus |
| `td unfocus` | Clear focus |
| `td whoami` | Show session identity |

## Work Sessions

| Command | Description |
|---------|-------------|
| `td ws start "name"` | Start work session |
| `td ws tag <ids...>` | Tag issues |
| `td ws tag <ids...> --no-start` | Tag issues without auto-starting open work |
| `td ws untag <ids...>` | Remove issues from the active work session |
| `td ws log "message"` | Log to all tagged |
| `td ws current` | Show session state |
| `td ws handoff` | Handoff all, end session |
| `td ws end` | End the current work session without generating handoffs |
| `td ws list` | List recent work sessions |
| `td ws show <session-id>` | Show a past work session |

## Notes

| Command | Description |
|---------|-------------|
| `td note add "title"` | Create a freeform note |
| `td note list` | List notes with filters such as `--pinned`, `--archived`, `--all`, `--search`, `--json` |
| `td note show <id>` | Show a note |
| `td note edit <id>` | Edit a note title or content |
| `td note pin <id>` / `td note unpin <id>` | Pin or unpin a note |
| `td note archive <id>` / `td note unarchive <id>` | Archive or restore a note |
| `td note delete <id>` | Soft-delete a note |

## Files

| Command | Description |
|---------|-------------|
| `td link <id> <files...>` | Link files or globs to an issue |
| `td unlink <id> <files...>` | Remove linked files |
| `td files <id>` | Show linked files and change status |

## System

| Command | Description |
|---------|-------------|
| `td init` | Initialize project |
| `td monitor` | Live TUI dashboard |
| `td serve` | Start the td HTTP API server |
| `td info` | Show project overview and database stats |
| `td stats analytics` | Show command usage analytics |
| `td stats errors` / `td errors` | Show failed command attempts |
| `td stats security` / `td security` | Show workflow exception audit events |
| `td undo` | Undo last action |
| `td last` | Show the last action performed |
| `td version` | Show version |
| `td export` | Export database |
| `td import [file]` | Import issues |
| `td upgrade` | Run database migrations |
| `td feature list` | List experimental features and resolved state |
| `td feature get <name>` | Get one feature flag |
| `td feature set <name> <true\|false>` | Set a project-level feature override |
| `td feature unset <name>` | Remove a project-level feature override |
| `td debug-stats` | Print runtime memory and goroutine stats as JSON |

## Feature-Gated Config Helpers

The current CLI hides the entire `td config` command family behind `sync_cli`, so these commands use the same env-based enablement as sync.

| Command | Description |
|---------|-------------|
| `td config set <key> <value>` | Set a global config value such as `sync.url` or autosync settings |
| `td config get <key>` | Read one config value |
| `td config list` | Print the global config as JSON |
| `td config associate [dir] <project>` | Associate a directory with a td project root |
| `td config associations` | List directory-to-project associations |
| `td config dissociate [dir]` | Remove a directory association |

## Feature-Gated Sync & Collaboration

These commands are available only when `sync_cli` is enabled through an environment override for the current process. See [Sync & Collaboration](./sync-collaboration.md) for setup and troubleshooting.

| Command | Description |
|---------|-------------|
| `td auth login` | Authenticate with a sync server |
| `td auth status` | Show current auth status |
| `td auth logout` | Remove local credentials |
| `td doctor` | Run sync diagnostics |
| `td sync init` | Guided sync setup after you have already logged in |
| `td sync` | Push then pull changes |
| `td sync --push` | Push only |
| `td sync --pull` | Pull only |
| `td sync --status` | Show local and server sync state |
| `td sync conflicts` | Show recent overwrite conflicts |
| `td sync tail` | Show recent sync activity, optionally with `-f` |
| `td sync-project create "name"` | Create and link a remote sync project |
| `td sync-project join [name-or-id]` | Join an invited project by picker, name, or ID |
| `td sync-project link <project-id>` | Link directly to a known project ID |
| `td sync-project unlink` | Remove the local remote-project link |
| `td sync-project list` | List available remote projects |
| `td sync-project members` | List members on the linked project |
| `td sync-project invite <email> [role]` | Invite a collaborator as `writer`, `reader`, or `owner` |
| `td sync-project role <user-id> <role>` | Change a member role |
| `td sync-project kick <user-id>` | Remove a member |

## Agent-Safe Rich Text Input

Use `--description-file` and `--acceptance-file` for markdown-heavy content so your shell does not mangle code fences, quotes, or blank lines. Pass `-` to read the field from stdin.

```bash
td create "Document sync failure modes" \
  --description-file docs/issue-description.md \
  --acceptance-file docs/issue-acceptance.md

cat docs/acceptance.md | td update td-a1b2 --append --acceptance-file -
```
