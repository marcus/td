# Multi-Agent Sessions: Independent Reviews for Sub-Agents

When an orchestrator spawns sub-agents (an implementer, a reviewer, a tester) and
they all run `td` commands, those commands can collapse into a **single td
session**. They share the same git branch, the same long-lived agent process
(the orchestrator's PID), and the same `.todos` checkout — which are exactly the
three dimensions td uses to key a session.

The practical failure: a reviewer sub-agent runs `td approve <id>`, but td sees
the reviewer and the implementer as the *same* session and treats the approval
as a self-review. The independent review you actually performed gets recorded as
non-independent.

This doc shows how to give each sub-agent context its **own** td session so
delegated reviews are recorded as genuinely independent.

## How session identity works

A session is keyed by `branch + agent_fingerprint + match_context_id`
(`internal/session/session.go`, `GetSessionByIdentity`). Two of those dimensions
are usually identical across an orchestrator's sub-agents:

- **branch** — they share the working tree, so the same branch.
- **agent_fingerprint** — derived from the stable parent agent PID; all
  sub-agents share the orchestrator process, so the fingerprint matches.

The third dimension, `match_context_id`, is the lever you control. It comes
solely from the `TD_CONTEXT_ID` environment variable (`matchContextID()`,
`session.go:105`). Set a distinct value per sub-agent and each one gets its own
session.

## Three routes (recommended ordering)

### 1. `TD_CONTEXT_ID` — recommended

Set a distinct `TD_CONTEXT_ID` per spawned sub-agent context. It feeds the
session lookup key without touching what `TD_SESSION_ID` means (exact-session
identity). When `TD_CONTEXT_ID` is unset you get today's behavior exactly — it is
fully backward compatible (empty matches the empty `match_context_id` of existing
rows).

Give the reviewer context its own session:

```bash
# Inside the reviewer sub-agent's context, before any td command:
export TD_CONTEXT_ID=reviewer-td-a1b2
td session --new           # creates an independent session under this context key

# Now the reviewer is a distinct session from the implementer:
td approve td-a1b2 --record-only --reason "Reviewed diff, tests pass"
# No --self-review needed: this is a genuinely independent session.
```

The implementer context, meanwhile, uses its own value (or none):

```bash
# Inside the implementer sub-agent's context:
export TD_CONTEXT_ID=impl-td-a1b2
td start td-a1b2
```

Because the two contexts carry different `TD_CONTEXT_ID`s, their sessions never
collapse, and the reviewer's approval is recorded as independent.

> This is the mechanism that was dogfooded while building the session-identity
> epic — the reviewer sub-agents used exactly this pattern.

### 2. Unique `TD_SESSION_ID` per sub-agent — interim / zero-schema

`TD_SESSION_ID` is already treated as explicit identity (the `explicit:` path in
`getContextID()`, `session.go:111`). Setting a unique one per sub-agent also
yields a distinct session, and it works on **older td builds** that predate
`TD_CONTEXT_ID`.

```bash
# Reviewer sub-agent context (works on older td too):
export TD_SESSION_ID=reviewer-td-a1b2
td approve td-a1b2 --record-only --reason "Reviewed diff, tests pass"
```

**Trade-off:** `TD_SESSION_ID` overrides the *whole* fingerprint / exact-session
notion, not just the context dimension. Prefer `TD_CONTEXT_ID` when the td build
supports it (it preserves `TD_SESSION_ID`'s exact-session meaning); reach for
`TD_SESSION_ID` only when you need the zero-schema interim path or
backward-compatibility.

### 3. Worktree isolation — forthcoming, not yet implemented

When each sub-agent runs in its own git worktree (e.g. Claude Code's
`isolation: "worktree"`), worktree-scoped session keying will give each sub-agent
a distinct session for free — no env var to set.

**This is not yet implemented.** Worktree identity is deferred to a later epic
(see `docs/plans/session-worktree-flow-recommendations.md`). Until it lands, use
route 1 or 2.

## When to use which

| Situation | Use |
|-----------|-----|
| Current td build, want clean separation | **`TD_CONTEXT_ID`** (route 1) |
| Older td build, or zero schema assumptions | Unique `TD_SESSION_ID` (route 2) |
| Each sub-agent in its own worktree | Worktree keying (route 3) — *not yet available* |

## Orchestrator checklist

1. For each spawned sub-agent context, export a distinct `TD_CONTEXT_ID`
   (e.g. `impl-<taskid>`, `reviewer-<taskid>`) before the sub-agent runs any
   `td` command.
2. In the reviewer context, run `td session --new` so the independent session is
   materialized, then `td approve <id> --record-only --reason "..."`.
3. The orchestrator (or any session) performs the final close using the recorded
   independent approval. See the review-model section in `CLAUDE.md`.

This keeps delegated reviews genuinely independent instead of collapsing into the
orchestrator's session.
