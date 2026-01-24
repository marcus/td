---
sidebar_position: 10
---

# AI Agent Integration

## Overview

td is designed for AI agents. Any agent that can run shell commands can use td for structured task management -- tracking issues, logging progress, handing off between contexts, and enforcing review before close.

Works with: Claude Code, Cursor, OpenAI Codex, GitHub Copilot, Gemini CLI, or any agent with shell access.

## Setup for Claude Code

Add to your project's `CLAUDE.md`:

```markdown
## MANDATORY: Use `td` for Task Management

Run `td usage --new-session` at conversation start (or after /clear).

Sessions are automatic. Optional:
- `td session "name"` to label the current session
- `td session --new` to force a new session

Use `td usage -q` after first read.
```

Claude Code reads `CLAUDE.md` at the start of every conversation, so this ensures td is always used.

## Setup for Other Agents

Add to your system prompt or project config:

```
Run `td usage --new-session` at conversation start.
Use td commands to track work: td start, td log, td handoff, td review.
```

The key requirement is that the agent runs `td usage --new-session` before doing any work. This gives it full context on what to do next.

## The `td usage` Command

`td usage` gives the agent everything it needs in one call:

- Current session info
- Focused issue with handoff state (what was done, what remains)
- Issues awaiting review
- Open issues by priority
- Workflow instructions

Flags:
- `--new-session` -- start a fresh session (use at conversation start)
- `-q` -- quiet mode, shorter output (use after first read)

## Recommended Agent Workflow

```bash
td usage --new-session     # 1. Get context at start
td start <id>              # 2. Begin work on an issue
td log "progress msg"      # 3. Track progress as you go
td handoff <id> --done "..." --remaining "..."  # 4. Before stopping
td review <id>             # 5. Submit for review
```

Steps 3-4 are critical for multi-context work. Logs and handoffs persist across context windows, so the next agent picks up exactly where you left off.

## Session Isolation for Agents

Each agent instance (terminal, context window) gets a unique session ID. This ensures:

- Agent A's work is reviewed by Agent B (no self-approval)
- Handoffs between contexts are explicit and trackable
- Review history shows which session made which changes

Sessions are created automatically based on the agent's terminal context. You can also force a new session with `td session --new` or label the current one with `td session "name"`.

## Multi-Agent Workflows

td enforces that the implementer cannot be the reviewer. This naturally supports multi-agent workflows:

```bash
# Agent 1 implements
td start td-a1b2
td log "implemented feature X"
td handoff td-a1b2 --done "Built X with tests" --remaining "Needs review"
td review td-a1b2

# Agent 2 reviews (different session)
td reviewable
td approve td-a1b2    # or: td reject td-a1b2 --reason "needs fix"
```

Because each agent context gets a different session ID, the system prevents the same agent from both implementing and approving a change.

## Tips

- **Always start with `td usage --new-session`** -- this is the single most important instruction for any agent.
- **Log frequently** -- short, hyper-concise messages. These survive context resets.
- **Handoff before stopping** -- if work is incomplete, `td handoff` captures state for the next agent.
- **Don't start new sessions mid-work** -- sessions track implementers. A new session mid-task bypasses review enforcement.
- **Use quiet mode after first read** -- `td usage -q` avoids repeating workflow instructions every time.
