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
td approve td-a1b2 --reason "Reviewed diff, tests pass"
# No --self-review needed: this is a genuinely independent session.
```

> An independent reviewer session can either close directly with
> `td approve <id> --reason "..."`, or record the approval *without* closing via
> `td approve <id> --record-only --reason "..."` and let another session perform
> the close later. Both the default `trusted` mode and `delegated` mode support
> record-only; `strict` and `balanced` reject it.

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
td approve td-a1b2 --reason "Reviewed diff, tests pass"
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

## Which review path to use

| Reviewing sub-agent has... | Use | Independence is |
|---|---|---|
| its own `TD_CONTEXT_ID` | the sub-agent runs `td approve` or `td approve --record-only` | mechanically verified |
| the orchestrator's session | `td approve --reviewed-by "<name>"` | asserted, not verified |

Prefer the first. Giving each sub-agent context its own `TD_CONTEXT_ID` costs one
environment variable and makes the guarantee real rather than social.

`--reviewed-by` is for the case where that is genuinely not available — a
sub-agent that shares the orchestrator's session still did the review, and
recording that is more honest than `--self-review`. td does not verify the name.

## Orchestrator checklist

1. For each spawned sub-agent context, export a distinct `TD_CONTEXT_ID`
   (e.g. `impl-<taskid>`, `reviewer-<taskid>`) before the sub-agent runs any
   `td` command.
2. In the reviewer context, run `td session --new` so the independent session is
   materialized, then either:
   - `td approve <id> --reason "..."` — the independent session reviews and
     closes in one step, or
   - `td approve <id> --record-only --reason "..."` — the independent session
     attests only, and the orchestrator (or any session) performs the final
     close with `td approve <id> --reason "..."` using that recorded approval.
3. Prefer record-only when the orchestrator wants to sequence the close itself
   (e.g. batching, or landing a commit first). Either way the approval on record
   came from a session that did not implement the work, which is the property
   that matters. See the review-model section in `CLAUDE.md`.

This keeps delegated reviews genuinely independent instead of collapsing into the
orchestrator's session.

## Reclaiming claims from killed agents

An agent that is killed mid-work — by a subscription usage limit, a wall-clock
timeout, a crashed harness — never runs `td handoff` and never runs
`td unstart`. Its `in_progress` claim stays on the issue, so the work never
returns to the ready queue. A supervisor that watches "ready work" sees the
fleet go quiet and reads it as idle rather than broken.

There are two commands, and the difference matters.

### `td unstart --session <id>` — exact, and what a supervisor should use

```bash
td unstart --session ses_abc123                  # preview: what it holds
td unstart --session ses_abc123 --force --json   # release exactly those claims
```

This releases every `in_progress` claim held by ONE named session. There is no
liveness heuristic: the caller already knows which session died, because it is
the one whose process it just killed. In a fleet of N parallel slots this is
the only safe reaper — releasing by idle time instead would sweep every other
slot that happens to be quiet, and two agents would then work the same issue.

A supervisor that kills a tick should map that tick's context to its session
(`TD_CONTEXT_ID=<ctx> td whoami --json`, field `session`) and release by id.
A session id that names no session and holds no claim is an error, not an
empty success, so a typo cannot pass silently.

### `td unstart --stale <duration>` — the backstop

```bash
td unstart --stale 2h                 # preview: what would be released
td unstart --stale 2h --force --json  # release, and report what was released
```

Use it for holders nobody is tracking — a crashed agent from another machine,
a session id you no longer have. It previews by default and mutates only with
`--force`, like `td session cleanup`. The `--json` envelope lists each claim
with its issue id, the holding session, `idle_seconds`, its last activity, and
which signal that activity came from, so a supervisor can log exactly what it
reclaimed. Released issues carry the reason in their history, so the cause is
visible in `td show`.

### How liveness is measured

A claim's liveness is the **most recent** of three signals:

1. the holder session's `last_activity`,
2. the issue's own `updated_at`, and
3. the newest history entry (log or action) on that issue, **by any session**.

Signals 2 and 3 exist because a session row is not a stable identity for a
running agent. Session identity is `branch + agent fingerprint + match context
+ worktree`, so an agent that runs `git checkout -b` — or moves to another
worktree, which every slot of a worktree-per-slot fleet does — mints a **new**
session row and stops heartbeating the one recorded in `implementer_session`.
Measuring only that row would read a live, working agent as three hours dead.

td also never releases a claim held by the calling session or by any session in
its identity lineage: its `previous_session_id` chain, and any session row
minted by the same agent process (same fingerprint and match context) on
another branch or worktree.

### Only mutations move the signal

**Read-only td commands do not refresh a session's activity.** `td list`,
`show`, `ready`, `next`, `search`, `stats`, `session list`, and `board list`
all leave `last_activity` untouched; only commands that write do — `start`,
`log`, `update`, `close`, `handoff`, `focus`, `whoami`, `td list <query>`, and
the rest of the mutating family.

This is deliberate: every td write takes a cross-process lock on the database,
and turning every read into a writer would put a whole fleet's `td list` calls
in contention with the `td start` that a tick depends on. The cost is that the
threshold must be read precisely — it is the longest a healthy agent can go
without **writing** to td, not without using it.

Two practical consequences:

- An agent that reads context and then works for two hours is invisible to
  `--stale`. If your agents do that, have them run `td log` (or `td whoami`,
  which is a cheap explicit heartbeat) periodically.
- Do not pick a threshold from how often your agents *use* td. Pick it from
  your tick timeout, and prefer `--session` when you know who died.

### Choosing the `--stale` threshold

**The threshold must exceed the longest a healthy agent can run without
writing to td** — in a supervisor loop, your tick timeout. There is
deliberately no default: too short a value releases a live agent's claim, a
second agent picks the issue up, and two agents work the same issue in
parallel. That is a worse failure than the leak this fixes.

td does not use `sessions.agent_pid` to sharpen this, because sessions sync
between machines and a pid from another host means nothing locally (worse, it
can collide with an unrelated live process).

The `d` suffix (`--stale 30d`) is td's own extension and is range-checked: an
out-of-range value is rejected rather than silently wrapping into a short one.

### What is never touched, and what is reported

- **The calling session's claims**, and those of any session in its identity
  lineage. Definitionally live.
- **Claims td cannot measure at all** — no implementer recorded, or no usable
  timestamp anywhere. These are reported under `unresolved` in JSON and as
  warnings in human output, and the envelope's `action` gains a
  `_with_unresolved` suffix so a caller that only reads `action` still sees
  that something was left behind. Release them explicitly with
  `td unstart <id>` once you have established the holder really is gone.

Note the failure direction: an unmeasurable holder is **never** released. An
unparseable timestamp used to read as the zero time, whose idle duration
exceeds every threshold — so "we cannot measure this" meant "release it", the
exact opposite of the intent.

### `td session cleanup` and reclamation do not fight

`td session cleanup --older-than <dur>` deletes idle session rows. Those rows
are what makes a leaked claim reclaimable, so cleanup **skips any session that
still holds an `in_progress` claim** and names it in its output (`held` in
JSON, a warning otherwise) together with the command that clears it. Without
that, a cron running both would delete the row first and strand the issue.

Reclamation covers the other direction too: if a holder's session row is gone
anyway, `--stale` falls back to the work recorded on the issue, so the claim
stays reclaimable rather than leaking forever.
