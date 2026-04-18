---
sidebar_position: 2
---

# Core Workflow

td manages the full lifecycle of issues from creation through review, with structured handoffs between AI sessions to prevent context loss.

## Creating Issues

Create issues with a title, type, and optional priority:

```bash
td create "Add user authentication" --type feature --priority P1
td create "Login button misaligned" --type bug
td create "Refactor auth module" --type task --priority P2
td create "Capture sync edge cases" --description-file docs/issue-description.md
```

**Types:** `bug`, `feature`, `task`, `epic`, `chore`

**Priorities:** `P0` (critical) through `P4` (lowest). Defaults to P2 if omitted.

For markdown-heavy descriptions or acceptance criteria, prefer `--description-file` and `--acceptance-file`. They preserve blank lines, indentation, and fenced code blocks exactly, and `-` reads the whole field from stdin:

```bash
cat docs/acceptance.md | td update td-a1b2 --append --acceptance-file -
```

## Starting Work

Pick up an issue to work on:

```bash
td start td-a1b2        # Begin work, sets status to in_progress
td next                  # Show highest-priority open issue
td focus td-a1b2         # Set current focus without changing status
```

`td start` transitions the issue to `in_progress` and records which session is working on it. Use `td focus` when you want to track what you're looking at without formally starting work.

## Logging Progress

Record decisions, blockers, and findings as you work:

```bash
td log "OAuth callback working"
td log --decision "Using JWT for stateless auth"
td log --blocker "Unclear on refresh token rotation"
td log --hypothesis "Token refresh might conflict with SSO"
td log --tried "Attempted session-based approach, too complex"
td log --result "Benchmarks show 2ms token validation"
```

Logs attach to your current focus issue. They build a timeline that future sessions can read to understand what happened and why.

## Handoffs

Handoffs are the key feature of td. They capture structured state so the next session can pick up work without guessing:

```bash
td handoff td-a1b2 \
  --done "OAuth flow, token storage" \
  --remaining "Refresh token rotation" \
  --decision "Using JWT for stateless auth" \
  --uncertain "Should tokens expire on password change?"
```

Each field serves a purpose:
- **--done**: What was completed this session
- **--remaining**: What still needs to be done
- **--decision**: Choices made and why (prevents re-litigating)
- **--uncertain**: Open questions for the next session to resolve

Without handoffs, the next AI session starts from scratch, re-reads code, and may redo work or contradict earlier decisions. Handoffs eliminate that waste.

## Review Workflow

Submit completed work for review:

```bash
td review td-a1b2         # Submit for review (status -> in_review)
td reviewable              # Show issues reviewable by current session
td approve td-a1b2         # Close the issue
td reject td-a1b2 --reason "Missing error handling"  # Back to in_progress
```

The session that implemented an issue **cannot** approve it — but the review policy is smarter than a blanket block.

### Balanced Review Policy (default)

td uses a **balanced review policy** that distinguishes between creating a task and implementing it:

- **Implementer self-approval is always blocked** — if you started or worked on a task, you can't approve it.
- **Creator-approval is allowed** when a *different* session did the implementation, as long as you provide a reason: `td approve td-a1b2 --reason "Reviewed agent output, tests pass"`.
- **All other previously-involved sessions remain blocked**.

This unlocks the common lead/worker pattern:

```bash
# Lead creates and assigns work
td add "Build feature X"

# Agent implements (different session)
td start td-a1b2
td log "implemented feature X"
td review td-a1b2

# Lead reviews and approves (same session that created)
td approve td-a1b2 --reason "Reviewed output, looks good"
```

Creator-exception approvals are audited in the security log (`td security`).

To revert to strict mode (no creator-exception): `td feature set balanced_review_policy false`.

## Issue Lifecycle

```
open --> in_progress --> in_review --> closed
              |              |
              v              | (reject)
          blocked -----------+
```

- **open**: Created, not yet started
- **in_progress**: Actively being worked on
- **blocked**: Waiting on a dependency (auto-unblocks when all dependencies close)
- **in_review**: Implementation complete, awaiting review
- **closed**: Approved and done

Rejection sends an issue back to `in_progress` with a reason attached, so the implementer knows what to fix.

## Session Isolation

Every terminal or context window gets an automatic session ID. This powers the review constraint: the implementing session cannot also approve its own work.

Why this matters:
- Forces structured handoffs between sessions rather than implicit assumptions
- A fresh session reading the code with new eyes catches issues the implementer missed
- Prevents a single long-lived session from both writing and rubber-stamping code
- Mirrors real code review, where a different person checks the work

The balanced review policy relaxes this for task *creators* who didn't implement the work — see [Review Workflow](#review-workflow) above.
