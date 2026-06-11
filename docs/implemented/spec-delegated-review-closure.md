# Spec: Delegated Review Closure (Review Attestations)

## Summary

td now supports a **delegated review** policy that separates the "review was recorded" event from the "issue was closed" event. Review must come from a session that did not participate in implementation, but once an independent review has been recorded, any session may close the issue.

This replaces the earlier "the closing session must be different from the implementer" framing with a stronger, more useful invariant: an independent review is required; the close itself may be delegated to any session and is audited separately.

- Feature flag: `review_policy_mode` with values `strict` | `balanced` | `delegated`.
- Default when this spec shipped (Step 5): `delegated` for new installs. (Superseded: the trusted-review-mode change later flipped the default to `trusted`, which keeps delegated's review-attestation rules and adds an audited self-review escape hatch — see docs/plans/review-policy-trusted-mode-plan.md.) Projects that previously relied on the legacy `balanced_review_policy=true` flag continue to resolve to `balanced` (with a one-time deprecation warning). Projects that explicitly set `review_policy_mode=strict` keep the original close-time session lock.
- Opt out of delegated via `TD_FEATURE_REVIEW_POLICY_MODE=strict` (or `balanced`), or `td feature set review_policy_mode strict|balanced`.

## Motivation

The previous model was centered on session-locked closure: whichever session pressed the "close" button had to be different from the implementer. That reasonably blocked unchecked self-approval, but it was a bad fit for modern orchestrated workflows where:

- a parent orchestrator coordinates work across multiple sub-agents,
- a reviewer sub-agent provides real independent review,
- the orchestrator (or implementer) needs to perform the final close after the review is done.

Under the old rule, the orchestrator often could not close the issue itself because it had prior involvement (creator, review-requester, or observer). That pushed users toward artificial session rotation — a workaround that defeats the audit trail and doesn't actually add safety.

The delegated-review model keeps the important guardrail (no self-review) while eliminating the friction (delegated close is fine).

## Data Model

### New / updated fields on `issues`

- `reviewed_at DATETIME` — when the active approval review was recorded.
- `review_requested_by_session TEXT` — session that last submitted the issue via `td review`.
- `closed_by_session TEXT` — session that performed the final close (may differ from `reviewer_session` under delegated mode).

### New `issue_reviews` table

Append-only review history:

```sql
CREATE TABLE issue_reviews (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL,
    reviewer_session TEXT NOT NULL,
    decision TEXT NOT NULL,           -- approved | changes_requested | approved_by_parent_cascade
    summary TEXT NOT NULL DEFAULT '',
    requested_by_session TEXT DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    superseded_at DATETIME,
    FOREIGN KEY (issue_id) REFERENCES issues(id)
);
```

Review rows are synced as a first-class entity. Implementation-relevant mutations (title, description, type, priority, minor, parent, linked files, dependencies, certain status transitions) supersede the active review so stale approvals cannot be reused.

### Session actions

`IssueSessionAction` gained:

- `review_approved`
- `review_changes_requested`
- `closed`

`ActionSessionReviewed` is retained as a read-only legacy value so older DB rows still scan.

## Policy Modes

| Mode | Reviewer rule | Close rule |
|------|---------------|------------|
| `strict` | No prior involvement allowed | Same session reviews and closes |
| `balanced` | Strict, plus creator-approval exception with `--reason` | Same session reviews and closes (creator may close own created, not implemented, work) |
| `delegated` | Session must not have implementation history (`started` / `unstarted`); `created` and `reviewed` do NOT count | An active approval review enables close by any session; non-reviewer closes require `--reason` |

Minor tasks (`--minor`) continue to bypass review requirements entirely in all modes.

## CLI

- `td approve <id>` — review + close (Mode A) in strict/balanced; also acts as close-using-recorded-approval (Mode C) in delegated mode.
- `td approve <id> --record-only --reason "..."` — record approval review without closing (Mode B, delegated only).
- `td approve <id> --record-only --decision changes_requested --reason "..."` — record a non-approving review.
- `td reviewable [--include-approved]` — list issues the session can review; with `--include-approved`, also list reviewed issues the session can close.
- `td review <id>` — stamps `review_requested_by_session` so orchestrators retain close permission.
- `td close <id>` — admin-only scope (duplicates, won't-fix, cleanup); `in_review` issues cannot be closed via `td close` under delegated mode.
- `td reject <id>` — supersedes any active approval, clears reviewer stamps.

## Monitor

- New `V` key records an approval review without closing.
- `a` remains the approve-and-close / close-using-recorded-approval action.
- Task list shows `Reviewable`, `Ready to Close`, and `Pending Review` buckets.
- Modal toggle exposes `changes_requested` decision.

## HTTP API

- `POST /v1/issues/{id}/reviews` — record an approval or changes_requested review.
- `POST /v1/issues/{id}/approve` — extended to support close-using-recorded-approval with the same policy decisions as the CLI.
- DTOs expose `reviewed_at`, `closed_by_session`, and active-review metadata.

## Wording Shifts

The following wording changes ship across docs, inline help, and agent instructions:

- Old: "you cannot approve issues you implemented"
- New: "you cannot review your own implementation, but you can close after an independent review has been recorded"

- Old: "a different closing session is required"
- New: "an independent review is required; the close may be delegated to any session"

## Rollout

| Step | Status | Scope |
|------|--------|-------|
| 1 | done | Data model + shared `internal/reviewpolicy` package, all surfaces routed through it |
| 2 | done | CLI workflow: `--record-only`, close-after-review, undo extensions |
| 3 | done | Monitor parity (new action + buckets), serve API (`POST /reviews`) |
| 4 | done | Documentation + inline guidance |
| 5 | done (this change) | Flipped default mode to `delegated`; deprecated `balanced_review_policy` |

After Step 5 a fresh install resolves to `delegated` mode. `strict` and `balanced` remain supported via `review_policy_mode=strict|balanced` (env or config). The legacy `balanced_review_policy` flag still loads without error, but emits a one-time deprecation warning when explicitly set and will be removed in a future release.

## Rollback and Downgrade

The plan guarantees that moving back to `strict` or `balanced` requires no data cleanup:

- The new issue columns (`reviewed_at`, `review_requested_by_session`, `closed_by_session`) remain readable by strict/balanced code paths; those modes ignore them for policy decisions but keep them intact in the DB for audit.
- Historical `issue_reviews` rows are harmless under strict/balanced — the table is written but never consulted by those modes' eligibility checks.
- Issues closed via the delegated-only path stay closed after a downgrade and continue to report reviewer/closer correctly via `td show`.
- To downgrade, set `review_policy_mode=strict` (or `balanced`) in the project config, or unset the env override. No schema or row-level migration is needed.

Do **not** hand-delete rows from `issue_reviews`, `closed_by_session`, or related columns as part of a downgrade. Audit history is preserved deliberately.

## Relation to Prior Specs

- [spec-agent-review-bypass-prevention.md](./spec-agent-review-bypass-prevention.md) introduced creator-session tracking and the `issue_session_history` table. Those guardrails remain in place; delegated review reuses `WasSessionImplementationInvolved` from that spec as the basis for reviewer-independence.
- [spec-balanced-review-policy.md](./spec-balanced-review-policy.md) introduced the balanced mode (creator-approval exception) via the `balanced_review_policy` feature flag. Delegated review **supersedes** balanced for the orchestrator pattern: where balanced handled the "creator + different implementer" case via an approval exception with `--reason`, delegated handles the broader "independent reviewer + delegated closer" case via explicit review attestations. `balanced_review_policy` is deprecated as of Step 5 and will be removed in a future release.

## Upgrading From Balanced (Default Prior to Step 5)

If your project ran td prior to Step 5 without setting any flag, it was resolving to `balanced` via the legacy `balanced_review_policy` default (`true`). After Step 5, that same project resolves to `delegated`. The practical differences:

- **Reviewer eligibility broadens**: any session without implementation history may review (balanced also allowed the creator via the creator-exception path, which delegated preserves via the "no implementation history" rule).
- **Close eligibility is now split from review eligibility**: once an independent approval is recorded, any session may close. Previously only a non-implementer session could close in the same step as approving.
- **`td reviewable` output splits**: issues with an active recorded approval no longer appear under "awaiting review" — pass `--include-approved` to also see the "ready to close" bucket.
- **`td approve --record-only`** is the new way to record an approval without closing; it is delegated-only.

To keep the old behavior, opt into `balanced` explicitly: set `review_policy_mode=balanced` in your project config (or `TD_FEATURE_REVIEW_POLICY_MODE=balanced` in the environment). The legacy `balanced_review_policy=true` flag also still maps to balanced, with a one-time deprecation warning per process.

## Authoritative Planning Doc

The planning and design rationale lives in [docs/plans/orchestrator-review-closure-plan.md](../plans/orchestrator-review-closure-plan.md). That document is the source of truth for the mental model ("review attestations, delegated close"), the data model choices, the cascade exemption, and the rollout sequence.
