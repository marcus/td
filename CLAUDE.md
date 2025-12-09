# CLAUDE.md

## Use `td` for Task Management

Run `td session --new "name"` then `td usage` for workflow. Use `td usage -q` after first read.

## Build

```bash
go build -o td .           # Build
go test ./...              # Test all
```

## Architecture

- `cmd/` - Cobra commands
- `internal/db/` - SQLite (schema.go)
- `internal/models/` - Issue, Log, Handoff, WorkSession
- `internal/session/` - Session ID (.todos/session)

Issue lifecycle: open → in_progress → in_review → closed (or blocked)

## Undo Support

Log actions via `database.LogAction()`. See `cmd/undo.go` for implementation.
