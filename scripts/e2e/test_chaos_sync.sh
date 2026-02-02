#!/usr/bin/env bash
#
# Chaos sync e2e test: randomized multi-actor mutations with convergence verification.
# Exercises create, update, delete, restore, status transitions, comments, logs,
# dependencies, boards, handoffs — all with configurable sync timing and conflict injection.
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"
source "$(dirname "$0")/chaos_lib.sh"

# ---- Defaults ----
ACTIONS=100
DURATION=0
SEED=$$
SYNC_MODE="adaptive"
VERBOSE=false
CONFLICT_RATE=20
BATCH_MIN=3
BATCH_MAX=10

# ---- Usage ----
usage() {
    cat <<EOF
Usage: bash scripts/e2e/test_chaos_sync.sh [OPTIONS]

Randomized chaos sync test: two actors perform random mutations with
configurable sync timing, conflict injection, and convergence verification.

Options:
  --actions N        Total actions to perform (default: 100)
  --duration N       Seconds to run; overrides --actions when >0 (default: 0)
  --seed N           RANDOM seed for reproducibility (default: \$\$)
  --sync-mode MODE   Sync strategy: adaptive, aggressive, random (default: adaptive)
  --verbose          Detailed per-action output (default: false)
  --conflict-rate N  Percentage of actions with simultaneous mutations (default: 20)
  --batch-min N      Min actions between syncs (default: 3)
  --batch-max N      Max actions between syncs (default: 10)
  -h, --help         Show this help

Examples:
  # Quick smoke test
  bash scripts/e2e/test_chaos_sync.sh --actions 20 --sync-mode aggressive --verbose

  # Standard run
  bash scripts/e2e/test_chaos_sync.sh --actions 100

  # Stress test with conflicts
  bash scripts/e2e/test_chaos_sync.sh --actions 500 --conflict-rate 30

  # Reproducible run
  bash scripts/e2e/test_chaos_sync.sh --seed 42 --actions 50

  # Time-based
  bash scripts/e2e/test_chaos_sync.sh --duration 60
EOF
}

# ---- Parse args ----
while [[ $# -gt 0 ]]; do
    case "$1" in
        --actions)        ACTIONS="$2"; shift 2 ;;
        --duration)       DURATION="$2"; shift 2 ;;
        --seed)           SEED="$2"; shift 2 ;;
        --sync-mode)      SYNC_MODE="$2"; shift 2 ;;
        --verbose)        VERBOSE=true; shift ;;
        --conflict-rate)  CONFLICT_RATE="$2"; shift 2 ;;
        --batch-min)      BATCH_MIN="$2"; shift 2 ;;
        --batch-max)      BATCH_MAX="$2"; shift 2 ;;
        -h|--help)        usage; exit 0 ;;
        *) echo "Unknown arg: $1" >&2; usage; exit 1 ;;
    esac
done

# ---- Setup ----
setup

# ---- Seed RANDOM for reproducibility ----
RANDOM=$SEED

# ---- Configure chaos_lib ----
CHAOS_SYNC_MODE="$SYNC_MODE"
CHAOS_SYNC_BATCH_MIN="$BATCH_MIN"
CHAOS_SYNC_BATCH_MAX="$BATCH_MAX"
CHAOS_VERBOSE="$VERBOSE"

# ---- Initial sync ----
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true

# ---- Config summary ----
if [ "$DURATION" -gt 0 ] 2>/dev/null; then
    _step "Chaos sync (duration: ${DURATION}s, seed: $SEED, sync: $SYNC_MODE, conflict: ${CONFLICT_RATE}%)"
else
    _step "Chaos sync (actions: $ACTIONS, seed: $SEED, sync: $SYNC_MODE, conflict: ${CONFLICT_RATE}%)"
fi

# ---- Main loop ----
START_TIME=$(date +%s)
MAX_ITERATIONS=$((ACTIONS * 5))
ITERATIONS=0

is_done() {
    if [ "$DURATION" -gt 0 ] 2>/dev/null; then
        local now
        now=$(date +%s)
        [ $(( now - START_TIME )) -ge "$DURATION" ]
    else
        [ "$CHAOS_ACTION_COUNT" -ge "$ACTIONS" ]
    fi
}

while ! is_done; do
    ITERATIONS=$((ITERATIONS + 1))
    if [ "$ITERATIONS" -ge "$MAX_ITERATIONS" ]; then
        _fail "Safety valve: $ITERATIONS iterations without completing $ACTIONS actions (completed: $CHAOS_ACTION_COUNT, skipped: $CHAOS_SKIPPED)"
        break
    fi
    # Burst mode: ~10% chance, single actor rapid-fires on one issue without sync
    if [ $(( RANDOM % 100 )) -lt 10 ]; then
        rand_choice a b; burst_actor="$_RAND_RESULT"
        exec_burst "$burst_actor"
    # Conflict round
    elif [ $(( RANDOM % 100 )) -lt "$CONFLICT_RATE" ] && [ "${#CHAOS_ISSUE_IDS[@]}" -gt 0 ]; then
        # Conflict round: both actors mutate without sync between
        select_issue not_deleted; local_id="$_CHAOS_SELECTED_ISSUE"
        if [ -n "$local_id" ]; then
            conflict_roll=$(( RANDOM % 100 ))
            if [ "$conflict_roll" -lt 30 ]; then
                # Field collision: both actors update same field on same issue
                exec_field_collision "$local_id"
            elif [ "$conflict_roll" -lt 45 ]; then
                # Delete-while-mutate: actor A deletes, actor B mutates unaware
                exec_delete_while_mutate "$local_id"
            else
                # Random action conflict (existing behavior)
                select_action; action_a="$_CHAOS_SELECTED_ACTION"
                select_action; action_b="$_CHAOS_SELECTED_ACTION"
                safe_exec "$action_a" "a"
                safe_exec "$action_b" "b"
            fi
        else
            # No valid target, fall through to normal round
            select_action; action="$_CHAOS_SELECTED_ACTION"
            rand_choice a b; actor="$_RAND_RESULT"
            safe_exec "$action" "$actor"
        fi
    else
        # Normal round: single actor, single action
        select_action; action="$_CHAOS_SELECTED_ACTION"
        rand_choice a b; actor="$_RAND_RESULT"
        safe_exec "$action" "$actor"
    fi

    # Sync check
    maybe_sync

    # Progress indicator every 10 actions
    if [ "$CHAOS_ACTION_COUNT" -gt 0 ] && [ $(( CHAOS_ACTION_COUNT % 10 )) -eq 0 ]; then
        if [ "$DURATION" -gt 0 ] 2>/dev/null; then
            local_elapsed=$(( $(date +%s) - START_TIME ))
            _ok "progress: $CHAOS_ACTION_COUNT actions, ${local_elapsed}s / ${DURATION}s"
        else
            _ok "progress: $CHAOS_ACTION_COUNT / $ACTIONS actions"
        fi
    fi
done

# ---- Final sync (double round-trip for convergence) ----
_step "Final sync"
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 1
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true

# ---- Convergence verification ----
DB_A="$CLIENT_A_DIR/.todos/issues.db"
DB_B="$CLIENT_B_DIR/.todos/issues.db"
verify_convergence "$DB_A" "$DB_B"

# ---- Summary stats ----
_step "Summary"
echo "  Total actions:          $CHAOS_ACTION_COUNT"
echo "  Total syncs:            $CHAOS_SYNC_COUNT"
echo "  Expected failures:      $CHAOS_EXPECTED_FAILURES"
echo "  Unexpected failures:    $CHAOS_UNEXPECTED_FAILURES"
echo "  Skipped (no target):    $CHAOS_SKIPPED"
echo "  Field collisions:       $CHAOS_FIELD_COLLISIONS"
echo "  Delete-mutate conflicts: $CHAOS_DELETE_MUTATE_CONFLICTS"
echo "  Bursts:                 $CHAOS_BURST_COUNT ($CHAOS_BURST_ACTIONS actions)"
echo "  Issues created:         ${#CHAOS_ISSUE_IDS[@]}"
echo "  Boards created:         ${#CHAOS_BOARD_NAMES[@]}"
echo "  Seed:                   $SEED (use --seed $SEED to reproduce)"

chaos_report

# ---- Final check ----
if [ "$CHAOS_UNEXPECTED_FAILURES" -gt 0 ]; then
    _fail "$CHAOS_UNEXPECTED_FAILURES unexpected failures"
fi

report
