#!/usr/bin/env bash
#
# Clock skew simulation test: verifies sync convergence when client clocks
# differ significantly. Tests LWW (last-write-wins) behavior under clock drift.
#
# This test directly manipulates timestamps in action_log entries to simulate
# clock skew rather than mocking system time.
#
# Key scenarios tested:
# - Forward skew: Client A's clock is 5 minutes ahead
# - Backward skew: Client A's clock is 5 minutes behind
# - Symmetric skew: A ahead, B behind
# - LWW resolution under clock skew
# - Soft-delete/restore ordering with skewed timestamps
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"
source "$(dirname "$0")/chaos_lib.sh"

# ---- Defaults ----
SEED=$$
VERBOSE=false
# Phase 1: Normal sync baseline
PHASE1_ACTIONS=15
# Phase 2: Clock skew scenarios
SKEW_FORWARD_MIN=5      # A's clock is 5 minutes ahead
SKEW_BACKWARD_MIN=5     # A's clock is 5 minutes behind
SKEW_SYMMETRIC_A_MIN=3  # A ahead by 3 minutes
SKEW_SYMMETRIC_B_MIN=3  # B behind by 3 minutes
# Report options
JSON_REPORT=""
REPORT_FILE=""

# ---- Clock skew counters ----
SKEW_FORWARD_TESTS=0
SKEW_BACKWARD_TESTS=0
SKEW_SYMMETRIC_TESTS=0
SKEW_LWW_AFFECTED=0
SKEW_SOFTDELETE_TESTS=0
SKEW_CONVERGENCE_PASS=0
SKEW_CONVERGENCE_FAIL=0

# ---- Usage ----
usage() {
    cat <<EOF
Usage: bash scripts/e2e/test_clock_skew.sh [OPTIONS]

Clock skew simulation: tests sync convergence when client clocks differ.
Manipulates action_log timestamps to simulate clock drift without mocking
system time.

Options:
  --seed N                  RANDOM seed for reproducibility (default: \$\$)
  --verbose                 Detailed per-action output (default: false)
  --phase1-actions N        Actions during baseline phase (default: 15)
  --forward-skew N          Minutes A is ahead (default: 5)
  --backward-skew N         Minutes A is behind (default: 5)
  --json-report PATH        Write JSON summary to file
  --report-file PATH        Write text report to file
  -h, --help                Show this help

Examples:
  # Quick smoke test
  bash scripts/e2e/test_clock_skew.sh --phase1-actions 5 --verbose

  # Standard run
  bash scripts/e2e/test_clock_skew.sh

  # Reproducible run
  bash scripts/e2e/test_clock_skew.sh --seed 42

  # Extreme skew test
  bash scripts/e2e/test_clock_skew.sh --forward-skew 30 --backward-skew 30
EOF
}

# ---- Parse args ----
while [[ $# -gt 0 ]]; do
    case "$1" in
        --seed)             SEED="$2"; shift 2 ;;
        --verbose)          VERBOSE=true; shift ;;
        --phase1-actions)   PHASE1_ACTIONS="$2"; shift 2 ;;
        --forward-skew)     SKEW_FORWARD_MIN="$2"; shift 2 ;;
        --backward-skew)    SKEW_BACKWARD_MIN="$2"; shift 2 ;;
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
CHAOS_SYNC_MODE="manual"  # We control syncs precisely
CHAOS_MID_TEST_CHECKS_ENABLED="false"  # Manual convergence checks

# ---- Initial sync ----
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true

# ---- Config summary ----
_step "Clock skew simulation test (seed: $SEED)"
echo "  Phase 1 (baseline):     $PHASE1_ACTIONS actions"
echo "  Forward skew:           +$SKEW_FORWARD_MIN min (A ahead)"
echo "  Backward skew:          -$SKEW_BACKWARD_MIN min (A behind)"
echo "  Symmetric skew:         A +$SKEW_SYMMETRIC_A_MIN / B -$SKEW_SYMMETRIC_B_MIN"

# ============================================================
# Helper: Offset timestamps in action_log for a client
# ============================================================
# Usage: offset_action_log_timestamps <db_path> <offset_seconds>
# Positive offset = clock ahead, Negative = clock behind
#
# Note: Go stores timestamps in format "2006-01-02 15:04:05.999999 -0700 MST m=+0.000"
# but can also be "2006-01-02 15:04:05 -0700 MST" (no microseconds) when time.Now()
# happens at an exact second boundary. We extract only the first 19 chars (YYYY-MM-DD HH:MM:SS)
# to handle both formats safely.
offset_action_log_timestamps() {
    local db="$1"
    local offset="$2"

    # Update unsynced action_log entries' timestamps
    # Extract datetime portion (first 19 chars: "YYYY-MM-DD HH:MM:SS") then apply offset
    if [ "$offset" -ge 0 ]; then
        sqlite3 "$db" "UPDATE action_log SET timestamp = datetime(substr(timestamp, 1, 19), '+$offset seconds') WHERE synced_at IS NULL AND timestamp IS NOT NULL;"
    else
        local abs_offset=${offset#-}
        sqlite3 "$db" "UPDATE action_log SET timestamp = datetime(substr(timestamp, 1, 19), '-$abs_offset seconds') WHERE synced_at IS NULL AND timestamp IS NOT NULL;"
    fi
}

# Usage: offset_issue_timestamps <db_path> <issue_id> <offset_seconds>
# Offsets both created_at and updated_at for an issue
# Note: Issue timestamps may also be in Go format, so extract only YYYY-MM-DD HH:MM:SS
offset_issue_timestamps() {
    local db="$1"
    local issue_id="$2"
    local offset="$3"

    if [ "$offset" -ge 0 ]; then
        sqlite3 "$db" "UPDATE issues SET created_at = datetime(substr(created_at, 1, 19), '+$offset seconds'), updated_at = datetime(substr(updated_at, 1, 19), '+$offset seconds') WHERE id = '$issue_id';"
    else
        local abs_offset=${offset#-}
        sqlite3 "$db" "UPDATE issues SET created_at = datetime(substr(created_at, 1, 19), '-$abs_offset seconds'), updated_at = datetime(substr(updated_at, 1, 19), '-$abs_offset seconds') WHERE id = '$issue_id';"
    fi
}

# Usage: get_unsynced_action_count <db_path>
get_unsynced_action_count() {
    local db="$1"
    sqlite3 "$db" "SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL;"
}

# ============================================================
# PHASE 1: Establish baseline with normal clocks
# ============================================================
_step "Phase 1: Baseline sync ($PHASE1_ACTIONS actions)"
CHAOS_TIME_START=$(date +%s)

for _ in $(seq 1 "$PHASE1_ACTIONS"); do
    rand_choice a b; local_actor="$_RAND_RESULT"

    # Weighted toward creates for more test material
    rand_int 1 10
    if [ "$_RAND_RESULT" -le 4 ]; then
        action="create"
    else
        select_action; action="$_CHAOS_SELECTED_ACTION"
    fi

    safe_exec "$action" "$local_actor"
done

# Sync to establish baseline
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 1
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true

_ok "Phase 1 complete: baseline established with ${#CHAOS_ISSUE_IDS[@]} issues"

DB_A="$CLIENT_A_DIR/.todos/issues.db"
DB_B="$CLIENT_B_DIR/.todos/issues.db"

# ============================================================
# PHASE 2: Forward clock skew (A's clock is ahead)
# ============================================================
_step "Phase 2: Forward clock skew (A +$SKEW_FORWARD_MIN min ahead)"

# A creates/updates with timestamps that appear to be from the future
_phase2_issues=()
for i in $(seq 1 3); do
    rand_title 60; _title="$_RAND_STR"
    output=$(td_a create "$_title" --type task --priority P1 2>&1) || true
    issue_id=$(echo "$output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -n "$issue_id" ]; then
        CHAOS_ISSUE_IDS+=("$issue_id")
        _phase2_issues+=("$issue_id")
        kv_set CHAOS_ISSUE_STATUS "$issue_id" "open"
        kv_set CHAOS_ISSUE_OWNER "$issue_id" "a"
        [ "$VERBOSE" = "true" ] && _ok "A created $issue_id (will have +${SKEW_FORWARD_MIN}m skew)"
    fi
done

# Offset A's unsynced actions to appear from the future
offset_seconds=$((SKEW_FORWARD_MIN * 60))
offset_action_log_timestamps "$DB_A" "$offset_seconds"
for _pid in "${_phase2_issues[@]}"; do
    offset_issue_timestamps "$DB_A" "$_pid" "$offset_seconds"
done
SKEW_FORWARD_TESTS=$((SKEW_FORWARD_TESTS + ${#_phase2_issues[@]}))

# B makes competing modifications to same issues (with normal clock)
for _pid in "${_phase2_issues[@]}"; do
    rand_title 60; _title="$_RAND_STR"
    # B updates the issue - but A's update will have a "future" timestamp
    td_b sync >/dev/null 2>&1 || true  # Get the issue first
    td_b update "$_pid" --title "$_title" >/dev/null 2>&1 || true
    [ "$VERBOSE" = "true" ] && _ok "B updated $_pid (normal clock)"
done

# Sync and verify
_step "Phase 2: Syncing with forward skew"
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 1
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true

# Check convergence
_conv_before=$HARNESS_FAILURES
verify_convergence_quick "$DB_A" "$DB_B" && _phase2_converged=true || _phase2_converged=false
if [ "$_phase2_converged" = "true" ]; then
    SKEW_CONVERGENCE_PASS=$((SKEW_CONVERGENCE_PASS + 1))
    _ok "Phase 2: Converged with forward skew"
else
    SKEW_CONVERGENCE_FAIL=$((SKEW_CONVERGENCE_FAIL + 1))
    _ok "Phase 2: Diverged with forward skew (may indicate LWW issue)"
fi

# ============================================================
# PHASE 3: Backward clock skew (A's clock is behind)
# ============================================================
_step "Phase 3: Backward clock skew (A -$SKEW_BACKWARD_MIN min behind)"

# A creates issues with timestamps that appear to be from the past
_phase3_issues=()
for i in $(seq 1 3); do
    rand_title 60; _title="$_RAND_STR"
    output=$(td_a create "$_title" --type bug --priority P2 2>&1) || true
    issue_id=$(echo "$output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -n "$issue_id" ]; then
        CHAOS_ISSUE_IDS+=("$issue_id")
        _phase3_issues+=("$issue_id")
        kv_set CHAOS_ISSUE_STATUS "$issue_id" "open"
        kv_set CHAOS_ISSUE_OWNER "$issue_id" "a"
        [ "$VERBOSE" = "true" ] && _ok "A created $issue_id (will have -${SKEW_BACKWARD_MIN}m skew)"
    fi
done

# Offset A's unsynced actions to appear from the past
offset_seconds=$((-SKEW_BACKWARD_MIN * 60))
offset_action_log_timestamps "$DB_A" "$offset_seconds"
for _pid in "${_phase3_issues[@]}"; do
    offset_issue_timestamps "$DB_A" "$_pid" "$offset_seconds"
done
SKEW_BACKWARD_TESTS=$((SKEW_BACKWARD_TESTS + ${#_phase3_issues[@]}))

# B updates these issues after syncing (will have "newer" timestamps)
td_b sync >/dev/null 2>&1 || true
for _pid in "${_phase3_issues[@]}"; do
    rand_title 60; _title="$_RAND_STR"
    td_b update "$_pid" --title "$_title" >/dev/null 2>&1 || true
    [ "$VERBOSE" = "true" ] && _ok "B updated $_pid (normal clock, should win LWW)"
done

# Sync and verify
_step "Phase 3: Syncing with backward skew"
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 1
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true

# Check convergence
verify_convergence_quick "$DB_A" "$DB_B" && _phase3_converged=true || _phase3_converged=false
if [ "$_phase3_converged" = "true" ]; then
    SKEW_CONVERGENCE_PASS=$((SKEW_CONVERGENCE_PASS + 1))
    _ok "Phase 3: Converged with backward skew"
else
    SKEW_CONVERGENCE_FAIL=$((SKEW_CONVERGENCE_FAIL + 1))
    _ok "Phase 3: Diverged with backward skew"
fi

# ============================================================
# PHASE 4: Symmetric clock skew (A ahead, B behind)
# ============================================================
_step "Phase 4: Symmetric skew (A +$SKEW_SYMMETRIC_A_MIN, B -$SKEW_SYMMETRIC_B_MIN min)"

# Create a shared issue first
rand_title 60; _title="$_RAND_STR"
output=$(td_a create "$_title" --type feature --priority P1 2>&1) || true
shared_issue=$(echo "$output" | grep -oE 'td-[0-9a-f]+' | head -n1)
if [ -n "$shared_issue" ]; then
    CHAOS_ISSUE_IDS+=("$shared_issue")
    kv_set CHAOS_ISSUE_STATUS "$shared_issue" "open"
    kv_set CHAOS_ISSUE_OWNER "$shared_issue" "a"
fi

# Sync so both clients have the issue
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true

# Both clients modify the same issue with symmetric clock skew
if [ -n "$shared_issue" ]; then
    # A updates (will be offset forward)
    td_a update "$shared_issue" --title "A-symmetric-update" >/dev/null 2>&1 || true
    offset_action_log_timestamps "$DB_A" $((SKEW_SYMMETRIC_A_MIN * 60))

    # B updates (will be offset backward)
    td_b update "$shared_issue" --title "B-symmetric-update" >/dev/null 2>&1 || true
    offset_action_log_timestamps "$DB_B" $((-SKEW_SYMMETRIC_B_MIN * 60))

    SKEW_SYMMETRIC_TESTS=$((SKEW_SYMMETRIC_TESTS + 1))
    [ "$VERBOSE" = "true" ] && _ok "Symmetric skew on $shared_issue: total drift = $((SKEW_SYMMETRIC_A_MIN + SKEW_SYMMETRIC_B_MIN))m"
fi

# Sync and verify
_step "Phase 4: Syncing with symmetric skew"
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 1
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true

verify_convergence_quick "$DB_A" "$DB_B" && _phase4_converged=true || _phase4_converged=false
if [ "$_phase4_converged" = "true" ]; then
    SKEW_CONVERGENCE_PASS=$((SKEW_CONVERGENCE_PASS + 1))
    _ok "Phase 4: Converged with symmetric skew"
else
    SKEW_CONVERGENCE_FAIL=$((SKEW_CONVERGENCE_FAIL + 1))
    _ok "Phase 4: Diverged with symmetric skew"
fi

# ============================================================
# PHASE 5: Soft-delete/restore with clock skew
# ============================================================
_step "Phase 5: Soft-delete/restore with clock skew"

# Create test issues
_phase5_issues=()
for i in $(seq 1 2); do
    rand_title 60; _title="$_RAND_STR"
    output=$(td_a create "$_title" --type task 2>&1) || true
    issue_id=$(echo "$output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -n "$issue_id" ]; then
        CHAOS_ISSUE_IDS+=("$issue_id")
        _phase5_issues+=("$issue_id")
        kv_set CHAOS_ISSUE_STATUS "$issue_id" "open"
    fi
done

td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true

# Test: A deletes with future timestamp, B restores with normal timestamp
if [ "${#_phase5_issues[@]}" -ge 1 ]; then
    _test_issue="${_phase5_issues[0]}"

    # A deletes (will have future timestamp)
    td_a delete "$_test_issue" >/dev/null 2>&1 || true
    CHAOS_DELETED_IDS+=("$_test_issue")
    offset_action_log_timestamps "$DB_A" $((SKEW_FORWARD_MIN * 60))

    # Push A's delete
    td_a sync >/dev/null 2>&1 || true
    td_b sync >/dev/null 2>&1 || true

    # B restores (normal timestamp - should this win or lose?)
    td_b restore "$_test_issue" >/dev/null 2>&1 || true

    SKEW_SOFTDELETE_TESTS=$((SKEW_SOFTDELETE_TESTS + 1))
    [ "$VERBOSE" = "true" ] && _ok "Delete/restore test on $_test_issue (A delete +${SKEW_FORWARD_MIN}m, B restore normal)"
fi

# Test: A deletes with past timestamp, B restores with normal timestamp
if [ "${#_phase5_issues[@]}" -ge 2 ]; then
    _test_issue2="${_phase5_issues[1]}"

    # A deletes (will have past timestamp)
    td_a delete "$_test_issue2" >/dev/null 2>&1 || true
    CHAOS_DELETED_IDS+=("$_test_issue2")
    offset_action_log_timestamps "$DB_A" $((-SKEW_BACKWARD_MIN * 60))

    # Push A's delete
    td_a sync >/dev/null 2>&1 || true
    td_b sync >/dev/null 2>&1 || true

    # B restores (normal timestamp - should win over past delete)
    td_b restore "$_test_issue2" >/dev/null 2>&1 || true

    SKEW_SOFTDELETE_TESTS=$((SKEW_SOFTDELETE_TESTS + 1))
    [ "$VERBOSE" = "true" ] && _ok "Delete/restore test on $_test_issue2 (A delete -${SKEW_BACKWARD_MIN}m, B restore normal)"
fi

# Final sync for soft-delete tests
_step "Phase 5: Final soft-delete sync"
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 1
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true

verify_convergence_quick "$DB_A" "$DB_B" && _phase5_converged=true || _phase5_converged=false
if [ "$_phase5_converged" = "true" ]; then
    SKEW_CONVERGENCE_PASS=$((SKEW_CONVERGENCE_PASS + 1))
    _ok "Phase 5: Converged with soft-delete skew"
else
    SKEW_CONVERGENCE_FAIL=$((SKEW_CONVERGENCE_FAIL + 1))
    _ok "Phase 5: Diverged with soft-delete skew"
fi

# ============================================================
# FINAL VERIFICATION
# ============================================================
_step "Final convergence verification"
verify_convergence "$DB_A" "$DB_B"

# Track overall convergence
_final_before=$HARNESS_FAILURES
verify_idempotency "$DB_A" "$DB_B"
verify_event_counts "$DB_A" "$DB_B"

# ============================================================
# SUMMARY STATS
# ============================================================
_step "Summary"
echo "  Total actions:                $CHAOS_ACTION_COUNT"
echo "  Total syncs:                  $CHAOS_SYNC_COUNT"
echo "  Issues created:               ${#CHAOS_ISSUE_IDS[@]}"
echo ""
echo "  -- Clock Skew Stats --"
echo "  Forward skew tests (A +$SKEW_FORWARD_MIN min):  $SKEW_FORWARD_TESTS"
echo "  Backward skew tests (A -$SKEW_BACKWARD_MIN min): $SKEW_BACKWARD_TESTS"
echo "  Symmetric skew tests:         $SKEW_SYMMETRIC_TESTS"
echo "  Soft-delete skew tests:       $SKEW_SOFTDELETE_TESTS"
echo "  Convergence passes:           $SKEW_CONVERGENCE_PASS"
echo "  Convergence failures:         $SKEW_CONVERGENCE_FAIL"
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
  "test": "clock_skew",
  "seed": $SEED,
  "pass": $([ "$HARNESS_FAILURES" -eq 0 ] && echo "true" || echo "false"),
  "totals": {
    "actions": $CHAOS_ACTION_COUNT,
    "syncs": $CHAOS_SYNC_COUNT,
    "issues_created": ${#CHAOS_ISSUE_IDS[@]},
    "expected_failures": $CHAOS_EXPECTED_FAILURES,
    "unexpected_failures": $CHAOS_UNEXPECTED_FAILURES
  },
  "clock_skew": {
    "forward_skew_minutes": $SKEW_FORWARD_MIN,
    "backward_skew_minutes": $SKEW_BACKWARD_MIN,
    "symmetric_skew_a_minutes": $SKEW_SYMMETRIC_A_MIN,
    "symmetric_skew_b_minutes": $SKEW_SYMMETRIC_B_MIN,
    "forward_skew_tests": $SKEW_FORWARD_TESTS,
    "backward_skew_tests": $SKEW_BACKWARD_TESTS,
    "symmetric_skew_tests": $SKEW_SYMMETRIC_TESTS,
    "softdelete_skew_tests": $SKEW_SOFTDELETE_TESTS,
    "convergence_passes": $SKEW_CONVERGENCE_PASS,
    "convergence_failures": $SKEW_CONVERGENCE_FAIL
  },
  "timing": {
    "wall_clock_seconds": $_json_wall_clock
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
