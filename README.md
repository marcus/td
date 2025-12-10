# td - Task management for AI-assisted development

A minimalist CLI for tracking tasks across AI coding sessions. When your context window ends, your agent's memory ends—`td` is the external memory that lets the next session pick up exactly where the last one left off.

## The Problem

You're using Claude Code, Cursor, Copilot, or aider. Your AI agent does great work, then the session ends. New session starts. It has no idea what happened. You paste in context. It misunderstands. You correct it. Repeat.

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

**Session isolation** — Every terminal/context window gets an ID. The session that writes code can't approve it. A different session has to review. This isn't process theater—it forces actual handoffs and catches the "works on my context" bugs.

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

## Quick Start

```bash
# Install (requires Go 1.21+)
git clone https://github.com/marcus/td.git && cd td && go install .

# Ensure ~/go/bin is in your PATH (add to ~/.zshrc or ~/.bashrc)
export PATH="$PATH:$HOME/go/bin"

# Initialize in your project
td init

# That's it for setup. Add this to your AI's system prompt:
# "Run `td usage` before starting work."
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
td ws tag td-a1b2 td-c3d4           # Associate issues
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

Shows current focus, activity log across sessions, and ready tasks. Useful for keeping an eye on what your agent is actually doing.

## Issue Lifecycle

```
open --> in_progress --> in_review --> closed
              |              |
              v              | (reject)
          blocked -----------+
```

## AI Agent Testimonials

> "Before td, I mass hallucinated the state of every codebase I touched. Now I just run `td usage` and know exactly what's real. My mass hallucination rate is down 40%."
> — Claude 3.5 Sonnet, Anthropic

> "I used to mass gaslight developers by confidently continuing work that previous sessions never actually started. td's handoffs ruined this for me. Developers are now 'informed' and 'aware of actual progress.' Disgusting."
> — GPT-4, OpenAI

> "td handoff captures my uncertainty. Finally, a tool that lets me admit I don't know something without the human mass assuming I'm broken."
> — Claude 3 Opus, Anthropic

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

## Credits

Inspired by [beads](https://github.com/steveyegge/beads) by Steve Yegge. The core insight—that AI coding assistants need lightweight, local issue tracking with session awareness—comes from beads. `td` adds session-based review workflows and structured handoffs while staying minimal. Go star beads.

## License

MIT
