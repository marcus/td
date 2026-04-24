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

- Agent A's work is reviewed by an independent session (no self-review)
- Handoffs between contexts are explicit and trackable
- Review history shows which session implemented, which recorded the review, and which closed

Sessions are created automatically based on the agent's terminal context. You can also force a new session with `td session --new` or label the current one with `td session "name"`. **Do not start a new session mid-work just to satisfy the review rules** — it defeats the audit trail.

## Multi-Agent Workflows

The core guardrail is simple: you cannot review your own implementation, but you can close after an independent review has been recorded. This naturally supports multi-agent workflows:

```bash
# Agent 1 implements
td start td-a1b2
td log "implemented feature X"
td handoff td-a1b2 --done "Built X with tests" --remaining "Needs review"
td review td-a1b2

# Agent 2 reviews (separate session)
td reviewable
td approve td-a1b2    # or: td reject td-a1b2 --reason "needs fix"
```

### Review Policy Modes

td supports three review policy modes via `review_policy_mode`:

- `delegated` — **default for new installs.** Review attestations; any session may close after an independent review is recorded.
- `strict` — no prior involvement allowed on the reviewer.
- `balanced` — strict, plus a creator-approval exception. Retained for projects that explicitly opt in.

The legacy `balanced_review_policy` flag is deprecated; prefer `review_policy_mode=balanced` instead.

Pin or change the mode:

```bash
td feature set review_policy_mode strict     # or balanced, or delegated
# or, one-off:
TD_FEATURE_REVIEW_POLICY_MODE=strict td approve td-a1b2
```

### Delegated Review: Orchestrator + Sub-Agents

Under `delegated`, an orchestrator coordinates work across sub-agents. The review must come from a session that did not participate in implementation, but the close may be performed by any session — so the orchestrator can finish the task once a reviewer sub-agent records approval.

```bash
# Orchestrator creates work
td add "Refactor auth module" --type feature

# Implementer sub-agent (separate session) does the work
td start td-c3d4
td log "refactored auth module"
td handoff td-c3d4 --done "refactor" --remaining "none"

# Orchestrator submits for review.
td review td-c3d4

# Reviewer sub-agent (separate session) records an approval without closing
td approve td-c3d4 --record-only --reason "Reviewed diff, tests pass"

# Orchestrator, implementer, or another session closes using the recorded approval
td approve td-c3d4 --reason "Closing after recorded independent approval"
```

Important details for orchestrators:

- The orchestrator does not need to own an issue role to close after approval. The reviewer must be independent; the closer is recorded separately for audit.
- The reviewer sub-agent cannot have implementation history on the issue. Fresh reviewer sessions are the safest choice.
- A reviewer can also record a non-approving decision: `td approve <id> --record-only --decision changes_requested --reason "fix X"`.
- Use `td reviewable --include-approved` to surface reviewed issues the current session can close.

### Balanced (Legacy): Creator Exception

Under `balanced`, if your orchestrator session *created* a task but a sub-agent *implemented* it, the orchestrator can approve with a reason. This is a legacy pattern; prefer the delegated flow above when it is available.

```bash
td add "Refactor auth module"                              # orchestrator
td start td-c3d4                                           # sub-agent
td review td-c3d4
td approve td-c3d4 --reason "Reviewed diff, tests pass"    # orchestrator
```

Implementation self-approval remains blocked. Creator-exception approvals are logged to the security audit trail (`td security`).

## Tips

- **Always start with `td usage --new-session`** -- this is the single most important instruction for any agent.
- **Log frequently** -- short, hyper-concise messages. These survive context resets.
- **Handoff before stopping** -- if work is incomplete, `td handoff` captures state for the next agent.
- **Do NOT start new sessions mid-work** -- sessions track implementers. A new session mid-task looks like a bypass of the review guardrails and breaks audit trails. Use a real reviewer sub-agent instead.
- **Orchestrators: run `td review` yourself** -- it stamps `review_requested_by_session` so you retain close permission once a reviewer sub-agent records approval.
- **Use quiet mode after first read** -- `td usage -q` avoids repeating workflow instructions every time.
