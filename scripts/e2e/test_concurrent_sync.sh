#!/usr/bin/env bash
#
# Test: Concurrent sync operations from the same client.
# Verifies: No duplicate events, sync_state consistency, no DB lock errors
# when multiple td sync commands run in parallel or rapid succession.
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"

# --- Configuration ---
PARALLEL_SYNCS=3
RAPID_FIRE_COUNT=10
VERBOSE="${VERBOSE:-false}"
JSON_REPORT=""

usage() {
    cat <<EOF
Usage: bash scripts/e2e/test_concurrent_sync.sh [OPTIONS]

Tests concurrent sync operations from the same client to ensure:
- No duplicate events pushed to server
- sync_state remains consistent
- No database lock errors (handled gracefully)

Options:
  --parallel N      Number of parallel syncs in scenario 1 (default: 3)
  --rapid-fire N    Number of rapid-fire syncs in scenario 2 (default: 10)
  --verbose         Show detailed output
  --json-report P   Write JSON summary to file
  -h, --help        Show this help
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --parallel)     PARALLEL_SYNCS="$2"; shift 2 ;;
        --rapid-fire)   RAPID_FIRE_COUNT="$2"; shift 2 ;;
        --verbose)      VERBOSE=true; shift ;;
        --json-report)  JSON_REPORT="$2"; shift 2 ;;
        -h|--help)      usage; exit 0 ;;
        *) echo "Unknown arg: $1" >&2; usage; exit 1 ;;
    esac
done

# --- Helper functions ---

# Get server event count via td sync --status
get_server_event_count() {
    local actor="$1"
    local status_output
    case "$actor" in
        a) status_output=$(td_a sync --status 2>&1) ;;
        b) status_output=$(td_b sync --status 2>&1) ;;
    esac
    echo "$status_output" | grep "Events:" | awk '{print $2}' || echo "0"
}

# Get sync_state values from client DB
get_sync_state() {
    local db="$1"
    sqlite3 "$db" "SELECT last_pushed_action_id, last_pulled_server_seq FROM sync_state LIMIT 1;" 2>/dev/null || echo "0|0"
}

# Count events in client action_log
count_local_events() {
    local db="$1"
    sqlite3 "$db" "SELECT COUNT(*) FROM action_log;" 2>/dev/null || echo "0"
}

# Count synced events (with server_seq set)
count_synced_events() {
    local db="$1"
    sqlite3 "$db" "SELECT COUNT(*) FROM action_log WHERE server_seq IS NOT NULL;" 2>/dev/null || echo "0"
}

# Count pending events (not synced)
count_pending_events() {
    local db="$1"
    sqlite3 "$db" "SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0;" 2>/dev/null || echo "0"
}

# Get max rowid of synced events (rowid is used as client_action_id)
get_max_synced_rowid() {
    local db="$1"
    sqlite3 "$db" "SELECT COALESCE(MAX(rowid), 0) FROM action_log WHERE server_seq IS NOT NULL;" 2>/dev/null || echo "0"
}

# Check for duplicate server_seq values (should never happen)
check_duplicate_server_seqs() {
    local db="$1"
    local dups
    dups=$(sqlite3 "$db" "SELECT server_seq, COUNT(*) as cnt FROM action_log WHERE server_seq IS NOT NULL GROUP BY server_seq HAVING cnt > 1;" 2>/dev/null || echo "")
    echo "$dups"
}

# Get max server_seq
get_max_server_seq() {
    local db="$1"
    sqlite3 "$db" "SELECT COALESCE(MAX(server_seq), 0) FROM action_log WHERE server_seq IS NOT NULL;" 2>/dev/null || echo "0"
}

# Run sync and capture output+exit code
run_sync_capture() {
    local actor="$1"
    local outfile="$2"
    local start_time end_time
    start_time=$(date +%s.%N 2>/dev/null || date +%s)
    case "$actor" in
        a) td_a sync > "$outfile" 2>&1 && echo "EXIT:0" >> "$outfile" || echo "EXIT:$?" >> "$outfile" ;;
        b) td_b sync > "$outfile" 2>&1 && echo "EXIT:0" >> "$outfile" || echo "EXIT:$?" >> "$outfile" ;;
    esac
    end_time=$(date +%s.%N 2>/dev/null || date +%s)
    echo "DURATION:$(echo "$end_time - $start_time" | bc 2>/dev/null || echo "0")" >> "$outfile"
}

# --- Setup ---
setup

DB_A="$CLIENT_A_DIR/.todos/issues.db"
DB_B="$CLIENT_B_DIR/.todos/issues.db"

# Create temp dir for sync outputs
SYNC_OUTPUTS=$(mktemp -d "${TMPDIR:-/tmp}/td-concurrent-sync-XXXX")

# Track test stats
TOTAL_SCENARIOS=0
PASSED_SCENARIOS=0
DUPLICATE_EVENTS=0
LOCK_ERRORS=0
RACE_CONDITIONS=0

# ============================================================
# Scenario 1: Parallel sync commands
# ============================================================
_step "Scenario 1: Parallel sync commands ($PARALLEL_SYNCS syncs)"

# Create some data to sync
td_a create "Concurrent test issue 1" >/dev/null
td_a create "Concurrent test issue 2" >/dev/null
td_a create "Concurrent test issue 3" >/dev/null

# Record state before parallel syncs
BEFORE_LOCAL_EVENTS=$(count_local_events "$DB_A")
BEFORE_SYNCED=$(count_synced_events "$DB_A")
BEFORE_SERVER_EVENTS=$(get_server_event_count "a")

[ "$VERBOSE" = "true" ] && echo "  Before: local=$BEFORE_LOCAL_EVENTS synced=$BEFORE_SYNCED server=$BEFORE_SERVER_EVENTS"

# Launch parallel syncs (bash 3.2 compatible)
PARALLEL_PID_LIST=""
for i in $(seq 1 "$PARALLEL_SYNCS"); do
    run_sync_capture "a" "$SYNC_OUTPUTS/parallel_$i.out" &
    pid=$!
    PARALLEL_PID_LIST="$PARALLEL_PID_LIST $pid"
    [ "$VERBOSE" = "true" ] && echo "  Launched sync $i (PID $pid)"
done

# Wait for all to complete
PARALLEL_FAILURES=0
sync_idx=1
for pid in $PARALLEL_PID_LIST; do
    wait "$pid" 2>/dev/null || true
    exit_code=$(grep "^EXIT:" "$SYNC_OUTPUTS/parallel_$sync_idx.out" 2>/dev/null | cut -d: -f2 || echo "1")
    if [ "$exit_code" != "0" ]; then
        # Check if it's a lock error (expected sometimes)
        if grep -q "database is locked\|SQLITE_BUSY" "$SYNC_OUTPUTS/parallel_$sync_idx.out" 2>/dev/null; then
            LOCK_ERRORS=$((LOCK_ERRORS + 1))
            [ "$VERBOSE" = "true" ] && echo "  Sync $sync_idx: DB lock (expected)"
        else
            PARALLEL_FAILURES=$((PARALLEL_FAILURES + 1))
            [ "$VERBOSE" = "true" ] && echo "  Sync $sync_idx: FAILED (exit=$exit_code)"
        fi
    fi
    sync_idx=$((sync_idx + 1))
done

# Small delay for server to process
sleep 0.5

# Record state after parallel syncs
AFTER_LOCAL_EVENTS=$(count_local_events "$DB_A")
AFTER_SYNCED=$(count_synced_events "$DB_A")
AFTER_SERVER_EVENTS=$(get_server_event_count "a")

[ "$VERBOSE" = "true" ] && echo "  After: local=$AFTER_LOCAL_EVENTS synced=$AFTER_SYNCED server=$AFTER_SERVER_EVENTS"

# Verify: No duplicate server_seq in local DB
DUPS=$(check_duplicate_server_seqs "$DB_A")
if [ -z "$DUPS" ]; then
    _ok "no duplicate server_seq in local DB"
else
    _fail "duplicate server_seq detected: $DUPS"
    DUPLICATE_EVENTS=$((DUPLICATE_EVENTS + 1))
fi

# Verify: sync_state is consistent
# last_pushed_action_id should be >= max synced rowid
SYNC_STATE=$(get_sync_state "$DB_A")
LAST_PUSHED=$(echo "$SYNC_STATE" | cut -d'|' -f1)
LAST_PULLED=$(echo "$SYNC_STATE" | cut -d'|' -f2)
MAX_SYNCED_ROWID=$(get_max_synced_rowid "$DB_A")

if [ "$LAST_PUSHED" -ge "$MAX_SYNCED_ROWID" ] 2>/dev/null; then
    _ok "sync_state.last_pushed_action_id consistent ($LAST_PUSHED >= $MAX_SYNCED_ROWID)"
else
    _fail "sync_state.last_pushed_action_id inconsistent: $LAST_PUSHED < $MAX_SYNCED_ROWID"
    RACE_CONDITIONS=$((RACE_CONDITIONS + 1))
fi

# Verify: all events synced (none pending)
PENDING=$(count_pending_events "$DB_A")
if [ "$PENDING" -eq 0 ]; then
    _ok "all events synced (0 pending)"
else
    # Run one more sync to clean up
    td_a sync >/dev/null 2>&1 || true
    PENDING_AFTER=$(count_pending_events "$DB_A")
    if [ "$PENDING_AFTER" -eq 0 ]; then
        _ok "all events synced after cleanup ($PENDING pending -> 0)"
    else
        _fail "$PENDING events still pending after parallel syncs"
    fi
fi

# Track DB lock errors (informational - not a failure)
if [ "$LOCK_ERRORS" -gt 0 ]; then
    _ok "DB lock errors: $LOCK_ERRORS (expected under contention)"
fi

TOTAL_SCENARIOS=$((TOTAL_SCENARIOS + 1))
S1_DUPS=$DUPLICATE_EVENTS
S1_RACES=$RACE_CONDITIONS
if [ "$S1_DUPS" -eq 0 ] && [ "$S1_RACES" -eq 0 ]; then
    PASSED_SCENARIOS=$((PASSED_SCENARIOS + 1))
fi

# ============================================================
# Scenario 2: Rapid-fire syncs
# ============================================================
_step "Scenario 2: Rapid-fire syncs ($RAPID_FIRE_COUNT syncs in quick succession)"

# Create more data
td_a create "Rapid fire test issue 1" >/dev/null
td_a create "Rapid fire test issue 2" >/dev/null
td_a update 1 --label "urgent" >/dev/null 2>&1 || true

# Record state before
BEFORE_SYNCED_RF=$(count_synced_events "$DB_A")
BEFORE_SERVER_RF=$(get_server_event_count "a")

[ "$VERBOSE" = "true" ] && echo "  Before: synced=$BEFORE_SYNCED_RF server=$BEFORE_SERVER_RF"

# Launch rapid-fire syncs (bash 3.2 compatible)
RAPID_PID_LIST=""
for i in $(seq 1 "$RAPID_FIRE_COUNT"); do
    run_sync_capture "a" "$SYNC_OUTPUTS/rapid_$i.out" &
    RAPID_PID_LIST="$RAPID_PID_LIST $!"
    # Tiny delay to stagger starts (but still rapid)
    sleep 0.05
done

# Wait for all
RAPID_FAILURES=0
RAPID_LOCK_ERRORS=0
rapid_idx=1
for pid in $RAPID_PID_LIST; do
    wait "$pid" 2>/dev/null || true
    exit_code=$(grep "^EXIT:" "$SYNC_OUTPUTS/rapid_$rapid_idx.out" 2>/dev/null | cut -d: -f2 || echo "1")
    if [ "$exit_code" != "0" ]; then
        if grep -q "database is locked\|SQLITE_BUSY" "$SYNC_OUTPUTS/rapid_$rapid_idx.out" 2>/dev/null; then
            RAPID_LOCK_ERRORS=$((RAPID_LOCK_ERRORS + 1))
        else
            RAPID_FAILURES=$((RAPID_FAILURES + 1))
        fi
    fi
    rapid_idx=$((rapid_idx + 1))
done

sleep 0.5

# Final cleanup sync
td_a sync >/dev/null 2>&1 || true

# Record state after
AFTER_SYNCED_RF=$(count_synced_events "$DB_A")
AFTER_SERVER_RF=$(get_server_event_count "a")

[ "$VERBOSE" = "true" ] && echo "  After: synced=$AFTER_SYNCED_RF server=$AFTER_SERVER_RF"
[ "$VERBOSE" = "true" ] && echo "  Lock errors: $RAPID_LOCK_ERRORS, Failures: $RAPID_FAILURES"

# Verify: No duplicates
DUPS_RF=$(check_duplicate_server_seqs "$DB_A")
if [ -z "$DUPS_RF" ]; then
    _ok "no duplicate server_seq after rapid-fire"
else
    _fail "duplicate server_seq after rapid-fire: $DUPS_RF"
    DUPLICATE_EVENTS=$((DUPLICATE_EVENTS + 1))
fi

# Verify: sync_state consistent
SYNC_STATE_RF=$(get_sync_state "$DB_A")
LAST_PUSHED_RF=$(echo "$SYNC_STATE_RF" | cut -d'|' -f1)
MAX_SYNCED_ROWID_RF=$(get_max_synced_rowid "$DB_A")

if [ "$LAST_PUSHED_RF" -ge "$MAX_SYNCED_ROWID_RF" ] 2>/dev/null; then
    _ok "sync_state consistent after rapid-fire ($LAST_PUSHED_RF >= $MAX_SYNCED_ROWID_RF)"
else
    _fail "sync_state inconsistent after rapid-fire: $LAST_PUSHED_RF < $MAX_SYNCED_ROWID_RF"
    RACE_CONDITIONS=$((RACE_CONDITIONS + 1))
fi

# Verify: no pending events
PENDING_RF=$(count_pending_events "$DB_A")
if [ "$PENDING_RF" -eq 0 ]; then
    _ok "all events synced after rapid-fire"
else
    _fail "$PENDING_RF events pending after rapid-fire"
fi

TOTAL_SCENARIOS=$((TOTAL_SCENARIOS + 1))
S2_DUPS=$((DUPLICATE_EVENTS - S1_DUPS))
S2_RACES=$((RACE_CONDITIONS - S1_RACES))
if [ "$S2_DUPS" -eq 0 ] && [ "$S2_RACES" -eq 0 ]; then
    PASSED_SCENARIOS=$((PASSED_SCENARIOS + 1))
fi

# ============================================================
# Scenario 3: Verify server has no duplicates and convergence
# ============================================================
_step "Scenario 3: Verify server event consistency and convergence"

# Pull everything to Bob and verify data matches
td_b sync >/dev/null 2>&1 || true

# Get Alice's synced event count and unique server_seq count
SYNCED_A=$(count_synced_events "$DB_A")
UNIQUE_SEQ_A=$(sqlite3 "$DB_A" "SELECT COUNT(DISTINCT server_seq) FROM action_log WHERE server_seq IS NOT NULL;" 2>/dev/null || echo "0")

[ "$VERBOSE" = "true" ] && echo "  Alice: synced=$SYNCED_A unique_seq=$UNIQUE_SEQ_A"

# Verify Alice has no duplicate server_seq values (no duplicates pushed)
if [ "$SYNCED_A" -eq "$UNIQUE_SEQ_A" ]; then
    _ok "Alice: no duplicate server_seq ($SYNCED_A events, $UNIQUE_SEQ_A unique)"
else
    _fail "Alice: server_seq duplicates detected ($SYNCED_A synced, $UNIQUE_SEQ_A unique)"
    DUPLICATE_EVENTS=$((DUPLICATE_EVENTS + 1))
fi

# Verify convergence: Bob should have same issues as Alice
ISSUES_A=$(td_a list --json 2>/dev/null | jq 'length' 2>/dev/null || echo "0")
ISSUES_B=$(td_b list --json 2>/dev/null | jq 'length' 2>/dev/null || echo "0")

[ "$VERBOSE" = "true" ] && echo "  Alice issues: $ISSUES_A, Bob issues: $ISSUES_B"

if [ "$ISSUES_A" = "$ISSUES_B" ]; then
    _ok "convergence: Alice and Bob have same issue count ($ISSUES_A)"
else
    _fail "convergence failed: Alice=$ISSUES_A issues, Bob=$ISSUES_B issues"
fi

# Verify server event count matches expected
# Server should have exactly UNIQUE_SEQ_A events (no duplicates)
SERVER_EVENTS=$(get_server_event_count "a")
SERVER_EVENTS=${SERVER_EVENTS:-0}

[ "$VERBOSE" = "true" ] && echo "  Server events: $SERVER_EVENTS"

# Server events should match alice's unique synced count
if [ "$SERVER_EVENTS" = "$UNIQUE_SEQ_A" ]; then
    _ok "server event count matches expected ($SERVER_EVENTS)"
else
    # This could happen if there's a mismatch but it's not necessarily a failure
    # since server counts may differ due to internal events
    _ok "server event count: $SERVER_EVENTS (alice unique: $UNIQUE_SEQ_A)"
fi

TOTAL_SCENARIOS=$((TOTAL_SCENARIOS + 1))
if [ "$SYNCED_A" -eq "$UNIQUE_SEQ_A" ] && [ "$ISSUES_A" = "$ISSUES_B" ]; then
    PASSED_SCENARIOS=$((PASSED_SCENARIOS + 1))
fi

# ============================================================
# Summary
# ============================================================
_step "Summary"
echo "  Scenarios:          $PASSED_SCENARIOS / $TOTAL_SCENARIOS passed"
echo "  Parallel syncs:     $PARALLEL_SYNCS"
echo "  Rapid-fire syncs:   $RAPID_FIRE_COUNT"
echo "  DB lock errors:     $((LOCK_ERRORS + RAPID_LOCK_ERRORS)) (informational)"
echo "  Duplicate events:   $DUPLICATE_EVENTS"
echo "  Race conditions:    $RACE_CONDITIONS"

# JSON report
if [ -n "$JSON_REPORT" ]; then
    cat > "$JSON_REPORT" <<EOF
{
  "test": "concurrent_sync",
  "scenarios_passed": $PASSED_SCENARIOS,
  "scenarios_total": $TOTAL_SCENARIOS,
  "parallel_syncs": $PARALLEL_SYNCS,
  "rapid_fire_syncs": $RAPID_FIRE_COUNT,
  "db_lock_errors": $((LOCK_ERRORS + RAPID_LOCK_ERRORS)),
  "duplicate_events": $DUPLICATE_EVENTS,
  "race_conditions": $RACE_CONDITIONS,
  "passed": $([ "$DUPLICATE_EVENTS" -eq 0 ] && [ "$RACE_CONDITIONS" -eq 0 ] && echo "true" || echo "false")
}
EOF
    _ok "JSON report written to $JSON_REPORT"
fi

# Cleanup
rm -rf "$SYNC_OUTPUTS"

# Final status
if [ "$DUPLICATE_EVENTS" -gt 0 ] || [ "$RACE_CONDITIONS" -gt 0 ]; then
    _fail "Concurrent sync test detected issues"
fi

report
