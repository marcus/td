# Test Gap Report

Generated 2026-05-15 against `codex/lint-fix-linter-fixes`. Lists production `.go`
files that lack a companion `_test.go` in the same package, grouped by directory
with a triage tier. The package-level coverage column reflects `go test -cover`
on each package as a whole — a high number means companion tests for sibling
files contribute, but the listed files are still missing direct unit tests.

## How this report was generated

```sh
go test -cover ./...
find . -name '*.go' ! -name '*_test.go' -print | while read f; do
  dir=$(dirname "$f"); base=$(basename "$f" .go)
  [ -f "$dir/${base}_test.go" ] || echo "$f"
done
```

99 source files have no companion test file. Many are covered indirectly by
sibling integration tests; this report flags them so each owner can decide
whether to add focused unit coverage.

## Tier definitions

- **P0 — Critical.** Pure logic, security boundary, or data correctness. Bugs
  here corrupt state or leak access. Should ship with direct unit tests.
- **P1 — Helpful.** Glue code with non-trivial branches (handlers, evaluators,
  workflow transitions). Direct tests catch regressions cheaply.
- **P2 — Nice.** Wiring, UI rendering, or thin CLI plumbing where focused unit
  tests give diminishing returns versus end-to-end coverage.

## This PR's contribution

Added direct unit tests for three deterministic helper files:

| File                          | Before    | After (file-level)          |
|-------------------------------|-----------|------------------------------|
| `internal/db/ids.go`          | indirect  | 75–100% per function        |
| `internal/output/markdown.go` | none      | 81–100% per function        |
| `internal/query/ast.go`       | none      | 100% on every `String()`    |

Package-level coverage now: `internal/output` 87.4%, `internal/query` 67.6%,
`internal/db` 59.2%. (Pre-existing baselines for the other two packages were
similar; the new tests close the file-level gaps without changing the totals
much since the packages are large.)

## Remaining gaps by package

### `cmd/` (25 files) — Cobra entrypoints

Package coverage: 21.3%. Most files are thin Cobra wrappers; the real logic
lives in `internal/`. Focused unit tests are usually not worth it. Prefer
end-to-end coverage via `test/e2e/`.

- **P1** `cmd/query.go`, `cmd/workflow.go`, `cmd/feature_gate.go`,
  `cmd/sync_conflicts.go`, `cmd/sync_init.go`, `cmd/doctor.go`,
  `cmd/doctor_fk.go` — branchy logic mixed with flag wiring; extract pure
  helpers and unit-test those.
- **P2** `cmd/monitor.go`, `cmd/board.go`, `cmd/task.go`, `cmd/auth.go`,
  `cmd/link.go`, `cmd/serve.go`, `cmd/defer.go`, `cmd/due.go`, `cmd/note.go`,
  `cmd/project.go`, `cmd/associate.go`, `cmd/stats*.go`, `cmd/config.go`,
  `cmd/rich_text_input.go`, `cmd/debug_stats.go` — covered by e2e or trivial.

### `cmd/td-sync/` (2 files) — Sync admin binary

Package coverage: 0.0%.

- **P0** `cmd/td-sync/admin.go` — admin API entrypoint; should at minimum have
  flag-parsing and signal-handling tests.
- **P2** `cmd/td-sync/main.go` — thin `main()`.

### `internal/db/` (16 files) — SQLite layer

Package coverage: 59.2%. The major CRUD files are exercised indirectly via
integration tests in sibling files.

- **P0** `internal/db/security.go`, `internal/db/migrations.go`,
  `internal/db/migration_fk_enforcement.go`, `internal/db/reviews_migration.go`
  — security and schema correctness; failures corrupt state. Migrations have
  some coverage via `migrations_actionlog_test.go` and `fk_enforcement_test.go`
  but specific paths in `migrations.go` (notably the file-path normalization
  branch around line 1137) deserve dedicated assertions.
- **P1** `internal/db/issues.go`, `internal/db/notes.go`, `internal/db/boards.go`,
  `internal/db/work_sessions.go`, `internal/db/search.go`,
  `internal/db/analytics.go`, `internal/db/stats.go`, `internal/db/labels.go`,
  `internal/db/conn.go` — covered indirectly; add focused tests around edge
  cases (empty results, soft-deleted rows, FTS escaping).
- **P2** `internal/db/lock_unix.go`, `internal/db/lock_windows.go` — OS-gated
  file-locking primitives; integration-tested implicitly.

### `internal/api/` (8 files) — Admin HTTP API

Package coverage: 62.0%. Use `internal/api/testharness_test.go` per the
`td-integration-test` skill.

- **P0** `internal/api/middleware.go`, `internal/api/errors.go` — auth and
  error envelope shape are externally observable contracts.
- **P1** `internal/api/projects.go`, `internal/api/members.go`,
  `internal/api/snapshot_query_source.go`, `internal/api/metrics.go`,
  `internal/api/dbpool.go`, `internal/api/config.go`.

### `internal/serve/` (6 files) — Local serve

Package coverage: 68.7%.

- **P1** `internal/serve/handlers_read.go`, `internal/serve/handlers_transitions.go`,
  `internal/serve/sse.go`, `internal/serve/context.go` — handler-level tests
  for status codes and envelope.
- **P2** `internal/serve/portfile_unix.go`, `internal/serve/portfile_windows.go`
  — OS-gated.

### `internal/serverdb/` (9 files) — Sync server DB

Package coverage: 61.7%.

- **P0** `internal/serverdb/apikeys.go`, `internal/serverdb/users.go`,
  `internal/serverdb/memberships.go`, `internal/serverdb/admin_users.go`,
  `internal/serverdb/admin_projects.go` — auth + membership correctness.
- **P1** `internal/serverdb/projects.go`, `internal/serverdb/sync_cursors.go`,
  `internal/serverdb/schema.go`.
- **P2** `internal/serverdb/device_auth_test_helpers.go` — helper.

### `internal/workflow/` (2 files)

Package coverage: 83.1%.

- **P1** `internal/workflow/transitions.go`, `internal/workflow/errors.go`
  — state-machine transitions deserve table-driven tests even if many paths
  are covered by integration tests.

### `internal/query/` (1 file remaining)

Package coverage: 67.6%.

- **P1** `internal/query/source.go` — query source abstraction; add a stub
  source and assert wiring.

### `internal/sync/`, `internal/syncclient/`, `internal/features/`

- **P1** `internal/sync/types.go` — DTOs; mostly serialization, but
  round-trip JSON tests are cheap.
- **P1** `internal/syncclient/client.go` — package coverage 0.0%; add a
  smoke test against an `httptest.Server` stub.
- **P2** `internal/features/sync_gate_map.go` — currently exercised by
  `features_test.go` but no direct test of the map.

### `pkg/monitor/` (10 files) and `pkg/monitor/modal/` (7) and `keymap/` (3)

Package coverages: 26.5% / 65.7% / 30.2%. TUI code; expensive to unit-test
because of Bubble Tea wiring.

- **P1** `pkg/monitor/actions.go`, `pkg/monitor/types.go`,
  `pkg/monitor/keymap/bindings.go`, `pkg/monitor/keymap/help.go`,
  `pkg/monitor/keymap/export.go` — pure logic suitable for unit tests.
- **P2** `pkg/monitor/view.go`, `pkg/monitor/styles.go`,
  `pkg/monitor/modal.go`, `pkg/monitor/board_editor.go`,
  `pkg/monitor/notes_modal.go`, `pkg/monitor/activity_table.go`,
  `pkg/monitor/getting_started.go`, `pkg/monitor/form_modal.go`,
  `pkg/monitor/modal/*.go` — visual rendering; prefer Betamax snapshots
  (see `betamax-docs` skill) over unit tests.

### `test/e2e/` (4 files) — Test infrastructure

- **P2** `test/e2e/random.go`, `test/e2e/actions.go`, `test/e2e/report.go`,
  `test/e2e/selection.go` — these *are* test code; the e2e suites exercise
  them in aggregate.

### Top-level

- **P2** `main.go` — thin entrypoint.

## Suggested next PRs

1. **Migration unit tests** (P0) — direct tests for
   `migration_fk_enforcement.go` and the path-normalization branch in
   `migrations.go`. Touches state correctness on every upgrade.
2. **Admin API handler tests** (P0) — extend
   `admin_integration_test.go` using the `td-integration-test` skill to
   cover `middleware.go` and `errors.go` directly.
3. **Auth / membership** (P0) — tests for `internal/serverdb/apikeys.go`,
   `users.go`, `memberships.go`. Pure functions with clear invariants.
4. **Workflow transitions** (P1) — table-driven tests for
   `internal/workflow/transitions.go` (every legal/illegal transition).
5. **Sync client smoke** (P1) — `internal/syncclient/client.go` is at 0%
   coverage; an `httptest.Server` stub plus a few request/response asserts
   would lift the package floor cheaply.
