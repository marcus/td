# CLAUDE.md

## MANDATORY: Use `td` for Task Management

Run `td usage --new-session` at conversation start (or after /clear). This tells you what to work on next.

Sessions are automatic (based on your terminal/agent context). Optional:
- `td session "name"` to label the current session
- `td session --new` to force a new session in the same context

**Do NOT start a new session mid-work.** Sessions track implementers—new session = bypass review.

Use `td usage -q` after first read.

## Build & Install

```bash
go build -o td .           # Build locally
go test ./...              # Test all
```

## Version & Release

```bash
# Commit changes with proper message
git add .
git commit -m "feat: description of changes

Details here

🤖 Generated with Claude Code

Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com>"

# Create version tag (bump from current version, e.g., v0.2.0 → v0.3.0)
git tag -a v0.3.0 -m "Release v0.3.0: description"

# Push commit and tag
git push origin main
git push origin v0.3.0

# Install locally with version
go install -ldflags "-X main.Version=v0.3.0" ./...

# Verify installation
td version
```

## Architecture

- `cmd/` - Cobra commands
- `internal/db/` - SQLite (schema.go)
- `internal/models/` - Issue, Log, Handoff, WorkSession
- `internal/session/` - Session ID (.todos/session)
- `pkg/monitor/` - TUI monitor (see [docs/modal-system.md](docs/modal-system.md) for modal architecture)

Issue lifecycle: open → in_progress → in_review → closed (or blocked)

## Undo Support

Log actions via `database.LogAction()`. See `cmd/undo.go` for implementation.
