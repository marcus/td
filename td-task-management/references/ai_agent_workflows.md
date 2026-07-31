# AI Agent Workflows with td

## Single-Issue Workflow

Use when focusing on one issue at a time:

```bash
# 1. Check what to work on
td usage

# 2. Start the issue
td start td-a1b2

# 3. As you work, log progress and decisions
td log "Implemented OAuth callback"
td log --decision "Using JWT for stateless auth"
td log --blocker "Unclear on refresh token rotation"

# 4. Before context ends, capture state
td handoff td-a1b2 \
  --done "OAuth flow, token storage" \
  --remaining "Refresh token rotation, logout flow" \
  --decision "Using JWT for stateless auth" \
  --uncertain "Should tokens expire on password change?"

# 5. Submit for review
td review td-a1b2

# 6. Prefer independent review when practical
# (Or, in trusted mode: --reviewed-by "<who>" to credit the reviewer,
#  or --self-review --reason to acknowledge reviewing your own work)
```

## Multi-Issue Workflow

Use when tackling related issues together:

```bash
# 1. Start a work session
td ws start "Auth implementation"

# 2. Tag issues to group them
td ws tag td-a1b2 td-c3d4 td-e5f6

# 3. As you work, log to the entire group
td ws log "Shared token storage implemented"
td ws log --decision "Storing refresh tokens in secure storage"

# 4. Before context ends, capture state for all
td ws handoff

# 5. Review completed issues
# Each issue goes to review independently
```

## Handling Blockers

When blocked:

```bash
# 1. Log the blocker
td log --blocker "Waiting for API specification from backend team"

# 2. Check what else you can work on
td next        # Next highest priority
td list --status open

# 3. Work on something else, come back later

# 4. If completely blocked, mark the issue
td block td-a1b2
```

## Resuming Work

When resuming an issue from a previous session:

```bash
# 1. Check current state
td usage

# 2. Get full context for the issue
td context td-a1b2

# 3. Start where you left off
td start td-a1b2

# 4. Continue logging and working
td log "Resumed work on refresh token rotation"
```

## Reviewing Code

When it's your turn to review:

```bash
# 1. See what's ready to review
td reviewable

# 2. Look at the issue details
td show td-a1b2

# 3. Approve or reject
td approve td-a1b2

# Or if issues need fixing:
td reject td-a1b2 --reason "Missing error handling for expired tokens"
```

## Tips for AI Agents

1. **Start a new context with `td usage --new-session -q`** - See current focus, pending reviews, and available work without repeating the full guide
2. **Log decisions and uncertainty when they aid continuity** - Use `--decision` and `--uncertain`
3. **Handoff work that will continue elsewhere** - Capture what is done and what remains
4. **Use work sessions for related issues** - Groups handoffs and makes tracking easier
5. **Track files with `td link`** - Helps future sessions know what changed
6. **Check blockers vs openwork** - If blocked, don't waste tokens retrying - work on something else
7. **Prefer independent review when practical** - Give a reviewing sub-agent its own `TD_CONTEXT_ID` so its independence is verifiable. In trusted mode, an involved session can instead record who reviewed the work with `td approve <id> --reviewed-by "<who>"`, or acknowledge a genuine self-review with `td approve <id> --self-review --reason "..."`. Never name a reviewer who did not review.
