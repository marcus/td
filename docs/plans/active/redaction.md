# Plan: supported redaction for td (client + sync server)

Status: proposed
Related: `td-ce7864` (this plan supersedes and expands it), `td-a11682` (ingress scrubbing — the
prevention half, tracked separately)
Motivating incident: `td-1983cb` (sidecar), 2026-08-09

## Problem

A proof harness dumped `os.Environ()` into `td log`. That wrote 134 environment variables — 43
credential-shaped — into `logs.message`, which autosynced to `sync.haplab.com` within seconds.

Removing it took ~40 minutes of hand-written SQL against four SQLite databases across two hosts,
with the sync server stopped. Nothing about that was reproducible, auditable, or safe to delegate.
The recovery itself was more dangerous than the leak: hand-edited production databases, no dry run,
no verification primitive that could be trusted (see "What the incident taught us").

Secrets in task logs are not an edge case. Agents pipe command output into `td log` constantly, and
that output routinely contains tokens. This will happen again.

## What the incident taught us

These are requirements, not background. Each maps to a design decision below.

1. **`events.payload` is a JSONB blob.** SQLite `LIKE` against a blob silently matches nothing. The
   first server scan reported a clean database that provably contained the secret. Any scan must
   render with `json()`/`json_extract` **and validate against a known-positive control**, or a
   negative result is meaningless. This single gotcha nearly ended the incident with a false
   all-clear.
2. **The `action_log` undo copy is easy to miss.** `action_log.new_data` held a full second copy of
   the log. Redacting `logs.message` alone leaves the secret one `td undo` away.
3. **Deleting rows is not erasing bytes.** Plaintext survives in the WAL and in freed pages until a
   `wal_checkpoint(TRUNCATE)` and `VACUUM`. The local DB went 59 MB → 18 MB on vacuum.
4. **A redact event does not purge history.** The event log is append-only; appending a correction
   converges *materialized state* but leaves the original payload sitting in `events.db`. Erasure
   and convergence are different operations and need different commands.
5. **Sidecar files leak too.** `command_usage.jsonl` had captured the invocation's flags.
6. **Cursors are the blast radius.** `sync_cursors` proved no peer had pulled the event yet, which
   downgraded the incident. Any redaction tool should report this, because it changes what a human
   has to do next.
7. **Live writers hold the DB.** Three `sidecar` processes had it open. The tool must use
   `busy_timeout` and never require killing the user's sessions.

## Design

### Two operations, deliberately separate

The user's instinct to split local from remote is correct, and the reason is sharper than
convenience: **these are different security operations with different authorization boundaries.**

| | Convergent redaction | History purge |
|---|---|---|
| Command | `td redact` | `td-sync admin redact` |
| Runs where | any client | the sync server host only |
| Mechanism | appends a `redact` event | rewrites stored event payloads in place |
| Propagates | yes, to every peer | n/a (already-synced peers need the event) |
| Reversible | no (by design) | no |
| Authorization | project member | host/root access |

Neither is sufficient alone. `td redact` converges every peer's materialized state but leaves
plaintext in the server's event log. `td-sync admin redact` erases the log but cannot reach a peer
that already pulled. **The documented recovery runs both**, in that order — converge first so peers
self-heal, then purge so nothing can be replayed.

Splitting them also keeps the destructive half behind SSH. A compromised API token should not be
able to rewrite history; that requires host access. This is the authorization boundary the incident
implicitly relied on, made explicit.

### 1. `td redact` — client CLI

```
td redact <entity-id> [--field <name>] --reason <text> [--dry-run] [--json]
td redact scan [--since <when>] [--session <id>] [--all-projects] [--json]
td redact status <entity-id> [--json]
```

`<entity-id>` is any prefixed id (`lg-…`, `ho-…`, `cm-…`, `td-…`). Field defaults per entity type:
`logs`→`message`, `handoffs`→all four text fields, `comments`→`text`, `issues`→`description`.

Behaviour:

1. Resolve the entity. Refuse if it does not exist (exit 2).
2. Rewrite the target field to a marker:
   `[redacted <date>: <reason>]`. Structure is preserved so replay still materializes a valid row.
3. Rewrite every local copy in the same transaction — **`action_log.previous_data`/`new_data`
   included**. Requirement 2.
4. Append a `redact` sync event so peers converge.
5. `wal_checkpoint(TRUNCATE)` + `VACUUM`, with `busy_timeout=30000`. Requirement 3, 7.
6. Write a **local** audit record — actor, timestamp, entity, field, reason, never the content.
   Not synced (see Resolved decisions §3).
7. Report peer exposure from `sync_cursors`: how many devices have pulled past this event.
   Requirement 6.

**Never prints the redacted content.** Not with `--dry-run`, not with `--json`, not on error. The
dry run reports *what would change* — entity, field, byte length, copy count — never the value. An
agent recovering from a leak must not re-leak it into a transcript. This is the one rule the tool
cannot be argued out of; there is no `--show` flag.

### 2. The `redact` sync event

New `ActionType = "redact"` in `internal/sync/events.go`, alongside `create`/`update`/`delete` in
`applyEventWithPrevious`. Payload carries entity type, id, redacted field names, marker text, and
reason — never the original.

Apply semantics:

- **Idempotent.** Applying twice is a no-op.
- **Ordering-safe.** Peers apply by `server_seq`; the redact event always has a higher seq than the
  event that introduced the secret, so a replay of the original cannot win.
- **Applies even if the entity is absent** (peer never received the original), recorded so a late
  arrival is redacted on landing rather than resurrecting.
- **Never conflicts.** A redact must not land in `sync_conflicts` — that table stores `local_data`
  and `remote_data`, which would reintroduce the plaintext it is removing.

The precedent is `scrubLocalOnlySyncPayload`, which already rewrites payloads on both push and
apply. This follows the same seam.

### 3. `td-sync admin redact` — server CLI

Lives in `cmd/td-sync/admin.go` beside the existing `admin` subcommands, operating directly on the
project DB. Host access only; no HTTP endpoint, no new admin scope.

```
td-sync admin redact       --project <id> --entity <id> [--seq <n>] --reason <text> --confirm
td-sync admin redact-scan  --project <id> [--all-projects] [--json]
```

- Refuses without `--confirm`; `--confirm` is not implied by `--json`.
- Rewrites `events.payload` via `json_set` on the target field — envelope preserved byte-for-byte,
  so `server_seq` and `count(*)` are unchanged and clients replay cleanly.
- Rewrites the materialized `project.db` row.
- Sweeps the snapshot cache at `<data>/projects/snapshots/<project>/*.db` (clean in this incident
  only because the cached seq predated the leak — do not rely on that).
- Checkpoints and vacuums both DBs. Requirement 3.
- Writes an audit record: who, when, entity, seq, reason. Never the content.
- **Does not require stopping the server.** Uses `busy_timeout` and a single `BEGIN IMMEDIATE`
  transaction. `VACUUM` is the only step that wants quiet; if it cannot get the lock the command
  reports that clearly and exits non-zero with a resume hint, rather than half-finishing. The four
  minutes of downtime in the incident should not be the standard recovery cost.

### 4. Scanning that can be trusted

Shared implementation behind both `td redact scan` and `td-sync admin redact-scan`, in a store-free
function per CLAUDE.md §2, so a future non-SQLite backend can adopt it.

Detects: `sk-ant-`, `sk-`, `ghp_`/`gho_`/`github_pat_`, `AKIA`/`ASIA`, `xox[baprs]-`, JWTs
(`eyJ…`), PEM headers, and `NAME=VALUE` / `NAME: VALUE` where NAME matches
`KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|AUTH|SESSION|COOKIE|PRIVATE|SIGNING|SALT|DSN`.

Two non-negotiables, both bought with 40 minutes of incident time:

- **JSONB-aware.** Render blob columns with `json()` before matching. Requirement 1.
- **Self-testing.** Every scan seeds an in-memory control row containing a synthetic secret and
  asserts it is found. If the control is missed, the scan **exits non-zero and reports "scanner
  unreliable"** rather than "0 findings". A scanner that cannot prove it works must never report
  clean.

Output is names, ids, offsets, lengths, and match *shape* — never values. `--json` for agents.

### 5. Rotation manifest

`td redact --manifest <path>` writes the exposed variable names with `len`, 4-char prefix, and
`sha256[:8]` fingerprint, mode 0600 — enough to identify what to rotate, useless to an attacker.
This was hand-rolled during the incident and is the artifact the human actually needs, because
redaction is not remediation: **rotate first, redact second.** The docs must lead with that.

### 6. Sidecar files

`td redact` also sweeps `.todos/command_usage.jsonl` and `agent_errors.jsonl` via atomic
temp-file + rename, redacting the offending field in place rather than dropping records.
Requirement 5.

### 7. Prevention: `secure_delete`

Set `PRAGMA secure_delete=ON` on td databases in the storage layer so freed pages are zeroed on
delete and future redactions do not depend on remembering to `VACUUM`.

This is a store-specific pragma, which CLAUDE.md §5 warns against — the deviation is deliberate and
scoped to the SQLite adapter, never business logic. A future backend implements the same guarantee
its own way, or documents that it cannot.

## Phasing

Each phase is independently shippable and leaves the tree better than it found it.

| Phase | Deliverable | Why this order |
|---|---|---|
| 1 | `td redact scan` + control-validated scanner + `--json` | Pure read. Answers "am I leaking right now?" across every existing project on day one. |
| 2 | `td redact` local-only (no event), `--dry-run`, `--manifest` | Covers unsynced projects completely, and is the piece an agent reaches for first. |
| 3 | `redact` sync event + apply path | Turns local redaction into convergent redaction. |
| 4 | `td-sync admin redact` + `redact-scan` | Closes history purge. Needs 3 shipped so peers converge before history is rewritten. |
| 5 | `secure_delete`, sidecar-file sweep, docs | Hardening and the runbook. |

Phases 1–2 alone would have cut the incident from ~40 minutes of ad-hoc SQL to a few commands
against the local DB.

## Testing

- **Round-trip:** seed a secret → scan finds it → redact → scan clean → control still passes.
- **Undo path:** redact, then `td undo`, and assert the secret does not come back. This is
  requirement 2 and the failure most likely to ship silently.
- **JSONB regression:** a fixture with a JSONB `payload` asserting the scanner finds a known
  positive. This test exists specifically to fail if someone reintroduces a `LIKE`-on-blob scan.
- **Convergence:** two-client harness — client A redacts, client B pulls, assert B's copy is
  redacted and no `sync_conflicts` row was written.
- **Late arrival:** client B pulls the redact event *before* the original create; assert the
  original does not resurrect.
- **Lock behaviour:** run with a concurrent open reader; assert the redaction succeeds and a
  blocked `VACUUM` reports cleanly instead of half-finishing.
- **No-leak invariant:** capture stdout/stderr across every command and flag combination
  (`--dry-run`, `--json`, error paths) and assert the secret never appears. Enforced by test, not
  by review.

## Docs

- `docs/guides/redacting-secrets.md` — the runbook: rotate → scan → redact → verify → purge
  history → confirm cursors. Non-interactive throughout, copy-pasteable by an agent.
- `docs/sync-server-ops-guide.md` — add the `td-sync admin redact` section.
- CLAUDE.md — one line under the td workflow pointing at the guide, so an agent that has just
  leaked something finds the path without searching.

## Resolved decisions

1. **No session-wide redaction.** `td redact --session ses_…` is out of scope. The blast radius of a
   mistyped session id is too large for an irreversible operation. Redaction stays entity-scoped;
   redacting several entities means several explicit commands. Revisit only if real use after
   phase 2 shows entity-at-a-time is the bottleneck.
2. **`--manifest` stays optional.** `td redact` does not refuse without it and does not nag. The
   docs lead with "rotate first, redact second" and that is where the guidance belongs — the tool
   does the job it was asked to do. No paternalism in the CLI.
3. **The audit record is local + server only, never synced.** It names entity, seq, reason, and
   actor but no content. Syncing it would broadcast "a secret was here" to every peer, which is a
   pointer to the leak for anyone holding stale data. Each node keeps its own record of what it
   redacted; there is no synced redaction ledger.

### Consequences for the design above

- No `--session` flag anywhere in the phase plan; `td redact scan --session <id>` remains
  (read-only scoping is safe and useful — it is how you find the entities to redact one by one).
- The `redact` sync event carries entity, field names, marker, and reason so peers can converge.
  It does **not** carry the audit record; that is written locally by whichever node performs the
  redaction.
- `td-sync admin redact` writes its audit record to the server host only.
