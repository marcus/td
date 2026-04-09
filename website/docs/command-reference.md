---
sidebar_position: 11
---

# Command Reference

Complete reference for the `td` CLI.

> **Note**: Most commands honor `--work-dir`. Commands in the sync section are feature-gated and appear when `sync_cli` is enabled.

## Core Commands

| Command | Description |
|---------|-------------|
| `td board` | Manage issue boards. Subcommands: `create`, `delete`, `edit`, `list`, `move`, `show`, `unposition` |
| `td create`, `td add`, `td new` | Create a new issue. Key flags: `--type`, `--priority`, `--labels`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--parent`, `--epic`, `--depends-on`, `--blocks`, `--minor`, `--defer`, `--due` |
| `td delete` | Soft-delete one or more issues |
| `td epic` | Convenience commands for epics. Subcommands: `create`, `list` |
| `td list`, `td ls` | List issues matching filters |
| `td note` | Manage freeform notes. Subcommands: `add`, `archive`, `delete`, `edit`, `list`, `pin`, `show`, `unarchive`, `unpin` |
| `td restore` | Restore soft-deleted issues |
| `td show`, `td context`, `td view`, `td get` | Display full details of one or more issues |
| `td task` | Convenience commands for tasks. Subcommands: `create`, `list` |
| `td update`, `td edit` | Update one or more fields on existing issues |

## Issue Commands

### Create

`td create` accepts a title plus a broad set of field flags.

```bash
td create "Add user auth" --type feature --priority P1
td create "Rich markdown issue" --description-file description.md --acceptance-file acceptance.md
cat acceptance.md | td create "Import from stdin" --acceptance-file -
```

Common flags:

| Flag | Description |
|------|-------------|
| `--title` | Explicit issue title |
| `--type`, `-t` | Issue type: `bug`, `feature`, `task`, `epic`, `chore` |
| `--priority`, `-p` | Priority: `P0` through `P4` |
| `--points` | Fibonacci story points |
| `--labels`, `-l` | Repeatable labels list |
| `--label`, `--tags`, `--tag` | Aliases for `--labels` |
| `--description`, `-d` | Description text |
| `--desc`, `--body`, `--notes` | Aliases for `--description` |
| `--description-file` | Read description from a file or `-` for stdin |
| `--acceptance` | Acceptance criteria |
| `--acceptance-file` | Read acceptance criteria from a file or `-` for stdin |
| `--parent` | Parent issue ID |
| `--epic` | Alias for `--parent` |
| `--depends-on` | Issues this issue depends on |
| `--blocks` | Issues this issue blocks |
| `--minor` | Mark as minor task and allow self-review |
| `--defer` | Defer until a future date |
| `--due` | Set a due date |

### Update

`td update` replaces fields on one or more existing issues.

```bash
td update td-a1b2 --priority P1 --description "Short inline note"
td update td-a1b2 --description-file description.md
cat acceptance.md | td update td-a1b2 --append --acceptance-file -
```

Common flags:

| Flag | Description |
|------|-------------|
| `--title` | New title |
| `--type` | New issue type |
| `--priority` | New priority |
| `--points` | New story points |
| `--labels`, `-l` | Replace labels |
| `--description`, `-d` | New description |
| `--desc`, `--body` | Aliases for `--description` |
| `--description-file` | Read description from a file or `-` for stdin |
| `--acceptance` | New acceptance criteria |
| `--acceptance-file` | Read acceptance criteria from a file or `-` for stdin |
| `--append` | Append instead of replace for text fields |
| `--parent` | New parent issue ID |
| `--depends-on` | Replace dependencies |
| `--blocks` | Replace blocked issues |
| `--defer` | Set or clear deferral |
| `--due` | Set or clear due date |
| `--status` | New status |
| `--sprint` | New sprint name |
| `--comment`, `-m` | Add a comment while updating |

### List

`td list` is the main issue query command. It supports direct filters and TDQ query expressions.

```bash
td list --status open --type bug --priority P1
td list --filter 'status = open AND type = bug'
td list --json --limit 20
```

Common flags:

| Flag | Description |
|------|-------------|
| `--all`, `-a` | Include closed and deferred issues |
| `--filter`, `-f` | TDQ expression |
| `--format` | `short`, `long`, or `json` |
| `--json` | JSON output |
| `--long` | Detailed multiline output |
| `--short` | Compact output |
| `--status`, `-s` | Status filter |
| `--type`, `-t` | Type filter |
| `--id`, `-i` | Issue ID filter |
| `--labels`, `-l` | Label filter |
| `--priority`, `-p` | Priority filter |
| `--points` | Point-range filter |
| `--search`, `-q` | Search title/description |
| `--implementer` | Filter by implementer session |
| `--reviewer` | Filter by reviewer session |
| `--parent` | Filter by parent issue |
| `--epic` | Filter by epic |
| `--reviewable` | Issues you can review |
| `--mine`, `-m` | Issues implemented by the current session |
| `--open`, `-o` | Only open issues |
| `--deferred` | Deferred issues only |
| `--overdue` | Overdue issues only |
| `--surfacing` | Issues resurfacing after deferral |
| `--due-soon` | Issues due soon |
| `--created` | Created date filter |
| `--updated` | Updated date filter |
| `--closed` | Closed date filter |
| `--sort` | Sort by field |
| `--reverse`, `-r` | Reverse sort order |
| `--limit`, `-n` | Max results |
| `--no-pager` | Disable paging |

### Show

`td show` displays full issue details.

```bash
td show td-abc1
td show td-abc1 --children
td show td-abc1 td-abc2 --json
```

Common flags:

| Flag | Description |
|------|-------------|
| `--children` | Show child issues inline |
| `--tree` | Render issue tree with descendants |
| `--json` | Machine-readable JSON |
| `--long` | Detailed multiline output |
| `--short` | Compact summary |
| `--render-markdown`, `-m` | Render markdown in descriptions |
| `--format` | Output format |

## Workflow Commands

| Command | Description |
|---------|-------------|
| `td approve` | Approve and close one or more issues |
| `td block` | Mark issue(s) as blocked |
| `td close`, `td done`, `td complete` | Close one or more issues without review |
| `td comment` | Add a comment to an issue |
| `td comments` | List comments for an issue |
| `td comments add` | Add a comment to an issue |
| `td defer` | Defer an issue until a future date |
| `td dep` | Manage dependencies between issues |
| `td due` | Set a due date on an issue |
| `td handoff` | Capture structured working state |
| `td log` | Append a log entry to the current issue |
| `td reject` | Reject and return to open |
| `td reopen` | Reopen closed issues |
| `td review`, `td submit`, `td finish` | Submit one or more issues for review |
| `td start`, `td begin` | Begin work on issue(s) |
| `td unblock` | Unblock issue(s) back to open status |
| `td unstart`, `td stop` | Revert issue(s) from in_progress to open |

### Workflow Examples

```bash
td start td-abc1
td log td-abc1 "Finished the API handler"
td handoff td-abc1 --done "Implemented login" --remaining "Add tests"
td review td-abc1 --reason "Ready for review"
td approve td-abc1 --reason "Looks good to me"
```

Common flags:

| Command | Flags |
|---------|-------|
| `td start` | `--force`, `--reason` |
| `td unstart` | `--reason` |
| `td block` | `--reason` |
| `td unblock` | `--reason` |
| `td reopen` | `--reason` |
| `td log` | `--issue`, `-i`, `--task`, `-T`, `--type`, `--blocker`, `--decision`, `--hypothesis`, `--tried`, `--result` |
| `td handoff` | `--done`, `--remaining`, `--decision`, `--uncertain`, `--message`, `--note` |
| `td review` | `--reason`, `-m`, `--comment`, `--message`, `--note`, `--notes`, `--minor`, `--json` |
| `td approve` | `--all`, `--reason`, `-m`, `--comment`, `--message`, `--note`, `--notes`, `--json` |
| `td reject` | `--reason`, `-m`, `--comment`, `--message`, `--note`, `--notes`, `--json` |
| `td close` | `--reason`, `-m`, `--comment`, `--message`, `--note`, `--notes`, `--self-close-exception` |
| `td defer` | `--clear` |
| `td due` | `--clear` |

## Query & Search

| Command | Description |
|---------|-------------|
| `td blocked-by` | Show what issues are waiting on this issue |
| `td critical-path` | Show the sequence of issues that unblocks the most work |
| `td depends-on`, `td deps`, `td dependencies` | Show what issues this issue depends on |
| `td query` | Search issues with TDQ query language |
| `td search` | Full-text search across issues |
| `td tree` | Visualize parent/child relationships |

### Query Examples

```bash
td query "status = open"
td query "type = bug AND priority <= P1"
td query "created >= -7d"
td search auth --show-score
td tree td-abc1 --depth 2
```

Common flags:

| Command | Flags |
|---------|-------|
| `td query` | `--examples`, `--fields`, `--explain`, `--limit`, `--sort`, `--output` |
| `td search` | `--status`, `--type`, `--labels`, `--priority`, `--limit`, `--json`, `--show-score` |
| `td blocked-by` | `--direct`, `--json` |
| `td depends-on` | `--json` |
| `td critical-path` | `--limit` |
| `td tree` | `--depth`, `--json` |

## Shortcuts

| Command | Description |
|---------|-------------|
| `td blocked` | List blocked issues |
| `td deleted` | Show soft-deleted issues |
| `td in-review`, `td ir` | List all issues currently in review |
| `td next` | Show highest-priority open issue |
| `td ready` | List open issues sorted by priority |
| `td reviewable` | Show issues awaiting review that you can review |

## Session Commands

| Command | Description |
|---------|-------------|
| `td check-handoff` | Check whether handoff is needed before exiting |
| `td focus` | Set the current working issue |
| `td resume` | Show context and set focus |
| `td session` | Name session, or `--new` at context start. Subcommands: `cleanup`, `list` |
| `td status`, `td current` | Show dashboard: session, focus, reviews, blocked, ready issues |
| `td unfocus` | Clear focus |
| `td usage` | Generate optimized context block for AI agents |
| `td whoami` | Show current session identity |
| `td ws`, `td worksession` | Work session commands |

### Session Examples

```bash
td focus td-abc1
td resume td-abc1
td usage --new-session
td ws start "feature-x"
td ws tag td-abc1 td-abc2
td ws handoff
```

Common flags:

| Command | Flags |
|---------|-------|
| `td check-handoff` | `--quiet`, `--json` |
| `td session` | `--new` |
| `td status` | `--json` |
| `td session list` | none |
| `td session cleanup` | none |
| `td usage` | `--compact`, `--quiet`, `--json`, `--new-session` |
| `td ws tag` | `--no-start` |
| `td ws log` | `--blocker`, `--decision`, `--hypothesis`, `--tried`, `--result` |
| `td ws handoff` | `--continue`, `--review`, `--done`, `--remaining`, `--decision`, `--uncertain` |
| `td ws show` | `--full` |

### Work Sessions

| Command | Description |
|---------|-------------|
| `td ws current` | Show current work session state |
| `td ws end` | End work session without handoff |
| `td ws handoff` | End work session and generate handoffs for all tagged issues |
| `td ws list` | List recent work sessions |
| `td ws log` | Log to the work session |
| `td ws show` | Show details of a past work session |
| `td ws start` | Start a named work session |
| `td ws tag` | Associate issues with the current work session |
| `td ws untag` | Remove issues from work session |

## Notes

| Command | Description |
|---------|-------------|
| `td note add` | Create a new note |
| `td note archive` | Archive a note |
| `td note delete` | Soft-delete a note |
| `td note edit` | Edit note title or content |
| `td note list`, `td note ls` | List notes |
| `td note pin` | Pin a note |
| `td note show` | Display a note |
| `td note unarchive` | Unarchive a note |
| `td note unpin` | Unpin a note |

```bash
td note add "Architecture decisions" --content "Keep the API small."
td note list --pinned
td note show nt-abc123 --json
```

Common flags:

| Command | Flags |
|---------|-------|
| `td note add` | `--content` |
| `td note list` | `--all`, `--archived`, `--pinned`, `--search`, `--json`, `--output`, `--limit` |
| `td note show` | `--json` |
| `td note edit` | `--title`, `--content` |

## Tasks & Epics

| Command | Description |
|---------|-------------|
| `td task create` | Create a new task |
| `td task list` | List all tasks |
| `td epic create` | Create a new epic |
| `td epic list` | List all epics |

```bash
td task create "Implement login endpoint" --priority P1
td task list --all
td epic create "Auth system" --priority P0
```

Common flags:

| Command | Flags |
|---------|-------|
| `td task create` | `--title`, `--priority`, `--description`, `--labels`, `--parent`, `--epic`, `--depends-on`, `--blocks`, `--minor` |
| `td task list` | `--all` |
| `td epic create` | `--title`, `--priority`, `--description`, `--labels`, `--parent`, `--epic`, `--depends-on`, `--blocks` |

## Boards

| Command | Description |
|---------|-------------|
| `td board create` | Create a new board |
| `td board delete` | Delete a board |
| `td board edit` | Edit a board's name or query |
| `td board list` | List all boards |
| `td board move` | Set an issue's position on a board |
| `td board show` | Show issues in a board |
| `td board unposition` | Remove an issue's explicit position from a board |

```bash
td board create "My Bugs" --query "type = bug AND status = open"
td board show "My Bugs" --status open --json
td board edit "My Bugs" --view-mode swimlanes
```

Common flags:

| Command | Flags |
|---------|-------|
| `td board create` | `--query` |
| `td board list` | `--json` |
| `td board show` | `--status`, `--json` |
| `td board edit` | `--name`, `--query`, `--view-mode` |

## Files

| Command | Description |
|---------|-------------|
| `td files` | List linked files with change status |
| `td link` | Link files to an issue |
| `td unlink` | Remove file associations |

```bash
td link td-abc1 src/main.go
td link td-abc1 src/*.go --role test
td files td-abc1 --changed
```

Common flags:

| Command | Flags |
|---------|-------|
| `td files` | `--changed`, `--untracked`, `--json` |
| `td link` | `--depends-on`, `--recursive`, `--role` |

## System Commands

Use `td info` for the project dashboard and `td stats` for analytics and diagnostics.

| Command | Description |
|---------|-------------|
| `td debug-stats` | Output runtime memory and goroutine statistics |
| `td errors` | View failed `td` command attempts |
| `td export` | Export database |
| `td feature` | Manage experimental feature flags |
| `td info` | Show database statistics and project overview |
| `td import` | Import issues |
| `td init` | Initialize a new `td` project |
| `td last` | Show the last action performed |
| `td monitor` | Live TUI dashboard |
| `td security` | View security exception log |
| `td serve` | Start the HTTP API server |
| `td stats` | View analytics, security, and error data |
| `td undo` | Undo the last action |
| `td upgrade` | Run database migrations |
| `td version` | Show version and check for updates |
| `td workflow` | Show issue status workflow |

### System Examples

```bash
td info --json
td stats analytics --since 7d
td workflow --mermaid
td export --format md --output issues.md
```

Common flags:

| Command | Flags |
|---------|-------|
| `td info` | `--json` |
| `td version` | `--check`, `--short` |
| `td undo` | `--list` |
| `td last` | `--n` |
| `td monitor` | `--interval` |
| `td serve` | `--addr`, `--cors`, `--interval`, `--port`, `--token` |
| `td export` | `--all`, `--format`, `--output`, `--render-markdown` |
| `td import` | `--dry-run`, `--force`, `--format` |
| `td workflow` | `--dot`, `--mermaid` |
| `td errors` | `--clear`, `--count`, `--json`, `--limit`, `--session`, `--since` |
| `td security` | `--clear`, `--json` |
| `td stats analytics` | `--clear`, `--json`, `--limit`, `--since` |

### Feature Flags

| Command | Description |
|---------|-------------|
| `td feature list` | List known feature flags and their resolved state |
| `td feature get` | Get a feature flag state |
| `td feature set` | Set a feature flag in local project config |
| `td feature unset` | Remove a local feature flag override |

### Stats Namespace

| Command | Description |
|---------|-------------|
| `td stats analytics`, `td stats usage` | View command usage analytics |
| `td stats errors` | View failed `td` command attempts |
| `td stats security` | View security exception log |

## Sync CLI

These commands are available when the `sync_cli` feature is enabled.

### Authentication

| Command | Description |
|---------|-------------|
| `td auth login` | Log in to sync server |
| `td auth logout` | Log out from sync server |
| `td auth status` | Show authentication status |

### Configuration

| Command | Description |
|---------|-------------|
| `td config associate` | Associate a directory with a td project |
| `td config associations`, `td config assoc` | List directory associations |
| `td config dissociate` | Remove a directory association |
| `td config get` | Get a config value |
| `td config list` | List all config values |
| `td config set` | Set a config value |

Valid config keys:

| Key | Description |
|-----|-------------|
| `sync.url` | Sync server URL |
| `sync.enabled` | Enable sync |
| `sync.auto.enabled` | Enable auto-sync |
| `sync.auto.debounce` | Auto-sync debounce interval |
| `sync.auto.interval` | Auto-sync polling interval |
| `sync.auto.pull` | Auto-sync pull behavior |
| `sync.auto.on_start` | Auto-sync on startup |
| `sync.snapshot_threshold` | Snapshot bootstrap threshold |

### Sync

| Command | Description |
|---------|-------------|
| `td sync` | Sync local data with remote server |
| `td sync conflicts` | Show recent sync conflicts |
| `td sync init` | Interactive guided setup for sync |
| `td sync tail` | Show recent push/pull events |

Common flags:

| Command | Flags |
|---------|-------|
| `td sync` | `--push`, `--pull`, `--status` |
| `td sync conflicts` | `--limit`, `--since` |
| `td sync tail` | `--follow`, `--lines` |

### Sync Diagnostics

| Command | Description |
|---------|-------------|
| `td doctor` | Run diagnostic checks for sync setup |

### Sync Projects

Alias: `sp`

| Command | Description |
|---------|-------------|
| `td sync-project create` | Create a remote sync project |
| `td sync-project invite` | Invite a user by email |
| `td sync-project join` | Join a remote sync project by name or ID |
| `td sync-project kick` | Remove a member from the project |
| `td sync-project link` | Link the local project to a remote sync project |
| `td sync-project list` | List remote sync projects |
| `td sync-project members` | List project members |
| `td sync-project role` | Change a member's role |
| `td sync-project unlink` | Unlink the local project from remote sync |

```bash
TD_FEATURE_SYNC_CLI=1 td auth login
TD_FEATURE_SYNC_CLI=1 td config set sync.enabled true
TD_FEATURE_SYNC_CLI=1 td sync-project create "Team project" --description "Shared backlog"
TD_FEATURE_SYNC_CLI=1 td sync tail -f
```

Common flags:

| Command | Flags |
|---------|-------|
| `td auth` | none |
| `td config associate` | none |
| `td config dissociate` | none |
| `td config get/set/list` | none |
| `td sync-project create` | `--description` |
| `td sync-project link` | `--force` |
| `td sync-project unlink` | `--force` |
