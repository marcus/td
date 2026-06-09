# Architecture Decision Records

This directory holds Architecture Decision Records (ADRs) for `td`.

## What is an ADR?

An ADR captures a single architectural decision: its context, the choice made, and the consequences that follow. ADRs are short, dated, and immutable once accepted — superseded decisions get a new ADR that links back to the original.

We use a lightweight [MADR](https://adr.github.io/madr/)-flavored format. See [`0000-template.md`](0000-template.md) for the template.

## When to write an ADR

Write an ADR when a change:

- Picks one approach over reasonable alternatives that other contributors might revisit later.
- Affects more than one package or crosses a public boundary (CLI, DB schema, sync protocol, sub-agent contracts).
- Codifies a policy that an agent or contributor is expected to follow (review rules, session scoping, storage location, etc.).

Tactical refactors, dependency bumps, and bug fixes do not need an ADR.

## Process

1. Copy `0000-template.md` to `NNNN-short-title.md`, using the next free number.
2. Fill in Status, Context, Decision, Consequences, and Alternatives. Keep it under ~300 lines.
3. Open a PR. Discuss the decision in the PR thread, not by rewriting the ADR repeatedly.
4. On merge, set Status to `Accepted`. If a later ADR overrides this one, update Status to `Superseded by NNNN` and add a link.

## Status values

- `Proposed` — under discussion in a PR.
- `Accepted` — merged and in effect.
- `Superseded by NNNN-...` — replaced by a later ADR.
- `Deprecated` — no longer in effect, but not directly replaced.

## Index

| #    | Title                                                                | Status   |
| ---- | -------------------------------------------------------------------- | -------- |
| 0001 | [Record architecture decisions](0001-record-architecture-decisions.md) | Accepted |
| 0002 | [SQLite as the issue store](0002-sqlite-storage.md)                  | Accepted |
| 0003 | [Delegated review policy](0003-delegated-review-policy.md)           | Accepted |
| 0004 | [Session scoping by branch and agent](0004-session-scoping.md)       | Accepted |
