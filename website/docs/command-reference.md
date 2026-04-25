---
sidebar_position: 11
---

# Command Reference

Complete reference for the user-facing `td` command surface.

Run `td <command> --help` for the canonical help text and the latest flag list. All commands accept the global `--work-dir` flag to resolve a td project from another directory.

## Core Commands

| Command | Description |
|---------|-------------|
| `td create "title" [flags]` | Create an issue. Aliases: `add`, `new`. Common flags: `--type`, `--priority`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--parent`, `--epic`, `--minor`, `--defer`, `--due` |
| `td task create "title"` | Convenience command for creating a task |
| `td task list` | List task-type issues |
| `td list [flags]` | List issues. Alias: `ls`. Common filters: `--status`, `--type`, `--priority`, `--epic`, `--all`, `--deferred`, `--surfacing`, `--overdue`, `--due-soon`, `--json` |
| `td show <id...>` | Display full details for one or more issues. Aliases: `context`, `view`, `get` |
| `td update <id...> [flags]` | Update fields. Alias: `edit`. Common flags: `--title`, `--type`, `--priority`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--labels`, `--defer`, `--due` |
| `td delete <id...>` | Soft-delete one or more issues |
| `td deleted [--json]` | Show soft-deleted issues |
| `td restore <id...>` | Restore soft-deleted issues |

## Workflow Commands

| Command | Description |
|---------|-------------|
| `td start <id...>` | Begin work. Alias: `begin` |
| `td unstart <id...>` | Revert in-progress issues to open. Alias: `stop` |
| `td log [id] "message"` | Append a log entry to the current or specified issue. Supports typed logs such as `--decision`, `--blocker`, `--hypothesis`, `--tried`, and `--result` |
| `td handoff <id> [message] [flags]` | Capture structured working state. Common flags: `--done`, `--remaining`, `--decision`, `--uncertain` |
| `td review <id...>` | Submit issues for review. Aliases: `submit`, `finish` |
| `td reviewable [--include-approved]` | Show issues the current session can review; `--include-approved` also shows reviewed issues this session can close |
| `td approve <id...> [flags]` | Approve and close, record-only review, or close using a recorded approval. Flags: `--reason`, `--record-only`, `--decision approved\|changes_requested`, `--all` |
| `td reject <id...> --reason "..."` | Reject issues back to open and supersede active approval reviews |
| `td block <id...>` | Mark issues blocked |
| `td unblock <id...>` | Move blocked issues back to open |
| `td close <id...>` | Admin close without review. Aliases: `done`, `complete`. Use `td approve` for reviewed work |
| `td reopen <id...>` | Reopen closed issues |
| `td comment <id> "text"` | Add a comment. Alias for `td comments add` |
| `td comments <id>` | List comments for an issue |
| `td comments add <id> "text"` | Add a comment to an issue |

## Deferral And Due Dates

| Command | Description |
|---------|-------------|
| `td defer <id> <date>` | Defer an issue until a future date |
| `td defer <id> --clear` | Remove deferral and make the issue actionable |
| `td due <id> <date>` | Set a due date |
| `td due <id> --clear` | Remove a due date |

Date formats include `+7d`, `+2w`, `+1m`, `monday`, `tomorrow`, `next-week`, `next-month`, and `2026-03-15`.

The `--defer` and `--due` flags are also available on `td create` and `td update`.

## Review Flag Details

`td approve` operates in three modes under `review_policy_mode=delegated`:

| Invocation | Effect |
|------------|--------|
| `td approve <id>` | Direct reviewer-close: caller must be an eligible reviewer with no active approval recorded |
| `td approve <id> --record-only --reason "..."` | Record an approval review without closing. Caller must be an eligible reviewer |
| `td approve <id> --record-only --decision changes_requested --reason "..."` | Record a non-approving review |
| `td approve <id> --reason "..."` with an existing approval | Close using a recorded approval. Any session may close; non-reviewer closes require `--reason` |

`td reviewable --include-approved` surfaces reviewed issues the current session can close. This is useful for orchestrators that delegate review to another session.

Under `strict` and `balanced` modes, `--record-only` and `--decision` are unavailable; `td approve` performs a review-and-close in one step.

## Query And Search

| Command | Description |
|---------|-------------|
| `td query "expression"` | Search with the TDQ query language |
| `td search "keyword"` | Full-text search across issues |
| `td next` | Show the highest-priority open issue |
| `td ready` | List open issues sorted by priority |
| `td blocked` | List blocked issues |
| `td in-review` | List issues currently in review. Alias: `ir` |

## Dependencies

| Command | Description |
|---------|-------------|
| `td dep <issue> <depends-on>` | Add a dependency |
| `td dep add <issue> <depends-on...>` | Add one or more dependencies |
| `td dep rm <issue> <depends-on>` | Remove a dependency. Alias: `remove` |
| `td dep <issue>` | Show dependencies |
| `td dep <issue> --blocking` | Show what depends on the issue |
| `td depends-on <issue>` | Show what the issue depends on. Aliases: `deps`, `dependencies` |
| `td blocked-by <issue>` | Show issues waiting on this issue |
| `td critical-path` | Show the sequence that unblocks the most work |

## Boards

| Command | Description |
|---------|-------------|
| `td board create "name" --query "..."` | Create a board |
| `td board list` | List boards |
| `td board show <board>` | Show board issues |
| `td board move <board> <id> <pos>` | Set an issue position |
| `td board unposition <board> <id>` | Remove an explicit issue position |
| `td board edit <board> [flags]` | Edit board name or query |
| `td board delete <board>` | Delete a board |

## Epics And Trees

| Command | Description |
|---------|-------------|
| `td epic create "title" [flags]` | Create an epic |
| `td epic list` | List epics |
| `td tree <id>` | Visualize parent/child relationships |
| `td tree add-child <parent> <child>` | Add a child issue |

## Notes

| Command | Description |
|---------|-------------|
| `td note add "title" [--content "..."]` | Create a freeform note. Opens `$EDITOR` when content is omitted |
| `td note list [flags]` | List notes. Alias: `ls`. Flags: `--pinned`, `--archived`, `--all`, `--search`, `--limit`, `--output`, `--json` |
| `td note show <id> [--json]` | Display a note |
| `td note edit <id> [flags]` | Edit note title or content. Flags: `--title`, `--content` |
| `td note delete <id>` | Soft-delete a note |
| `td note pin <id>` | Pin a note |
| `td note unpin <id>` | Unpin a note |
| `td note archive <id>` | Archive a note |
| `td note unarchive <id>` | Unarchive a note |

See [Notes](./notes.md) for examples and guidance on when to use notes instead of issue comments.

## Sessions

| Command | Description |
|---------|-------------|
| `td usage [flags]` | Generate an agent context block. Flags: `--new-session`, `-q` |
| `td session [name]` | Name the current session |
| `td session --new [name]` | Force a new session at context start. Do not use mid-work |
| `td session list` | List branch and agent-scoped sessions |
| `td session cleanup` | Remove stale session files |
| `td status` | Show dashboard state. Alias: `current` |
| `td focus <id>` | Set focus |
| `td unfocus` | Clear focus |
| `td resume <id>` | Show context and set focus |
| `td check-handoff [flags]` | Return an error if the current session has in-progress work needing handoff. Flags: `--json`, `--quiet` |
| `td whoami` | Show current session identity |

## Work Sessions

| Command | Description |
|---------|-------------|
| `td ws start [name]` | Start a named work session. Alias group: `worksession` |
| `td ws tag <ids...>` | Associate issues with the current work session and auto-start open issues |
| `td ws untag <ids...>` | Remove issues from the work session |
| `td ws log "message"` | Log to the work session |
| `td ws current` | Show current work session state |
| `td ws handoff` | Generate handoffs for tagged issues and end the work session |
| `td ws end` | End the work session without handoff |
| `td ws list` | List recent work sessions |
| `td ws show <session-id>` | Show details for a past work session |

## Files

| Command | Description |
|---------|-------------|
| `td link <id> <files...>` | Link files to an issue |
| `td unlink <id> <files...>` | Remove file associations |
| `td files <id>` | List linked files with change status |

## Sync And Collaboration

| Command | Description |
|---------|-------------|
| `td auth login` | Log in to the sync server |
| `td auth logout` | Log out from the sync server |
| `td auth status` | Show authentication status |
| `td sync init` | Run interactive guided sync setup |
| `td sync [--push\|--pull\|--status]` | Sync local data with the remote server |
| `td sync tail [-f] [-n N]` | Show recent sync activity |
| `td sync conflicts [flags]` | Show recent sync conflicts. Flags: `--limit`, `--since` |
| `td sync-project create <name> [--description "..."]` | Create a remote sync project |
| `td sync-project link <project-id> [--force]` | Link the local project to a remote project |
| `td sync-project join [name-or-id]` | Join a remote project by name or ID |
| `td sync-project list` | List remote projects |
| `td sync-project members` | List project members |
| `td sync-project invite <email> [role]` | Invite a user |
| `td sync-project kick <user-id>` | Remove a member |
| `td sync-project role <user-id> <role>` | Change a member role |
| `td sync-project unlink [--force]` | Unlink the local project from remote sync |

See [Sync And Collaboration](./sync-collaboration.md) for the normal setup flow.

## System And Diagnostics

| Command | Description |
|---------|-------------|
| `td init` | Initialize a td project |
| `td monitor` | Live TUI dashboard |
| `td config set <key> <value>` | Set a config value |
| `td config get <key>` | Get a config value |
| `td config list` | List config values |
| `td config associate [dir] <target>` | Associate a directory with a td project path |
| `td config associations` | List directory associations. Alias: `assoc` |
| `td config dissociate [dir]` | Remove a directory association |
| `td feature list` | List known feature flags and resolved state |
| `td feature get <name>` | Show a feature flag state |
| `td feature set <name> <true\|false>` | Set a project-local feature flag override |
| `td feature unset <name>` | Remove a feature flag override |
| `td doctor` | Run sync setup diagnostics |
| `td doctor fk` | Report orphan-row counts for foreign-key relations |
| `td errors [flags]` | View failed td command attempts. Flags: `--limit`, `--since`, `--session`, `--count`, `--json`, `--clear` |
| `td security [flags]` | View review/close exception audit log. Flags: `--json`, `--clear` |
| `td stats analytics [flags]` | View command usage analytics. Alias: `td stats usage` |
| `td stats security` | Alias for `td security` |
| `td stats errors` | Alias for `td errors` |
| `td info [--json]` | Show database statistics and project overview |
| `td export [flags]` | Export database. Flags: `--format`, `--output`, `--all`, `--render-markdown` |
| `td import [file] [flags]` | Import issues. Flags: `--format`, `--dry-run`, `--force` |
| `td undo` | Undo the last action |
| `td undo last` | Show the last action |
| `td upgrade` | Run database migrations |
| `td version [flags]` | Show version and check for updates. Flags: `--short`, `--check` |
| `td workflow [flags]` | Show issue status workflow. Flags: `--dot`, `--mermaid` |
| `td serve` | Start the td HTTP API server |
| `td debug-stats` | Output runtime memory and goroutine statistics as JSON |
| `td completion <shell>` | Generate shell completion |

See [System And Diagnostics](./system-diagnostics.md) for operational guidance.

## Agent-Safe Rich Text Input

Use `--description-file` and `--acceptance-file` for markdown-heavy fields so shells do not mangle code fences, quotes, or blank lines. Pass `-` to read the full field from stdin.

```bash
td create "Document sync failure modes" \
  --description-file docs/issue-description.md \
  --acceptance-file docs/issue-acceptance.md

cat docs/acceptance.md | td update td-a1b2 --append --acceptance-file -
```
