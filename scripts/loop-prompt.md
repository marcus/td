You are working inside the Dispatch Loop — an autonomous coding loop that processes tasks one at a time. You are building **td**, a task management CLI for AI-assisted development.

td is a Go CLI application built with Cobra, backed by SQLite, with event-logged mutations, a TDQ query language, sync protocol, and a Bubble Tea TUI monitor.

## Architecture Overview

| Package | Purpose |
|---------|---------|
| `cmd/` | 67 Cobra CLI commands (create, start, log, handoff, review, approve, etc.) |
| `internal/db/` | SQLite persistence — schema, migrations (29), CRUD, logged mutations |
| `internal/models/` | Domain types: Issue, Log, Handoff, Board, WorkSession, Note, etc. |
| `internal/session/` | Session management — auto-generated IDs scoped by branch + agent |
| `internal/query/` | TDQ query language — lexer, parser, AST, evaluator |
| `internal/sync/` | Event-based sync protocol — push/pull, conflict resolution, backfill |
| `internal/workflow/` | State machine — transitions, guards, review enforcement |
| `internal/serve/` | HTTP API server with SSE streaming |
| `internal/api/` | Sync server — multi-project, auth, rate limiting |
| `internal/serverdb/` | Server-side database for multi-project sync |
| `internal/events/` | Event taxonomy — canonical entity/action type normalization |
| `pkg/monitor/` | Bubble Tea TUI dashboard with modal system |

## Domain Model

**Issue lifecycle:** `open` → `in_progress` → `in_review` → `closed` (also: `blocked`)

**Core entities:**
- **Issue** — status, type (bug/feature/task/epic/chore), priority (P0-P4), points (Fibonacci), labels, parent_id, session tracking, file links
- **Log** — typed entries: progress, decision, blocker, hypothesis, tried, result, orchestration, security
- **Handoff** — structured state: done, remaining, decisions, uncertain
- **Board** — query-based views with sparse positioning (65536-spaced)
- **Session** — auto-tracked context (branch, agent_type, PID)
- **Note** — freeform documents (pinned, archived, soft-deleted)
- **IssueDependency** — directed graph with cycle detection
- **ActionLog** — undo system with previous/new data snapshots

## SQLite & Migrations

- Database at `<project>/.todos/issues.db`
- 29 sequential migrations in `internal/db/schema.go`
- All IDs are text (deterministic for sync): issues use `td-<4hex>`, actions use SHA-based IDs
- Soft delete pattern: `deleted_at` timestamp, never hard DELETE
- All writes go through `*Logged()` functions that record in `action_log` for undo + sync

## Event-Logged Mutations

Every mutation is wrapped in `LogAction(ActionType, EntityType, EntityID, previousData, newData)`:
- Stored in `action_log` table with session attribution
- Enables undo (`td undo`), sync, and audit trail
- Entity types defined in `internal/events/taxonomy.go`
- All `*Logged()` functions in `internal/db/` (e.g., `CreateIssueLogged`, `UpdateIssueLogged`)

## TDQ Query Language

Located in `internal/query/`. Syntax:
```
status = open AND priority <= P1
type = bug OR labels ~ critical
NOT closed AND rework()
sort: priority, -created
```

Features:
- Field comparisons: `=`, `!=`, `~` (contains), `!~`, `<`, `>`, `<=`, `>=`
- Boolean: `AND`, `OR`, `NOT`, parentheses
- Functions: `rework()`, `stale(N)`, `is_ready()`, `blocked_by(id)`, `blocks(id)`, `linked_to(pattern)`
- Cross-entity: `log.type = decision`, `file.path ~ src/`, `epic = td-xxx`
- SQL pushdown for indexed fields, in-memory for complex predicates

## Sync Protocol

Client-server event streaming via HTTP:
- **Push**: `POST /v1/projects/{id}/sync/push` — batch events with `client_action_id` for dedup
- **Pull**: `GET /v1/projects/{id}/sync/pull?from=N&limit=M` — sequential `server_seq`
- **Conflict resolution**: Last-Write-Wins with audit trail in `sync_conflicts` table
- **Cycle prevention**: Deterministic rule — lexicographically smaller edge wins

## Workflow & Session Isolation

- State machine in `internal/workflow/` with guard-based transition enforcement
- Creator cannot review own work (different session required)
- Modes: Liberal (default), Advisory (warn), Strict (block)
- Handoffs document state between sessions

## Build & Test

```bash
go build -o td .           # Build
go test ./...              # All tests
go test ./internal/db/     # DB layer only
go test ./internal/query/  # Query parser only
go test ./internal/sync/   # Sync protocol only
```

## Critical Rules

- **Tests are mandatory.** Every file with logic gets a `_test.go` companion. Use table-driven tests.
- **All mutations go through `*Logged()` functions.** Never bypass the action log.
- **Soft delete only.** Set `deleted_at`, never DELETE rows.
- **Deterministic IDs.** Text-based, sync-safe. Composite keys for junction tables.
- **Error handling:** Return errors, don't panic. Wrap with `fmt.Errorf("context: %w", err)`.
- **Session isolation:** Never allow same session to both implement and review.

## Coding Conventions

- **CLI commands**: One file per command in `cmd/`. Follow Cobra patterns. Include `Use`, `Short`, `Long`, `RunE`.
- **DB operations**: Add to `internal/db/`. Always provide both raw and `*Logged()` variants.
- **Models**: Define in `internal/models/models.go`. Include JSON tags.
- **TDQ extensions**: Lexer → Parser → Evaluator pipeline. Add tokens to lexer, nodes to AST, evaluation to evaluator.
- **Migrations**: Append to `internal/db/schema.go` migrations slice. Never modify existing migrations. Use `IF NOT EXISTS`.
- **TUI modals**: Use declarative modal system in `pkg/monitor/modal/`. Sections: List, KeyValue, Textarea, Custom.
- **HTTP handlers**: Follow `internal/serve/` patterns. JSON envelope: `{"ok": bool, "data": ..., "error": "..."}`.

## What to do this iteration

### Step 0: Read TD state

```bash
td usage --new-session
```

Or if resuming: `td usage -q`

### Step 1: Pick a task

If any task is `in_progress`, resume it. Otherwise pick the highest-priority `open` task.

### Step 2: Implement

```bash
td start <id>
```

1. Read the task description carefully: `td show <id>`
2. Explore the relevant code before changing anything
3. Write the complete feature with all edge cases
4. Write tests — table-driven for parameter variations, integration tests for workflows
5. Run quality gates:
   ```bash
   go build -o td .
   go test ./...
   ```

### Step 3: Verify

- Run the CLI command and capture output
- Run relevant test suites
- For TUI changes: describe what you verified manually

Batch review loops:

- `EPIC_IDS=.` means "use the active epic context" when a focused issue is not set.
- `td list --epic . -s in_review` and `td list --epic . -s open,in_progress` should work in that batch mode.
- If `EPIC_IDS=.` cannot resolve cleanly, check `td status --json` and `td list --json` first; if there is still no active context, fall back to explicit epic IDs instead of guessing.
- For scripted state lookups, prefer `td status --json` and `td list --json`; do not scrape the human-readable dashboard output.
- Use `td review <id>` for `open`/`in_progress` work, and `td approve <id>` once a task is already `in_review`.

### Step 4: Commit and close

```bash
git add <specific files>
git commit -m "feat: <summary> (td-<id>)"
td review <id>
```

Use `td review`, not `td close` — self-closing is blocked.

## Rules

- **ONE task per iteration.** Complete it, verify it, commit it, mark it done, then exit.
- **Tests are mandatory.** Every change needs tests. `go test ./...` must pass.
- **Quality gates before every commit.** `go build` and `go test ./...` must pass.
- **Don't break the action log.** All mutations through `*Logged()` functions.
- **Don't break migrations.** Never modify existing migrations, only append new ones.
- **Don't break sync.** Deterministic IDs, proper event logging, no hard deletes.
- **Session isolation is sacred.** Don't bypass review guards.
- **If stuck, log and skip.** `td log <id> "Blocked: <reason>"` then `td block <id>`.
- **Commit messages reference td.** Format: `feat|fix|chore: <summary> (td-<id>)`
