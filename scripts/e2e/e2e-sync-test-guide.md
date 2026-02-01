# E2E Sync Test Suite

Live integration tests that build real `td` + `td-sync` binaries, start a local server, and exercise the full sync flow between two simulated clients (alice and bob).

## Structure

```
scripts/e2e/
  harness.sh        # shared setup/teardown/assertions — source this
  run-all.sh        # runs every test_*.sh, reports pass/fail
  test_*.sh         # individual test scripts
  GUIDE.md          # this file
```

## Running

```bash
bash scripts/e2e/run-all.sh          # run all tests
bash scripts/e2e/test_basic_sync.sh  # run one test
```

Each test gets its own random port and temp directory. Tests can run sequentially via `run-all.sh` (not parallel — each builds binaries independently).

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
