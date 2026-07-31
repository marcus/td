---
sidebar_position: 10
---

# AI Agent Integration

## Overview

td gives coding agents durable task context across conversations: current work,
useful progress, decisions, handoffs, and reviews. Any agent that can run shell
commands can use it.

Works with: Claude Code, Cursor, OpenAI Codex, GitHub Copilot, Gemini CLI, or any agent with shell access.

## Project Guidance

`td init` can add this compact block to `AGENTS.md`, `CLAUDE.md`, or another
recognized agent file:

```markdown
## Working with td

td keeps task context durable across sessions. In a new context, run `td usage --new-session -q` to see current work.

Use your judgment about how much tracking a task needs. For substantive work: `td start <id>`, record progress with `td log`, hand off with `td handoff <id>`, then `td review <id>`.

Closing needs a review. Say who did it (default trusted mode; delegated/strict allow only the first):

- independent session: `td approve <id> --reason "..."`
- a sub-agent: `td approve <id> --reviewed-by "<who>"`
- you: `td approve <id> --self-review --reason "..."`

Prefer a reviewer with its own `TD_CONTEXT_ID`; never name one who did not review.

Run `td usage` or `td <command> --help`.
```

The block intentionally leaves task-level judgment to the agent. It points to
the normal lifecycle and keeps detailed or uncommon commands in on-demand help.

## The `td usage` Command

Start a new agent context with:

```bash
td usage --new-session -q
```

The compact output shows current work, handoffs, reviews, and available issues.
Run `td usage` without `-q` when the workflow overview would help.

## Typical Workflow

```bash
td start <id>
td log "Implemented the parser"        # When the progress will aid continuity
td log --decision "Preserve comments"  # When the reasoning matters later
td handoff <id> --done "..." --remaining "..."  # When another context will continue
td review <id>
```

Use `td ws` commands when several related issues benefit from shared logs and a
shared handoff.

## Reviews

The default `trusted` policy prefers independent review while allowing an
explicit, audited self-review.

For an independent review:

```bash
td approve <id> --reason "Reviewed diff; tests pass"
```

When you implemented the work yourself, trusted mode asks you to say who
reviewed it:

```bash
# A sub-agent reviewed it — credit them (no reason required)
td approve <id> --reviewed-by "code-reviewer sub-agent"

# You reviewed your own work — say so
td approve <id> --self-review --reason "Reviewed own diff; tests pass"
```

td cannot verify `--reviewed-by`; it exists so an orchestrator recording a
sub-agent's review writes a true record instead of claiming a self-review it did
not perform. Naming a reviewer who did not review is worse than an honest
self-review, because it reads as independent in the audit trail.

A reviewer can also attest without closing — `td approve <id> --record-only
--reason "..."` — after which any session may close. This works in the default
trusted mode and in delegated. Projects that need a mechanical independence
boundary can set `review_policy_mode=delegated` or `strict`, where an involved
session cannot approve at all. See `td approve --help` for the complete policy
and closure options.

## Tips

- Keep logs concise and add them when they will help a later context.
- Leave a handoff when work will continue elsewhere.
- Let sessions reflect real agent contexts; do not create one merely to make a
  review appear independent.
- Use `td context <id>` to refresh an issue and `td <command> --help` whenever
  the next command is unclear.
