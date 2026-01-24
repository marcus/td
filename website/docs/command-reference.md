---
sidebar_position: 11
---

# Command Reference

Complete reference for all `td` commands.

## Core Commands

| Command | Description |
|---------|-------------|
| `td create "title" [flags]` | Create issue. Flags: `--type`, `--priority`, `--description`, `--parent`, `--epic`, `--minor` |
| `td list [flags]` | List issues. Flags: `--status`, `--type`, `--priority`, `--epic` |
| `td show <id>` | Display full issue details |
| `td update <id> [flags]` | Update fields. Flags: `--title`, `--type`, `--priority`, `--description`, `--labels` |
| `td delete <id>` | Soft-delete issue |
| `td restore <id>` | Restore soft-deleted issue |

## Workflow Commands

| Command | Description |
|---------|-------------|
| `td start <id>` | Begin work (status -> in_progress) |
| `td unstart <id>` | Revert to open |
| `td log "message" [flags]` | Log progress. Flags: `--decision`, `--blocker`, `--hypothesis`, `--tried`, `--result` |
| `td handoff <id> [flags]` | Capture state. Flags: `--done`, `--remaining`, `--decision`, `--uncertain` |
| `td review <id>` | Submit for review |
| `td reviewable` | Show reviewable issues |
| `td approve <id>` | Approve and close |
| `td reject <id> --reason "..."` | Reject back to in_progress |
| `td block <id>` | Mark as blocked |
| `td unblock <id>` | Unblock to open |
| `td close <id>` | Admin close (not for completed work) |
| `td reopen <id>` | Reopen closed issue |
| `td comment <id> "text"` | Add comment |

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
