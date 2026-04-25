---
sidebar_position: 11
---

# Command Reference

Complete reference for the user-facing `td` CLI command surface.

Run `td <command> --help` for the exact flags accepted by your installed version.

## Core Commands

| Command | Description |
|---------|-------------|
| `td create "title" [flags]` | Create an issue. Aliases: `add`, `new`. Common flags: `--type`, `--priority`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--parent`, `--epic`, `--minor`, `--defer`, `--due` |
| `td list [flags]` | List issues matching filters. Alias: `ls`. Common flags: `--status`, `--type`, `--priority`, `--epic`, `--json` |
| `td show <id...>` | Display full issue details. Aliases: `context`, `view`, `get` |
| `td update <id> [flags]` | Update fields on an issue. Alias: `edit`. Common flags: `--title`, `--type`, `--priority`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--labels`, `--append`, `--defer`, `--due` |
| `td delete <id...>` | Soft-delete one or more issues |
| `td deleted [--json]` | Show soft-deleted issues |
| `td restore <id...>` | Restore soft-deleted issues |
| `td task ...` | Shortcuts for working with tasks |
| `td epic ...` | Shortcuts for working with epics |

## Workflow Commands

| Command | Description |
|---------|-------------|
| `td start <id...>` | Begin work. Alias: `begin` |
| `td unstart <id...>` | Revert in-progress issues to open. Alias: `stop` |
| `td log "message" [flags]` | Append progress to the current issue. Flags include `--decision`, `--blocker`, `--hypothesis`, `--tried`, `--result` |
| `td handoff <id> [flags]` | Capture structured working state. Flags: `--done`, `--remaining`, `--decision`, `--uncertain` |
| `td review <id...>` | Submit issues for review. Aliases: `submit`, `finish` |
| `td reviewable [--include-approved]` | Show issues the current session can review; optionally include reviewed issues that can be closed |
| `td approve <id...> [flags]` | Approve and close, record-only review, or close using a recorded approval. Flags include `--reason`, `--record-only`, `--decision approved\|changes_requested`, `--all` |
| `td reject <id> --reason "..."` | Reject an issue and return it to open |
| `td block <id...>` | Mark issues as blocked |
| `td unblock <id...>` | Unblock issues back to open |
| `td close <id...>` | Admin close without review. Aliases: `done`, `complete` |
| `td reopen <id...>` | Reopen closed issues |
| `td comment <id> "text"` | Add a comment. Alias for `td comments add` |
| `td comments <id>` | List comments for an issue |
| `td comments add <id> "text"` | Add a comment to an issue |

## Review Flag Details

`td approve` can be used in delegated review flows:

| Invocation | Effect |
|------------|--------|
| `td approve <id>` | Direct reviewer-close. The caller must be an eligible reviewer when no active approval is already recorded |
| `td approve <id> --record-only --reason "..."` | Record an approval review without closing. The caller must be an eligible reviewer |
| `td approve <id> --record-only --decision changes_requested --reason "..."` | Record a non-approving review without closing |
| `td approve <id> --reason "..."` | Close using an existing recorded approval. Non-reviewer closes require a reason |

Use `td reviewable --include-approved` to find reviewed issues that can be closed after an independent approval has been recorded.

## Deferral and Due Dates

| Command | Description |
|---------|-------------|
| `td defer <id> <date>` | Defer an issue until a future date |
| `td defer <id> --clear` | Remove deferral |
| `td due <id> <date>` | Set a due date |
| `td due <id> --clear` | Remove a due date |

Date formats include `+7d`, `+2w`, `+1m`, `monday`, `tomorrow`, `next-week`, `next-month`, and `2026-03-15`.

List filters include `--all`, `--deferred`, `--surfacing`, `--overdue`, and `--due-soon`.

## Agent-Safe Rich Text Input

Use `--description-file` and `--acceptance-file` for markdown-heavy fields so shells do not mangle code fences, quotes, or blank lines. Pass `-` to read the full field from stdin.

```bash
td create "Document sync failure modes" \
  --description-file docs/issue-description.md \
  --acceptance-file docs/issue-acceptance.md

cat docs/acceptance.md | td update td-a1b2 --append --acceptance-file -
```

## Query and Search

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
| `td dep add <issue> <depends-on>` | Add dependency |
| `td dep rm <issue> <depends-on>` | Remove dependency |
| `td dep <issue>` | Show dependencies |
| `td dep <issue> --blocking` | Show issues blocked by this issue |
| `td depends-on <issue>` | Show what an issue depends on. Aliases: `deps`, `dependencies` |
| `td blocked-by <issue>` | Show issues waiting on this issue |
| `td critical-path` | Show the sequence that unblocks the most work |

## Boards

| Command | Description |
|---------|-------------|
| `td board create "name" --query "..."` | Create board |
| `td board list` | List boards |
| `td board show <board>` | Show board |
| `td board move <board> <id> <pos>` | Position issue |
| `td board edit <board> [flags]` | Edit board |
| `td board delete <board>` | Delete board |

## Epics and Trees

| Command | Description |
|---------|-------------|
| `td epic create "title" [flags]` | Create epic |
| `td epic list` | List epics |
| `td tree <id>` | Show parent/child tree |
| `td tree add-child <parent> <child>` | Add child |

## Notes

| Command | Description |
|---------|-------------|
| `td note add <title> [--content "..."]` | Create a freeform note. Opens `$EDITOR` when content is omitted |
| `td note list [flags]` | List notes. Alias: `ls`. Flags include `--pinned`, `--archived`, `--all`, `--search`, `--limit`, `--json`, `--output table\|json` |
| `td note show <id> [--json]` | Display note details |
| `td note edit <id> [flags]` | Edit title or content. Flags: `--title`, `--content`; opens `$EDITOR` if neither is provided |
| `td note delete <id>` | Soft-delete a note |
| `td note pin <id>` | Pin a note |
| `td note unpin <id>` | Unpin a note |
| `td note archive <id>` | Archive a note |
| `td note unarchive <id>` | Unarchive a note |

## Sessions

| Command | Description |
|---------|-------------|
| `td usage [flags]` | Generate optimized context for agents. Flags: `--new-session`, `-q` |
| `td session [name]` | Name the current session |
| `td session --new` | Force a new session at context start |
| `td status` | Show dashboard view. Alias: `current` |
| `td focus <id>` | Set current focus |
| `td unfocus` | Clear focus |
| `td resume [id]` | Show context and set focus |
| `td check-handoff [flags]` | Exit nonzero when in-progress work needs handoff. Flags: `--json`, `--quiet` |
| `td whoami` | Show current session identity |

## Work Sessions

| Command | Description |
|---------|-------------|
| `td ws start "name"` | Start a multi-issue work session. Alias: `worksession` |
| `td ws tag <ids...>` | Tag issues |
| `td ws log "message"` | Log to all tagged issues |
| `td ws current` | Show work session state |
| `td ws handoff` | Handoff tagged issues and end the work session |

## Files

| Command | Description |
|---------|-------------|
| `td link <id> <files...>` | Link files to an issue |
| `td unlink <id> <files...>` | Remove file associations |
| `td files <id>` | List linked files with change status |

## Sync and Collaboration

| Command | Description |
|---------|-------------|
| `td auth login` | Log in to the sync server |
| `td auth logout` | Log out from the sync server |
| `td auth status` | Show authentication status |
| `td sync` | Push and pull local events with the linked remote project |
| `td sync --push` | Push only |
| `td sync --pull` | Pull only |
| `td sync --status` | Show sync status only |
| `td sync init` | Interactive guided sync setup |
| `td sync conflicts` | Show recent sync conflicts |
| `td sync tail` | Show recent sync activity |
| `td sync-project create <name> [--description "..."]` | Create and auto-link a remote sync project. Alias: `sp` |
| `td sync-project link <project-id> [--force]` | Link local project to a remote sync project |
| `td sync-project join [name-or-id]` | Join a remote sync project by name or ID |
| `td sync-project list` | List remote sync projects |
| `td sync-project members` | List project members |
| `td sync-project invite <email> [role]` | Invite a member. Valid roles: `owner`, `writer`, `reader`; default: `writer` |
| `td sync-project kick <user-id>` | Remove a member |
| `td sync-project role <user-id> <role>` | Change a member role. Valid roles: `owner`, `writer`, `reader` |
| `td sync-project unlink [--force]` | Unlink local project from remote sync |
| `td doctor` | Run sync setup diagnostics |

## System and Diagnostics

| Command | Description |
|---------|-------------|
| `td init` | Initialize a project |
| `td monitor` | Live TUI dashboard |
| `td serve` | Start the td HTTP API server |
| `td undo` | Undo the last action |
| `td last` | Show the last action performed |
| `td version [--short] [--check]` | Show version and optionally check for updates |
| `td export [flags]` | Export issues. Flags: `--format json\|md`, `--output`, `--all`, `--render-markdown` |
| `td import [file] [flags]` | Import issues. Flags: `--format json\|md`, `--dry-run`, `--force` |
| `td info [--json]` | Show database statistics and project overview. Alias: `stats` |
| `td stats analytics` | Show command usage analytics. Alias: `td stats usage` |
| `td stats security` | Show security exception log. Alias for `td security` |
| `td stats errors` | Show failed command attempts. Alias for `td errors` |
| `td config list` | List all config values |
| `td config get <key>` | Get a config value |
| `td config set <key> <value>` | Set a config value |
| `td config associate [dir] <target>` | Associate a directory with a td project |
| `td config associations` | List directory associations. Alias: `assoc` |
| `td config dissociate [dir]` | Remove a directory association |
| `td feature list` | List known feature flags and resolved states |
| `td feature get <flag>` | Get feature flag state |
| `td feature set <flag> <value>` | Set a local feature flag override |
| `td feature unset <flag>` | Remove a local feature flag override |
| `td errors [flags]` | View failed td command attempts. Flags: `--limit`, `--since`, `--session`, `--json`, `--count`, `--clear` |
| `td security [flags]` | View security exception audit log. Flags: `--json`, `--clear` |
| `td workflow [--dot] [--mermaid]` | Show issue status workflow |
| `td upgrade` | Run pending database migrations |
| `td completion <shell>` | Generate shell completion |
| `td debug-stats` | Output runtime memory and goroutine statistics as JSON |
