#!/usr/bin/env bash
#
# Server restart mid-test scenario: kill the sync server mid-test, let clients
# accumulate local changes, restart server, and sync. Tests server durability
# and client retry logic.
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"
source "$(dirname "$0")/chaos_lib.sh"

# ---- Defaults ----
SEED=$$
VERBOSE=false
# Phase 1: Both clients sync normally
PHASE1_ACTIONS=50
# Phase 2: Server down, clients accumulate local mutations
PHASE2_ACTIONS_A=30
PHASE2_ACTIONS_B=30
# Report options
JSON_REPORT=""
REPORT_FILE=""

# ---- Server restart-specific counters ----
RESTART_DOWNTIME_START=0
RESTART_DOWNTIME_END=0
RESTART_DOWNTIME_SECONDS=0
RESTART_MUTATIONS_DURING_OUTAGE_A=0
RESTART_MUTATIONS_DURING_OUTAGE_B=0
RESTART_SYNC_FAILURES_DURING_OUTAGE=0
RESTART_EVENTS_BEFORE_RESTART=0
RESTART_EVENTS_AFTER_RESTART=0

# ---- Usage ----
usage() {
    cat <<EOF
Usage: bash scripts/e2e/test_server_restart.sh [OPTIONS]

Server restart mid-test scenario: kills the sync server mid-test, lets clients
accumulate local changes, restarts server, and syncs. Tests server durability
and client retry logic.

Options:
  --seed N              RANDOM seed for reproducibility (default: \$\$)
  --verbose             Detailed per-action output (default: false)
  --phase1-actions N    Actions during initial sync phase (default: 50)
  --offline-actions-a N Mutations by A during outage (default: 30)
  --offline-actions-b N Mutations by B during outage (default: 30)
  --json-report PATH    Write JSON summary to file
  --report-file PATH    Write text report to file
  -h, --help            Show this help

Examples:
  # Quick smoke test
  bash scripts/e2e/test_server_restart.sh --phase1-actions 20 --offline-actions-a 10 --offline-actions-b 10 --verbose

  # Standard run
  bash scripts/e2e/test_server_restart.sh

  # Reproducible run
  bash scripts/e2e/test_server_restart.sh --seed 42

  # Stress test with many offline mutations
  bash scripts/e2e/test_server_restart.sh --offline-actions-a 50 --offline-actions-b 50
EOF
}

# ---- Parse args ----
while [[ $# -gt 0 ]]; do
    case "$1" in
        --seed)              SEED="$2"; shift 2 ;;
        --verbose)           VERBOSE=true; shift ;;
        --phase1-actions)    PHASE1_ACTIONS="$2"; shift 2 ;;
        --offline-actions-a) PHASE2_ACTIONS_A="$2"; shift 2 ;;
        --offline-actions-b) PHASE2_ACTIONS_B="$2"; shift 2 ;;
        --json-report)       JSON_REPORT="$2"; shift 2 ;;
        --report-file)       REPORT_FILE="$2"; shift 2 ;;
        -h|--help)           usage; exit 0 ;;
        *) echo "Unknown arg: $1" >&2; usage; exit 1 ;;
    esac
done

# ---- Setup ----
HARNESS_ACTORS=2
export HARNESS_ACTORS
setup

# ---- Seed RANDOM for reproducibility ----
RANDOM=$SEED

# ---- Configure chaos_lib ----
CHAOS_VERBOSE="$VERBOSE"
CHAOS_SYNC_MODE="adaptive"
CHAOS_SYNC_BATCH_MIN=3
CHAOS_SYNC_BATCH_MAX=8

# ---- Initial sync ----
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true

# ---- Config summary ----
_step "Server restart test (seed: $SEED)"
echo "  Phase 1 (normal):       $PHASE1_ACTIONS actions with syncs"
echo "  Phase 2 (server down):  A: $PHASE2_ACTIONS_A actions, B: $PHASE2_ACTIONS_B actions (sync fails expected)"
echo "  Phase 3:                Server restarts, sync accumulated changes"

# ============================================================
# PHASE 1: Both clients sync normally
# ============================================================
_step "Phase 1: Normal sync period ($PHASE1_ACTIONS actions)"
CHAOS_TIME_START=$(date +%s)

for _ in $(seq 1 "$PHASE1_ACTIONS"); do
    # Alternate actors
    rand_choice a b; local_actor="$_RAND_RESULT"

    # Pick and execute action
    select_action; action="$_CHAOS_SELECTED_ACTION"
    safe_exec "$action" "$local_actor"

    # Maybe sync
    maybe_sync
done

# Sync both to establish consistent baseline before restart
_step "Phase 1: Final sync before server restart"
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 1
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true

# Record event counts before restart
DB_A="$CLIENT_A_DIR/.todos/issues.db"
DB_B="$CLIENT_B_DIR/.todos/issues.db"
RESTART_EVENTS_BEFORE_RESTART=$(sqlite3 "$DB_A" "SELECT MAX(server_seq) FROM action_log WHERE server_seq IS NOT NULL;" 2>/dev/null || echo "0")
RESTART_EVENTS_BEFORE_RESTART=${RESTART_EVENTS_BEFORE_RESTART:-0}

_ok "Phase 1 complete: $CHAOS_ACTION_COUNT actions, ${#CHAOS_ISSUE_IDS[@]} issues, ${#CHAOS_BOARD_NAMES[@]} boards"
_ok "Server events before restart: $RESTART_EVENTS_BEFORE_RESTART"

# ============================================================
# PHASE 2: Server down - clients accumulate local mutations
# ============================================================
_step "Phase 2: Stopping server"
RESTART_DOWNTIME_START=$(date +%s)
stop_server

_step "Phase 2a: Actor A mutations during outage ($PHASE2_ACTIONS_A actions)"
for _ in $(seq 1 "$PHASE2_ACTIONS_A"); do
    # Weighted toward creates and updates for testing batch push
    rand_int 1 100
    if [ "$_RAND_RESULT" -le 30 ]; then
        action="create"
    elif [ "$_RAND_RESULT" -le 60 ]; then
        action="update"
    else
        select_action; action="$_CHAOS_SELECTED_ACTION"
    fi

    safe_exec "$action" "a"
    RESTART_MUTATIONS_DURING_OUTAGE_A=$((RESTART_MUTATIONS_DURING_OUTAGE_A + 1))

    # Attempt sync (should fail gracefully)
    if [ $(( RANDOM % 4 )) -eq 0 ]; then
        sync_output=$(td_a sync 2>&1) || true
        if echo "$sync_output" | grep -qE "connection refused|error|failed|dial tcp"; then
            RESTART_SYNC_FAILURES_DURING_OUTAGE=$((RESTART_SYNC_FAILURES_DURING_OUTAGE + 1))
            [ "$VERBOSE" = "true" ] && _ok "expected sync failure during outage (A)"
        fi
    fi
done

_step "Phase 2b: Actor B mutations during outage ($PHASE2_ACTIONS_B actions)"
for _ in $(seq 1 "$PHASE2_ACTIONS_B"); do
    # Weighted toward creates and updates
    rand_int 1 100
    if [ "$_RAND_RESULT" -le 30 ]; then
        action="create"
    elif [ "$_RAND_RESULT" -le 60 ]; then
        action="update"
    else
        select_action; action="$_CHAOS_SELECTED_ACTION"
    fi

    safe_exec "$action" "b"
    RESTART_MUTATIONS_DURING_OUTAGE_B=$((RESTART_MUTATIONS_DURING_OUTAGE_B + 1))

    # Attempt sync (should fail gracefully)
    if [ $(( RANDOM % 4 )) -eq 0 ]; then
        sync_output=$(td_b sync 2>&1) || true
        if echo "$sync_output" | grep -qE "connection refused|error|failed|dial tcp"; then
            RESTART_SYNC_FAILURES_DURING_OUTAGE=$((RESTART_SYNC_FAILURES_DURING_OUTAGE + 1))
            [ "$VERBOSE" = "true" ] && _ok "expected sync failure during outage (B)"
        fi
    fi
done

RESTART_DOWNTIME_END=$(date +%s)
RESTART_DOWNTIME_SECONDS=$((RESTART_DOWNTIME_END - RESTART_DOWNTIME_START))

_ok "Phase 2 complete: A: $RESTART_MUTATIONS_DURING_OUTAGE_A mutations, B: $RESTART_MUTATIONS_DURING_OUTAGE_B mutations"
_ok "Server downtime: ${RESTART_DOWNTIME_SECONDS}s, sync failures: $RESTART_SYNC_FAILURES_DURING_OUTAGE"

# ============================================================
# PHASE 3: Restart server, sync accumulated changes
# ============================================================
_step "Phase 3: Restarting server"
start_server

# Wait for healthz (already done in start_server, but verify)
wait_for "curl -sf $SERVER_URL/healthz" 10 || _fatal "Server not healthy after restart"
_ok "Server healthy after restart"

# ============================================================
# PHASE 4: Sync accumulated changes
# ============================================================
_step "Phase 4: Syncing accumulated changes"

# Multiple sync rounds to ensure convergence
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 1
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true
# One more round for good measure
sleep 1
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true

# Record events after restart sync
RESTART_EVENTS_AFTER_RESTART=$(sqlite3 "$DB_A" "SELECT MAX(server_seq) FROM action_log WHERE server_seq IS NOT NULL;" 2>/dev/null || echo "0")
RESTART_EVENTS_AFTER_RESTART=${RESTART_EVENTS_AFTER_RESTART:-0}

_ok "Events pushed after restart: $((RESTART_EVENTS_AFTER_RESTART - RESTART_EVENTS_BEFORE_RESTART))"

# ============================================================
# PHASE 5: Verification
# ============================================================

# Verify server data durability: pre-restart data should still exist
_step "Server data durability check"
_pre_restart_issues=$(sqlite3 "$DB_A" "SELECT COUNT(*) FROM issues;")
if [ "${#CHAOS_ISSUE_IDS[@]}" -eq 0 ]; then
    _ok "No issues created this run (skipping durability check)"
elif [ "$_pre_restart_issues" -gt 0 ]; then
    _ok "Pre-restart issues preserved: $_pre_restart_issues total issues"
else
    _fail "No issues found after restart"
fi

# Convergence verification
_step "Convergence verification"
_conv_failures_before=$HARNESS_FAILURES
verify_convergence "$DB_A" "$DB_B"
_conv_failures_after=$HARNESS_FAILURES
if [ "$_conv_failures_after" -eq "$_conv_failures_before" ]; then
    kv_set CHAOS_CONVERGENCE_RESULTS "A_vs_B" "pass"
    CHAOS_CONVERGENCE_PASSED=$(( CHAOS_CONVERGENCE_PASSED + 1 ))
else
    kv_set CHAOS_CONVERGENCE_RESULTS "A_vs_B" "fail"
    CHAOS_CONVERGENCE_FAILED=$(( CHAOS_CONVERGENCE_FAILED + 1 ))
fi

# Idempotency verification
verify_idempotency "$DB_A" "$DB_B"

# Event count verification
verify_event_counts "$DB_A" "$DB_B"

# ============================================================
# SUMMARY STATS
# ============================================================
_step "Summary"
echo "  Total actions:                $CHAOS_ACTION_COUNT"
echo "  Total syncs:                  $CHAOS_SYNC_COUNT"
echo "  Issues created:               ${#CHAOS_ISSUE_IDS[@]}"
echo "  Boards created:               ${#CHAOS_BOARD_NAMES[@]}"
echo "  Expected failures:            $CHAOS_EXPECTED_FAILURES"
echo "  Unexpected failures:          $CHAOS_UNEXPECTED_FAILURES"
echo ""
echo "  -- Server Restart Stats --"
echo "  Server downtime:              ${RESTART_DOWNTIME_SECONDS}s"
echo "  Mutations during outage (A):  $RESTART_MUTATIONS_DURING_OUTAGE_A"
echo "  Mutations during outage (B):  $RESTART_MUTATIONS_DURING_OUTAGE_B"
echo "  Sync failures during outage:  $RESTART_SYNC_FAILURES_DURING_OUTAGE"
echo "  Events before restart:        $RESTART_EVENTS_BEFORE_RESTART"
echo "  Events after restart:         $RESTART_EVENTS_AFTER_RESTART"
echo "  Events pushed post-restart:   $((RESTART_EVENTS_AFTER_RESTART - RESTART_EVENTS_BEFORE_RESTART))"
echo ""
echo "  Seed:                         $SEED (use --seed $SEED to reproduce)"

CHAOS_TIME_END=$(date +%s)

# ---- Detailed report ----
chaos_report "$REPORT_FILE"

# ---- JSON report for CI ----
if [ -n "$JSON_REPORT" ]; then
    _json_wall_clock=0
    if [ "$CHAOS_TIME_START" -gt 0 ]; then
        _json_wall_clock=$(( CHAOS_TIME_END - CHAOS_TIME_START ))
    fi

    cat > "$JSON_REPORT" <<ENDJSON
{
  "test": "server_restart",
  "seed": $SEED,
  "pass": $([ "$CHAOS_UNEXPECTED_FAILURES" -eq 0 ] && [ "$CHAOS_CONVERGENCE_FAILED" -eq 0 ] && echo "true" || echo "false"),
  "totals": {
    "actions": $CHAOS_ACTION_COUNT,
    "syncs": $CHAOS_SYNC_COUNT,
    "issues_created": ${#CHAOS_ISSUE_IDS[@]},
    "boards_created": ${#CHAOS_BOARD_NAMES[@]},
    "expected_failures": $CHAOS_EXPECTED_FAILURES,
    "unexpected_failures": $CHAOS_UNEXPECTED_FAILURES
  },
  "server_restart": {
    "phase1_actions": $PHASE1_ACTIONS,
    "downtime_seconds": $RESTART_DOWNTIME_SECONDS,
    "mutations_during_outage_a": $RESTART_MUTATIONS_DURING_OUTAGE_A,
    "mutations_during_outage_b": $RESTART_MUTATIONS_DURING_OUTAGE_B,
    "sync_failures_during_outage": $RESTART_SYNC_FAILURES_DURING_OUTAGE,
    "events_before_restart": $RESTART_EVENTS_BEFORE_RESTART,
    "events_after_restart": $RESTART_EVENTS_AFTER_RESTART
  },
  "convergence": {
    "passed": $CHAOS_CONVERGENCE_PASSED,
    "failed": $CHAOS_CONVERGENCE_FAILED
  },
  "timing": {
    "wall_clock_seconds": $_json_wall_clock,
    "sync_seconds": $CHAOS_TIME_SYNCING,
    "mutation_seconds": $CHAOS_TIME_MUTATING
  }
}
ENDJSON
    _ok "JSON report written to $JSON_REPORT"
fi

# ---- Final check ----
if [ "$CHAOS_UNEXPECTED_FAILURES" -gt 0 ]; then
    _fail "$CHAOS_UNEXPECTED_FAILURES unexpected failures"
fi

report
