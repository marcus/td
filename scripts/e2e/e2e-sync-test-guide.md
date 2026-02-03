# E2E Sync Test Suite

Live integration tests that build real `td` + `td-sync` binaries, start a local server, and exercise the full sync flow between two simulated clients (alice and bob).

## Structure

```
scripts/e2e/
  harness.sh                    # shared setup/teardown/assertions — source this
  chaos_lib.sh                  # chaos test library (44 actions, verification, state tracking)
  run-all.sh                    # runs every test_*.sh, reports pass/fail
  run_regression_seeds.sh       # runs known seeds from regression_seeds.json
  regression_seeds.json         # database of reproducible test seeds
  test_*.sh                     # individual test scripts
```

## Running

```bash
bash scripts/e2e/run-all.sh          # run all tests
bash scripts/e2e/run-all.sh --full   # include real-data tests
bash scripts/e2e/test_basic_sync.sh  # run one test
bash scripts/e2e/test_alternating_actions.sh --actions 8  # alternating multi-actor test
bash scripts/e2e/test_chaos_sync.sh --actions 100  # chaos stress test
```

Each test gets its own random port and temp directory. Tests can run sequentially via `run-all.sh` (not parallel — each builds binaries independently).

## Real-data tests (manual)

These depend on local databases and are only run with `--full`:

- `test_sync_real_data.sh` — runs against a single issues DB (default `$HOME/code/td/.todos/issues.db` or a custom path).
- `test_sync_real_data_all_projects.sh` — reads `~/.config/sidecar/config.json` and runs the same test for every project DB it finds.
- `test_monitor_autosync.sh` — verifies that `td monitor`'s periodic auto-sync pushes edits made while the monitor is running. Uses `expect` for pseudo-TTY. Requires `expect` installed.

## Alternating actions test

`test_alternating_actions.sh` alternates Alice/Bob mutations across issues (create → start → log → comment → review → approve) plus board operations, then compares final DB state.

```bash
bash scripts/e2e/test_alternating_actions.sh --actions 6
```

## Chaos sync test

`test_chaos_sync.sh` is a comprehensive stress test that randomly exercises every td mutation type across two or three syncing clients. It randomly selects from 44 action types (create, update, delete, status transitions, comments, logs, dependencies, boards, handoffs, file links, work sessions) with realistic frequency weights, generates arbitrary-length content with edge-case injection, and verifies full DB convergence after sync.

```bash
bash scripts/e2e/test_chaos_sync.sh                              # default: 100 actions
bash scripts/e2e/test_chaos_sync.sh --actions 500                # more actions
bash scripts/e2e/test_chaos_sync.sh --duration 60                # run for 60 seconds
bash scripts/e2e/test_chaos_sync.sh --seed 42 --actions 50       # reproducible
bash scripts/e2e/test_chaos_sync.sh --sync-mode aggressive       # sync after every action
bash scripts/e2e/test_chaos_sync.sh --conflict-rate 30 --verbose # 30% simultaneous mutations
bash scripts/e2e/test_chaos_sync.sh --actors 3                   # three-actor test
```

| Flag | Default | Effect |
|------|---------|--------|
| `--actions N` | `100` | Total actions to perform |
| `--duration N` | — | Run for N seconds (overrides --actions) |
| `--seed N` | `$$` | RANDOM seed for reproducibility |
| `--sync-mode MODE` | `adaptive` | `adaptive` (3-10 action batches), `aggressive` (every action), `random` (25% chance) |
| `--verbose` | off | Print every action detail |
| `--conflict-rate N` | `20` | % of rounds where both clients mutate before syncing |
| `--batch-min N` | `3` | Min actions between syncs (adaptive mode) |
| `--batch-max N` | `10` | Max actions between syncs (adaptive mode) |
| `--actors N` | `2` | Number of actors (2 or 3) |
| `--mid-test-checks` | on | Enable periodic convergence checks during chaos |
| `--inject-failures` | off | Inject ~7% partial sync failures |
| `--json-report PATH` | — | Write JSON report for CI |

The action library lives in `chaos_lib.sh`. Each action type has an `exec_<action>` function that handles preconditions, state tracking, and expected-failure detection.

## Scenario-based tests

These tests exercise specific sync edge cases:

### Network partition (`test_network_partition.sh`)

Simulates a client going offline, accumulating mutations, then reconnecting:

```bash
bash scripts/e2e/test_network_partition.sh --phase1-actions 20 --offline-actions 40 --online-actions 30
```

- Phase 1: Both clients sync normally
- Phase 2: Actor A goes offline (no sync), accumulates 30-50 mutations while B continues
- Phase 3: A reconnects and syncs large batch
- Verifies: Tombstone conflicts, field collisions, batch sync correctness

### Late-joining client (`test_late_join.sh`)

Tests a new client joining after substantial history exists:

```bash
bash scripts/e2e/test_late_join.sh --phase1-issues 50 --phase3-actions 30
```

- Phase 1: A and B create 50+ issues with syncs
- Phase 2: C joins late, links to project, performs initial sync
- Phase 3: All three actors continue chaos
- Verifies: Full history transfer, convergence across all actor pairs

### Server restart (`test_server_restart.sh`)

Tests server crash/restart resilience:

```bash
bash scripts/e2e/test_server_restart.sh --phase1-actions 30 --offline-actions-a 20 --offline-actions-b 20
```

- Phase 1: Normal chaos with syncs
- Phase 2: Server stops, clients accumulate local changes
- Phase 3: Server restarts, clients sync accumulated changes
- Verifies: Server data durability, client retry logic, convergence

### Create-delete-recreate (`test_create_delete_recreate.sh`)

Stresses tombstone vs new-entity disambiguation:

```bash
bash scripts/e2e/test_create_delete_recreate.sh --cycles 15 --extra-chaos 20
```

- Rapid cycles of create → sync → delete → sync → create similar → sync
- Concurrent conflicts: A deletes while B creates similar-titled issue
- Verifies: Deleted entities stay deleted, new entities are distinct

### Parent deletion cascade (`test_parent_delete_cascade.sh`)

Tests orphan handling when parent is deleted:

```bash
bash scripts/e2e/test_parent_delete_cascade.sh --verbose
```

- Creates parent with children, deletes parent, verifies orphan state
- Tests concurrent modification: B modifies child while A deletes parent
- Tests deep hierarchies (grandchildren)
- Verifies: Orphan state consistency across sync

### Large payload (`test_large_payload.sh`)

Stress tests with large data:

```bash
bash scripts/e2e/test_large_payload.sh --payload-size large
```

| Size | Description | Comments | Dependencies |
|------|-------------|----------|--------------|
| `normal` | 10K chars | 50 | 20 |
| `large` | 25K chars | 100 | 40 |
| `xlarge` | 50K chars | 200 | 80 |

- Verifies: No truncation, performance metrics

### Event ordering (`test_event_ordering.sh`)

Verifies causal ordering in event logs:

```bash
bash scripts/e2e/test_event_ordering.sh --actors 2
```

- Creates hierarchical data with parent-child relationships
- Verifies: Updates don't precede creates, children don't precede parents, monotonic server_seq

## Regression seed suite

Run known seeds that previously caught bugs:

```bash
bash scripts/e2e/run_regression_seeds.sh              # run all seeds
bash scripts/e2e/run_regression_seeds.sh --fixed-only # CI: only seeds for fixed bugs (should pass)
bash scripts/e2e/run_regression_seeds.sh --unfixed-only # verify unfixed bugs still fail
bash scripts/e2e/run_regression_seeds.sh --json-output # CI-friendly JSON output
```

Add new seeds to `regression_seeds.json` when bugs are found:

```json
{
  "seed": 12345,
  "test": "chaos_sync",
  "description": "Field collision causes permanent divergence",
  "added": "2024-02-01",
  "args": {"actions": 50, "conflict_rate": 30},
  "fixed": true
}
```

## Extending the chaos test

When adding new syncable mutations to td, add a corresponding `exec_<action>` function in `chaos_lib.sh` and register it in the `ACTION_WEIGHTS` array. This ensures the new feature gets exercised under randomized multi-client conditions. Follow the existing pattern: check preconditions, run the command, update state tracking, handle expected failures.

## Writing a New Test

Create `scripts/e2e/test_<name>.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/harness.sh"

# 1. Call setup — this builds binaries, starts server, auths alice+bob,
#    creates a project, and links both clients.
setup
# Options:
#   setup --auto-sync                    # enable post-mutation auto-sync
#   setup --auto-sync --debounce "1s"    # custom debounce
#   setup --auto-sync --interval "3s"    # custom periodic interval

# 2. Use td_a / td_b to run commands as alice / bob.
#    Each runs in its own project dir with isolated HOME.
td_a create "My test issue" >/dev/null
td_a sync >/dev/null 2>&1
td_b sync >/dev/null 2>&1

# 3. Query state with td list/show --json and jq.
BOB_LIST=$(td_b list --json 2>/dev/null)
COUNT=$(echo "$BOB_LIST" | jq 'length')

# 4. Assert.
assert_eq "bob sees 1 issue" "$COUNT" "1"

# 5. Always end with report.
report
```

## Harness Reference

### setup options

| Flag | Default | Effect |
|------|---------|--------|
| (none) | — | auto-sync off, explicit `td sync` only |
| `--auto-sync` | — | enable auto-sync (post-mutation push+pull) |
| `--debounce "Xs"` | `"2s"` | min interval between auto-syncs |
| `--interval "Xs"` | `"10s"` | periodic sync interval |

`on_start` is always `false` in tests to avoid debounce interference (startup sync consumes the debounce window, causing the post-mutation sync to be skipped).

### Environment after setup

| Variable | Description |
|----------|-------------|
| `WORKDIR` | Temp dir (cleaned up on exit) |
| `TD_BIN` | Path to built `td` binary |
| `SERVER_URL` | `http://localhost:<random-port>` |
| `PROJECT_ID` | Remote project ID (both clients linked) |
| `SERVER_PID` | Server process ID |
| `CLIENT_A_DIR` | Alice's project directory |
| `CLIENT_B_DIR` | Bob's project directory |
| `HOME_A`, `HOME_B` | Isolated HOME dirs (config + auth) |

### Client helpers

```bash
td_a <args...>   # run td as alice (cd's into CLIENT_A_DIR, sets HOME)
td_b <args...>   # run td as bob
td_c <args...>   # run td as carol (3-actor tests)
```

### Server lifecycle (for restart tests)

```bash
stop_server      # kill server, wait for exit
start_server     # restart with same config, wait for healthz
restart_server   # stop + start
```

### Late joiner setup

```bash
setup_late_joiner "c"   # create client C mid-test, auth, link to project
```

### Assertions

```bash
assert_eq "description" "$actual" "$expected"
assert_ge "description" "$actual" "$minimum"
assert_contains "description" "$haystack" "$needle"
assert_json_field "description" "$json" '.jq.expr' "$expected"
```

All assertions increment counters. `report` at the end prints PASS/FAIL with counts. Non-assertion failures use `_fail` (increments failure count) or `_fatal` (exits immediately).

### Polling for async results

For auto-sync tests where you need to wait for propagation:

```bash
# Poll pattern: bob syncs repeatedly until condition is met
TIMEOUT=20
elapsed=0
while [ "$elapsed" -lt "$TIMEOUT" ]; do
    td_b sync >/dev/null 2>&1
    # ... check condition ...
    if [ condition_met ]; then break; fi
    sleep 2
    elapsed=$((elapsed + 2))
done
```

### Logging and debugging

```bash
_step "Description"     # prints section header
_ok "Detail"            # prints green OK line
_fail "Detail"          # prints red FAIL, increments failure count
_fatal "Detail"         # prints red FATAL, exits immediately
```

Server logs are at `$WORKDIR/server.log`. On failure, `report` prints the path.

### Verification functions (chaos_lib.sh)

For tests that source `chaos_lib.sh`:

```bash
verify_convergence "$DB_A" "$DB_B"       # full convergence check (issues, comments, logs, etc.)
verify_convergence_quick "$DB_A" "$DB_B" # lightweight check (issues, boards, positions only)
verify_idempotency "$DB_A" "$DB_B" 3     # N round-trips produce no changes
verify_event_counts "$DB_A" "$DB_B"      # event count and distribution comparison
verify_event_ordering "$DB_A"            # causal ordering in single DB
verify_event_ordering_cross_db "$DB_A" "$DB_B"  # cross-DB ordering consistency
maybe_check_convergence                  # periodic check (call after maybe_sync)
```

## Conventions

- File names: `test_<descriptive_name>.sh`
- Use `>/dev/null` or `>/dev/null 2>&1` to suppress td output unless you need it
- Use `--json` output and `jq` for assertions — don't parse human-readable output
- For `td show --json`, logs are in `.logs` (absent when empty, use `.logs // []`)
- For `td list --json`, use `--status all` if testing non-open statuses
- Keep tests focused: one scenario per file
- When testing auto-sync timing, use the polling pattern above with generous timeouts
- When testing multiple mutations, add `sleep` between them to clear the debounce window

## Gotchas

- **Debounce + on_start interaction**: If `on_start` is true, the startup sync of a command (e.g., `td start`) sets `lastAutoSyncAt`, causing the post-mutation sync to be debounced away. The harness sets `on_start: false` to avoid this.
- **td show --json logs field**: Omitted entirely when there are 0 logs (not an empty array). Always use `.logs // []` in jq.
- **Issue IDs**: Extract with `grep -oE 'td-[0-9a-f]+'` from `td create` output (`CREATED td-abc123`).
- **Project IDs**: Extract with `grep -oE 'p_[0-9a-f]+'` from `td sync-project create` output.
