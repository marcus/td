# Spec: Balanced Review Policy

> **Superseded for orchestrator flows by** [spec-delegated-review-closure.md](./spec-delegated-review-closure.md).
> Balanced mode remains available as a policy mode (`review_policy_mode=balanced`) for projects that want the creator-exception behavior without the full delegated-review model. The broader orchestrator use case — independent reviewer + delegated close — is now handled by the `delegated` mode described in that spec.

## Summary

TD now supports a balanced review policy that reduces coordinator friction in multi-agent workflows while preserving anti-rubber-stamp guardrails.

- Feature flag: `balanced_review_policy`
- Default: `true` (enabled by default)
- Scope: CLI + DB reviewability filters + snapshot query source

## Problem

The previous policy blocked approval for any session that had *any* prior involvement on an issue (creator, implementer, or any history entry).  
That prevented common lead/worker patterns:

1. Lead creates issue
2. Sub-agent implements issue
3. Lead reviews/approves

Under strict policy, step 3 was blocked because the lead was the creator.

## New Policy

For non-minor issues:

1. **Hard block remains** for implementation self-approval:
   - reviewer is current implementer, or
   - reviewer has `started`/`unstarted` history on the issue.
2. **Creator-only exception allowed** when:
   - reviewer is creator,
   - implementer is a different session,
   - reviewer has no implementation-history actions (`started`, `unstarted`),
   - approval includes a non-empty reason (`--reason` / aliases).
3. **All other previously-involved non-creators remain blocked**.
4. **Minor tasks remain full bypass** by design.

## Security and Audit

Creator-exception approvals are audited in two places:

1. Issue log entry with `security` log type.
2. `.todos/security_events.jsonl` via `db.LogSecurityEvent`.

`td security` / `td stats security` now shows both:
- self-close exceptions
- creator-approval exceptions

## Feature Flag and Rollback

This policy is behind `balanced_review_policy` and defaults to ON.

To disable quickly (rollback to strict behavior):

```bash
td feature set balanced_review_policy false
```

Or via env override for a process/session:

```bash
TD_FEATURE_BALANCED_REVIEW_POLICY=false td reviewable
```

To re-enable:

```bash
td feature set balanced_review_policy true
```

## Implementation Notes

- `internal/features/features.go`
  - Added `BalancedReviewPolicy` feature (default `true`).
- `cmd/review.go`
  - `approve` now evaluates balanced eligibility.
  - Creator exception requires reason and emits security audit events.
- `cmd/review_policy.go`
  - Centralized policy helpers used by command flows.
- `internal/db/issues.go`
  - `ListIssuesOptions` gained `BalancedReviewPolicy`.
  - `ReviewableBy` SQL supports strict and balanced modes.
- `internal/api/snapshot_query_source.go`
  - Mirrored `ReviewableBy` strict/balanced SQL behavior.
- `cmd/list.go`, `cmd/status.go`, `cmd/context.go`, `cmd/system.go`
  - Reviewable views now use policy-aware filtering.

## Tests Added

- `cmd/review_policy_test.go`
  - Eligibility matrix for strict/balanced/minor cases.
  - Feature flag resolution in `reviewableByOptions`.
- `internal/db/db_test.go`
  - Extended `TestReviewableByFilter` with balanced-policy creator exception case.
- `internal/db/bypass_prevention_test.go`
  - Added `TestWasSessionImplementationInvolved`.
