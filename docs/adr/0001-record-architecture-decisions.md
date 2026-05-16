# 0001. Record architecture decisions

- **Status:** Accepted
- **Date:** 2026-05-16
- **Deciders:** td maintainers

## Context

`td` has grown a number of architectural decisions that are only visible by reading the code, `CLAUDE.md`, or scattered specs under `docs/implemented/` and `docs/deprecated/`. New contributors — human and agent — repeatedly re-derive the same context, and there is no canonical place to record _why_ a choice was made or what alternatives were rejected.

Existing `docs/` content mixes specs, plans, guides, and post-hoc investigations. None of those formats are designed to be immutable, dated, and cross-linked the way decision records need to be.

## Decision

Adopt lightweight Architecture Decision Records (ADRs) stored in `docs/adr/`. ADRs use a MADR-flavored template (`0000-template.md`) with Status, Context, Decision, Consequences, and Alternatives sections. They are numbered sequentially, immutable once Accepted, and superseded rather than rewritten.

The `docs/adr/README.md` describes when an ADR is required and the workflow for proposing, accepting, and superseding one.

## Consequences

- Future architectural changes that meet the criteria in `docs/adr/README.md` must include or update an ADR.
- Agents and contributors have a single index (`docs/adr/README.md`) to consult before proposing structural changes.
- The `docs/implemented/` and `docs/plans/` directories continue to hold long-form specs; ADRs link out to them where useful but stay short.
- Some up-front cost retroactively recording existing decisions (ADRs 0002–0004 in this initial batch).

## Alternatives considered

- **Keep using free-form docs.** Rejected: no consistent structure, no clear status lifecycle, and nothing pins down rejected alternatives.
- **Use GitHub Discussions or issues for decisions.** Rejected: not versioned with the code, harder to discover from a checkout, and decays when issues are closed or archived.
- **Heavier ADR formats (Nygard, Y-Statements with full attributes).** Rejected: the MADR-lite shape is enough for a single-repo project and lowers the bar to writing one.

## References

- [MADR](https://adr.github.io/madr/) — Markdown Architecture Decision Records.
- Michael Nygard, ["Documenting Architecture Decisions"](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions).
