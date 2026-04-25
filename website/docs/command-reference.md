---
sidebar_position: 11
---

# Command Reference

Complete reference for the user-facing `td` command surface. Run `td <command> --help` for the exact flag list on your installed version.

## Global Flags

| Flag | Description |
|------|-------------|
| `-h, --help` | Show help for a command |
| `-v, --version` | Show the td version |
| `-w, --work-dir <path>` | Resolve `.td-root` and git worktrees from a specific project directory |

## Core Commands

| Command | Description |
|---------|-------------|
| `td create [title]` | Create an issue. Aliases: `add`, `new`. Common flags: `--type`, `--priority`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--labels`, `--parent`, `--epic`, `--points`, `--minor`, `--depends-on`, `--blocks`, `--defer`, `--due` |
| `td task create [title]` | Create a task. Shorthand for `td add --type task` |
| `td task list` | List tasks. Shorthand for `td list --type task` |
| `td list [filters]` | List issues. Aliases: `ls`. Common flags: `--status`, `--type`, `--priority`, `--labels`, `--epic`, `--parent`, `--filter`, `--search`, `--mine`, `--reviewable`, `--json`, `--format`, `--limit`, `--sort`, `--reverse`, `--all` |
| `td show [issue-id...]` | Show issue details. Aliases: `context`, `view`, `get`. Flags: `--children`, `--tree`, `--short`, `--long`, `--json`, `--render-markdown` |
| `td update [issue-id...]` | Update issue fields. Alias: `edit`. Common flags: `--title`, `--type`, `--priority`, `--status`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--append`, `--labels`, `--parent`, `--points`, `--sprint`, `--depends-on`, `--blocks`, `--defer`, `--due`, `--comment` |
| `td delete <id...>` | Soft-delete one or more issues |
| `td deleted [--json]` | Show soft-deleted issues |
| `td restore <id...>` | Restore soft-deleted issues |

## Workflow Commands

| Command | Description |
|---------|-------------|
| `td start [issue-id...]` | Begin work and record the current session as implementer. Alias: `begin`. Flags: `--force`, `--reason` |
| `td unstart [issue-id...]` | Revert issues from `in_progress` to `open`. Alias: `stop` |
| `td log "message"` | Append a log entry to the current or focused issue. Use `--decision`, `--blocker`, `--hypothesis`, `--tried`, and `--result` for structured log entries |
| `td handoff <issue-id> [message]` | Capture structured handoff state. Flags: `--done`, `--remaining`, `--decision`, `--uncertain`, `--message`, `--note` |
| `td review [issue-id...]` | Submit work for review. Aliases: `submit`, `finish` |
| `td reviewable` | Show issues awaiting review that the current session can review |
| `td approve [issue-id...]` | Approve and close reviewable issues. Flags: `--all`, `--reason`, `--json` |
| `td reject [issue-id...] --reason "..."` | Reject issues back to `open`. Flags include `--reason` and `--json` |
| `td block [issue-id...]` | Mark issues as blocked. Flag: `--reason` |
| `td unblock [issue-id...]` | Move blocked issues back to `open`. Flag: `--reason` |
| `td close [issue-id...]` | Direct admin close for duplicates, won't-fix, or cleanup. Aliases: `done`, `complete`. Agents should use `review` plus `approve` for completed work |
| `td reopen [issue-id...]` | Reopen closed issues back to `open` |
| `td comment [issue-id] "text"` | Add a comment. Alias for `td comments add` |
| `td comments [issue-id]` | List comments on an issue |
| `td comments add [issue-id] "text"` | Add a comment to an issue |

## Deferral & Due Dates

| Command | Description |
|---------|-------------|
| `td defer <id> <date>` | Defer an issue until a future date |
| `td defer <id> --clear` | Remove deferral |
| `td due <id> <date>` | Set an issue due date |
| `td due <id> --clear` | Remove due date |

Date inputs support forms such as `+7d`, `+2w`, `+1m`, `monday`, `tomorrow`, `next-week`, `next-month`, and `2026-03-15`.

`td create` and `td update` also accept `--defer` and `--due`. `td list` includes temporal filters: `--deferred`, `--surfacing`, `--overdue`, and `--due-soon`.

## Agent-Safe Rich Text Input

Use file-backed fields for markdown-heavy descriptions and acceptance criteria so shells do not mangle code fences, quotes, or blank lines. Pass `-` to read from stdin.

```bash
td create "Document sync failure modes" \
  --description-file docs/issue-description.md \
  --acceptance-file docs/issue-acceptance.md

cat docs/acceptance.md | td update td-a1b2 --append --acceptance-file -
```

## Query & Search

| Command | Description |
|---------|-------------|
| `td query "expression"` | Search issues with the TDQ query language |
| `td search "keyword"` | Full-text search across issues |
| `td next` | Show the highest-priority open issue |
| `td ready` | List open issues sorted by priority |
| `td blocked` | List blocked issues |
| `td in-review` | List issues currently in review. Alias: `ir` |

## Dependencies

| Command | Description |
|---------|-------------|
| `td dep add <issue> <depends-on>` | Add a dependency |
| `td dep rm <issue> <depends-on>` | Remove a dependency |
| `td dep <issue>` | Show what an issue depends on |
| `td dep <issue> --blocking` | Show what depends on an issue |
| `td depends-on <issue>` | Alias for showing dependencies |
| `td blocked-by <issue>` | Show issues waiting on this issue |
| `td critical-path` | Show the sequence of issues that unblocks the most work |

## Boards, Epics, and Trees

| Command | Description |
|---------|-------------|
| `td board create "name" --query "..."` | Create a query-backed board |
| `td board list` | List boards |
| `td board show <board>` | Show issues on a board |
| `td board move <board> <id> <position>` | Set an issue position on a board |
| `td board unposition <board> <id>` | Remove an explicit board position |
| `td board edit <board> [flags]` | Edit a board name or query |
| `td board delete <board>` | Delete a board |
| `td epic create "title"` | Create an epic |
| `td epic list` | List epics |
| `td tree [issue-id]` | Visualize parent/child relationships. Flags: `--depth`, `--json` |
| `td tree add-child <parent> <child>` | Add a child issue to a parent issue |

## Notes

| Command | Description |
|---------|-------------|
| `td note add <title>` | Create a freeform note. Use `--content` to avoid opening `$EDITOR` |
| `td note list` | List non-archived notes. Aliases: `ls`. Flags: `--pinned`, `--archived`, `--all`, `--search`, `--limit`, `--json`, `--output json` |
| `td note show <id>` | Show a note. Flag: `--json` |
| `td note edit <id>` | Edit title and/or content. Flags: `--title`, `--content` |
| `td note delete <id>` | Soft-delete a note |
| `td note pin <id>` / `td note unpin <id>` | Pin or unpin a note |
| `td note archive <id>` / `td note unarchive <id>` | Archive or unarchive a note |

## Sessions and Work Sessions

| Command | Description |
|---------|-------------|
| `td usage` | Generate an optimized context block for AI agents. Flags: `--new-session`, `--quiet`, `--compact`, `--json` |
| `td session [name]` | Name the current session. `td session --new` forces a new session at context start |
| `td session list` | List sessions scoped by branch and agent |
| `td session cleanup` | Remove stale session files |
| `td status` | Show session, focus, reviews, blocked, and ready issues. Alias: `current`. Flag: `--json` |
| `td focus [issue-id]` | Set the current working issue |
| `td unfocus` | Clear focus |
| `td resume [issue-id]` | Show context and set focus |
| `td whoami` | Show current session identity |
| `td check-handoff` | Return exit code 1 when the current session has in-progress work that needs handoff. Flags: `--json`, `--quiet` |
| `td ws start [name]` | Start a named work session. Alias group: `worksession` |
| `td ws tag [issue-ids...]` | Associate issues with the work session. Open issues are auto-started unless `--no-start` is passed |
| `td ws untag [issue-ids...]` | Remove issues from the work session |
| `td ws log "message"` | Log to the work session and tagged issues |
| `td ws current` | Show the current work session |
| `td ws list` | List recent work sessions |
| `td ws show <id>` | Show a past work session |
| `td ws handoff` | Generate handoffs for tagged issues. Flags: `--done`, `--remaining`, `--decision`, `--uncertain`, `--review`, `--continue` |
| `td ws end` | End the work session without handoff |

## Files

| Command | Description |
|---------|-------------|
| `td link [issue-id] [file-pattern...]` | Link files to an issue. Flags: `--role implementation\|test\|reference\|config`, `--recursive` |
| `td link <issue-id> --depends-on <other-id>` | Add a dependency through the link command |
| `td unlink [issue-id] [file-pattern]` | Remove file associations |
| `td files [issue-id]` | Show linked files and change status. Flags: `--changed`, `--untracked`, `--json` |

## Sync and Collaboration

Sync commands are feature-gated by `sync_cli`. If they are not available in your process, start td with `TD_FEATURE_SYNC_CLI=true`.

| Command | Description |
|---------|-------------|
| `td auth login` | Log in to the sync server with the device-code flow |
| `td auth logout` | Clear local sync credentials |
| `td auth status` | Show the current sync account, server, and API key prefix |
| `td sync init` | Interactive guided sync setup |
| `td sync` | Push and pull local data with the linked remote project |
| `td sync --push` / `td sync --pull` | Push-only or pull-only sync |
| `td sync --status` | Show local and server sync status |
| `td sync conflicts` | Show recent sync conflicts. Flags: `--limit`, `--since` |
| `td sync tail` | Show recent sync activity. Flags: `--lines`, `--follow` |
| `td sync-project create <name>` | Create a remote project and auto-link the local project when possible |
| `td sync-project list` | List remote sync projects |
| `td sync-project join [name-or-id]` | Choose or match a remote project and link it locally |
| `td sync-project link <project-id>` | Link the local project to a remote project. Flag: `--force` |
| `td sync-project unlink` | Unlink local sync state. Flag: `--force` |
| `td sync-project members` | List members of the linked project |
| `td sync-project invite <email> [role]` | Invite a user. Valid roles are `owner`, `writer`, and `reader`; omitted role defaults to `writer` |
| `td sync-project kick <user-id>` | Remove a member |
| `td sync-project role <user-id> <role>` | Change a member role. Valid roles are `owner`, `writer`, and `reader` |

## System and Diagnostics

| Command | Description |
|---------|-------------|
| `td init` | Initialize a td project |
| `td config get <key>` / `td config set <key> <value>` | Read or write configuration |
| `td config list` | List configuration values |
| `td config associate [dir] <target>` | Associate a directory with a td project in `~/.config/td/associations.json` |
| `td config associations` | List directory associations. Alias: `assoc` |
| `td config dissociate [dir]` | Remove a directory association |
| `td feature list` | List known feature flags and their resolved state |
| `td feature get <name>` | Show a feature flag state |
| `td feature set <name> <true\|false>` | Set a local project feature flag override |
| `td feature unset <name>` | Remove a local feature flag override |
| `td doctor` | Run diagnostic checks for sync setup |
| `td errors` | View failed td command attempts. Flags: `--limit`, `--since`, `--session`, `--count`, `--json`, `--clear` |
| `td security` | View creator-approval and self-close audit events. Flags: `--json`, `--clear` |
| `td stats` | View usage statistics, security events, and errors |
| `td stats analytics` | Show command usage analytics. Flags: `--since`, `--limit`, `--json`, `--clear` |
| `td stats errors` | Alias surface for `td errors` |
| `td stats security` | Alias surface for `td security` |
| `td info` | Show database statistics and project overview. Flag: `--json` |
| `td debug-stats` | Output Go runtime memory and goroutine statistics as JSON |
| `td workflow` | Show the issue status workflow. Flags: `--dot`, `--mermaid` |
| `td upgrade` | Run pending database migrations |
| `td version` | Show version and check for updates. Flags: `--short`, `--check` |
| `td last` | Show the last action, or `-n <count>` recent actions |
| `td export` | Export the database. Flags: `--format json\|md`, `--output`, `--all`, `--render-markdown` |
| `td import [file]` | Import issues. Flags: `--format json\|md`, `--dry-run`, `--force` |
| `td undo` | Undo the last action |
| `td monitor` | Open the live TUI dashboard |
| `td serve` | Start the HTTP API server. Flags: `--addr`, `--port`, `--token`, `--cors`, `--interval` |
| `td completion <shell>` | Generate shell completion scripts |
