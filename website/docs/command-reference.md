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

## Notes

| Command | Description |
|---------|-------------|
| `td note add "title" [--content "..."]` | Create a freeform note. Opens `$EDITOR` when content is omitted |
| `td note list [flags]` | List notes. Flags: `--pinned`, `--archived`, `--all`, `--search`, `--limit`, `--json` |
| `td note show <id>` | Display a note |
| `td note edit <id> [flags]` | Edit note title or content. Flags: `--title`, `--content` |
| `td note delete <id>` | Soft-delete a note |
| `td note pin <id>` | Pin a note |
| `td note unpin <id>` | Unpin a note |
| `td note archive <id>` | Archive a note |
| `td note unarchive <id>` | Restore an archived note |

## Workflow Commands

| Command | Description |
|---------|-------------|
| `td start <id>` | Begin work (status -> in_progress) |
| `td unstart <id>` | Revert to open |
| `td log "message" [flags]` | Log progress. Flags: `--decision`, `--blocker`, `--hypothesis`, `--tried`, `--result` |
| `td handoff <id> [flags]` | Capture state. Flags: `--done`, `--remaining`, `--decision`, `--uncertain` |
| `td review <id> [flags]` | Submit for review. Flags: `--reason`, `--minor`, `--json` |
| `td reviewable` | Show reviewable issues |
| `td approve <id> [flags]` | Approve and close. Flags: `--reason`, `--all`, `--json` |
| `td reject <id> --reason "..."` | Reject back to in_progress |
| `td block <id>` | Mark as blocked |
| `td unblock <id>` | Unblock to open |
| `td close <id>` | Admin close (not for completed work) |
| `td reopen <id>` | Reopen closed issue |
| `td comment <id> "text"` | Add comment |
| `td workflow [--mermaid\|--dot]` | Show the issue status workflow |

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
| `td resume <id>` | Show issue context and set focus |
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

## Sync & Collaboration

These commands are registered when the `sync_cli` feature is enabled, for example with `TD_ENABLE_FEATURE=sync_cli`.

| Command | Description |
|---------|-------------|
| `td auth login` | Log in to a sync server with the device flow |
| `td auth status` | Show saved auth status |
| `td auth logout` | Remove local sync credentials |
| `td sync [flags]` | Push and pull local project data. Flags: `--push`, `--pull`, `--status` |
| `td sync init` | Interactive sync setup |
| `td sync tail [flags]` | Show recent sync activity. Flags: `--follow`, `--lines` |
| `td sync conflicts [flags]` | Show recent sync conflicts. Flags: `--limit`, `--since` |
| `td sync-project create "name" [--description "..."]` | Create and link a remote project |
| `td sync-project join [name-or-id]` | Join a remote project by prompt, name, or ID |
| `td sync-project link <project-id> [--force]` | Link local database to a remote project |
| `td sync-project unlink [--force]` | Unlink local database from remote sync |
| `td sync-project list` | List remote projects visible to the current user |
| `td sync-project members` | List project members |
| `td sync-project invite <email> [role]` | Invite a member as `owner`, `writer`, or `reader` |
| `td sync-project role <user-id> <role>` | Change a member role |
| `td sync-project kick <user-id>` | Remove a project member |

## Configuration

| Command | Description |
|---------|-------------|
| `td config list` | List sync config as JSON |
| `td config get <key>` | Read a sync config key |
| `td config set <key> <value>` | Set a sync config key |
| `td config associate [dir] <target>` | Associate a directory with a td project |
| `td config associations` | List directory associations |
| `td config dissociate [dir]` | Remove a directory association |
| `td feature list` | List known feature flags and resolved state |
| `td feature get <name>` | Show one feature flag |
| `td feature set <name> <true\|false>` | Set a project-local feature flag |
| `td feature unset <name>` | Remove a project-local feature override |

## System

| Command | Description |
|---------|-------------|
| `td init` | Initialize project |
| `td monitor` | Live TUI dashboard |
| `td undo` | Undo last action |
| `td undo --list` | List recent undoable actions in the current session |
| `td last [-n N]` | Show recent actions in the current session |
| `td doctor` | Run sync diagnostics |
| `td doctor fk` | Report foreign-key orphan counts without modifying data |
| `td errors [flags]` | View failed command attempts. Flags: `--since`, `--session`, `--limit`, `--json`, `--count`, `--clear` |
| `td security [flags]` | View review/close exception audit log. Flags: `--json`, `--clear` |
| `td version` | Show version |
| `td export` | Export database |
| `td import` | Import issues |
| `td stats [subcommand]` | Usage statistics |
| `td stats analytics` | Command usage analytics |
| `td stats errors` | Failed command attempts |
| `td stats security` | Security exception log |
