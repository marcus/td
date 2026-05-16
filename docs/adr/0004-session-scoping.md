# 0004. Session scoping by branch and agent

- **Status:** Accepted
- **Date:** 2026-05-16
- **Deciders:** td maintainers

## Context

`td` work happens across many concurrent contexts: a developer in a terminal, the TUI monitor, several AI agents (Claude Code, Cursor, codex, etc.) running in parallel, and short-lived sub-agents spawned by an orchestrator. Every implementation log, handoff, and review action must be attributable to a session — both for audit and for the [delegated review policy](0003-delegated-review-policy.md) to decide whether a reviewer is independent.

Two failure modes shaped the design:

- **Too coarse.** A single "current session" per machine collapses parallel agent work into one identity, so review independence cannot be enforced.
- **Too fine.** A fresh session per command shreds continuity; logs from a single piece of work scatter across many session IDs and handoffs lose their referent.

Earlier iterations also tried environment-variable based session IDs, which agents could trivially set or copy, defeating the guardrail.

## Decision

Sessions are DB-backed and scoped by the tuple `(git branch, agent fingerprint)`. The fingerprint is derived from the agent's stable parent process and type (see `internal/session/agent_fingerprint.go`); the branch comes from `git`. The first call from a given branch+agent reuses any existing session row; otherwise `td` creates one. `td session --new` forces a new session on the same branch+agent intentionally; the policy in `CLAUDE.md` is to **not** rotate sessions mid-work.

Session identity, lookup, and creation live in `internal/session/`. Sessions are stored in the project's SQLite DB ([ADR 0002](0002-sqlite-storage.md)).

## Consequences

- Parallel agents on the same checkout get distinct sessions automatically, even without any explicit naming — review independence checks can rely on session IDs.
- Switching branches naturally starts a new session, matching how feature work is organized.
- Sub-agents spawned with their own process tree (different PID parent) get their own fingerprint and thus their own session, which is what the delegated review flow assumes.
- Agents cannot fake a different session by setting an env var; they would need a different process tree or branch.
- The branch dimension means worktrees and branch-per-task workflows produce many sessions; this is intentional and lets the audit trail follow the work.
- `td session --new` exists for the rare case where an operator legitimately needs a fresh session; documentation warns against using it to bypass review rules.

## Alternatives considered

- **Per-process sessions.** Rejected: every CLI invocation would be its own session, destroying continuity for logs and handoffs.
- **Per-terminal (TTY-keyed) sessions.** Rejected: agents often run headless without a TTY, and a single TTY can host multiple agents.
- **Env-var driven session IDs (`TD_SESSION_ID`).** Rejected: trivially spoofable, which would let an agent pose as the reviewer of its own work.
- **Per-user sessions.** Rejected: too coarse for multi-agent workflows on one machine; would re-introduce the self-review problem the review policy is designed to prevent.

## References

- `internal/session/session.go` — `GetOrCreate`, `ForceNewSession`, branch+agent lookup.
- `internal/session/agent_fingerprint.go` — agent fingerprint derivation.
- `docs/implemented/proposal-session-identity.md` — original design discussion.
- [ADR 0003](0003-delegated-review-policy.md) — relies on session independence.
