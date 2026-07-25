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

td keeps task context durable across agent sessions. At the start of a new context, run `td usage --new-session -q` to see the current work.

Use your judgment about how much tracking the task needs. For substantive work, use `td start <id>`, record useful progress or decisions with `td log`, hand off unfinished work with `td handoff <id>`, and submit completed work with `td review <id>`.

Prefer an independent review when practical. In the default trusted mode, self-review is allowed and audited:

`td approve <id> --self-review --reason "..."`

Run `td usage` for workflow guidance or `td <command> --help` for details.
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

When self-review is appropriate:

```bash
td approve <id> --self-review --reason "Reviewed own diff; tests pass"
```

The `--self-review` flag requires a reason and records the review accordingly.
Projects that require a hard independence boundary can set
`review_policy_mode=delegated` or `strict`. Delegated mode also supports
`--record-only` review followed by closure from another session. See
`td approve --help` for the complete policy and closure options.

## Tips

- Keep logs concise and add them when they will help a later context.
- Leave a handoff when work will continue elsewhere.
- Let sessions reflect real agent contexts; do not create one merely to make a
  review appear independent.
- Use `td context <id>` to refresh an issue and `td <command> --help` whenever
  the next command is unclear.
