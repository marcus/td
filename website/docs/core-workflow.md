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
td review td-a1b2                # Submit for review (status -> in_review)
td reviewable                     # Issues you can independently review
td reviewable --include-approved  # Also show reviewed issues you can close
td approve td-a1b2                # Approve and close
td reject td-a1b2 --reason "Missing error handling"  # Back to open
```

**You cannot review your own implementation, but you can close after an independent review has been recorded.** An independent review is required; the close itself may be delegated to any involved session.

### Review Policy Modes

td exposes three policy modes via `review_policy_mode`:

- `delegated` — review attestations with delegated close. **Default for new installs.** Reviewer independence is enforced; any involved role (creator, implementer, reviewer, review-requester) may perform the final close once an independent review has been recorded.
- `strict` — no prior involvement allowed on the reviewer at all. Preserved for orchestrators that want the legacy close-time session lock.
- `balanced` — strict, plus a creator-approval exception (see below). Retained for projects that explicitly opt in.

The legacy `balanced_review_policy` flag is **deprecated**; prefer `review_policy_mode=balanced` instead. Setting the legacy flag still works but emits a one-time deprecation warning.

Set the mode per-project or via env:

```bash
td feature set review_policy_mode strict   # or balanced, or delegated
# or, one-off:
TD_FEATURE_REVIEW_POLICY_MODE=strict td approve td-a1b2
```

### Balanced (Legacy) — Creator Exception

Under `balanced`, a session that *created* a task (but didn't implement it) can approve with a reason:

- **Implementer self-approval is always blocked** — if you started or worked on a task, you can't approve it.
- **Creator-approval is allowed** when a *different* session did the implementation, with `--reason`.
- **All other previously-involved sessions remain blocked**.

```bash
# Lead creates work, agent implements, lead approves
td add "Build feature X"
td start td-a1b2           # agent session
td review td-a1b2
td approve td-a1b2 --reason "Reviewed output, looks good"   # lead session
```

Creator-exception approvals are audited in `td security`.

### Delegated — Review Attestations and Delegated Close

Under `delegated`, the review step and the close step are separate:

- A reviewer session records an approval (or requests changes) via `td approve --record-only --reason "..."`.
- Once an approval review exists, **any involved session** (creator, implementer, review-requester, reviewer-of-record) may close with `td approve`.

Two flows are natural under this mode:

**Direct reviewer-close** — the reviewer both approves and closes in one step:

```bash
td review td-a1b2
td approve td-a1b2                 # reviewer session: approve + close
```

**Record approval, close later** — a reviewer sub-agent attests, and the orchestrator or implementer closes when convenient:

```bash
td review td-a1b2                                                          # orchestrator submits
td approve td-a1b2 --record-only --reason "Reviewed diff, tests pass"      # reviewer records
td approve td-a1b2                                                         # orchestrator closes
```

The second flow is the typical orchestrator pattern. The orchestrator must own at least one role on the issue (creator, implementer, reviewer-of-record, or `review_requested_by_session`); submitting the issue with `td review` is the simplest way to reserve the close permission.

A reviewer can also record a non-approving decision:

```bash
td approve td-a1b2 --record-only --decision changes_requested --reason "fix X"
```

Use `td reviewable --include-approved` to surface reviewed issues you're allowed to close.

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

Every terminal or context window gets an automatic session ID. This powers the core review guardrail: the review must come from a session that did not participate in implementation.

Why this matters:
- Forces structured handoffs between sessions rather than implicit assumptions
- A fresh session reading the code with new eyes catches issues the implementer missed
- Prevents a single long-lived session from both writing and rubber-stamping code
- Mirrors real code review, where a different person checks the work

**Do not start a new session mid-work just to satisfy the review rules.** Sessions track implementers, and an artificial mid-task rotation defeats the audit trail. Use a real reviewer sub-agent or a separate agent context instead.

Under `delegated` mode the *review* must be independent but the *close* may be delegated to any involved session — see [Review Workflow](#review-workflow) above. Under `balanced` mode, a creator-approval exception allows the same session that created a task to approve it once a different session implemented it.
