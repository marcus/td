#!/usr/bin/env bash
#
# Event ordering verification test: verifies causal ordering consistency
# in action_log across multiple clients after sync operations.
#
# Tests:
# - Updates should not appear before creates for same entity
# - Child creates should not appear before parent creates
# - Deletes should appear after creates
# - server_seq should be monotonically increasing (no duplicates)
# - Cross-database consistency for server_seq values
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"
source "$(dirname "$0")/chaos_lib.sh"

# ---- Defaults ----
SEED=$$
VERBOSE=false
ISSUES=30
EXTRA_ACTIONS=50
ACTORS=2
JSON_REPORT=""
REPORT_FILE=""

# ---- Usage ----
usage() {
    cat <<EOF
Usage: bash scripts/e2e/test_event_ordering.sh [OPTIONS]

Event ordering verification test: creates data with multiple actors,
syncs, then verifies causal ordering in action_log across all clients.

Options:
  --seed N           RANDOM seed for reproducibility (default: \$\$)
  --verbose          Detailed per-action output (default: false)
  --issues N         Number of issues to create (default: 30)
  --extra-actions N  Extra actions (updates, status changes, etc.) (default: 50)
  --actors N         Number of actors: 2 or 3 (default: 2)
  --json-report PATH Write JSON summary to file
  --report-file PATH Write text report to file
  -h, --help         Show this help

Examples:
  # Quick smoke test
  bash scripts/e2e/test_event_ordering.sh --issues 10 --extra-actions 20 --verbose

  # Standard run
  bash scripts/e2e/test_event_ordering.sh

  # Three-actor test
  bash scripts/e2e/test_event_ordering.sh --actors 3

  # Reproducible run
  bash scripts/e2e/test_event_ordering.sh --seed 42
EOF
}

# ---- Parse args ----
while [[ $# -gt 0 ]]; do
    case "$1" in
        --seed)           SEED="$2"; shift 2 ;;
        --verbose)        VERBOSE=true; shift ;;
        --issues)         ISSUES="$2"; shift 2 ;;
        --extra-actions)  EXTRA_ACTIONS="$2"; shift 2 ;;
        --actors)         ACTORS="$2"; shift 2 ;;
        --json-report)    JSON_REPORT="$2"; shift 2 ;;
        --report-file)    REPORT_FILE="$2"; shift 2 ;;
        -h|--help)        usage; exit 0 ;;
        *) echo "Unknown arg: $1" >&2; usage; exit 1 ;;
    esac
done

# ---- Setup ----
HARNESS_ACTORS="$ACTORS"
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
if [ "$ACTORS" -ge 3 ]; then
    td_c sync >/dev/null 2>&1 || true
fi

# ---- Config summary ----
_step "Event ordering test (seed: $SEED, issues: $ISSUES, extra: $EXTRA_ACTIONS, actors: $ACTORS)"

# ============================================================
# PHASE 1: Create hierarchical data
# ============================================================
_step "Phase 1: Creating issues with parent-child relationships"
CHAOS_TIME_START=$(date +%s)

# Create some root issues first
for i in $(seq 1 "$((ISSUES / 3))"); do
    rand_choice a b; local_actor="$_RAND_RESULT"
    safe_exec "create" "$local_actor"
    maybe_sync
done

_ok "Created ${#CHAOS_ISSUE_IDS[@]} root issues"

# Create child issues (referencing existing parents)
_step "Phase 1b: Creating child issues"
for i in $(seq 1 "$((ISSUES / 3))"); do
    rand_choice a b; local_actor="$_RAND_RESULT"
    # Force create_child action if we have issues
    if [ "${#CHAOS_ISSUE_IDS[@]}" -gt 0 ]; then
        safe_exec "create_child" "$local_actor"
    else
        safe_exec "create" "$local_actor"
    fi
    maybe_sync
done

_ok "Created ${#CHAOS_ISSUE_IDS[@]} total issues (including children)"

# Create remaining issues (mix of root and child)
_step "Phase 1c: Creating remaining issues"
for i in $(seq 1 "$((ISSUES / 3))"); do
    rand_choice a b; local_actor="$_RAND_RESULT"
    safe_exec "create" "$local_actor"
    maybe_sync
done

# ============================================================
# PHASE 2: Mutations (updates, comments, status changes, deletes)
# ============================================================
_step "Phase 2: Mutations ($EXTRA_ACTIONS actions)"

for _ in $(seq 1 "$EXTRA_ACTIONS"); do
    if [ "$ACTORS" -ge 3 ]; then
        rand_choice a b c; local_actor="$_RAND_RESULT"
    else
        rand_choice a b; local_actor="$_RAND_RESULT"
    fi
    select_action; action="$_CHAOS_SELECTED_ACTION"
    safe_exec "$action" "$local_actor"
    maybe_sync
done

_ok "Completed $EXTRA_ACTIONS mutation actions"

# ============================================================
# PHASE 3: Final sync (full round-robin for convergence)
# ============================================================
_step "Final sync (round-robin)"
if [ "$ACTORS" -ge 3 ]; then
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

# ============================================================
# PHASE 4: Event Ordering Verification
# ============================================================
DB_A="$CLIENT_A_DIR/.todos/issues.db"
DB_B="$CLIENT_B_DIR/.todos/issues.db"

# Verify ordering in each database independently
_ordering_failures_before=$HARNESS_FAILURES
verify_event_ordering "$DB_A"
_ordering_a_ok=$?

verify_event_ordering "$DB_B"
_ordering_b_ok=$?

if [ "$ACTORS" -ge 3 ]; then
    DB_C="$CLIENT_C_DIR/.todos/issues.db"
    verify_event_ordering "$DB_C"
    _ordering_c_ok=$?
fi

# Cross-database consistency check
verify_event_ordering_cross_db "$DB_A" "$DB_B"
_cross_ab_ok=$?

if [ "$ACTORS" -ge 3 ]; then
    verify_event_ordering_cross_db "$DB_A" "$DB_C"
    _cross_ac_ok=$?
fi

_ordering_failures_after=$HARNESS_FAILURES
ORDERING_FAILURES=$((_ordering_failures_after - _ordering_failures_before))

# ============================================================
# PHASE 5: Standard Convergence Verification (for completeness)
# ============================================================
_conv_failures_before=$HARNESS_FAILURES
verify_convergence "$DB_A" "$DB_B"
_conv_failures_after=$HARNESS_FAILURES
if [ "$_conv_failures_after" -eq "$_conv_failures_before" ]; then
    kv_set CHAOS_CONVERGENCE_RESULTS "A_vs_B" "pass"
    CHAOS_CONVERGENCE_PASSED=$((CHAOS_CONVERGENCE_PASSED + 1))
else
    kv_set CHAOS_CONVERGENCE_RESULTS "A_vs_B" "fail"
    CHAOS_CONVERGENCE_FAILED=$((CHAOS_CONVERGENCE_FAILED + 1))
fi

if [ "$ACTORS" -ge 3 ]; then
    _step "Convergence verification (A vs C)"
    _conv_failures_before=$HARNESS_FAILURES
    verify_convergence "$DB_A" "$DB_C"
    _conv_failures_after=$HARNESS_FAILURES
    if [ "$_conv_failures_after" -eq "$_conv_failures_before" ]; then
        kv_set CHAOS_CONVERGENCE_RESULTS "A_vs_C" "pass"
        CHAOS_CONVERGENCE_PASSED=$((CHAOS_CONVERGENCE_PASSED + 1))
    else
        kv_set CHAOS_CONVERGENCE_RESULTS "A_vs_C" "fail"
        CHAOS_CONVERGENCE_FAILED=$((CHAOS_CONVERGENCE_FAILED + 1))
    fi
fi

# ---- Event count verification ----
verify_event_counts "$DB_A" "$DB_B"

# ============================================================
# SUMMARY
# ============================================================
_step "Summary"
echo "  Actors:                  $ACTORS"
echo "  Total actions:           $CHAOS_ACTION_COUNT"
echo "  Total syncs:             $CHAOS_SYNC_COUNT"
echo "  Issues created:          ${#CHAOS_ISSUE_IDS[@]}"
echo "  Expected failures:       $CHAOS_EXPECTED_FAILURES"
echo "  Unexpected failures:     $CHAOS_UNEXPECTED_FAILURES"
echo ""
echo "  -- Event Ordering Stats --"
echo "  Ordering checks:         $CHAOS_EVENT_ORDERING_CHECKS"
echo "  Ordering violations:     $CHAOS_EVENT_ORDERING_VIOLATIONS"
echo "  Ordering test failures:  $ORDERING_FAILURES"
echo ""
echo "  Seed:                    $SEED (use --seed $SEED to reproduce)"

CHAOS_TIME_END=$(date +%s)

# ---- Detailed report ----
chaos_report "$REPORT_FILE"

# ---- JSON report for CI ----
if [ -n "$JSON_REPORT" ]; then
    _json_wall_clock=0
    if [ "$CHAOS_TIME_START" -gt 0 ]; then
        _json_wall_clock=$((CHAOS_TIME_END - CHAOS_TIME_START))
    fi

    cat > "$JSON_REPORT" <<ENDJSON
{
  "test": "event_ordering",
  "seed": $SEED,
  "pass": $([ "$CHAOS_UNEXPECTED_FAILURES" -eq 0 ] && [ "$CHAOS_EVENT_ORDERING_VIOLATIONS" -eq 0 ] && echo "true" || echo "false"),
  "totals": {
    "actions": $CHAOS_ACTION_COUNT,
    "syncs": $CHAOS_SYNC_COUNT,
    "issues_created": ${#CHAOS_ISSUE_IDS[@]},
    "expected_failures": $CHAOS_EXPECTED_FAILURES,
    "unexpected_failures": $CHAOS_UNEXPECTED_FAILURES
  },
  "event_ordering": {
    "checks": $CHAOS_EVENT_ORDERING_CHECKS,
    "violations": $CHAOS_EVENT_ORDERING_VIOLATIONS,
    "test_failures": $ORDERING_FAILURES
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

if [ "$CHAOS_EVENT_ORDERING_VIOLATIONS" -gt 0 ]; then
    _fail "$CHAOS_EVENT_ORDERING_VIOLATIONS event ordering violations"
fi

report
