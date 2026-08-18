# 0002. SQLite as the issue store

- **Status:** Accepted
- **Date:** 2026-05-16
- **Deciders:** td maintainers

## Context

`td` is a per-project CLI for issue and session tracking that runs locally for developers and AI agents. It needs durable structured storage for issues, logs, handoffs, work sessions, boards, and review state. Concurrent access from a CLI, a TUI monitor, and embedded sidecar processes is common; network-attached databases would force every invocation through a server and break the offline, per-checkout model.

The storage layer also has to support relatively rich queries (filters, joins across issues/logs/handoffs, ordering by board position) and frequent schema evolution — `internal/db/schema.go` has already moved through 30+ schema versions.

## Decision

Use SQLite as the single source of truth for project state. The database lives at `<project>/.todos/issues.db`, scoped to the project directory and committed to no remote by default. Schema is defined in `internal/db/schema.go` with an integer `SchemaVersion` and idempotent migration logic; access goes through `internal/db/` helpers.

Cross-machine synchronization is layered on top via the sync subsystem (see `docs/sync-*-guide.md` and `docs/implemented/sync-plan-03-merged.md`), not by switching storage engines.

## Consequences

- Zero-config local install: `td` works on any machine with the binary; no external database to provision.
- Each project has an isolated DB; switching projects is just `cd`. This matches the way agents are scoped to a checkout.
- Multi-process writers (CLI + monitor + sidecar) must coordinate via WAL mode and short transactions; long-running readers should not block writers.
- Schema changes require bumping `SchemaVersion` and writing migrations; ad-hoc schema drift is not supported.
- Cross-host collaboration requires the sync layer; the DB file itself is not safe to share over network filesystems.
- An explicit decision to _not_ adopt a server DB (Postgres, Turso/libSQL) is recorded; see the deprecated `docs/deprecated/spec-turso-libsql-support.md` for the libSQL exploration and its outcome.

## Alternatives considered

- **Flat files (JSON / YAML per issue).** Rejected: poor query performance, painful concurrent edits, and weak guarantees on partial writes.
- **Embedded key-value store (BoltDB, Badger).** Rejected: relational queries across issues/logs/handoffs would be reimplemented by hand; SQLite gives them for free.
- **Server database (Postgres).** Rejected: breaks the offline, per-checkout model and adds a deployment dependency for every user.
- **libSQL / Turso as primary storage.** Explored and deprecated (`docs/deprecated/spec-turso-libsql-support.md`); kept SQLite local with optional sync layered on top instead.

## References

- `internal/db/schema.go` — schema and version constant.
- `docs/sync-client-guide.md`, `docs/sync-server-ops-guide.md` — sync layer that complements local SQLite.
- `docs/deprecated/spec-turso-libsql-support.md` — rejected libSQL plan.
