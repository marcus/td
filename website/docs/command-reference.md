---
sidebar_position: 11
---

# Command Reference

Complete reference for all `td` commands.

> Dedicated pages cover boards, dependencies, epics, file tracking, analytics, work sessions, and the monitor UI in more detail. This page stays focused on the command surface.

## Core Commands

| Command | Description |
|---------|-------------|
| `td create "title" [flags]` | Create an issue. Flags: `--type`, `--priority`, `--points`, `--labels`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--parent`, `--epic`, `--minor`, `--defer`, `--due`, `--depends-on`, `--blocks` |
| `td task create "title" [flags]` | Create a task. Alias for `td create --type task`. Uses the same creation flags plus `--title`, `--priority`, `--description`, `--labels`, `--parent`, `--epic`, `--depends-on`, `--blocks`, and `--minor` |
| `td task list [flags]` | List tasks. Alias for `td list --type task` |
| `td list [flags]` | List issues. Flags: `--id`, `--status`, `--type`, `--labels`, `--priority`, `--points`, `--search`, `--implementer`, `--reviewer`, `--reviewable`, `--parent`, `--epic`, `--mine`, `--open`, `--created`, `--updated`, `--closed`, `--sort`, `--reverse`, `--limit`, `--format`, `--json`, `--long`, `--short`, `--all`, `--deferred`, `--overdue`, `--surfacing`, `--due-soon`, `--filter` |
| `td show <id> [flags]` | Display issue details. Flags: `--json`, `--format json`, `--short`, `--long`, `--children`, `--tree`, `--render-markdown` |
| `td update <id> [flags]` | Update an issue. Flags: `--title`, `--type`, `--priority`, `--points`, `--labels`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--sprint`, `--parent`, `--depends-on`, `--blocks`, `--append`, `--status`, `--comment`, `--note`, `--defer`, `--due` |
| `td delete <id>` | Soft-delete one or more issues. Flags: `--force`, `--yes` |
| `td restore <id>` | Restore soft-deleted issues |

### Examples

```bash
td create "Add user auth" --type feature --priority P1
td task create "Fix login bug" --priority P1
td show td-a1b2 --children
```

## Workflow Commands

| Command | Description |
|---------|-------------|
| `td start <id...> [flags]` | Begin work and record the current session. Flags: `--force`, `--reason` |
| `td unstart <id...> [flags]` | Revert issues from `in_progress` back to `open`. Flag: `--reason` |
| `td log [issue-id] "message" [flags]` | Append a progress log. Flags: `--issue`, `--task`, `--type`, `--blocker`, `--decision`, `--hypothesis`, `--tried`, `--result` |
| `td handoff <id> [message] [flags]` | Capture structured working state. Flags: `--done`, `--remaining`, `--decision`, `--uncertain`, `--note`, `--message` |
| `td review <id...> [flags]` | Submit issues for review. Flags: `--reason`, `--message`, `--comment`, `--note`, `--notes`, `--minor`, `--json` |
| `td approve <id...> [flags]` | Approve and close reviewable issues. Flags: `--reason`, `--message`, `--comment`, `--note`, `--notes`, `--all`, `--json` |
| `td reject <id...> [flags]` | Reject and return issues to `open`. Flags: `--reason`, `--message`, `--comment`, `--note`, `--notes`, `--json` |
| `td close <id...> [flags]` | Close issues without review. Flags: `--reason`, `--comment`, `--message`, `--note`, `--notes`, `--self-close-exception` |
| `td reopen <id...> [flags]` | Reopen closed issues. Flag: `--reason` |
| `td block <id...> [flags]` | Mark issues as blocked. Flag: `--reason` |
| `td unblock <id...> [flags]` | Clear blocked status. Flag: `--reason` |
| `td comment <id> "text"` | Add a comment to an issue |
| `td comments <id>` | List comments for an issue |
| `td comments add <id> "text"` | Add a comment using the nested command form |

### Examples

```bash
td start td-a1b2 --reason "Picked up after review"
td handoff td-a1b2 --done "Implemented auth" --remaining "Write tests"
td review td-a1b2 --reason "Ready for review"
td approve td-a1b2 --reason "Looks good"
```

## Listing, Query & Search

| Command | Description |
|---------|-------------|
| `td next` | Show the highest-priority open issue |
| `td ready` | List open issues sorted by priority |
| `td blocked` | List blocked issues |
| `td in-review` | List issues currently in review |
| `td reviewable` | List issues you can review |
| `td deleted` | Show soft-deleted issues |
| `td query "expression" [flags]` | Search with TDQ. Flags: `--output`, `--limit`, `--sort`, `--explain`, `--examples`, `--fields` |
| `td search "keyword" [flags]` | Full-text search across issues, logs, comments, and handoffs. Flags: `--status`, `--type`, `--labels`, `--priority`, `--limit`, `--json`, `--show-score` |
| `td list [flags]` | Main listing command. Use `--filter` for TDQ expressions or `--status`, `--type`, `--labels`, `--priority`, `--sort`, `--reverse`, `--json`, `--long`, `--short`, `--all`, `--deferred`, `--overdue`, `--surfacing`, or `--due-soon` |

### Examples

```bash
td list --reviewable --mine
td query "status = open AND priority <= P1"
td search "auth" --labels backend --show-score
```

## Deferral & Due Dates

| Command | Description |
|---------|-------------|
| `td defer <id> <date>` | Defer an issue until a future date |
| `td defer <id> --clear` | Remove deferral and make the issue actionable |
| `td due <id> <date>` | Set a due date on an issue |
| `td due <id> --clear` | Remove a due date |

Date formats include `+7d`, `+2w`, `+1m`, `monday`, `tomorrow`, `next-week`, `next-month`, and ISO dates such as `2026-03-15`.

The `--defer` and `--due` flags are also available on `td create` and `td update`.

**List filters:** `--all` (include deferred), `--deferred`, `--surfacing`, `--overdue`, `--due-soon`

### Examples

```bash
td defer td-a1b2 monday
td due td-a1b2 2026-03-15
td update td-a1b2 --defer +7d --due friday
```

## Agent-Safe Rich Text Input

Use `--description-file` and `--acceptance-file` for markdown-heavy fields so shells do not mangle code fences, quotes, or blank lines. Pass `-` to read the full field from stdin.

```bash
td create "Document sync failure modes" \
  --description-file docs/issue-description.md \
  --acceptance-file docs/issue-acceptance.md

cat docs/acceptance.md | td update td-a1b2 --append --acceptance-file -
```

## Dependencies

| Command | Description |
|---------|-------------|
| `td blocked-by <issue>` | Show issues waiting on this issue |
| `td depends-on <issue>` | Show what this issue depends on |
| `td dep <issue>` | Manage dependencies between issues |
| `td dep <issue> --blocking` | Show what depends on this issue |
| `td dep add <issue> <depends-on...>` | Add one or more dependencies |
| `td dep rm <issue> <depends-on>` | Remove a dependency |
| `td critical-path` | Show the sequence that unblocks the most work |

### Examples

```bash
td dep add td-a1b2 td-c3d4 td-e5f6
td dep td-a1b2 --blocking
td critical-path --limit 5
```

## Boards

| Command | Description |
|---------|-------------|
| `td board create "name" --query "..."` | Create a board from a TDQ query |
| `td board list` | List boards |
| `td board show <board>` | Show issues in a board |
| `td board edit <board> [flags]` | Edit board name, query, or view mode. Flags: `--name`, `--query`, `--view-mode` |
| `td board move <board> <id> <pos>` | Set an issue's position on a board |
| `td board unposition <board> <id>` | Remove an explicit board position |
| `td board delete <board>` | Delete a board |

## Epics & Trees

| Command | Description |
|---------|-------------|
| `td epic create "title" [flags]` | Create an epic. Flags: `--title`, `--priority`, `--description`, `--labels`, `--parent`, `--epic`, `--depends-on`, `--blocks` |
| `td epic list` | List epics |
| `td tree <id> [flags]` | Show a parent/child tree. Flags: `--depth`, `--json` |

Use `td show --children` when you want the same hierarchy inline on a single issue page.

## Sessions & Context

| Command | Description |
|---------|-------------|
| `td status [flags]` | Show the session dashboard. Flag: `--json` |
| `td focus <id>` | Set the current working issue |
| `td unfocus` | Clear focus |
| `td check-handoff [flags]` | Check whether handoff is needed. Flags: `--quiet`, `--json` |
| `td resume <id>` | Show issue context and set focus |
| `td usage [flags]` | Generate an optimized AI context block. Flags: `--compact`, `-q`, `--json`, `--new-session` |
| `td whoami` | Show current session identity |
| `td session [name] [flags]` | Name the current session or create a new one. Flag: `--new` |
| `td session list` | List recent sessions |
| `td session cleanup` | Clean up old sessions. Flags: `--older-than`, `--force` |

## Work Sessions

| Command | Description |
|---------|-------------|
| `td ws start "name"` | Start a named work session |
| `td ws tag <ids...>` | Associate issues with the current work session. Flag: `--no-start` |
| `td ws untag <ids...>` | Remove issues from the work session |
| `td ws log "message"` | Log to the work session and tagged issues |
| `td ws current` | Show the current work session state |
| `td ws handoff` | End the work session and generate handoffs |
| `td ws end` | End the work session without handoff |
| `td ws list` | List recent work sessions |
| `td ws show [session-id]` | Show a past work session |

### Examples

```bash
td ws start "core fix"
td ws tag td-a1b2 td-c3d4
td ws log "Verified the hot path"
td ws handoff
```

## Files

| Command | Description |
|---------|-------------|
| `td link <id> <files...>` | Link files to an issue. Flags: `--role`, `--recursive`, `--depends-on` |
| `td unlink <id> <file-pattern>` | Remove file associations |
| `td files <id> [flags]` | List linked files and status. Flags: `--json`, `--changed`, `--untracked` |

## Notes

| Command | Description |
|---------|-------------|
| `td note add <title> [flags]` | Create a note. Flag: `--content` |
| `td note list [flags]` | List notes. Flags: `--pinned`, `--archived`, `--all`, `--search`, `--limit`, `--output`, `--json` |
| `td note show <id> [flags]` | Display a note. Flag: `--json` |
| `td note edit <id> [flags]` | Edit a note. Flags: `--title`, `--content` |
| `td note delete <id>` | Soft-delete a note |
| `td note pin <id>` | Pin a note |
| `td note unpin <id>` | Unpin a note |
| `td note archive <id>` | Archive a note |
| `td note unarchive <id>` | Unarchive a note |

### Examples

```bash
td note add "Architecture decisions" --content "Use a simple queue."
td note list --pinned --search auth
td note edit nt-abc123 --title "Updated title"
```

## Project Sync

> **Note**: `td auth`, `td config`, `td feature`, `td sync-project`, and `td sync` are available when the sync CLI feature is enabled.

| Command | Description |
|---------|-------------|
| `td sync-project create <name> [flags]` | Create a remote sync project. Flag: `--description` |
| `td sync-project join [name-or-id]` | Join a remote sync project interactively or by name/ID |
| `td sync-project link <project-id> [flags]` | Link the local project to a remote sync project. Flag: `--force` |
| `td sync-project unlink [flags]` | Unlink the local project from remote sync. Flag: `--force` |
| `td sync-project list` | List remote sync projects |
| `td sync-project members` | List project members |
| `td sync-project invite <email> [role]` | Invite a user. Role defaults to `writer` |
| `td sync-project kick <user-id>` | Remove a member from the project |
| `td sync-project role <user-id> <role>` | Change a member's role |

### Examples

```bash
td auth login
td sync-project create "My Team" --description "Shared backlog"
td sync-project link 123e4567-e89b-12d3-a456-426614174000
td sync-project members
```

## Auth, Config & Feature Flags

| Command | Description |
|---------|-------------|
| `td auth login` | Log in to the sync server |
| `td auth logout` | Log out from the sync server |
| `td auth status` | Show authentication status |
| `td config set <key> <value>` | Set a sync config value |
| `td config get <key>` | Get a sync config value |
| `td config list` | List all sync config values |
| `td config associate [dir] <target>` | Associate a directory with a td project |
| `td config associations` | List directory associations |
| `td config dissociate [dir]` | Remove a directory association |
| `td feature list` | List feature flags and resolved state |
| `td feature get <name>` | Get a feature flag state |
| `td feature set <name> true/false` | Set a feature flag override |
| `td feature unset <name>` | Remove a feature flag override |

Config keys currently supported by `td config`:

`sync.url`, `sync.enabled`, `sync.auto.enabled`, `sync.auto.debounce`, `sync.auto.interval`, `sync.auto.pull`, `sync.auto.on_start`, `sync.snapshot_threshold`

## Sync & Server

| Command | Description |
|---------|-------------|
| `td sync [flags]` | Sync local data with the remote server. Flags: `--push`, `--pull`, `--status` |
| `td sync init` | Interactive guided setup for sync |
| `td sync conflicts [flags]` | Show recent sync conflicts. Flags: `--limit`, `--since` |
| `td sync tail [flags]` | Show recent sync activity. Flags: `-f/--follow`, `-n/--lines` |
| `td serve [flags]` | Start the HTTP API server. Flags: `--port`, `--addr`, `--token`, `--cors`, `--interval` |

### Examples

```bash
td sync --status
td sync tail -f -n 0
td serve --port 0 --addr localhost
```

## Diagnostics & Stats

| Command | Description |
|---------|-------------|
| `td doctor` | Run diagnostic checks for sync setup |
| `td debug-stats` | Output runtime memory and goroutine statistics as JSON |
| `td errors [flags]` | View failed command attempts. Flags: `--clear`, `--count`, `--limit`, `--session`, `--since`, `--json` |
| `td security [flags]` | View security exception log. Flags: `--clear`, `--json` |
| `td stats analytics [flags]` | View command usage analytics. Flags: `--clear`, `--json`, `--since`, `--limit` |
| `td stats errors [flags]` | Alias for `td errors` |
| `td stats security [flags]` | Alias for `td security` |
| `td monitor [flags]` | Live TUI dashboard. Flag: `--interval` |
| `td workflow [flags]` | Show the issue status workflow. Flags: `--mermaid`, `--dot` |
| `td undo [flags]` | Undo the last action. Flag: `--list` |
| `td last` | Show the last action performed |

## System Utilities

| Command | Description |
|---------|-------------|
| `td init` | Initialize a new td project |
| `td info [flags]` | Show database statistics and project overview. Flag: `--json` |
| `td version [flags]` | Show the version and check for updates. Flags: `--check`, `--short` |
| `td export [flags]` | Export issues and related data. Flags: `--format`, `--output`, `--all`, `--render-markdown` |
| `td import [flags]` | Import issues. Flags: `--format`, `--dry-run`, `--force` |
| `td upgrade` | Run database migrations |
