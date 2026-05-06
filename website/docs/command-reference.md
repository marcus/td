---
sidebar_position: 11
---

# Command Reference

Complete reference for public `td` commands. For task-oriented guides, see [Core Workflow](./core-workflow.md), [Notes](./notes.md), [Configuration](./configuration.md), [Sync CLI](./sync-cli.md), and [Directory Associations](./directory-associations.md).

## Global Flags

| Flag | Description |
|------|-------------|
| `-h, --help` | Show help for a command. |
| `-v, --version` | Show version information. |
| `-w, --work-dir <dir>` | Resolve the td project from a specific directory. Useful for scripts and worktrees. |

Example:

```bash
td -w /path/to/project list --status open
```

## Core Commands

| Command | Description | Example |
|---------|-------------|---------|
| `td create "title" [flags]` | Create an issue. Aliases: `add`, `new`. Common flags: `--type`, `--priority`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--labels`, `--parent`, `--epic`, `--minor`, `--defer`, `--due`. | `td create "Add user auth" --type feature --priority P1` |
| `td list [flags]` | List issues. Alias: `ls`. Common filters: `--status`, `--type`, `--priority`, `--epic`, `--all`, `--deferred`, `--surfacing`, `--overdue`, `--due-soon`, `--json`. | `td list --status in_progress` |
| `td show <id...>` | Display full issue details. Aliases: `context`, `view`, `get`. | `td show td-a1b2` |
| `td update <id...> [flags]` | Update issue fields. Alias: `edit`. Common flags: `--title`, `--type`, `--priority`, `--description`, `--description-file`, `--acceptance`, `--acceptance-file`, `--labels`, `--defer`, `--due`. | `td update td-a1b2 --priority P1` |
| `td delete <id...>` | Soft-delete issues. | `td delete td-a1b2` |
| `td restore <id...>` | Restore soft-deleted issues. | `td restore td-a1b2` |
| `td task create "title"` | Create a task. Shorthand for `td add --type task`. | `td task create "Update docs"` |
| `td task list` | List tasks. Shorthand for `td list --type task`. | `td task list` |
| `td epic create "title"` | Create an epic. Shorthand for `td add --type epic`. | `td epic create "Docs backlog"` |
| `td epic list` | List epics. Shorthand for `td list --type epic`. | `td epic list` |

## Workflow Commands

| Command | Description | Example |
|---------|-------------|---------|
| `td start <id...>` | Begin work and record the current session as implementer. Alias: `begin`. | `td start td-a1b2` |
| `td unstart <id...>` | Revert in-progress issues to open and clear implementer session. Alias: `stop`. | `td unstart td-a1b2` |
| `td log [id] "message" [flags]` | Log progress on an issue or the current focus. Common flags: `--decision`, `--blocker`, `--hypothesis`, `--tried`, `--result`. | `td log td-a1b2 --decision "Use SQLite WAL"` |
| `td handoff <id> [message] [flags]` | Capture structured working state. Common flags: `--done`, `--remaining`, `--decision`, `--uncertain`. | `td handoff td-a1b2 --done "Parser complete" --remaining "Tests"` |
| `td review <id...>` | Submit issues for independent review. Aliases: `submit`, `finish`. | `td review td-a1b2` |
| `td reviewable [--include-approved]` | Show issues the current session can review; include recorded approvals that can be closed with `--include-approved`. | `td reviewable --include-approved` |
| `td approve <id...> [flags]` | Approve and close, record-only review, or close using a recorded approval. Flags: `--reason`, `--record-only`, `--decision approved\|changes_requested`, `--all`. | `td approve td-a1b2 --record-only --reason "Diff and tests reviewed"` |
| `td reject <id...> --reason "..."` | Reject reviewed issues back to open and supersede active approval reviews. | `td reject td-a1b2 --reason "Missing regression test"` |
| `td close <id...>` | Admin close for duplicates, won't-fix, and cleanup. Aliases: `done`, `complete`. Use `td approve` for reviewed work. | `td close td-a1b2 --reason "Duplicate"` |
| `td block <id...>` | Mark issues blocked. | `td block td-a1b2` |
| `td unblock <id...>` | Return blocked issues to open. | `td unblock td-a1b2` |
| `td reopen <id...>` | Reopen closed issues. | `td reopen td-a1b2` |
| `td comment <id> "text"` | Add a comment. Alias for `td comments add`. | `td comment td-a1b2 "Follow up with reviewer"` |
| `td comments <id>` | List comments for an issue. | `td comments td-a1b2` |
| `td comments add <id> "text"` | Add a comment through the comments command group. | `td comments add td-a1b2 "Needs screenshots"` |

## Deferral And Due Dates

| Command | Description | Example |
|---------|-------------|---------|
| `td defer <id> <date>` | Hide an issue until a future date. | `td defer td-a1b2 +7d` |
| `td defer <id> --clear` | Remove deferral. | `td defer td-a1b2 --clear` |
| `td due <id> <date>` | Set a due date. | `td due td-a1b2 next-week` |
| `td due <id> --clear` | Remove due date. | `td due td-a1b2 --clear` |

Date formats include `+7d`, `+2w`, `+1m`, `monday`, `tomorrow`, `next-week`, `next-month`, and `2026-03-15`.

## Review Flag Details

`td approve` operates in three modes under `review_policy_mode=delegated`:

| Invocation | Effect |
|------------|--------|
| `td approve <id>` | Direct reviewer close. Caller must be an eligible reviewer with no active approval recorded. |
| `td approve <id> --record-only --reason "..."` | Record an approval without closing. Caller must be an eligible reviewer. |
| `td approve <id> --record-only --decision changes_requested --reason "..."` | Record a non-approving review. |
| `td approve <id> --reason "..."` with an existing approval | Close using a recorded approval. Any session may close; non-reviewer closes require `--reason`. |

`td reviewable --include-approved` surfaces reviewed issues the current session can close, which is useful for orchestrators that delegated review to a sub-agent.

Under `strict` and `balanced` modes, `--record-only` and `--decision` are unavailable; `td approve` performs review-and-close in one step.

## Agent-Safe Rich Text Input

Use `--description-file` and `--acceptance-file` for markdown-heavy fields so shells do not mangle code fences, quotes, or blank lines. Pass `-` to read the full field from stdin.

```bash
td create "Document sync failure modes" \
  --description-file docs/issue-description.md \
  --acceptance-file docs/issue-acceptance.md

cat docs/acceptance.md | td update td-a1b2 --append --acceptance-file -
```

## Query And Search

| Command | Description | Example |
|---------|-------------|---------|
| `td query "expression"` | Search issues with TDQ. | `td query 'status:open priority:P1'` |
| `td search "keyword"` | Full-text search across titles, descriptions, logs, and handoffs. | `td search "snapshot"` |
| `td next` | Show the highest-priority open issue. | `td next` |
| `td ready` | List open issues sorted by priority. | `td ready` |
| `td blocked` | List blocked issues. | `td blocked` |
| `td deleted` | List soft-deleted issues. | `td deleted` |
| `td in-review` | List issues currently in review. Alias: `ir`. | `td in-review` |

## Dependencies And Trees

| Command | Description | Example |
|---------|-------------|---------|
| `td dep <issue>` | Show dependencies for an issue. | `td dep td-a1b2` |
| `td dep add <issue> <depends-on...>` | Add dependencies. | `td dep add td-a1b2 td-c3d4` |
| `td dep rm <issue> <depends-on>` | Remove a dependency. Alias: `remove`. | `td dep rm td-a1b2 td-c3d4` |
| `td dep <issue> --blocking` | Show issues blocked by this issue. | `td dep td-a1b2 --blocking` |
| `td depends-on <issue>` | Show what an issue depends on. Aliases: `deps`, `dependencies`. | `td depends-on td-a1b2` |
| `td blocked-by <issue>` | Show what issues are waiting on this issue. | `td blocked-by td-a1b2` |
| `td critical-path` | Show the issue sequence that unblocks the most work. | `td critical-path` |
| `td tree <issue>` | Visualize parent/child relationships. | `td tree td-a1b2` |
| `td tree add-child <parent> <child>` | Add a child issue to a parent. | `td tree add-child td-a1b2 td-c3d4` |

## Boards

| Command | Description | Example |
|---------|-------------|---------|
| `td board list` | List boards. | `td board list` |
| `td board create <name> --query "..."` | Create a query-backed board. | `td board create "Review" --query "status:in_review"` |
| `td board show <board>` | Show issues in a board. | `td board show Review` |
| `td board edit <board> [flags]` | Edit board name or query. | `td board edit Review --query "status:open"` |
| `td board move <board> <id> <position>` | Set an issue position. | `td board move Review td-a1b2 1` |
| `td board unposition <board> <id>` | Remove an explicit board position. | `td board unposition Review td-a1b2` |
| `td board delete <board>` | Delete a board. | `td board delete Review` |

## Notes

See [Notes](./notes.md) for the full notes workflow.

| Command | Description | Example |
|---------|-------------|---------|
| `td note add <title> [--content "..."]` | Create a freeform note. | `td note add "Architecture decisions" --content "Use WAL"` |
| `td note list [flags]` | List notes. Alias: `ls`. Flags: `--pinned`, `--archived`, `--all`, `--search`, `--limit`, `--json`, `--output`. | `td note list --search sync` |
| `td note show <id>` | Show note details. | `td note show nt-a1b2c3` |
| `td note edit <id> [flags]` | Edit a note title or content, or open the editor. Flags: `--title`, `--content`. | `td note edit nt-a1b2c3 --title "Sync notes"` |
| `td note delete <id>` | Soft-delete a note. | `td note delete nt-a1b2c3` |
| `td note pin <id>` / `td note unpin <id>` | Pin or unpin a note. | `td note pin nt-a1b2c3` |
| `td note archive <id>` / `td note unarchive <id>` | Archive or unarchive a note. | `td note archive nt-a1b2c3` |

## Sessions

| Command | Description | Example |
|---------|-------------|---------|
| `td usage [flags]` | Generate an agent context block. Flags: `--new-session`, `-q`. | `td usage --new-session` |
| `td session [name]` | Name the current session. | `td session "docs backfill"` |
| `td session --new` | Force a new session at context start. Do not use mid-work to bypass review. | `td session --new` |
| `td session list` | List branch and agent-scoped sessions. | `td session list` |
| `td session cleanup` | Remove stale session files. | `td session cleanup` |
| `td status` | Show current session dashboard. Alias: `current`. | `td status` |
| `td focus <id>` | Set the focused issue. | `td focus td-a1b2` |
| `td unfocus` | Clear focus. | `td unfocus` |
| `td resume [id]` | Show context and set focus. | `td resume td-a1b2` |
| `td check-handoff` | Exit non-zero when in-progress work needs handoff. | `td check-handoff` |
| `td whoami` | Show current session identity. | `td whoami` |

## Work Sessions

| Command | Description | Example |
|---------|-------------|---------|
| `td ws start [name]` | Start a named multi-issue work session. | `td ws start "Docs pass"` |
| `td ws tag <ids...>` | Add issues to the work session. By default, open issues are started. | `td ws tag td-a1b2 td-c3d4` |
| `td ws untag <ids...>` | Remove issues from the work session. | `td ws untag td-a1b2` |
| `td ws log "message"` | Log to the work session and all tagged issues. | `td ws log "Updated shared parser"` |
| `td ws current` | Show current work session state. | `td ws current` |
| `td ws handoff` | Generate handoffs for all tagged issues and end the session. | `td ws handoff` |
| `td ws end` | End a work session without handoff. | `td ws end` |
| `td ws list` | List recent work sessions. | `td ws list` |
| `td ws show [session-id]` | Show details for a past work session. | `td ws show ws-a1b2` |

## Files

| Command | Description | Example |
|---------|-------------|---------|
| `td link <id> <file-pattern...>` | Link files to an issue. | `td link td-a1b2 cmd/root.go docs/plan.md` |
| `td unlink <id> <file-pattern>` | Remove file associations. | `td unlink td-a1b2 docs/plan.md` |
| `td files <id>` | List linked files and change status. | `td files td-a1b2` |

## Configuration

See [Configuration](./configuration.md) and [Directory Associations](./directory-associations.md) for details.

| Command | Description | Example |
|---------|-------------|---------|
| `td config list` | Print sync config JSON from `~/.config/td/config.json`. | `td config list` |
| `td config get <key>` | Read a sync config value. | `td config get sync.url` |
| `td config set <key> <value>` | Set a sync config value. | `td config set sync.auto.interval 10m` |
| `td config associate [dir] <target>` | Associate a directory with a canonical td project. | `td config associate ../feature-worktree .` |
| `td config associations` | List directory associations. Alias: `assoc`. | `td config assoc` |
| `td config dissociate [dir]` | Remove a directory association. | `td config dissociate ../feature-worktree` |
| `td feature list` | List known boolean feature flags and resolved state. | `td feature list` |
| `td feature get <name>` | Show one boolean feature state. | `td feature get sync_cli` |
| `td feature set <name> <true\|false>` | Set a boolean feature override. | `td feature set sync_cli true` |
| `td feature unset <name>` | Remove a boolean feature override. | `td feature unset sync_autosync` |

## Sync And Auth

See [Sync CLI](./sync-cli.md) for the operational sync flow.

| Command | Description | Example |
|---------|-------------|---------|
| `td auth login` | Log in to the configured sync server. | `td auth login` |
| `td auth status` | Show stored authentication status. | `td auth status` |
| `td auth logout` | Clear stored sync credentials. | `td auth logout` |
| `td sync init` | Interactive sync setup for server, auth, and project link. | `td sync init` |
| `td sync [flags]` | Push and pull sync events. Flags: `--status`, `--push`, `--pull`. | `td sync --status` |
| `td sync tail [flags]` | Show recent sync activity. Flags: `-f`, `--follow`, `-n`, `--lines`. | `td sync tail -f` |
| `td sync conflicts [flags]` | Show recent conflicts. Flags: `--since`, `--limit`. | `td sync conflicts --since 24h` |
| `td sync-project create <name>` | Create a remote project and try to link the local database. | `td sync-project create "Docs project"` |
| `td sync-project link <project-id>` | Link local project to a remote project. Flag: `--force`. | `td sync-project link prj_abc123` |
| `td sync-project unlink` | Unlink local project from remote sync. | `td sync-project unlink` |
| `td sync-project list` | List remote projects. | `td sync-project list` |
| `td sync-project join [name-or-id]` | Join a project by name or ID. | `td sync-project join "Docs project"` |
| `td sync-project members` | List project members. | `td sync-project members` |
| `td sync-project invite <email> [role]` | Invite a member. Roles: `owner`, `writer`, `reader`. | `td sync-project invite dev@example.com writer` |
| `td sync-project role <user-id> <role>` | Change a member role. | `td sync-project role usr_abc reader` |
| `td sync-project kick <user-id>` | Remove a member. | `td sync-project kick usr_abc` |
| `td doctor` | Run sync setup diagnostics. | `td doctor` |

## System And Diagnostics

| Command | Description | Example |
|---------|-------------|---------|
| `td init` | Initialize `.todos` for a project. | `td init` |
| `td monitor` | Launch the live TUI dashboard. | `td monitor` |
| `td serve [flags]` | Start the local HTTP API server. Common flags: `--port`, `--addr`, `--token`, `--cors`, `--interval`. | `td serve --port 8080` |
| `td undo` | Undo the last reversible action in this session. | `td undo` |
| `td last [-n <count>]` | Show the last recorded actions. | `td last -n 5` |
| `td version` | Show version and check for updates. | `td version` |
| `td info` | Show database statistics and project overview. | `td info --json` |
| `td stats` | Unified statistics command. | `td stats` |
| `td stats analytics` | Show local CLI usage analytics. Alias: `td stats usage`. | `td stats analytics` |
| `td stats errors` | Show failed td command attempts. Alias: `td errors`. | `td stats errors` |
| `td stats security` | Show review/close exception audit log. Alias: `td security`. | `td stats security` |
| `td errors` | Show failed td command attempts. | `td errors --limit 20` |
| `td security` | Show review/close exception audit log. | `td security` |
| `td debug-stats` | Output runtime memory and goroutine stats as JSON. | `td debug-stats` |
| `td export` | Export the database. | `td export > td-export.json` |
| `td import <file>` | Import issues. | `td import td-export.json` |
| `td upgrade` | Run pending database migrations. | `td upgrade` |
| `td workflow` | Show the issue status workflow. | `td workflow` |
| `td completion <shell>` | Generate shell completion. | `td completion zsh` |
| `td help [command]` | Show command help. | `td help sync` |
