# td - Task management for AI-assisted development

A minimalist local task and session management CLI designed for AI-assisted development workflows. Built for **session continuity**—capturing working state so new context windows can resume where previous ones stopped.

## Why td?

AI coding assistants are stateless. Each conversation starts fresh. This creates real problems:

- **Lost context**: The AI doesn't know what was tried, what worked, what's uncertain
- **No handoff**: When you start a new session, there's no structured way to resume
- **Self-approval**: Nothing stops an AI from "completing" its own work without review

`td` solves these by treating AI sessions as first-class participants in a development workflow.

## Key Ideas

**Session-aware**: Each terminal or AI context gets a session ID. The session that implements work cannot approve it—a different session must review. This enforces actual handoffs.

**Handoff-native**: Structured state capture (`done`, `remaining`, `decisions`, `uncertain`) instead of freeform notes. Future sessions get exactly what they need to continue.

**Non-invasive**: No hooks, no plugins, no IDE integrations required. It's a CLI that writes to a local SQLite database. Works with Claude Code, Cursor, Copilot, aider, or any agentic coding system. Add `td usage` to your AI's system prompt and you're done.

**Local-first**: Everything stays in `.todos/` in your project. Git-friendly exports if you need them.

## Installation

```bash
# Clone and install globally (requires Go 1.21+)
git clone https://github.com/marcus/td.git
cd td
go install .
```

This installs `td` to your `$GOPATH/bin` (typically `~/go/bin`). Make sure this is in your `PATH`.

## Quick Start

```bash
# Initialize in your project
td init

# Create an issue
td create "Add user authentication" --type feature --priority P1

# Start work
td start td-a1b2

# Log progress as you go
td log "OAuth callback working"
td log --decision "Using JWT for stateless auth"
td log --blocker "Unclear on refresh token rotation"

# Capture state before stopping
td handoff td-a1b2 --done "OAuth flow" --remaining "Token refresh" --uncertain "Rotation policy"

# Submit for review
td review td-a1b2
```

A different session (new terminal, new AI conversation) can then review:

```bash
td reviewable        # See what needs review
td approve td-a1b2   # Close it out
```

## For AI Agents

Add this to your AI's system prompt or CLAUDE.md:

```
Before starting work, run `td usage` to see current state and available issues.
Use `td start <id>` when beginning work on an issue.
Use `td log "message"` to track progress.
Use `td handoff <id>` before stopping to capture state for the next session.
```

The `td usage` command generates an optimized context block showing:
- Current session identity
- Focused issue with last handoff state
- Issues awaiting review (that this session can review)
- High-priority open issues
- Command reference

## Work Sessions (Multi-Issue)

For AI agents working across multiple related issues:

```bash
td ws start "Auth implementation"   # Start a work session
td ws tag td-a1b2 td-c3d4           # Associate issues
td ws log "Shared token storage"    # Log fans out to all tagged issues
td ws handoff                       # Capture state for all, end session
```

## Commands

| Action | Command |
|--------|---------|
| See current state | `td usage` |
| Create issue | `td create "title" --type feature` |
| Start work | `td start <id>` |
| Log progress | `td log "message"` |
| Log blocker | `td log --blocker "stuck on X"` |
| Capture state | `td handoff <id>` |
| Submit for review | `td review <id>` |
| See reviewable | `td reviewable` |
| Approve | `td approve <id>` |
| List issues | `td list` |
| What's next | `td next` |

## Issue Lifecycle

```
open --> in_progress --> in_review --> closed
              |              |
              v              | (reject)
          blocked -----------+
```

The key constraint: the session that implements cannot approve. This isn't bureaucracy—it ensures handoffs actually happen.

## File Tracking

Link files to issues for change detection:

```bash
td link td-a1b2 src/auth/*.go
td files td-a1b2              # Shows [modified], [unchanged], [new], [deleted]
```

Files are tracked by SHA at link time. `td files` compares against current state.

## Credits

This project is heavily inspired by [beads](https://github.com/steveyegge/beads) by Steve Yegge. The core insight—that AI coding assistants need lightweight, local issue tracking with session awareness—comes directly from beads.

`td` is an evolution that adds session-based review workflows, structured handoffs, and work sessions while removing some features to stay minimal. But the foundational idea and much of the design philosophy belongs to Steve's original work. Go star beads.

## Design Philosophy

- **Minimal**: Does one thing. Not a project management suite.
- **Local**: SQLite in `.todos/`. No server, no sync, no account.
- **Portable**: Works with any AI tool that can run shell commands.
- **Opinionated**: Enforces handoffs. That's the point.

## Tech Stack

- Go
- SQLite (pure Go, no CGO)
- Cobra for CLI
- No external dependencies at runtime

## License

MIT
