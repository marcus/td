# Plan: Review Attestations and Delegated Closure for Orchestrated Agent Work

## Summary

td should stop treating "the session that closes the issue" as the primary review guardrail.

The stronger and more useful invariant is:

- an issue still needs an independent review
- that review must come from a session that did not participate in implementation
- once that review exists, an involved session can perform the final close

This keeps the important protection against unchecked self-review, while removing the current friction where an orchestrator or implementer cannot finish a task even after a distinct reviewing sub-agent has approved it.

## Why This Change Is Needed

The current model is still centered on session-locked approval:

- `cmd/review_policy.go` blocks approval based on prior session involvement, with a narrow creator-only exception
- `internal/db/issues.go` duplicates that policy in SQL via `ReviewableByFilter`
- `pkg/monitor/actions.go` uses a different rule and only blocks the current implementer
- `internal/serve/handlers_transitions.go` does not enforce the same approval guardrails at all
- repo guidance in `AGENTS.md`, `CLAUDE.md`, and website docs still frames "different closing session" as the main safety mechanism

That was reasonable when the main risk was one long-lived agent session silently approving its own work. It is a worse fit now that:

- orchestrators commonly delegate implementation and review to sub-agents
- the useful boundary is "independent review happened" rather than "a different session pressed the final button"
- today’s creator-exception policy solves only one coordinator workflow and still leaves broader orchestration flows awkward

## Goals

- Keep independent review as a first-class requirement for non-minor work.
- Let an orchestrator, creator, or implementer close an issue after a qualified review has already been recorded.
- Separate "review recorded" from "issue closed" in both the data model and user-facing workflow.
- Make CLI, monitor, DB query filters, snapshot query source, and serve/API all enforce the same policy.
- Preserve clear audit history showing who implemented, who reviewed, and who closed.
- Update repo guidance and inline help so agents stop creating artificial sessions just to satisfy a close-time lock.

## Non-Goals

- Removing sessions from td.
- Removing review requirements for non-minor issues.
- Solving full agent lineage/orchestration attribution in the first pass.
- Redesigning the whole issue lifecycle or work-session system.
- Bundling unrelated workflow changes into the same rollout.
- Changing the minor-task bypass: `Minor == true` issues continue to skip review and close eligibility checks entirely, including in `in_review` state. The new model applies only to non-minor issues.

## Recommended Policy Shift

### Current Mental Model

"The closer must be a different session than the implementer."

### Proposed Mental Model

"The review must be performed by a different, implementation-independent session. The closer may be an involved session once that review exists."

This changes td from a close-time session lock to a review-attestation model.

## Proposed Workflow

### Direct reviewer-close flow

1. Implementer submits issue for review with `td review <id>`.
2. A qualified reviewer runs `td approve <id>`.
3. td records the review and closes the issue in one step.

This preserves the current fast path for straightforward review workflows.

### Reviewer records approval, orchestrator closes later

1. Implementer submits issue for review with `td review <id>`.
2. Reviewing sub-agent records approval without closing.
3. Orchestrator or implementer closes the issue after inspecting that recorded review.

This is the workflow the current system makes awkward, and the new policy should treat it as normal rather than exceptional.

### Recommended CLI shape

Keep `td review` as the "submit for review" command.

Extend `td approve` so it supports two modes:

- `td approve <id>`:
  - if the caller is an eligible reviewer and no active approval exists, record the review and close immediately
  - if an active qualifying approval already exists, close using that approval
- `td approve <id> --record-only --reason "..."`:
  - record an approval review without closing

This keeps the mental model compact and avoids introducing another top-level workflow verb.

## Core Policy Rules

### 1. Reviewer eligibility should be based on implementation independence

For non-minor issues, a session may record an approval review only if:

- the issue is `in_review`
- the session is not the current `implementer_session`
- the session has no implementation history on the issue
  - specifically no `started` or `unstarted` actions in `issue_session_history`
  - `created` and `reviewed` actions must NOT count as implementation history, so a creator-orchestrator that never ran `td start` remains eligible and repeat reviewers remain eligible across re-review cycles

This is simpler and better aligned with the real safety goal than the current "no prior involvement at all" rule.

`WasSessionImplementationInvolved` in `internal/db/issue_relations.go` already filters on `started|unstarted`. The new policy package should reuse that helper rather than re-implementing the action filter.

### 2. Closing should require a qualifying review, not a separate closer

For non-minor issues, a session may close an issue if either:

- it is an eligible reviewer closing directly in the same action, or
- the issue already has an active qualifying approval review

The active independent approval review is the close gate. The closer's session is still important audit metadata via `closed_by_session`, but it is not a permission predicate once the approval exists.

Earlier drafts considered limiting delegated close to explicit issue roles:

- creator session
- implementer session
- review-requesting session
- reviewer of record

Implementation feedback showed that role-based close still forced orchestrators into brittle session ownership patterns. The final policy should allow any session to close after the independent approval, requiring `--reason` when the closer differs from the reviewer-of-record.

The plan should not use `issue_session_history` as the proxy for orchestrator involvement in the first pass because that table currently records only a narrow action set and will miss sessions that coordinated work through logs, handoffs, or work-session metadata.

### 3. Review freshness must be enforced

A recorded approval review must become stale if implementation-relevant issue data changes after the review.

The conservative first-pass rule should be explicit invalidation, not `reviewed_at >= issue.updated_at`.

Recommended behavior:

- any implementation-relevant mutation supersedes the active review row in `issue_reviews`
- any transition that moves work back out of `in_review` clears `reviewer_session` and `reviewed_at`
- review-recording and review-closing operations should not rely on `updated_at` comparisons because current issue writes already bump `updated_at`

"Implementation-relevant mutation" must be defined explicitly. Suggested set:

- `description`, `title`, `type`, `priority`, `minor`, `parent_id`
- any change to `status` other than the direct `in_review -> closed` close path
- attaching or detaching `linked_files`, `dependencies`, `work_session_tags`
- cascades that re-parent the issue

Pure-metadata changes that should NOT supersede:

- `due_date`, label assignments, notes, comments, `log` entries that are not status transitions

The concrete list should live in `internal/reviewpolicy` as a named predicate (e.g. `IsReviewInvalidatingMutation`) that both the DB write path and sync import path call.

Implementation should prefer being safely conservative over accidentally reusing stale approval.

### 4. Reviewer and closer must both be visible in audit data

The model must stop overloading `reviewer_session` to mean both "who reviewed" and "who closed".

After this change td should always be able to answer:

- who implemented the issue
- who recorded the approval review
- who performed the final close

## Data Model Changes

### Issues table

Add:

- `reviewed_at DATETIME`
- `review_requested_by_session TEXT DEFAULT ''`
- `closed_by_session TEXT DEFAULT ''`

Keep:

- `implementer_session`
- `creator_session`
- `reviewer_session`

After the change:

- `reviewer_session` means the active reviewer of record, not necessarily the closer
- `review_requested_by_session` means the session that asked for or submitted the current review cycle
- `closed_by_session` captures the session that performed the final close
- direct reviewer-close simply sets both fields to the same session

**Backfill for historical rows**: existing closed issues have `reviewer_session` set at close time, and `closed_by_session` will be empty. The migration must backfill `closed_by_session = reviewer_session` for rows where `status = 'closed'` and `reviewer_session != ''`. Without this, every historical issue appears to have been closed by "nobody", which breaks audit tooling and `td show`. Leave `reviewed_at` NULL for historical rows; do not synthesize from `closed_at`, since that would misrepresent review timing.

### New `issue_reviews` table

Add a new append-only review history table:

```sql
CREATE TABLE issue_reviews (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL,
    reviewer_session TEXT NOT NULL,
    decision TEXT NOT NULL,           -- approved | changes_requested
    summary TEXT NOT NULL DEFAULT '',
    requested_by_session TEXT DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    superseded_at DATETIME,
    FOREIGN KEY (issue_id) REFERENCES issues(id)
);
```

Purpose:

- retain full review history instead of only the latest reviewer stamp
- support record-only approvals cleanly
- make stale/superseded reviews explicit
- snapshot who requested the review cycle for audit and closer-eligibility checks
- support future UI/API surfaces without re-parsing logs

### Session history actions

Expand `models.IssueSessionAction` so td can distinguish review and close events more clearly.

Recommended additions:

- `review_approved`
- `review_changes_requested`
- `closed`

Keep existing implementation-tracking actions intact because `WasSessionImplementationInvolved` should continue to rely only on implementation actions.

The existing `ActionSessionReviewed` should be deprecated in favor of the split `review_approved` / `review_changes_requested` pair, but must remain a valid enum value so older DB rows still scan. Document it as read-only in `internal/models/models.go`.

### Optional session lineage metadata

This plan does not require parent/child session lineage to land the main workflow change.

However, a follow-up-compatible schema extension should be reserved:

- `sessions.parent_session_id`
- `sessions.role` or `sessions.kind`

If orchestrators later want stronger lineage-aware policies or richer audit views, those fields will make the next step easier without blocking the first release.

## Shared Policy Architecture

The current policy is scattered and partially duplicated. That will get worse if review recording and delegated closure are added piecemeal.

### Move policy out of `cmd`

Create a new internal package, for example:

- `internal/reviewpolicy`

This package should own:

- reviewer eligibility checks
- close eligibility checks
- review freshness checks
- helper structs describing active review state
- user-facing rejection reasons that other surfaces can reuse

### Stop duplicating policy logic across surfaces

The following should all call into the same policy layer:

- `cmd/review.go`
- `cmd/status.go`
- `cmd/context.go`
- `cmd/system.go`
- `cmd/list.go`
- `pkg/monitor/actions.go`
- `pkg/monitor/data.go`
- `internal/serve/handlers_transitions.go`
- `internal/api/snapshot_query_source.go`

`internal/db/issues.go` should still expose filter helpers, but those helpers should be driven by the shared policy model rather than growing another hand-maintained branch of business logic.

## Feature Flag and Compatibility Plan

Do not silently replace the current policy in one step.

### Add a named-enum review policy mode

Prefer a single named-enum setting over stacked booleans. Cascading two booleans (such as a new flag > `balanced_review_policy` > default) hides the real three-way choice and makes "what mode am I in right now?" hard to answer from one line of config.

Recommended shape: a single string feature, for example `review_policy_mode`, with values:

- `strict` (current non-balanced behavior)
- `balanced` (current balanced creator-exception behavior)
- `delegated` (new review-attestation behavior)

Default: `strict` during the first release.

For backward compatibility during rollout only, `balanced_review_policy=true` should be mapped to `review_policy_mode=balanced` at load time, with a deprecation warning, and removed in Step 5.

If a boolean flag is kept for simplicity, make it mutually exclusive with `balanced_review_policy` at load time and emit an error on conflicting values rather than silently preferring one. Do not ship a silent precedence matrix.

Suggested rollout behavior:

- the new mode ships disabled by default
- when inactive, the current strict/balanced behavior continues unchanged
- when active, all policy decisions flow through delegated-review rules

### Balanced review policy transition

`balanced_review_policy` should not be removed in the same change that introduces review attestations.

Recommended sequence:

1. land the new data model and shared policy behind `review_policy_mode=delegated`
2. support both policy modes during rollout
3. update docs and guidance to prefer the new model
4. once the new path is stable, deprecate and remove `balanced_review_policy`

This avoids mixing a conceptual simplification with a risky migration in the same step.

## Close-Path Hardening

The delegated-review plan is incomplete unless it also closes the existing direct-close bypasses.

### Recommendation

- keep `td close` as an admin-only path for duplicates, won’t-fix, and cleanup work
- do not allow `td close` to become a backdoor for reviewed implementation work
- make `in_review -> closed` a policy-checked `approve` path only
- when an issue in `in_review` receives a `td close` call, reject it with a clear message pointing the caller to `td approve`; do not silently demote to `open` first

### Required implementation consequences

- update `cmd/review.go` close handling so `td close` is either explicitly admin-scoped or policy-checked against the new review layer
- update `internal/workflow/transitions.go` so raw `open|in_progress|blocked -> closed` transitions cannot bypass the new rules for implementation work
- update serve/API close endpoints or handlers, including any `POST /v1/issues/{id}/close` path, so they follow the same decision model as the CLI
- reconcile with `pkg/monitor/actions.go` approval cascade: when an epic is approved, the cascade closes descendants in `open`, `in_progress`, or `in_review` directly (`models.ActionApprove`) without reviewing each one. **Decision: preserve current behavior.** Approving a parent continues to close descendants in bulk. This is recorded as a named exemption in the shared policy package, for example `reviewpolicy.CascadeFromParentApproval`, and the cascade call site must route through that exemption rather than bypassing the policy package entirely. Each cascaded descendant gets `closed_by_session` set to the approver and an `issue_reviews` row written with `decision = 'approved_by_parent_cascade'` and `reviewer_session` = parent approver, so audit output can tell cascaded closes from individually-reviewed closes. **Known risk:** this is a per-issue review bypass. Under the new delegated-close model, orchestrators are likely to lean on epics more heavily, which widens the hole. Not fixing it in this rollout is an explicit choice; revisit in a follow-up if abuse patterns appear.
- document the boundary clearly: `close` is administrative, `approve` is review-complete closure

If this is not part of the rollout, the new review-attestation model will coexist with an easier bypass and the implementation will drift from the plan immediately.

## CLI Changes

### `td approve`

Extend approve to support three explicit paths:

1. `review + close`
   - current session is an eligible reviewer
   - no active approval review exists yet
   - td records approval and closes in one transaction

2. `record approval only`
   - `--record-only`
   - current session is an eligible reviewer
   - td records approval, updates `reviewer_session` and `reviewed_at`, but leaves status as `in_review`

3. `close using recorded approval`
   - issue already has an active approval review
   - any current session may close
   - if current session is not the reviewer-of-record, `--reason` is required
   - td sets `closed_by_session` and `closed_at`

Recommended guardrails:

- `--record-only` should require `--reason` or equivalent review summary text
- `--record-only` should support `--decision changes_requested` as an alternative to recording an approval; if the only supported decision is approval, rename the flag to `--approve-only` to make intent clear
- close-using-recorded-approval should require a reason when `closed_by_session != reviewer_session`
- close-using-recorded-approval should require that `review_requested_by_session` or another explicit issue role matches the caller
- error messages should explain whether the caller needs to review first or whether the issue needs a fresher review

### `td reject`

Retain the existing "return from `in_review`" behavior, but also:

- supersede any active approval review
- clear `reviewer_session`
- clear `reviewed_at`
- leave historical `issue_reviews` rows intact for audit purposes

### `td reviewable`

Redefine this command to mean:

- issues the current session can independently review now

It should exclude:

- issues already carrying an active approval review
- issues where the current session participated in implementation

**Compatibility note**: this is a semi-breaking change for any automation that parses `td reviewable` output to drive review bots. Add a `--include-approved` flag (or separate `td list --ready-to-close`) that surfaces the new "reviewed and awaiting close" bucket, and call it out in the release notes. Orchestrators that today parse `reviewable` to find "everything in review" will otherwise silently lose visibility into issues a peer has already reviewed.

### Status and context surfaces

Update:

- `td status`
- `td context`
- `td system`
- `td list --reviewable`

to distinguish at least these states:

- awaiting review
- reviewed and ready to close
- pending review from my own implementation

This is important so orchestrators can find "review complete, just close it" tasks without manually inspecting each issue.

### `td show`

Extend issue detail output to include:

- reviewer of record
- reviewed at
- closed by
- a compact view of recent review history from `issue_reviews`

## Monitor / TUI Changes

The monitor should reflect the same split between review and close.

### Data model and categorization

Update `pkg/monitor/data.go` and related types so the task list can distinguish:

- reviewable by me
- reviewed / ready to close
- pending review
- my current work

### Actions

Do not keep a single ambiguous "approve" action that sometimes means "record review" and sometimes means "close" without explanation.

Recommended monitor behavior:

- add a distinct "record approval review" action
- keep a distinct "close reviewed issue" action
- show which action is available in the footer/help based on selection

### Detail views

Update modal/detail rendering so users can see:

- who reviewed
- whether the review is still fresh
- whether an active approval makes the issue ready to close

## Serve / HTTP API Changes

This change is a good opportunity to fix an existing parity gap: the serve transition handlers currently do not enforce the same review guardrails as the CLI.

### Recommended API behavior

Keep the state machine simple, but make the review step explicit in the API:

- `POST /v1/issues/{id}/review` stays "submit for review"
- add a review-recording endpoint, for example `POST /v1/issues/{id}/reviews`
- keep `POST /v1/issues/{id}/approve` as the final close endpoint

Recommended request shapes:

- review record:
  - decision
  - summary
  - optional `record_only` is not needed if the endpoint itself is review-only
- approve:
  - optional close reason
  - server validates existence and freshness of a qualifying review if caller is not directly reviewing-and-closing

### Response payloads

Extend issue DTOs and transition responses with:

- `reviewed_at`
- `closed_by_session`
- optionally a compact `active_review` summary

### Snapshot query source

`internal/api/snapshot_query_source.go` must gain the same review-aware filters as the live DB path so query behavior does not diverge between local and snapshot-backed reads.

## Database and Query Work

### New DB helpers

Add DB helpers for:

- create review record
- list review history for an issue
- fetch active approval review
- supersede stale reviews
- determine whether a recorded approval makes the issue ready to close

### Schema consumers and sync rollout checklist

The schema work is broader than the core issue table and the new review table.

The implementation plan should explicitly cover:

- `internal/db/issues.go` and `internal/db/issues_logged.go` column lists and scan paths
- `internal/db/import.go` import/upsert handling
- stats readers such as `internal/db/stats.go`
- admin snapshot and counting code such as `internal/api/admin_snapshots.go`
- snapshot query source updates in `internal/api/snapshot_query_source.go`
- sync command/entity registration in `cmd/sync.go`
- sync taxonomy and validation in `internal/events/taxonomy.go` and related sync validators
- any export, backup, or JSON/system output that serializes issue fields directly

`issue_reviews` should be treated as a first-class syncable entity from the first release. Keeping it local-only would mean that a reviewer on one machine records an approval, the orchestrator on another machine (after sync) cannot see the approval, and the delegated-close path becomes unusable across machines — which defeats the premise of the feature. Register the entity in `cmd/sync.go` and add taxonomy entries in `internal/events/taxonomy.go` at the same time the table is introduced.

Supersede events must also sync: if sync only carried INSERTs, a stale-but-not-yet-superseded review could be reused on a peer that hadn't yet observed the superseding mutation. Either sync `superseded_at` updates explicitly, or recompute supersede locally on every relevant import.

Recommended file split:

- add a focused file such as `internal/db/reviews.go`
- keep issue list/query helpers in `issues.go`
- keep implementation-history helpers in `issue_relations.go` unless there is a better home

### Query filter updates

Extend list/query options with fields that can support both CLI and monitor surfaces, for example:

- `ReadyToCloseBy`
- `HasActiveReview`

This avoids having every caller reconstruct these categories in memory after a broader query.

## Audit and Logging

Valid delegated closure should become a first-class workflow, not a security exception.

### What should be logged

- review-record event in `issue_reviews`
- issue log entry when approval review is recorded
- issue log entry when final close happens
- session history entries for review and close actions

### Action log and undo semantics

The two-step flow needs explicit action semantics; otherwise undo and audit output will be misleading.

Recommended action model:

- keep `ActionApprove` for the final `in_review -> closed` transition
- keep `ActionClose` for admin close paths only
- add a new action type for record-only approval review, for example `ActionReviewApprove`

Undo requirements:

- undoing a record-only approval review must restore `reviewer_session` / `reviewed_at` and supersede or remove the created review row
- undoing a final approval must restore the prior issue state and any pre-existing active review state
- direct reviewer-close must record enough metadata to undo both the issue transition and the created review row if both happened in one command

Concretely, the action log payload must be extended with a serialized pre-image of:

- the prior `issue_reviews.id` of the then-active review (if any), so supersede can be reverted
- the prior `reviewer_session`, `reviewed_at`, `review_requested_by_session`, `closed_by_session`, and `closed_at` values
- the `issue_reviews` row inserted by this action (so redo semantics work)

`cmd/undo.go` and the action-log payload structs in `internal/models` must both be updated in the same change — otherwise undo will silently leave orphaned or stale review rows.

The plan should call this out up front rather than leaving it to implementation cleanup.

### What should stay in security logs

Reserve `.todos/security_events.jsonl` for actual exceptions or suspicious bypass attempts, not routine orchestrator-close-after-review behavior.

If a closer attempts to close without a qualifying review, that is still a good candidate for security/audit logging.

## Testing Plan

This change touches behavior, storage, and guidance. It needs broad coverage.

### Schema and migration tests

Add migration coverage for:

- new issue columns
- new `issue_reviews` table
- backward compatibility when opening older DBs

### Policy unit tests

Add exhaustive unit tests around:

- who can record approval reviews
- who can close after recorded review
- stale review invalidation
- minor-task bypass behavior
- direct reviewer-close path
- creator/orchestrator/implementer close-after-review path
- parent-approval cascade exemption (cascaded descendants get `closed_by_session` stamped and an `approved_by_parent_cascade` review row; non-cascaded closes never use that decision value)

### Cross-surface parity tests (required, not optional)

The reason policy drifted in the current code is that each surface tested its own behavior in isolation. Add a table-driven suite under `internal/reviewpolicy` that enumerates scenarios (role × prior involvement × active review state × policy mode × minor) and asserts that the CLI, monitor action handler, serve transition handler, and snapshot query source return the same decision for each row. A new surface cannot be added to the codebase without being wired into this suite.

### CLI tests

Extend `cmd/review_test.go` and related tests for:

- `td approve --record-only`
- close using previously recorded approval
- required reason handling
- user-facing messages when review is stale or missing
- `td reviewable`, `td status`, and `td context` output changes
- `td close` bypass prevention under the new policy

### DB query tests

Extend:

- `internal/db/db_test.go`
- `internal/db/bypass_prevention_test.go`

to verify:

- reviewable filtering
- ready-to-close filtering
- implementation-history checks still block self-review
- any closer is allowed only when approval review exists

### Monitor tests

Add or extend tests covering:

- category assignment in `pkg/monitor/data_test.go`
- approval-review action vs close action behavior
- footer/help text changes
- detail rendering showing reviewer and closer separately

### Serve / API tests

Add tests for:

- review-record endpoint
- approve endpoint using recorded approval
- stale review rejection
- DTO fields for reviewer/closer metadata
- parity with CLI policy
- close endpoint hardening so non-review close paths cannot bypass review requirements

### Undo and action-log tests

Add targeted coverage for:

- record-only approval undo
- direct reviewer-close undo
- close-using-recorded-approval undo
- action-log payloads carrying enough metadata to restore `issue_reviews` and active-review fields correctly

## Documentation Changes

### Repo guidance

Update:

- `AGENTS.md`
- `CLAUDE.md`
- `internal/agent/instructions.go`

Key messaging changes:

- do not start a new session mid-work just to satisfy td review rules
- use a real reviewer session or sub-agent to record review
- after an independent review is recorded, the orchestrator or implementer may close normally

### Website docs

Update at least:

- `website/docs/intro.md`
- `website/docs/core-workflow.md`
- `website/docs/ai-integration.md`
- `docs/primer.md`
- `website/docs/command-reference.md`

The docs should stop describing "different closing session" as the core rule and instead explain:

- review must be independent
- close may be delegated after review
- reviewer and closer are tracked separately

### Implemented specs

Add a new implemented spec after shipping and cross-link it from:

- `docs/implemented/spec-balanced-review-policy.md`
- `docs/implemented/spec-agent-review-bypass-prevention.md`

This creates a clean documentation chain showing how td moved from strict session locks to review attestations.

## Inline Guidance Changes

The command-line guidance needs as much attention as the docs.

Update:

- `td usage`
- `td approve --help`
- `td reviewable --help`
- `td status`
- `cmd/context.go` workflow text
- monitor help/footer text

Recommended wording shift:

- old: "you cannot approve issues you implemented"
- new: "you cannot review your own implementation, but you can close after an independent review has been recorded"

This is the behavior change users will feel most directly.

## Rollout Sequence

### Step 1: Data model and shared policy package

- add schema changes (new issue columns, `issue_reviews` table, migration backfill for historical closed rows)
- add DB helpers in `internal/db/reviews.go`
- introduce `internal/reviewpolicy` and port **all** current policy logic into it without behavior change — CLI, monitor, serve, and snapshot query source must all route through it before any new feature lands
- gate new behavior behind `review_policy_mode`
- add taxonomy + sync registration for `issue_reviews` at the same time the table is introduced

The goal of Step 1 is to reach parity-before-behavior-change. Do not move to Step 2 until CLI, monitor, serve, and snapshot query source all compute identical decisions through the shared package on the existing policy surface.

### Step 2: CLI workflow

- implement record-only approval
- implement close-after-review
- update status/context/list/show output

### Step 3: Monitor and serve parity

- expose the new review-record and close-after-review actions in the monitor
- add explicit serve/API review recording endpoint
- align snapshot query behavior with live DB path

### Step 4: Documentation and guidance

- update repo instructions
- update website docs
- update inline help and command text

### Step 5: Flag flip and cleanup

- enable the new mode by default once stable
- deprecate `balanced_review_policy` (with load-time warning for one release, then remove)
- remove no-longer-needed creator-exception wording and code paths

### Rollback and downgrade

If `review_policy_mode=delegated` is enabled and later disabled, td must remain readable and usable:

- the new issue columns and `issue_reviews` table remain populated; older policy modes simply ignore them
- issues closed via the delegated-only path stay closed and continue to report reviewer/closer correctly
- no data cleanup is required on downgrade; document this explicitly in release notes so users aren't tempted to hand-delete rows

## Risks and Mitigations

### Risk: stale approval gets reused after work changes

Mitigation:

- store `reviewed_at`
- clear or supersede active review state on relevant transitions
- test update/reject/re-review flows aggressively

### Risk: policy diverges again across CLI, monitor, and serve

Mitigation:

- move policy to a shared internal package before changing behavior
- treat parity tests as required, not optional

### Risk: audit trail becomes harder to read

Mitigation:

- add `closed_by_session`
- keep `reviewer_session` as reviewer of record
- persist full review history in `issue_reviews`
- surface both reviewer and closer in `td show` and API DTOs

### Risk: users keep working around the old rule by creating new sessions

Mitigation:

- change repo guidance and inline messages in the same rollout
- explicitly document the intended orchestrator/sub-agent workflow

## File Map

Likely code touch points:

- `internal/models/models.go`
- `internal/db/schema.go`
- `internal/db/issues.go`
- `internal/db/issue_relations.go`
- `internal/db/security.go`
- `internal/features/features.go`
- new `internal/db/reviews.go`
- new `internal/reviewpolicy/*`
- `cmd/review.go`
- `cmd/review_policy.go` or its replacement
- `cmd/context.go`
- `cmd/status.go`
- `cmd/list.go`
- `cmd/system.go`
- `cmd/show.go`
- `cmd/undo.go`
- `pkg/monitor/data.go`
- `pkg/monitor/actions.go`
- `pkg/monitor/view.go`
- `pkg/monitor/model.go`
- `pkg/monitor/types.go`
- `pkg/monitor/input.go`
- `pkg/monitor/keymap/` (new action bindings and help text)
- `internal/workflow/transitions.go`
- `internal/serve/handlers_transitions.go`
- `internal/serve/response.go`
- `internal/db/issues_logged.go`
- `internal/db/import.go`
- `internal/db/stats.go`
- `internal/api/admin_snapshots.go`
- `internal/api/snapshot_query_source.go`
- `cmd/sync.go`
- `internal/events/taxonomy.go`
- `AGENTS.md`
- `CLAUDE.md`
- `internal/agent/instructions.go`
- `docs/primer.md`
- `website/docs/intro.md`
- `website/docs/core-workflow.md`
- `website/docs/ai-integration.md`
- `website/docs/command-reference.md`

## Recommendation

Implement this as a deliberate policy shift from "separate closing session" to "independent review attestation".

That change matches the way orchestrated agent workflows actually work now:

- sub-agents can still provide real review separation
- orchestrators do not need awkward session gymnastics to finish work
- td keeps the protection that matters most: implementation cannot silently approve itself
