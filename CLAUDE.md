# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Important: Use td for Task Management

**This project uses `td` itself for issue tracking and task management.** Before starting work:
1. Run `td session --new` to create a new session (enables cross-session reviews)
2. Run `td usage` to see current state, available issues, AND reviewable tasks. Reviewable issues are only visible to sessions that didn't implement them
   - **After first read, use `td usage -q` to save context** (hides workflow instructions)
3. Use `td start <id>` when beginning work on an issue
4. Use `td log "message"` to track progress
5. Use `td handoff <id>` before stopping to capture state for the next session

## Build and Test Commands

```bash
go build -o td .           # Build the binary
go test ./...              # Run all tests
go test ./internal/db      # Run tests for a specific package
```

## Project Overview

`td` is a minimalist local task/session management CLI for AI-assisted development workflows. It's designed for **session continuity**—capturing working state so new context windows can resume where previous ones stopped.

Key design principles:
- **Session-aware**: Tracks who worked on what (implementer vs reviewer)
- **Handoff-native**: Structured state capture, not just status flags
- **Review workflow**: Implementers cannot approve their own work
- **Local-first**: SQLite database in `.todos/`, git-friendly

## Architecture

```
td/
├── cmd/            # Cobra command definitions (one file per command)
├── internal/
│   ├── db/         # SQLite operations and schema
│   ├── models/     # Core types: Issue, Log, Handoff, WorkSession
│   ├── session/    # Session ID management (.todos/session file)
│   ├── config/     # Config management (.todos/config.json)
│   ├── git/        # Git state capture (commit SHA, branch, dirty files)
│   └── output/     # JSON/text formatters
└── main.go
```

**Data flow**: Commands in `cmd/` call `internal/db` for persistence, use `internal/session` for identity, and `internal/output` for formatting.

**Issue lifecycle**: `open` → `in_progress` → `in_review` → `closed` (or `blocked` at any point). The `implementer_session` field prevents self-approval.

## Key Types

- `models.Issue`: Core issue with status, priority (P0-P4), points (Fibonacci), labels
- `models.Handoff`: Structured state (done/remaining/decisions/uncertain)
- `models.Log`: Progress entries with types (progress, blocker, hypothesis, tried, result)
- `models.WorkSession`: Multi-issue work sessions for agents

## Database

SQLite at `.todos/issues.db`. Schema in `internal/db/schema.go`. Tables:
- `issues`: Core issue data
- `logs`: Append-only progress logs
- `handoffs`: Versioned handoff snapshots
- `git_snapshots`: Git state at start/handoff
- `work_sessions` + `work_session_issues`: Multi-issue sessions
- `issue_dependencies`: Block/depends-on relationships
- `issue_files`: File-to-issue links with change tracking
- `action_log`: Undo support - tracks reversible actions

## Undo Support

Commands that modify data should log actions for undo via `database.LogAction()`. When adding new commands or modifying existing ones that change issue state, dependencies, or file links:

1. Capture previous state with `json.Marshal(issue)` before changes
2. After successful update, log the action with `database.LogAction(&models.ActionLog{...})`
3. Use appropriate `ActionType` from `models` package

**Currently supported:** create, delete, update, start, review, approve, reject

**See `cmd/undo.go`** for undo logic and `internal/models/models.go` for ActionType definitions.
