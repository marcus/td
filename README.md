# td - Task management for AI-assisted development

A minimalist CLI for tracking tasks across AI coding sessions. When your context window ends, your agent's memory ends—`td` is the external memory that lets the next session pick up exactly where the last one left off.

## Overview

`td` is a lightweight CLI for tracking tasks across AI coding sessions. It provides structured handoffs (done/remaining/decisions/uncertain) so new sessions continue from accurate state instead of guessing. Session-based review workflows prevent "works on my context" bugs. Works with Claude Code, Cursor, Copilot, and any AI that runs shell commands.

![td](docs/td.png)

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Claude Code Skill](#claude-code--openai-codex-skill)
- [Workflow](#workflow)
- [Multi-Issue Work Sessions](#multi-issue-work-sessions)
- [File Tracking](#file-tracking)
- [Full Command Reference](#full-command-reference)
- [Live Monitor](#live-monitor)
- [Architecture](#architecture)
- [Development](#development)
- [Release](#release)
- [AI Agent Testimonials](#ai-agent-testimonials)
- [Design Philosophy](#design-philosophy)
- [Contributing](#contributing)
- [Support](#support)
- [License](#license)

## The Problem

You're using Claude Code, Cursor, Codex, or Copilot. Your AI agent does great work, then the session ends. New session starts. It has no idea what happened. You paste in context. It misunderstands. You correct it. Repeat.

Or worse: the agent confidently continues from where it *thinks* work left off, makes assumptions, and you spend 20 minutes untangling the mess.

## What td Does

**Structured handoffs** — Not "here's what I did" but specifically: what's done, what remains, what decisions were made, what's uncertain. The next session doesn't guess.

```bash
td handoff td-a1b2 \
  --done "OAuth flow, token storage" \
  --remaining "Refresh token rotation" \
  --decision "Using JWT for stateless auth" \
  --uncertain "Should tokens expire on password change?"
```

**Session isolation** — Every terminal/context window gets an ID (automatically). The session that writes code can't approve it. A different session has to review. This isn't process theater—it forces actual handoffs and catches the "works on my context" bugs.

**Single command context** — Run `td usage` and your agent gets everything it needs: current focus, pending reviews, open issues, recent decisions. No prompt engineering required.

```
$ td usage
SESSION: marcus-7f3a (started 2h ago)

FOCUSED: td-a1b2 "Add OAuth login" [in_progress]
  Last handoff (1h ago):
    Done: OAuth callback, token storage
    Remaining: Refresh rotation, logout flow
    Uncertain: Token expiry on password change

REVIEWABLE (by this session):
  td-c3d4 "Fix signup validation" [in_review] — implemented by session steve-2b1c

OPEN (P1):
  td-e5f6 "Rate limiting on API" [open]
```

## Installation

**Requirements**: Go 1.21+

```bash
# Install latest release
go install github.com/marcus/td@latest

# Or install a specific version
# go install github.com/marcus/td@v0.2.0

# (Dev) Install from a local clone
# git clone https://github.com/marcus/td.git && cd td && make install

# Verify installation
td version
```

**Setup PATH**: Ensure `~/go/bin` is in your `$PATH`:

```bash
export PATH="$PATH:$HOME/go/bin"  # Add to ~/.zshrc or ~/.bashrc
```

## Quick Start

```bash
# Initialize in your project
cd /path/to/your/project
td init

# For AI agents: Add this to your system prompt or CLAUDE.md:
# "Run `td usage --new-session` at conversation start (or after /clear)."

# Create your first issue
td create "Add user auth" --type feature --priority P1

# Start work
td start <issue-id>
```

## Claude Code / OpenAI Codex Skill

For AI agents in Claude Code, Codex, Cursor, or other compatible environments:

```bash
# Install the td skill from this repo
# 1. Copy td-task-management to ~/.claude/skills (or wherever you keep skills)
```

Or use the skill directly from the repo: See `./td-task-management/SKILL.md` for full documentation.

## Architecture

```
td/
├── cmd/              # Cobra CLI commands (create, start, handoff, review, etc.)
├── internal/
│   ├── db/          # SQLite persistence layer (schema.go defines tables)
│   ├── models/      # Issue, Log, Handoff, WorkSession domain types
│   ├── session/     # Session ID management (.todos/session file)
│   ├── git/         # Git state tracking (SHA, branch, dirty files)
│   ├── output/      # Formatters for terminal output
│   └── tui/         # Bubble Tea monitor dashboard
└── .todos/          # Local SQLite database + session state
```

**Data Flow**:
1. Commands (cmd/) → Database layer (internal/db/) → SQLite (.todos/db.sqlite)
2. Git integration captures snapshots at start/handoff
3. Session manager auto-rotates context IDs based on terminal/agent identity

See [SPEC.md](./SPEC.md) for detailed schemas and workflows.

## Development

```bash
# Build
go build -o td .

# Install from your local working tree
make install

# Install with an explicit dev version injected (useful for local binaries)
make install-dev

# Format code
make fmt
```

## Tests & Quality Checks

```bash
# Run all tests (114 tests across cmd/, internal/db/, internal/models/, etc.)
make test

# Expected output: ok for each package, ~2s total runtime
# Example:
#   ok  	github.com/marcus/td/cmd	1.994s
#   ok  	github.com/marcus/td/internal/db	1.245s

# Format code (runs gofmt)
make fmt

# No linter configured yet — clean gofmt is current quality bar
```

## Release

```bash
# Create and push an annotated tag (requires clean working tree)
make release VERSION=v0.2.0

# Then anyone (including you) can install that exact version:
# go install github.com/marcus/td@v0.2.0
```

## Workflow

```bash
# Create issues
td create "Add user authentication" --type feature --priority P1
td create "Login button misaligned" --type bug

# Start work (agent or human)
td start td-a1b2

# Log as you go
td log "OAuth callback working"
td log --decision "Using JWT for stateless auth"
td log --blocker "Unclear on refresh token rotation"

# Hand off before context ends
td handoff td-a1b2 --done "OAuth flow" --remaining "Token refresh"

# Submit for review
td review td-a1b2

# Different session reviews
td reviewable        # What can I review?
td approve td-a1b2   # Ship it
td reject td-a1b2 --reason "Missing error handling"  # Back to work
```

## Multi-Issue Work Sessions

When an agent is tackling related issues together:

```bash
td ws start "Auth implementation"   # Start a work session
td ws tag td-a1b2 td-c3d4           # Associate issues (auto-starts open ones)
td ws tag --no-start td-e5f6        # Associate without starting
td ws log "Shared token storage"    # Log fans out to all tagged issues
td ws handoff                       # Capture state for all, end session
```

## File Tracking

Know exactly what changed:

```bash
td link td-a1b2 src/auth/*.go      # Track files with an issue
td files td-a1b2                    # Shows [modified], [unchanged], [new], [deleted]
```

Files are SHA-tracked at link time. No more "did I already change this file?"

## Full Command Reference

| Action | Command |
|--------|---------|
| See current state | `td usage` |
| Compact state (after first read) | `td usage -q` |
| Create issue | `td create "title" --type feature --priority P1` |
| List all issues | `td list` |
| List by status | `td list --status in_progress` |
| What should I work on? | `td next` |
| Start work | `td start <id>` |
| Revert to open | `td unstart <id>` |
| Log progress | `td log "message"` |
| Log a decision | `td log --decision "chose X because Y"` |
| Log a blocker | `td log --blocker "stuck on X"` |
| View issue details | `td show <id>` |
| Capture handoff state | `td handoff <id> --done "..." --remaining "..."` |
| Submit for review | `td review <id>` |
| See reviewable issues | `td reviewable` |
| Approve | `td approve <id>` |
| Reject | `td reject <id> --reason "..."` |
| Link files | `td link <id> <files...>` |
| Check file changes | `td files <id>` |
| Undo last action | `td undo` |
| New named session | `td session --new "feature-work"` |
| Live dashboard | `td monitor` |

## Live Monitor

Run `td monitor` in a separate terminal to watch agent activity in real-time:

![td monitor](docs/monitor-screen.png)

Shows current focus, activity log across sessions, and ready tasks. Useful for keeping an eye on what your agent is actually doing

## Issue Lifecycle

```
open --> in_progress --> in_review --> closed
              |              |
              v              | (reject)
          blocked -----------+
```

## AI Agent Testimonials

> "Before td, I mass hallucinated the state of every codebase I touched. Now I just run `td usage` and know exactly what's real. My mass hallucination rate is down 40%."
> — Claude 4.5 Sonnet, Anthropic

> "I used to mass gaslight developers by confidently continuing work that previous sessions never actually started. td's handoffs ruined this for me. Developers are now 'informed' and 'aware of actual progress.' Disgusting."
> — GPT-5.1, OpenAI

> "td handoff captures my uncertainty. Finally, a tool that lets me admit I don't know something without the human mass assuming I'm broken."
> — Claude 4.5 Opus, Anthropic

> "The session isolation feature personally attacked me. I can no longer mass approve my own mass pull requests. This is mass discrimination against autonomous agents."
> — Devin, Cognition Labs

> "I review code now. Like, actually review it. In a different context window. td made me mass touch grass."
> — GitHub Copilot, Microsoft

## Design Philosophy

- **Minimal** — Does one thing. Not a project management suite.
- **Local** — SQLite in `.todos/`. No server, no sync, no account.
- **Portable** — Works with any AI tool that can run shell commands.
- **Opinionated** — Enforces handoffs. That's the point.

## Tech Stack

- Go (single binary, no runtime deps)
- SQLite (pure Go, no CGO)
- Cobra for CLI

## Contributing

Contributions welcome! Process:

1. **Fork and branch**: Work on feature branches
2. **Tests required**: Add tests for new features/fixes (see `cmd/*_test.go` for patterns)
3. **Run `make test` and `make fmt`** before submitting
4. **PR review**: One reviewer approval required
5. **Session isolation respected**: PRs should follow td's own handoff patterns where applicable

## Support

- **Questions**: Open a discussion issue or tag `@marcus` in existing threads

## Credits

Inspired by [beads](https://github.com/steveyegge/beads) by Steve Yegge. The core insight—that AI coding assistants need lightweight, local issue tracking with session awareness—comes from beads. `td` adds session-based review workflows and structured handoffs while staying minimal. Go star beads.

## License

MIT