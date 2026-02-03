# Sync Test Coverage Inventory

Comprehensive inventory of sync testing coverage and identified gaps.

## Test Layers

```
┌─────────────────────────────────────────────────────────────────┐
│  scripts/e2e/           Bash E2E (15 test scripts)             │
│  └─ Black-box, builds real binaries, full CLI path             │
├─────────────────────────────────────────────────────────────────┤
│  test/e2e/              Go Chaos Oracle (11 test functions)    │
│  └─ Black-box, typed verification, reproducible seeds          │
├─────────────────────────────────────────────────────────────────┤
│  test/syncharness/      Go Integration (25 scenarios)          │
│  └─ Real sync functions, simulated multi-device                │
├─────────────────────────────────────────────────────────────────┤
│  internal/sync/         Unit Tests (36+ tests)                 │
│  internal/api/          └─ Event application, client/server    │
│  internal/serverdb/        logic, HTTP handlers, auth          │
└─────────────────────────────────────────────────────────────────┘
```

## Entity Coverage

| Entity | Bash E2E | Go Oracle | Syncharness | Unit |
|--------|----------|-----------|-------------|------|
| issues | ✅ Full | ✅ Full | ✅ Full | ✅ |
| comments | ✅ Full | ✅ Full | ✅ Basic | ✅ |
| logs | ✅ Full | ✅ Full | ✅ Basic | ✅ |
| handoffs | ✅ Full | ✅ Full | ✅ Basic | ✅ |
| boards | ✅ Full | ✅ Full | ✅ Full | ✅ |
| board_issue_positions | ✅ Full | ✅ Full | ✅ Full | ✅ |
| issue_dependencies | ✅ Full | ✅ Full | ✅ Full | ✅ |
| issue_files | ✅ Full | ✅ Basic | ✅ Full | ✅ |
| work_sessions | ✅ Full | ✅ Basic | ✅ Full | ✅ |
| work_session_issues | ✅ Full | ✅ Basic | ✅ Full | ✅ |

## Sync Pattern Coverage

### Convergence & Consistency

| Pattern | Covered | Tests |
|---------|---------|-------|
| Basic push/pull | ✅ | All tests |
| Multi-actor (2) | ✅ | chaos_sync, Go oracle |
| Multi-actor (3) | ✅ | chaos_sync --actors 3, Go oracle |
| Convergence verification | ✅ | verify_convergence(), AssertConverged() |
| Idempotent re-sync | ✅ | verify_idempotency(), TestVerifyIdempotency |
| Read-your-writes | ✅ | Go oracle verification |

### Conflict Resolution

| Pattern | Covered | Tests |
|---------|---------|-------|
| Last-write-wins (same field) | ✅ | field_merge_test, chaos |
| Field-level merge (different fields) | ✅ | TestConcurrentDifferentFieldEdits |
| Conflict recording | ✅ | composite_sync_test |
| Delete vs update | ✅ | create_delete_recreate |
| Tombstone handling | ✅ | create_delete_recreate, chaos |
| Resurrection prevention | ✅ | board_soft_delete_test |

### Network & Resilience

| Pattern | Covered | Tests |
|---------|---------|-------|
| Network partition | ✅ | test_network_partition.sh, TestPartitionRecovery |
| Large batch sync | ✅ | network_partition (40+ offline mutations) |
| Server restart | ✅ | test_server_restart.sh, TestServerRestart |
| Late-joining client | ✅ | test_late_join.sh |
| Rate limiting | ✅ | ratelimit_test.go |
| Clock skew | ✅ | test_clock_skew.sh (±5 min drift, symmetric skew) |
| Concurrent sync (same client) | ✅ | test_concurrent_sync.sh (parallel + rapid-fire) |
| Soak/endurance (30+ min) | ✅ | test_chaos_sync.sh --soak (memory, FDs, WAL, goroutines) |

### Operations & Lifecycle

| Pattern | Covered | Tests |
|---------|---------|-------|
| Undo before sync | ✅ | undo_sync_test.go (event filtered, never sent) |
| Undo after sync | ✅ | undo_sync_test.go (compensating event propagates) |
| Undo with remote modification | ✅ | undo_sync_test.go (LWW determines outcome) |
| Undo/redo toggle | ✅ | undo_sync_test.go (state toggles correctly) |

### Data Integrity

| Pattern | Covered | Tests |
|---------|---------|-------|
| Causal ordering | ✅ | test_event_ordering.sh, TestVerifyCausalOrdering |
| Monotonic server_seq | ✅ | verify_event_ordering(), TestVerifyMonotonicSequence |
| Action log convergence | ✅ | Go oracle verification |
| Large payloads (10K+ chars) | ✅ | test_large_payload.sh |
| Many comments (50+) | ✅ | test_large_payload.sh |
| Many dependencies (20+) | ✅ | test_large_payload.sh |
| Edge-case content (emoji, CJK, SQL injection) | ✅ | chaos_lib.sh edge data |

### Cascade & Hierarchy

| Pattern | Covered | Tests |
|---------|---------|-------|
| Parent-child creation | ✅ | chaos (create_child) |
| Parent delete → orphan children | ✅ | test_parent_delete_cascade.sh |
| Board delete → cascade positions | ✅ | board_soft_delete_test |
| Cascade handoff | ✅ | chaos (cascade_handoff) |
| Cascade review | ✅ | chaos (cascade_review) |

## Action Coverage (44 actions in chaos_lib.sh)

| Category | Actions | Weight |
|----------|---------|--------|
| CRUD | create, update, update_append, update_bulk, delete, restore | High |
| Status | start, unstart, review, approve, reject, close, reopen, block | Medium |
| Bulk | bulk_start, bulk_review, bulk_close | Low |
| Content | comment, log_progress, log_blocker, log_decision, log_hypothesis, log_result | Medium |
| Dependencies | dep_add, dep_rm | Medium |
| Boards | board_create, board_edit, board_move, board_unposition, board_delete, board_view_mode | Low |
| Files | link, unlink | Low |
| Work Sessions | ws_start, ws_tag, ws_untag, ws_end, ws_handoff | Low |
| Hierarchy | create_child, cascade_handoff, cascade_review | Low |
| Handoffs | handoff | Low |

## Verification Functions

| Function | Location | What It Checks |
|----------|----------|----------------|
| `verify_convergence()` | chaos_lib.sh | All tables match between two DBs |
| `verify_convergence_quick()` | chaos_lib.sh | Issues, boards, positions only |
| `verify_idempotency()` | chaos_lib.sh | N round-trips produce no changes |
| `verify_event_counts()` | chaos_lib.sh | Event count and distribution parity |
| `verify_event_ordering()` | chaos_lib.sh | Causal ordering in single DB |
| `verify_event_ordering_cross_db()` | chaos_lib.sh | Cross-DB ordering consistency |
| `verify_soak_metrics()` | chaos_lib.sh | Memory, FDs, WAL, goroutines within thresholds |
| `AssertConverged()` | syncharness | Go integration convergence |
| `UndoLastAction()` | syncharness | Undo simulation for sync tests |
| 8 oracle verifications | test/e2e/verify.go | Comprehensive Go-based checks |

---

## Identified Gaps

### High Priority (recommended next)

| Gap | Description | Impact | Effort |
|-----|-------------|--------|--------|
| Three-way field conflicts | A, B, C all edit same field simultaneously | Medium | Low |
| Schema migration | Old client version talks to new server (unknown fields) | Medium | Medium |

### Medium Priority

| Gap | Description | Impact | Effort |
|-----|-------------|--------|--------|
| Dependency cycle injection | Concurrent additions forming A→B→C→A across clients | Medium | Low |
| Deleted entity event leakage | Verify no new events reference deleted entity IDs after sync | Medium | Low |
| Conflict resolution determinism | Same seed always picks same LWW winner | Low | Low |

### Low Priority (edge cases)

| Gap | Description |
|-----|-------------|
| Server disk full | Storage failure during sync |
| Client DB corruption | Recovery from corrupt local DB |
| Mobile network conditions | High latency, packet loss simulation |
| Cross-project dependencies | Dependencies across projects |
| Very long offline | Days offline, massive divergence |
| Snapshot-and-compare | Earlier snapshots consistent over time |


---

## Test Execution Summary

### Quick CI Suite (~2 min)
```bash
go test -v -count=1 -timeout 5m -run TestSmoke ./test/e2e/
bash scripts/e2e/run_regression_seeds.sh --fixed-only
```

### Standard Suite (~10 min)
```bash
go test ./...
bash scripts/e2e/run-all.sh
```

### Full Chaos (~30 min)
```bash
go test -v -count=1 -timeout 30m -run TestChaosSync ./test/e2e/ -args -chaos.actions=500
bash scripts/e2e/test_chaos_sync.sh --actions 500 --actors 3
```

### Scenario-Specific
```bash
bash scripts/e2e/test_network_partition.sh --offline-actions 50
bash scripts/e2e/test_late_join.sh --phase1-issues 100
bash scripts/e2e/test_large_payload.sh --payload-size xlarge
bash scripts/e2e/test_server_restart.sh --offline-actions-a 30
bash scripts/e2e/test_clock_skew.sh --verbose
bash scripts/e2e/test_concurrent_sync.sh --parallel 5 --rapid-fire 15
bash scripts/e2e/test_chaos_sync.sh --soak 30m --actions 500  # endurance test
```

---

## Adding New Tests

When adding new sync functionality:

1. **Unit test** in `internal/sync/` for the core logic
2. **Integration test** in `test/syncharness/` for multi-client behavior
3. **Action executor** in `chaos_lib.sh` (`exec_<action>`) + add to `ACTION_WEIGHTS`
4. **Scenario test** in `scripts/e2e/test_<scenario>.sh` if it's a new edge case

See `scripts/e2e/e2e-sync-test-guide.md` for bash test patterns and `docs/sync-testing-guide.md` for the full test architecture.
