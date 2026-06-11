# Plan: Trusted Review Mode — Orchestrator Approval Without Session Locks

Status: implemented (trusted is now the default review_policy_mode; stages 1-5 complete)
Builds on: docs/plans/orchestrator-review-closure-plan.md (delegated mode, shipped)

## Summary

Loosen td's review guardrails so an orchestrating agent can approve and close
tasks directly, without needing a session-independent reviewer. The hard block
on "implementer cannot approve" becomes an audited attestation instead of a
wall. Independent review remains the *recommended* flow and stays fully
supported and visible in the audit trail — it just stops being enforced by
refusing the command.

## Background: Where We Are Now

The `delegated` mode (current default) already removed most of the friction:

- Creator-only orchestrators ARE eligible reviewers (creating an issue does
  not disqualify you).
- Any session may close once an independent approval is recorded
  (`td approve --record-only` by a reviewer, then anyone closes).
- Minor issues bypass everything.

The one remaining hard rule, enforced in
`internal/reviewpolicy/policy.go:evaluateReviewerDelegated`:

> A session that implemented the issue (current implementer, or any
> started/unstarted history row) can never record an approval.

In practice this still forces orchestrators into workarounds:

- An orchestrator that briefly ran `td start` (to triage, to fix a one-line
  nit, to unblock) is permanently disqualified from approving that issue.
- Fully autonomous single-agent loops (ralph-style overnight runs) must spawn
  a reviewer sub-agent purely to satisfy the policy, even when the orchestrator
  has already run the review itself with full diff context.
- The policy assumes the model can't be trusted to review its own work.
  For current-generation models running as orchestrators, the review *quality*
  is no longer the bottleneck — the policy mostly generates session-juggling
  ceremony and "cannot approve" error loops.

What we still want to keep:

- **An audit trail that distinguishes independent review from self-review.**
  Trust does not mean losing the record of who reviewed what.
- **A review step at all.** The `open → in_progress → in_review → closed`
  lifecycle is valuable even when one agent walks an issue through it: it
  forces an explicit "I checked this" moment and a logged reason.
- **The ability to opt back into strictness** per project (CI bots, teams
  that mix humans and agents, high-stakes repos).

## Options Considered

### Option A — Remove the restriction entirely

Delete the implementer check from `evaluateReviewerDelegated`. Anyone approves
anything; `--reason` still required.

- Pros: simplest; zero ceremony.
- Cons: self-approval becomes indistinguishable from independent approval in
  the flow (audit can still infer it, but nothing marks it at decision time);
  no friction at all means an implementing sub-agent that ignores instructions
  can silently self-approve — exactly the case we still want visible.

### Option B — Trusted mode with audited self-approval (recommended)

Add a fourth mode, `trusted`, and make it the default. The implementer-cannot-
approve rule becomes: self-approval is **allowed with an explicit
acknowledgment**, recorded distinctly in the audit trail.

- The blocked path turns into a one-flag path: `td approve td-x --reason "..."`
  works for everyone *except* the implementer-of-record; the implementer adds
  `--self-review` (exact flag name open to bikeshedding) to acknowledge it.
- The approval row records `self_review = true` plus the implementer linkage,
  so `td list`, `td show`, the monitor, and sync history all show whether a
  close came from independent review or self-attestation.
- Orchestrators with incidental implementation history (the `td start` nit-fix
  case) approve with no extra flag — only the *implementer-of-record/
  substantive history* triggers the self-review acknowledgment.

This is the middle ground the request asks for: independent review remains the
default-documented flow, the implementer not self-approving remains the norm
(it requires a deliberate extra flag), but nothing hard-blocks an intelligent
orchestrator from finishing its own work.

### Option C — Soft warning only

Keep delegated semantics but downgrade the rejection to a warning + proceed.

- Cons: warnings in agent loops are noise that gets ignored; no audit
  distinction; doesn't actually express a policy. Worst of both.

**Recommendation: Option B**, with Option A available as
`review_policy_mode=open` only if discussion concludes even the flag is too
much ceremony. (My read: one flag the model passes intentionally is cheap, and
the audit distinction it buys is the entire remaining value of the system.)

## Design (Option B)

### Mode semantics

| | strict | balanced | delegated | **trusted** (new default) |
|---|---|---|---|---|
| Creator approves | no | with `--reason` | yes | yes |
| Involved-but-not-implementer approves | no | no | yes | yes |
| Implementer approves | no | no | no | **yes, with `--self-review`** |
| Close after recorded approval | n/a | n/a | anyone | anyone |
| Direct approve+close in one step | independent only | independent only | independent only | **anyone (self needs flag)** |

### Reviewer eligibility (`internal/reviewpolicy/policy.go`)

Add `ModeTrusted Mode = "trusted"` and:

```go
// ReviewerEligibilityInput gains:
SelfReviewAcknowledged bool // caller passed --self-review (or UI equivalent)

// ReviewerEligibility gains:
SelfReview bool // decision is a self-review; callers stamp it on the review row
```

`evaluateReviewerTrusted`:

1. If session is not the implementer and has no implementation history →
   allowed (same as delegated).
2. If session is the implementer/has implementation history:
   - with `SelfReviewAcknowledged` → allowed, `SelfReview: true`,
     `RequiresReason: true`.
   - without → rejected with a *teaching* message: tell the agent it can either
     delegate review to an independent session (preferred) or re-run with
     `--self-review --reason "..."`. The error text is the guardrail now —
     make it state the norm, not just the syntax.

`EvaluateCloseEligibility` in trusted mode mirrors delegated's three cases,
with case 2 (in_review, no recorded approval) routing through the trusted
reviewer predicate so direct approve-and-close works for the implementer with
the flag. Case 3 (not in_review) keeps the existing gate — trusted mode does
not let anyone close an issue that never entered review, except the existing
creator-open-bypass for never-implemented throwaways.

### Data model

`issue_reviews` gains a `self_review INTEGER NOT NULL DEFAULT 0` column
(new migration, plus sync/action_log plumbing per the defer/due precedent in
`cmd/` and sync import/export). Decision constants unchanged — self-review is
an attribute of an `approved` row, not a new decision type.

### CLI (`cmd/review.go`)

- New `--self-review` flag on `td approve` (valid in trusted mode only;
  in other modes it errors with "requires review_policy_mode=trusted").
- `--self-review` implies the `--reason` requirement (reuse the existing
  required-reason machinery from `--record-only`).
- Approval output and `td show` render `(self-review)` on the review line.
- Update help text for `td approve`, `td review`, `td usage`, `td context`.

### Other surfaces (must stay in policy lockstep — this was the whole point
of `internal/reviewpolicy`)

- `internal/db/issues.go` `ReviewableByFilter`: in trusted mode the filter no
  longer excludes self-implemented issues (everything in_review is reviewable
  by you; the flag requirement is enforced at action time, not query time).
- `pkg/monitor/actions.go`: approve action in trusted mode prompts a confirm
  modal for self-review ("You implemented this. Approve as self-review?") and
  records the same audit bit.
- `internal/serve/handlers_transitions.go` and
  `internal/api/snapshot_query_source.go`: accept a `self_review` field on the
  approve transition; same predicate via the shared package.

### Feature flag & rollout (`internal/features/features.go`)

- `ParseMode` accepts `trusted`; `ResolveReviewPolicyMode` default flips
  `delegated → trusted`.
- Explicit `review_policy_mode=` settings are honored unchanged, so projects
  that pinned strict/balanced/delegated see no behavior change.
- Env override `TD_FEATURE_REVIEW_POLICY_MODE=trusted` works day one for
  testing before the default flips (mirror the delegated rollout: ship the
  mode opt-in first, flip the default in the next minor release once the
  orchestrate/ralph skills have exercised it).

### Docs & agent guidance

- `CLAUDE.md` / `AGENTS.md`: rewrite the "Review Model" section. The norm
  becomes: *prefer* delegating review to an independent sub-agent; when you
  are the orchestrator and have already reviewed the diff yourself, approve
  with `--self-review --reason` instead of fabricating a session. Explicitly
  delete the "do NOT start a new session mid-work" workaround warnings that
  exist only because of the old wall.
- `td usage` output: same message, shorter.
- Skills that encode the old ceremony (`orchestrate`, `td-ralph-loop`,
  `td-review-session`, `td-task-management`) need their review steps updated
  in a follow-up pass.

### Tests

- `internal/reviewpolicy/policy_test.go`: trusted-mode table cases — non-
  implementer approve, implementer without flag (rejected, message mentions
  both paths), implementer with flag (allowed + SelfReview), close cases 1–3,
  minor bypass, unknown-mode fail-closed still routes to strict.
- `cmd/approve_*_test.go`: flag gating per mode, reason requirement, audit
  field round-trip, `td show` rendering.
- Parity tests (`parity_surface_test.go` etc.): CLI / monitor / serve /
  snapshot all agree on trusted-mode decisions.
- Sync: `self_review` survives export/import.

## Sequencing

1. **Policy core** — `ModeTrusted`, predicates, tests. Pure, no callers.
2. **Migration + models** — `self_review` column, scan/write, action log, sync.
3. **CLI** — `--self-review` on approve, reason plumbing, help text.
4. **Parity surfaces** — ReviewableByFilter, monitor confirm modal, serve/API.
5. **Default flip + docs** — resolver default `trusted`, CLAUDE.md/AGENTS.md,
   usage/context text, skill updates.

Steps 1–4 ship behind the opt-in env/feature value; step 5 flips the default.

## Resolved Decisions (2026-06-10)

Owner approved Option B with all four recommendations:

1. **Flag name** = `--self-review`. Names what's being acknowledged, not the
   mechanics.
2. **What triggers the flag** = the same condition the current `delegated`
   mode hard-blocks on: `SessionIsImplementer || HasImplementationHistory`
   (implementer-of-record, or any substantive `started`/`unstarted` history
   row). We convert that exact reject condition into a flag-gated allow rather
   than introducing a new, looser predicate. This supersedes the earlier
   summary line that implied the `td start` nit-fix case needs no flag — for a
   clean, parity-stable implementation the trigger condition is unchanged from
   delegated; only the *outcome* (reject → allow-with-flag) changes. A session
   that merely created/viewed the issue (no started history) still needs no
   flag, exactly as in delegated mode.
3. **Option A (`open` mode)**: NOT registered now. Every mode is parity-test
   surface forever; add it only if someone asks.
4. **Per-issue strictness escalation**: deferred. The per-project mode covers
   the realistic cases; revisit if a concrete need appears.
