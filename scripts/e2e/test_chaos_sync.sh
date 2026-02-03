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
ACTORS=2
INJECT_FAILURES=false
MID_TEST_CHECKS=true
JSON_REPORT=""
REPORT_FILE=""
SOAK_MODE=false
SOAK_DURATION="${SOAK_DURATION:-30m}"
SOAK_COLLECT_INTERVAL=30  # seconds between metric collections

# ---- Usage ----
usage() {
    cat <<EOF
Usage: bash scripts/e2e/test_chaos_sync.sh [OPTIONS]

Randomized chaos sync test: multiple actors perform random mutations with
configurable sync timing, conflict injection, and convergence verification.

Options:
  --actions N        Total actions to perform (default: 100)
  --duration N       Seconds to run; overrides --actions when >0 (default: 0)
  --soak [DURATION]  Enable soak/endurance mode; collects metrics every 30s
                     Duration format: 30m, 1h, 5m (default: 30m, or SOAK_DURATION env)
  --seed N           RANDOM seed for reproducibility (default: \$\$)
  --sync-mode MODE   Sync strategy: adaptive, aggressive, random (default: adaptive)
  --verbose          Detailed per-action output (default: false)
  --conflict-rate N  Percentage of actions with simultaneous mutations (default: 20)
  --batch-min N      Min actions between syncs (default: 3)
  --batch-max N      Max actions between syncs (default: 10)
  --actors N         Number of actors: 2 or 3 (default: 2)
  --inject-failures  Inject partial sync failures (~7% of syncs) (default: false)
  --mid-test-checks  Enable periodic convergence checks during test (default: true)
  --no-mid-test-checks  Disable periodic convergence checks
  --json-report PATH Write JSON summary to file (for CI integration)
  --report-file PATH Write text report to file instead of stdout
  -h, --help         Show this help

Soak Mode:
  Soak mode runs for extended periods to detect resource leaks. Metrics collected:
  - Memory (Go runtime): alloc_mb, sys_mb, num_gc, goroutines
  - File descriptors: server process FD count
  - SQLite: WAL file sizes
  - Disk: .todos directory growth

  Configure thresholds via env vars:
    SOAK_MEM_GROWTH_PERCENT=50    Max memory growth %
    SOAK_MAX_FD_COUNT=100         Max file descriptors
    SOAK_MAX_WAL_MB=50            Max WAL size in MB
    SOAK_MAX_GOROUTINES=50        Max goroutine count
    SOAK_MAX_DIR_GROWTH_MB=100    Max directory growth in MB

Examples:
  # Quick smoke test
  bash scripts/e2e/test_chaos_sync.sh --actions 20 --sync-mode aggressive --verbose

  # Standard run
  bash scripts/e2e/test_chaos_sync.sh --actions 100

  # Three-actor fan-out test
  bash scripts/e2e/test_chaos_sync.sh --actions 50 --actors 3

  # Stress test with conflicts
  bash scripts/e2e/test_chaos_sync.sh --actions 500 --conflict-rate 30

  # Reproducible run
  bash scripts/e2e/test_chaos_sync.sh --seed 42 --actions 50

  # With sync failure injection
  bash scripts/e2e/test_chaos_sync.sh --actions 50 --inject-failures

  # Time-based
  bash scripts/e2e/test_chaos_sync.sh --duration 60

  # Soak test (30 minutes, detect resource leaks)
  bash scripts/e2e/test_chaos_sync.sh --soak 30m --actions 500

  # Quick soak test for CI (5 minutes)
  bash scripts/e2e/test_chaos_sync.sh --soak 5m --actions 100
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
        --actors)         ACTORS="$2"; shift 2 ;;
        --inject-failures) INJECT_FAILURES=true; shift ;;
        --mid-test-checks) MID_TEST_CHECKS=true; shift ;;
        --no-mid-test-checks) MID_TEST_CHECKS=false; shift ;;
        --json-report)    JSON_REPORT="$2"; shift 2 ;;
        --report-file)    REPORT_FILE="$2"; shift 2 ;;
        --soak)
            SOAK_MODE=true
            # Check if next arg is a duration (not a flag)
            if [[ $# -gt 1 && ! "$2" =~ ^- ]]; then
                SOAK_DURATION="$2"
                shift 2
            else
                shift
            fi
            ;;
        -h|--help)        usage; exit 0 ;;
        *) echo "Unknown arg: $1" >&2; usage; exit 1 ;;
    esac
done

# ---- Soak mode setup ----
# Convert soak duration to seconds and set DURATION
if [ "$SOAK_MODE" = "true" ]; then
    # Parse duration (supports 30m, 1h, 5m, 60 for seconds)
    _soak_parse_duration() {
        local dur="$1"
        if [[ "$dur" =~ ^([0-9]+)m$ ]]; then
            echo $(( ${BASH_REMATCH[1]} * 60 ))
        elif [[ "$dur" =~ ^([0-9]+)h$ ]]; then
            echo $(( ${BASH_REMATCH[1]} * 3600 ))
        elif [[ "$dur" =~ ^([0-9]+)s?$ ]]; then
            echo "${BASH_REMATCH[1]}"
        else
            echo "60"  # default 1 minute if unparseable
        fi
    }
    DURATION=$(_soak_parse_duration "$SOAK_DURATION")
fi

# ---- Setup ----
HARNESS_ACTORS="$ACTORS"
export HARNESS_ACTORS
setup

# Configure mid-test convergence checks
CHAOS_MID_TEST_CHECKS_ENABLED="$MID_TEST_CHECKS"

# ---- Seed RANDOM for reproducibility ----
RANDOM=$SEED

# ---- Configure chaos_lib ----
CHAOS_SYNC_MODE="$SYNC_MODE"
CHAOS_SYNC_BATCH_MIN="$BATCH_MIN"
CHAOS_SYNC_BATCH_MAX="$BATCH_MAX"
CHAOS_VERBOSE="$VERBOSE"
CHAOS_INJECT_FAILURES="$INJECT_FAILURES"

# ---- Soak metrics initialization ----
if [ "$SOAK_MODE" = "true" ]; then
    init_soak_metrics
    _step "Soak mode enabled (duration: ${SOAK_DURATION}, metrics: $SOAK_METRICS_FILE)"
fi
_SOAK_LAST_COLLECT=0

# ---- Initial sync ----
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
if [ "$ACTORS" -ge 3 ]; then
    td_c sync >/dev/null 2>&1 || true
fi

# ---- Config summary ----
_chaos_inject_label=""
if [ "$INJECT_FAILURES" = "true" ]; then
    _chaos_inject_label=", inject-failures: on"
fi
_soak_label=""
if [ "$SOAK_MODE" = "true" ]; then
    _soak_label=", soak: on"
fi
if [ "$DURATION" -gt 0 ] 2>/dev/null; then
    _step "Chaos sync (duration: ${DURATION}s, seed: $SEED, sync: $SYNC_MODE, conflict: ${CONFLICT_RATE}%, actors: $ACTORS${_chaos_inject_label}${_soak_label})"
else
    _step "Chaos sync (actions: $ACTIONS, seed: $SEED, sync: $SYNC_MODE, conflict: ${CONFLICT_RATE}%, actors: $ACTORS${_chaos_inject_label}${_soak_label})"
fi

# ---- Main loop ----
CHAOS_TIME_START=$(date +%s)
START_TIME=$CHAOS_TIME_START
# Safety valve: in duration mode, don't cap iterations (use duration as limit)
if [ "$DURATION" -gt 0 ] 2>/dev/null; then
    # Estimate ~10 actions/second max, with 5x buffer
    MAX_ITERATIONS=$((DURATION * 50))
else
    MAX_ITERATIONS=$((ACTIONS * 5))
fi
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
    # Helper: pick a random actor
    if [ "$ACTORS" -ge 3 ]; then
        rand_choice a b c; _chaos_actor="$_RAND_RESULT"
    else
        rand_choice a b; _chaos_actor="$_RAND_RESULT"
    fi

    # Burst mode: ~10% chance, single actor rapid-fires on one issue without sync
    if [ $(( RANDOM % 100 )) -lt 10 ]; then
        exec_burst "$_chaos_actor"
    # Conflict round
    elif [ $(( RANDOM % 100 )) -lt "$CONFLICT_RATE" ] && [ "${#CHAOS_ISSUE_IDS[@]}" -gt 0 ]; then
        # Conflict round: pick a pair (or triple) of actors to conflict
        if [ "$ACTORS" -ge 3 ]; then
            rand_int 1 4
            case "$_RAND_RESULT" in
                1) _conf_a="a"; _conf_b="b" ;;
                2) _conf_a="a"; _conf_b="c" ;;
                3) _conf_a="b"; _conf_b="c" ;;
                4) _conf_a="a"; _conf_b="b"; _conf_c="c" ;;  # triple
            esac
        else
            _conf_a="a"; _conf_b="b"; _conf_c=""
        fi
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
                # Random action conflict
                select_action; action_a="$_CHAOS_SELECTED_ACTION"
                select_action; action_b="$_CHAOS_SELECTED_ACTION"
                safe_exec "$action_a" "$_conf_a"
                safe_exec "$action_b" "$_conf_b"
                if [ -n "${_conf_c:-}" ]; then
                    select_action; action_c="$_CHAOS_SELECTED_ACTION"
                    safe_exec "$action_c" "$_conf_c"
                fi
            fi
            _conf_c=""
        else
            # No valid target, fall through to normal round
            select_action; action="$_CHAOS_SELECTED_ACTION"
            safe_exec "$action" "$_chaos_actor"
        fi
    else
        # Normal round: single actor, single action
        select_action; action="$_CHAOS_SELECTED_ACTION"
        safe_exec "$action" "$_chaos_actor"
    fi

    # Sync check
    maybe_sync

    # Mid-test convergence check (after sync, at configured interval)
    maybe_check_convergence

    # Soak metrics collection (every SOAK_COLLECT_INTERVAL seconds)
    if [ "$SOAK_MODE" = "true" ]; then
        local_now=$(date +%s)
        if [ $(( local_now - _SOAK_LAST_COLLECT )) -ge "$SOAK_COLLECT_INTERVAL" ]; then
            collect_soak_metrics
            _SOAK_LAST_COLLECT=$local_now
            if [ "$VERBOSE" = "true" ]; then
                _ok "soak: collected metrics at ${local_now}s"
            fi
        fi
    fi

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

# Final soak metrics collection
if [ "$SOAK_MODE" = "true" ]; then
    collect_soak_metrics
fi

# ---- Final sync (full round-robin for convergence) ----
_step "Final sync"
if [ "$ACTORS" -ge 3 ]; then
    # Full round-robin: A B C A B C
    td_a sync >/dev/null 2>&1 || true
    td_b sync >/dev/null 2>&1 || true
    td_c sync >/dev/null 2>&1 || true
    sleep 1
    td_a sync >/dev/null 2>&1 || true
    td_b sync >/dev/null 2>&1 || true
    td_c sync >/dev/null 2>&1 || true
else
    td_a sync >/dev/null 2>&1 || true
    td_b sync >/dev/null 2>&1 || true
    sleep 1
    td_b sync >/dev/null 2>&1 || true
    td_a sync >/dev/null 2>&1 || true
fi

# ---- Convergence verification ----
DB_A="$CLIENT_A_DIR/.todos/issues.db"
DB_B="$CLIENT_B_DIR/.todos/issues.db"

# Track convergence results: capture failures before/after each verify
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

if [ "$ACTORS" -ge 3 ]; then
    DB_C="$CLIENT_C_DIR/.todos/issues.db"

    _step "Convergence verification (A vs C)"
    _conv_failures_before=$HARNESS_FAILURES
    verify_convergence "$DB_A" "$DB_C"
    _conv_failures_after=$HARNESS_FAILURES
    if [ "$_conv_failures_after" -eq "$_conv_failures_before" ]; then
        kv_set CHAOS_CONVERGENCE_RESULTS "A_vs_C" "pass"
        CHAOS_CONVERGENCE_PASSED=$(( CHAOS_CONVERGENCE_PASSED + 1 ))
    else
        kv_set CHAOS_CONVERGENCE_RESULTS "A_vs_C" "fail"
        CHAOS_CONVERGENCE_FAILED=$(( CHAOS_CONVERGENCE_FAILED + 1 ))
    fi

    _step "Convergence verification (B vs C)"
    _conv_failures_before=$HARNESS_FAILURES
    verify_convergence "$DB_B" "$DB_C"
    _conv_failures_after=$HARNESS_FAILURES
    if [ "$_conv_failures_after" -eq "$_conv_failures_before" ]; then
        kv_set CHAOS_CONVERGENCE_RESULTS "B_vs_C" "pass"
        CHAOS_CONVERGENCE_PASSED=$(( CHAOS_CONVERGENCE_PASSED + 1 ))
    else
        kv_set CHAOS_CONVERGENCE_RESULTS "B_vs_C" "fail"
        CHAOS_CONVERGENCE_FAILED=$(( CHAOS_CONVERGENCE_FAILED + 1 ))
    fi
fi

# ---- Idempotency verification ----
verify_idempotency "$DB_A" "$DB_B"

# ---- Event count verification ----
verify_event_counts "$DB_A" "$DB_B"
if [ "$ACTORS" -ge 3 ]; then
    _step "Event count verification (A vs C)"
    verify_event_counts "$DB_A" "$DB_C"
fi

# ---- Soak metrics verification ----
SOAK_THRESHOLD_FAILED=0
if [ "$SOAK_MODE" = "true" ] && [ -f "$SOAK_METRICS_FILE" ]; then
    if ! verify_soak_metrics "$SOAK_METRICS_FILE"; then
        SOAK_THRESHOLD_FAILED=1
    fi

    _step "Soak metrics summary"
    soak_metrics_summary "$SOAK_METRICS_FILE"
    echo "  Metrics file:     $SOAK_METRICS_FILE"
fi

# ---- Summary stats ----
_step "Summary"
echo "  Actors:                 $ACTORS"
echo "  Total actions:          $CHAOS_ACTION_COUNT"
echo "  Total syncs:            $CHAOS_SYNC_COUNT"
echo "  Expected failures:      $CHAOS_EXPECTED_FAILURES"
echo "  Unexpected failures:    $CHAOS_UNEXPECTED_FAILURES"
echo "  Skipped (no target):    $CHAOS_SKIPPED"
echo "  Field collisions:       $CHAOS_FIELD_COLLISIONS"
echo "  Delete-mutate conflicts: $CHAOS_DELETE_MUTATE_CONFLICTS"
echo "  Bursts:                 $CHAOS_BURST_COUNT ($CHAOS_BURST_ACTIONS actions)"
echo "  Injected sync failures: $CHAOS_INJECTED_FAILURES"
echo "  Edge-case data used:    $CHAOS_EDGE_DATA_USED"
echo "  Issues created:         ${#CHAOS_ISSUE_IDS[@]}"
echo "  Boards created:         ${#CHAOS_BOARD_NAMES[@]}"
echo "  Seed:                   $SEED (use --seed $SEED to reproduce)"
if [ "$SOAK_MODE" = "true" ]; then
    echo "  Soak duration:          ${SOAK_DURATION} (${DURATION}s)"
    echo "  Soak thresholds:        $SOAK_VERIFY_PASSED passed, $SOAK_VERIFY_FAILED failed"
fi

CHAOS_TIME_END=$(date +%s)

chaos_report "$REPORT_FILE"

# JSON report for CI
if [ -n "$JSON_REPORT" ]; then
    chaos_report_json "$JSON_REPORT"
fi

# ---- Final check ----
if [ "$CHAOS_UNEXPECTED_FAILURES" -gt 0 ]; then
    _fail "$CHAOS_UNEXPECTED_FAILURES unexpected failures"
fi

if [ "$SOAK_THRESHOLD_FAILED" -gt 0 ]; then
    _fail "soak test: resource thresholds exceeded"
fi

report
