# 0003. Delegated review policy

- **Status:** Accepted
- **Date:** 2026-05-16
- **Deciders:** td maintainers

## Context

`td` is used heavily by AI agents that can both implement and close their own work. Earlier versions enforced a strict review policy: the session that recorded any implementation activity on an issue could not also close it. That rule prevented unchecked self-review, but it also blocked legitimate orchestrator patterns where a parent agent delegates implementation to one sub-agent, review to another, and then performs the administrative close itself.

Two intermediate modes accumulated over time:

- `strict` — original behavior; no prior involvement of any kind is allowed when closing.
- `balanced` — strict, plus a creator-approval exception when a `--reason` is supplied.

Neither cleanly supported the orchestrator → implementer → reviewer → orchestrator flow that AI-driven workflows actually use, and bolt-on exceptions made the policy hard to reason about. See `docs/implemented/spec-agent-review-bypass-prevention.md`, `docs/implemented/spec-balanced-review-policy.md`, and `docs/implemented/spec-delegated-review-closure.md` for the history.

## Decision

Introduce a third mode, `delegated`, controlled by the `review_policy_mode` setting (env `TD_FEATURE_REVIEW_POLICY_MODE` or `td feature set review_policy_mode delegated`). Under `delegated`:

- A review attestation must come from a session that did **not** participate in implementation.
- Once an independent approval is recorded (`td approve --record-only --reason ...`), **any** session — including the orchestrator that submitted for review — may perform the final close via `td approve --reason ...`.
- The closer is audited separately from the reviewer via `closed_by_session`.

Shared eligibility logic lives in `internal/reviewpolicy/`. `strict` and `balanced` remain available for backward compatibility; `delegated` is opt-in now and is expected to become the default in a future release.

## Consequences

- Orchestrator agents can drive the full lifecycle (`add → start → handoff → review → approve --record-only → approve`) by delegating to sub-agents without faking new sessions just to satisfy the guardrail. `CLAUDE.md` explicitly forbids starting a new session mid-work for this reason.
- Audit trail remains intact: the independent reviewer is recorded, and the closer is recorded separately. A close by a non-reviewer requires `--reason`.
- Three modes increase surface area for configuration and tests; `internal/reviewpolicy/` centralizes the rules to keep them reviewable.
- Documentation and onboarding must explain which mode is active; `CLAUDE.md` is the canonical reference for agents.

## Alternatives considered

- **Stay on `strict` only.** Rejected: forces orchestrator agents into awkward workarounds (new sessions per phase) that obscure the real audit trail.
- **Make `balanced` the default and extend it.** Rejected: the creator-approval exception is a narrower carve-out than what delegated workflows need, and stacking more exceptions onto `balanced` would muddy its semantics.
- **Drop the review guardrail entirely for agents.** Rejected: the guardrail's purpose — preventing unchecked self-review — still holds. Delegation preserves the guarantee that an independent session reviewed the work.
- **Encode policy in each command.** Rejected: leads to drift between `td review`, `td approve`, and the monitor. The `internal/reviewpolicy/` package was introduced specifically to keep these aligned.

## References

- `CLAUDE.md` — "Review Model (Delegated Review)" section.
- `internal/reviewpolicy/` — shared eligibility logic.
- `docs/implemented/spec-agent-review-bypass-prevention.md`
- `docs/implemented/spec-balanced-review-policy.md`
- `docs/implemented/spec-delegated-review-closure.md`
- `docs/plans/orchestrator-review-closure-plan.md`
