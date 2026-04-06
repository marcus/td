---
sidebar_position: 13
---

# Command Reference

Complete reference for the `td` CLI, including note management, work session helpers, diagnostics, and the feature-gated sync workflow.

> **Note**: Sync and config commands are hidden unless the `sync_cli` feature is enabled. Use `export TD_ENABLE_FEATURE=sync_cli`, or run `td feature set sync_cli true` and restart your shell before using `td sync`, `td auth`, `td config`, `td doctor`, or `td sync-project`.

## Core Issue Commands

| Command | Description |
|---------|-------------|
| `td create "title" [flags]` | Create an issue. Common flags: `--type`, `--priority`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--parent`, `--epic`, `--minor`, `--defer`, `--due` |
| `td list [flags]` | List issues. Common filters: `--status`, `--type`, `--priority`, `--epic`, `--all`, `--deferred`, `--surfacing`, `--overdue`, `--due-soon` |
| `td show &lt;id&gt;` | Display full details for one or more issues |
| `td update &lt;id&gt; [flags]` | Update issue fields, including rich text fields from files or stdin |
| `td delete &lt;id&gt;` | Soft-delete one or more issues |
| `td restore &lt;id&gt;` | Restore one or more soft-deleted issues |
| `td deleted` | List soft-deleted issues |
| `td task create "title"` | Create a task shortcut (`--type task`) |
| `td task list` | List task issues only |
| `td epic create "title"` | Create an epic |
| `td epic list` | List epics |
| `td note add "title"` | Create a freeform note |
| `td note list` | List notes with filters such as `--pinned`, `--archived`, `--search`, `--json` |
| `td note show &lt;id&gt;` | Show a note |
| `td note edit &lt;id&gt;` | Edit a note title or content |
| `td note pin &lt;id&gt;` / `td note unpin &lt;id&gt;` | Pin or unpin a note |
| `td note archive &lt;id&gt;` / `td note unarchive &lt;id&gt;` | Archive or restore a note |
| `td note delete &lt;id&gt;` | Soft-delete a note |

## Workflow Commands

| Command | Description |
|---------|-------------|
| `td start &lt;id&gt;` | Move issue to `in_progress` |
| `td unstart &lt;id&gt;` | Move issue back to `open` |
| `td block &lt;id&gt;` | Mark one or more issues blocked |
| `td unblock &lt;id&gt;` | Return blocked issues to `open` |
| `td review &lt;id&gt;` | Submit one or more issues for review |
| `td approve &lt;id&gt; [--reason "..."]` | Approve and close issues |
| `td reject &lt;id&gt; --reason "..."` | Reject issues back to active work |
| `td close &lt;id&gt;` | Close issues directly without review |
| `td reopen &lt;id&gt;` | Reopen closed issues |
| `td handoff &lt;id&gt; [flags]` | Capture done/remaining/decision/uncertain state before ending a session |
| `td resume [id]` | Show context and set focus |
| `td log "message" [flags]` | Add progress, blocker, decision, hypothesis, tried, or result logs |
| `td comment &lt;id&gt; "text"` | Add a comment shortcut |
| `td comments &lt;id&gt;` | List comments for an issue |
| `td comments add &lt;id&gt; "text"` | Add a comment via the comment subcommand |
| `td workflow` | Print the issue state workflow summary |
| `td check-handoff` | Exit non-zero when the current focus still needs a handoff |

## Query, Dependencies, And Boards

| Command | Description |
|---------|-------------|
| `td query "expression"` | Run a TDQ query |
| `td search "keyword"` | Full-text search across issues |
| `td next` | Show the highest-priority open issue |
| `td ready` | List ready issues by priority |
| `td blocked` | List blocked issues |
| `td in-review` | List issues in review |
| `td reviewable` | List issues you can review from this session |
| `td dep add &lt;issue&gt; &lt;depends-on&gt;...` | Add dependencies |
| `td dep rm &lt;issue&gt; &lt;depends-on&gt;` | Remove dependencies |
| `td dep &lt;issue&gt;` | Show dependencies for an issue |
| `td dep &lt;issue&gt; --blocking` | Show issues this work item blocks |
| `td blocked-by &lt;issue&gt;` | Show issues waiting on this issue |
| `td depends-on &lt;issue&gt;` | Show issues this issue depends on |
| `td critical-path` | Show the best unblocking order |
| `td tree &lt;id&gt;` | Show the issue tree, with optional `--depth` or `--json` |
| `td board create "name" --query "..."` | Create a board from a TDQ query |
| `td board list` | List boards |
| `td board show &lt;board&gt;` | Show board results, optionally filtered by status |
| `td board move &lt;board&gt; &lt;id&gt; &lt;position&gt;` | Set explicit ordering for a card on a board |
| `td board unposition &lt;board&gt; &lt;id&gt;` | Remove a board-specific position override |
| `td board edit &lt;board&gt;` | Rename a board or change its query |
| `td board delete &lt;board&gt;` | Delete a board |

## Sessions And Work Sessions

| Command | Description |
|---------|-------------|
| `td usage [--new-session] [-q]` | Generate AI-friendly context for the current project |
| `td session [name]` | Label the current session |
| `td session --new` | Force a new session at context start |
| `td session list` | List branch- and agent-scoped sessions |
| `td session cleanup` | Remove stale session files |
| `td whoami` | Show the current session identity |
| `td status` | Show the dashboard view |
| `td focus &lt;id&gt;` | Set the focused issue |
| `td unfocus` | Clear the focus |
| `td ws start "name"` | Start a multi-issue work session |
| `td ws tag &lt;ids...&gt;` | Tag issues into the active work session |
| `td ws tag &lt;ids...&gt; --no-start` | Tag issues without auto-starting open work |
| `td ws untag &lt;ids...&gt;` | Remove issues from the active work session |
| `td ws log "message"` | Log once to the work session and all tagged issues |
| `td ws current` | Show the active work session |
| `td ws handoff` | Generate handoffs for tagged issues and end the session |
| `td ws end` | End the current work session without handoff generation |
| `td ws list` | List recent work sessions |
| `td ws show &lt;session-id&gt;` | Show a past work session |

## Files And Directory Associations

| Command | Description |
|---------|-------------|
| `td link &lt;id&gt; &lt;files...&gt;` | Link files or globs to an issue |
| `td unlink &lt;id&gt; &lt;files...&gt;` | Remove linked files |
| `td files &lt;id&gt;` | Show linked files and change status |
| `td config associate [dir] &lt;project&gt;` | Associate a directory with a td project root |
| `td config associations` | List directory-to-project associations |
| `td config dissociate [dir]` | Remove a directory association |

## System And Diagnostics

| Command | Description |
|---------|-------------|
| `td init` | Initialize a td project |
| `td monitor` | Launch the live TUI dashboard |
| `td serve` | Start the td HTTP API server |
| `td info` | Show project overview and database stats |
| `td stats analytics` | Show command usage analytics |
| `td stats errors` / `td errors` | Show failed command attempts |
| `td stats security` / `td security` | Show workflow exception audit events |
| `td undo` | Undo the last action |
| `td last` | Show the last action performed |
| `td export` | Export the database |
| `td import &lt;file&gt;` | Import issues |
| `td upgrade` | Run database migrations |
| `td feature list` | List experimental features and resolved state |
| `td feature get &lt;name&gt;` | Get one feature flag |
| `td feature set &lt;name&gt; &lt;true\|false&gt;` | Set a project-level feature override |
| `td feature unset &lt;name&gt;` | Remove a project-level feature override |
| `td debug-stats` | Print runtime memory and goroutine stats as JSON |
| `td version` | Show the current version and update info |

## Sync And Collaboration Commands

These commands are available only when `sync_cli` is enabled. See [Sync & Collaboration](./sync-collaboration.md) for setup and troubleshooting.

| Command | Description |
|---------|-------------|
| `td auth login` | Authenticate with a sync server |
| `td auth status` | Show current auth status |
| `td auth logout` | Remove local credentials |
| `td config set &lt;key&gt; &lt;value&gt;` | Set sync config such as `sync.url` or autosync behavior |
| `td config get &lt;key&gt;` | Read one config value |
| `td config list` | Print config as JSON |
| `td doctor` | Run sync diagnostics |
| `td sync init` | Guided sync setup |
| `td sync` | Push then pull changes |
| `td sync --push` | Push only |
| `td sync --pull` | Pull only |
| `td sync --status` | Show local and server sync state |
| `td sync conflicts` | Show recent overwrite conflicts |
| `td sync tail` | Show recent sync activity, optionally with `-f` |
| `td sync-project create "name"` | Create and link a remote sync project |
| `td sync-project join [name-or-id]` | Join an invited project by picker, name, or ID |
| `td sync-project link &lt;project-id&gt;` | Link directly to a known project ID |
| `td sync-project unlink` | Remove the local remote-project link |
| `td sync-project list` | List available remote projects |
| `td sync-project members` | List members on the linked project |
| `td sync-project invite &lt;email&gt; [role]` | Invite a collaborator as `writer`, `reader`, or `owner` |
| `td sync-project role &lt;user-id&gt; &lt;role&gt;` | Change a member role |
| `td sync-project kick &lt;user-id&gt;` | Remove a member |

## Agent-Safe Rich Text Input

Use `--description-file` and `--acceptance-file` for markdown-heavy content so your shell does not mangle code fences, quotes, or blank lines. Pass `-` to read the field from stdin.

```bash
td create "Document sync failure modes" \
  --description-file docs/issue-description.md \
  --acceptance-file docs/issue-acceptance.md

cat docs/acceptance.md | td update td-a1b2 --append --acceptance-file -
```
