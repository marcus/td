#!/usr/bin/env bash
#
# Network partition simulation test: one client goes offline, accumulates
# mutations, then reconnects and syncs a large batch.
#
# Tests the "offline-first" stress case where clients accumulate substantial
# local changes during network unavailability.
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"
source "$(dirname "$0")/chaos_lib.sh"

# ---- Defaults ----
SEED=$$
VERBOSE=false
# Phase 1: Both clients sync normally
PHASE1_ACTIONS=25
# Phase 2: A offline, B online - both accumulate mutations
PHASE2_OFFLINE_ACTIONS=40
PHASE2_ONLINE_ACTIONS=30
# Report options
JSON_REPORT=""
REPORT_FILE=""

# ---- Partition-specific counters ----
PARTITION_OFFLINE_MUTATIONS_A=0
PARTITION_ONLINE_MUTATIONS_B=0
PARTITION_RECONNECT_BATCH_SIZE=0
PARTITION_TOMBSTONE_CONFLICTS=0
PARTITION_FIELD_CONFLICTS=0

# ---- Usage ----
usage() {
    cat <<EOF
Usage: bash scripts/e2e/test_network_partition.sh [OPTIONS]

Network partition simulation: one client goes offline, accumulates mutations,
then reconnects and syncs a large batch. Tests offline-first sync scenarios.

Options:
  --seed N                  RANDOM seed for reproducibility (default: \$\$)
  --verbose                 Detailed per-action output (default: false)
  --phase1-actions N        Actions during initial sync phase (default: 25)
  --offline-actions N       Mutations while offline (default: 40)
  --online-actions N        Mutations by online client during partition (default: 30)
  --json-report PATH        Write JSON summary to file
  --report-file PATH        Write text report to file
  -h, --help                Show this help

Examples:
  # Quick smoke test
  bash scripts/e2e/test_network_partition.sh --phase1-actions 10 --offline-actions 20 --online-actions 15 --verbose

  # Standard run
  bash scripts/e2e/test_network_partition.sh

  # Reproducible run
  bash scripts/e2e/test_network_partition.sh --seed 42

  # Stress test with large offline batch
  bash scripts/e2e/test_network_partition.sh --offline-actions 100 --online-actions 50
EOF
}

# ---- Parse args ----
while [[ $# -gt 0 ]]; do
    case "$1" in
        --seed)             SEED="$2"; shift 2 ;;
        --verbose)          VERBOSE=true; shift ;;
        --phase1-actions)   PHASE1_ACTIONS="$2"; shift 2 ;;
        --offline-actions)  PHASE2_OFFLINE_ACTIONS="$2"; shift 2 ;;
        --online-actions)   PHASE2_ONLINE_ACTIONS="$2"; shift 2 ;;
        --json-report)      JSON_REPORT="$2"; shift 2 ;;
        --report-file)      REPORT_FILE="$2"; shift 2 ;;
        -h|--help)          usage; exit 0 ;;
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
_step "Network partition test (seed: $SEED)"
echo "  Phase 1 (normal):       $PHASE1_ACTIONS actions with syncs"
echo "  Phase 2 (partition):    A offline ($PHASE2_OFFLINE_ACTIONS actions), B online ($PHASE2_ONLINE_ACTIONS actions)"
echo "  Phase 3:                A reconnects and syncs large batch"

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

# Sync both to establish consistent baseline
_step "Phase 1: Final sync"
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 1
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true

_ok "Phase 1 complete: $CHAOS_ACTION_COUNT actions, ${#CHAOS_ISSUE_IDS[@]} issues, ${#CHAOS_BOARD_NAMES[@]} boards"

# ============================================================
# PHASE 2: Network Partition
# Actor A goes offline - accumulates local mutations without sync
# Actor B continues normally with syncs
# ============================================================
_step "Phase 2: Network partition"
echo "  Actor A: offline (no syncs)"
echo "  Actor B: online (normal syncs)"

# Track issues created by each actor during partition for conflict scenarios
PARTITION_ISSUES_A=()
PARTITION_ISSUES_B=()

# --- Phase 2a: Actor A offline mutations ---
_step "Phase 2a: Actor A offline mutations ($PHASE2_OFFLINE_ACTIONS actions)"
_phase2a_start=$CHAOS_ACTION_COUNT

for _ in $(seq 1 "$PHASE2_OFFLINE_ACTIONS"); do
    # Weighted action selection - bias toward creates and updates for conflict potential
    rand_int 1 100
    if [ "$_RAND_RESULT" -le 25 ]; then
        # 25% create - more creates offline to test large batch push
        action="create"
    elif [ "$_RAND_RESULT" -le 50 ]; then
        # 25% update existing
        action="update"
    else
        # 50% random action
        select_action; action="$_CHAOS_SELECTED_ACTION"
    fi

    safe_exec "$action" "a"
    PARTITION_OFFLINE_MUTATIONS_A=$((PARTITION_OFFLINE_MUTATIONS_A + 1))

    # Track creates for conflict setup (bash 3.2 compatible - no negative indices)
    if [ "$action" = "create" ] && [ "${#CHAOS_ISSUE_IDS[@]}" -gt 0 ]; then
        _last_idx=$(( ${#CHAOS_ISSUE_IDS[@]} - 1 ))
        PARTITION_ISSUES_A+=("${CHAOS_ISSUE_IDS[$_last_idx]}")
    fi

    # NO SYNC - A is offline
done

_ok "Actor A offline: $((CHAOS_ACTION_COUNT - _phase2a_start)) actions without sync"

# --- Phase 2b: Actor B online mutations (with syncs) ---
_step "Phase 2b: Actor B online mutations ($PHASE2_ONLINE_ACTIONS actions)"
_phase2b_start=$CHAOS_ACTION_COUNT

# B should modify some issues that A created before partition (conflict setup)
# and create some of its own
_conflict_setup_done=false

for i in $(seq 1 "$PHASE2_ONLINE_ACTIONS"); do
    # First few actions: set up conflicts with pre-partition issues
    if [ "$i" -le 5 ] && [ "${#CHAOS_ISSUE_IDS[@]}" -gt "${#PARTITION_ISSUES_A[@]}" ]; then
        # Find an issue that existed before A went offline
        for _potential_id in "${CHAOS_ISSUE_IDS[@]}"; do
            _is_partition_a=false
            for _pa_id in "${PARTITION_ISSUES_A[@]:-}"; do
                [ "$_potential_id" = "$_pa_id" ] && _is_partition_a=true && break
            done
            if [ "$_is_partition_a" = "false" ] && ! is_chaos_deleted "$_potential_id"; then
                # B modifies an issue that A might also modify when offline
                rand_int 1 3
                case "$_RAND_RESULT" in
                    1) # Update same fields A might update
                        rand_title 100
                        chaos_run_td "b" update "$_potential_id" --title "$_RAND_STR" >/dev/null 2>&1 || true
                        PARTITION_FIELD_CONFLICTS=$((PARTITION_FIELD_CONFLICTS + 1))
                        [ "$VERBOSE" = "true" ] && _ok "conflict setup: B updated $_potential_id"
                        ;;
                    2) # Delete (tombstone conflict if A modifies)
                        if [ $(( RANDOM % 3 )) -eq 0 ]; then
                            chaos_run_td "b" delete "$_potential_id" >/dev/null 2>&1 || true
                            CHAOS_DELETED_IDS+=("$_potential_id")
                            PARTITION_TOMBSTONE_CONFLICTS=$((PARTITION_TOMBSTONE_CONFLICTS + 1))
                            [ "$VERBOSE" = "true" ] && _ok "conflict setup: B deleted $_potential_id"
                        fi
                        ;;
                    3) # Status change
                        rand_choice start review close
                        chaos_run_td "b" "$_RAND_RESULT" "$_potential_id" >/dev/null 2>&1 || true
                        [ "$VERBOSE" = "true" ] && _ok "conflict setup: B status-changed $_potential_id"
                        ;;
                esac
                CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))
                _conflict_setup_done=true
                break
            fi
        done
    fi

    # Regular actions
    rand_int 1 100
    if [ "$_RAND_RESULT" -le 20 ]; then
        action="create"
    else
        select_action; action="$_CHAOS_SELECTED_ACTION"
    fi

    safe_exec "$action" "b"

    # Track creates (bash 3.2 compatible - no negative indices)
    if [ "$action" = "create" ] && [ "${#CHAOS_ISSUE_IDS[@]}" -gt 0 ]; then
        _last_idx_b=$(( ${#CHAOS_ISSUE_IDS[@]} - 1 ))
        PARTITION_ISSUES_B+=("${CHAOS_ISSUE_IDS[$_last_idx_b]}")
    fi

    # B syncs normally
    maybe_sync
done

# Ensure B has synced all changes
td_b sync >/dev/null 2>&1 || true

PARTITION_ONLINE_MUTATIONS_B=$((CHAOS_ACTION_COUNT - _phase2b_start))
_ok "Actor B online: $PARTITION_ONLINE_MUTATIONS_B actions with syncs"

# ============================================================
# PHASE 3: Reconnection - A syncs large batch
# ============================================================
_step "Phase 3: Actor A reconnects and syncs"

# Count events before A's sync
DB_A="$CLIENT_A_DIR/.todos/issues.db"
_events_before_sync=$(sqlite3 "$DB_A" "SELECT MAX(id) FROM sync_events;" 2>/dev/null || echo "0")
_events_before_sync=${_events_before_sync:-0}

# A reconnects and syncs (pushes large batch of offline mutations)
_ok "A pushing ~$PARTITION_OFFLINE_MUTATIONS_A offline mutations..."
td_a sync >/dev/null 2>&1 || true

# Count events after A's sync to measure batch size
_events_after_sync=$(sqlite3 "$DB_A" "SELECT MAX(id) FROM sync_events;" 2>/dev/null || echo "0")
_events_after_sync=${_events_after_sync:-0}
PARTITION_RECONNECT_BATCH_SIZE=$(( _events_after_sync - _events_before_sync ))
[ "$PARTITION_RECONNECT_BATCH_SIZE" -lt 0 ] && PARTITION_RECONNECT_BATCH_SIZE=0

_ok "A reconnect sync complete (batch: ~$PARTITION_RECONNECT_BATCH_SIZE events)"

# ============================================================
# FINAL SYNC: Full round-robin for convergence
# ============================================================
_step "Final sync (round-robin)"
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 1
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true

# ============================================================
# CONVERGENCE VERIFICATION
# ============================================================
DB_A="$CLIENT_A_DIR/.todos/issues.db"
DB_B="$CLIENT_B_DIR/.todos/issues.db"

# Track convergence results
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

# ---- Idempotency verification ----
verify_idempotency "$DB_A" "$DB_B"

# ---- Event count verification ----
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
echo "  -- Network Partition Stats --"
echo "  Offline mutations (A):        $PARTITION_OFFLINE_MUTATIONS_A"
echo "  Online mutations (B):         $PARTITION_ONLINE_MUTATIONS_B"
echo "  Reconnect batch size:         ~$PARTITION_RECONNECT_BATCH_SIZE events"
echo "  Issues created offline (A):   ${#PARTITION_ISSUES_A[@]}"
echo "  Issues created online (B):    ${#PARTITION_ISSUES_B[@]}"
echo "  Field conflict setups:        $PARTITION_FIELD_CONFLICTS"
echo "  Tombstone conflict setups:    $PARTITION_TOMBSTONE_CONFLICTS"
echo ""
echo "  Seed:                         $SEED (use --seed $SEED to reproduce)"

CHAOS_TIME_END=$(date +%s)

# ---- Detailed report ----
chaos_report "$REPORT_FILE"

# ---- JSON report for CI ----
if [ -n "$JSON_REPORT" ]; then
    # Generate partition-specific JSON (no 'local' - we're outside a function)
    _json_wall_clock=0
    if [ "$CHAOS_TIME_START" -gt 0 ]; then
        _json_wall_clock=$(( CHAOS_TIME_END - CHAOS_TIME_START ))
    fi

    cat > "$JSON_REPORT" <<ENDJSON
{
  "test": "network_partition",
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
  "partition": {
    "phase1_actions": $PHASE1_ACTIONS,
    "offline_mutations_a": $PARTITION_OFFLINE_MUTATIONS_A,
    "online_mutations_b": $PARTITION_ONLINE_MUTATIONS_B,
    "reconnect_batch_size": $PARTITION_RECONNECT_BATCH_SIZE,
    "issues_created_offline_a": ${#PARTITION_ISSUES_A[@]},
    "issues_created_online_b": ${#PARTITION_ISSUES_B[@]},
    "field_conflict_setups": $PARTITION_FIELD_CONFLICTS,
    "tombstone_conflict_setups": $PARTITION_TOMBSTONE_CONFLICTS
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
