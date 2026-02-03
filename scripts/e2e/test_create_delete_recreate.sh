#!/usr/bin/env bash
#
# Create-delete-recreate sync test: stress tombstone vs new-entity disambiguation.
#
# Tests rapid cycles where entities are created, deleted, and recreated with
# similar data. Verifies deleted entities remain deleted and new entities are
# correctly identified as distinct.
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"
source "$(dirname "$0")/chaos_lib.sh"

# ---- Defaults ----
SEED=$$
VERBOSE=false
# Number of create-delete-recreate cycles
CYCLES=15
# Extra chaos actions after cycles
EXTRA_CHAOS_ACTIONS=20
# Report options
JSON_REPORT=""
REPORT_FILE=""

# ---- Test-specific counters ----
CDR_CREATES=0
CDR_DELETES=0
CDR_RECREATES=0
CDR_TOMBSTONE_VERIFIED=0
CDR_NEW_ENTITY_VERIFIED=0
CDR_CONCURRENT_CONFLICTS=0
CDR_TOMBSTONE_VIOLATIONS=0  # deleted entity was resurrected incorrectly

# ---- Tracked entities for verification ----
# These arrays store IDs for verification at the end
CDR_DELETED_IDS=()        # IDs that MUST remain deleted
CDR_RECREATED_IDS=()      # IDs of recreated issues (should be distinct from deleted)
CDR_DELETED_TITLES=()     # Titles of deleted issues (for similarity testing)
CDR_RECREATED_TITLES=()   # Titles of recreated issues

# ---- Usage ----
usage() {
    cat <<EOF
Usage: bash scripts/e2e/test_create_delete_recreate.sh [OPTIONS]

Create-delete-recreate stress test: verifies tombstone disambiguation when
entities are rapidly created, deleted, and new entities with similar data
are created. Tests that deleted entities stay deleted and new entities are
correctly identified as new.

Options:
  --seed N                  RANDOM seed for reproducibility (default: \$\$)
  --verbose                 Detailed per-action output (default: false)
  --cycles N                Number of create-delete-recreate cycles (default: 15)
  --extra-chaos N           Extra random actions after cycles (default: 20)
  --json-report PATH        Write JSON summary to file
  --report-file PATH        Write text report to file
  -h, --help                Show this help

Examples:
  # Quick smoke test
  bash scripts/e2e/test_create_delete_recreate.sh --cycles 5 --verbose

  # Standard run
  bash scripts/e2e/test_create_delete_recreate.sh

  # Reproducible run
  bash scripts/e2e/test_create_delete_recreate.sh --seed 42

  # Stress test with many cycles
  bash scripts/e2e/test_create_delete_recreate.sh --cycles 30 --extra-chaos 50
EOF
}

# ---- Parse args ----
while [[ $# -gt 0 ]]; do
    case "$1" in
        --seed)             SEED="$2"; shift 2 ;;
        --verbose)          VERBOSE=true; shift ;;
        --cycles)           CYCLES="$2"; shift 2 ;;
        --extra-chaos)      EXTRA_CHAOS_ACTIONS="$2"; shift 2 ;;
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
CHAOS_SYNC_BATCH_MIN=2
CHAOS_SYNC_BATCH_MAX=5

# ---- Initial sync ----
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true

# ---- Config summary ----
_step "Create-delete-recreate test (seed: $SEED)"
echo "  Cycles:                 $CYCLES create-delete-recreate cycles"
echo "  Extra chaos actions:    $EXTRA_CHAOS_ACTIONS"
echo "  Tombstone test focus:   deleted entities must stay deleted"

# ============================================================
# Helper: Create an issue with specific title prefix for tracking
# ============================================================
_cdr_create() {
    local actor="$1"
    local title_prefix="$2"

    # Generate a unique title with edge-case potential
    rand_int 1 100
    local edge_chance="$_RAND_RESULT"
    local title=""
    if [ "$edge_chance" -le 15 ]; then
        # 15% chance of edge-case data in title
        rand_int 0 $(( ${#_CHAOS_EDGE_STRINGS[@]} - 1 ))
        local edge_str="${_CHAOS_EDGE_STRINGS[$_RAND_RESULT]}"
        # Ensure non-empty
        [ -z "$edge_str" ] && edge_str="edge-empty-$$"
        title="$title_prefix: $edge_str"
    else
        rand_title 100
        title="$title_prefix: $_RAND_STR"
    fi

    rand_choice task bug feature chore; local type_val="$_RAND_RESULT"
    rand_choice P0 P1 P2 P3; local priority="$_RAND_RESULT"
    rand_choice 1 2 3 5 8 13; local points="$_RAND_RESULT"

    local output rc=0
    output=$(chaos_run_td "$actor" create "$title" --type "$type_val" --priority "$priority" --points "$points" 2>&1) || rc=$?

    if [ "$rc" -ne 0 ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "create failed (expected): $output"
        return 1
    fi

    local issue_id
    issue_id=$(echo "$output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -z "$issue_id" ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "no issue ID in output: $output"
        return 1
    fi

    # Track in chaos state
    CHAOS_ISSUE_IDS+=("$issue_id")
    kv_set CHAOS_ISSUE_STATUS "$issue_id" "open"
    kv_set CHAOS_ISSUE_OWNER "$issue_id" "$actor"

    # Return the ID via global
    _CDR_LAST_CREATED_ID="$issue_id"
    _CDR_LAST_CREATED_TITLE="$title"

    [ "$CHAOS_VERBOSE" = "true" ] && _ok "create: $issue_id '$title' by $actor"
    CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))
    return 0
}

_CDR_LAST_CREATED_ID=""
_CDR_LAST_CREATED_TITLE=""

# ============================================================
# PHASE 1: Create-Delete-Recreate Cycles
# ============================================================
_step "Phase 1: Create-delete-recreate cycles ($CYCLES cycles)"
CHAOS_TIME_START=$(date +%s)

for cycle in $(seq 1 "$CYCLES"); do
    # Alternate primary actor each cycle
    if [ $(( cycle % 2 )) -eq 1 ]; then
        actor="a"
    else
        actor="b"
    fi

    _step "Cycle $cycle/$CYCLES (actor $actor)"

    # --- Step 1: Create original issue ---
    if _cdr_create "$actor" "CDR-cycle-$cycle-original"; then
        original_id="$_CDR_LAST_CREATED_ID"
        original_title="$_CDR_LAST_CREATED_TITLE"
        CDR_CREATES=$((CDR_CREATES + 1))

        # Sync to propagate create
        td_a sync >/dev/null 2>&1 || true
        td_b sync >/dev/null 2>&1 || true
        CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 2))

        # --- Step 2: Delete the issue ---
        delete_output=""
        delete_rc=0
        delete_output=$(chaos_run_td "$actor" delete "$original_id" 2>&1) || delete_rc=$?
        if [ "$delete_rc" -eq 0 ]; then
            CHAOS_DELETED_IDS+=("$original_id")
            CDR_DELETED_IDS+=("$original_id")
            CDR_DELETED_TITLES+=("$original_title")
            CDR_DELETES=$((CDR_DELETES + 1))
            CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))
            [ "$CHAOS_VERBOSE" = "true" ] && _ok "delete: $original_id by $actor"

            # Sync to propagate delete (tombstone)
            td_a sync >/dev/null 2>&1 || true
            td_b sync >/dev/null 2>&1 || true
            CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 2))

            # --- Step 3: Create NEW issue with similar title ---
            if _cdr_create "$actor" "CDR-cycle-$cycle-recreated"; then
                recreated_id="$_CDR_LAST_CREATED_ID"
                recreated_title="$_CDR_LAST_CREATED_TITLE"
                CDR_RECREATED_IDS+=("$recreated_id")
                CDR_RECREATED_TITLES+=("$recreated_title")
                CDR_RECREATES=$((CDR_RECREATES + 1))

                # Sync to propagate new entity
                td_a sync >/dev/null 2>&1 || true
                td_b sync >/dev/null 2>&1 || true
                CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 2))

                [ "$CHAOS_VERBOSE" = "true" ] && _ok "cycle $cycle: created $original_id -> deleted -> recreated $recreated_id"
            fi
        else
            [ "$CHAOS_VERBOSE" = "true" ] && _ok "delete failed: $delete_output"
        fi
    fi

    # Random sync during cycle
    maybe_sync
done

_ok "Phase 1 complete: $CDR_CREATES creates, $CDR_DELETES deletes, $CDR_RECREATES recreates"

# ============================================================
# PHASE 2: Concurrent Create-Delete Conflict
# Tests: Actor B creates while Actor A deletes same-titled issue
# ============================================================
_step "Phase 2: Concurrent conflict scenarios"

# Create a few issues for conflict setup
for i in $(seq 1 5); do
    if _cdr_create "a" "CDR-conflict-$i"; then
        original_id="$_CDR_LAST_CREATED_ID"
        original_title="$_CDR_LAST_CREATED_TITLE"

        # Sync A's create
        td_a sync >/dev/null 2>&1 || true

        # Now A deletes while B is "offline" (no sync yet)
        chaos_run_td "a" delete "$original_id" >/dev/null 2>&1 || true
        CHAOS_DELETED_IDS+=("$original_id")
        CDR_DELETED_IDS+=("$original_id")
        CDR_DELETES=$((CDR_DELETES + 1))
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

        # B (unaware of delete) creates issue with similar title
        if _cdr_create "b" "CDR-conflict-$i-concurrent"; then
            concurrent_id="$_CDR_LAST_CREATED_ID"
            CDR_RECREATED_IDS+=("$concurrent_id")
            CDR_CONCURRENT_CONFLICTS=$((CDR_CONCURRENT_CONFLICTS + 1))

            # Now sync both - tombstone should win for original, new entity should exist
            td_a sync >/dev/null 2>&1 || true
            td_b sync >/dev/null 2>&1 || true
            sleep 0.5
            td_b sync >/dev/null 2>&1 || true
            td_a sync >/dev/null 2>&1 || true
            CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 4))

            [ "$CHAOS_VERBOSE" = "true" ] && _ok "conflict: A deleted $original_id, B created $concurrent_id concurrently"
        fi
    fi
done

_ok "Phase 2 complete: $CDR_CONCURRENT_CONFLICTS concurrent conflict scenarios"

# ============================================================
# PHASE 3: Extra chaos with create/delete bias
# ============================================================
_step "Phase 3: Extra chaos ($EXTRA_CHAOS_ACTIONS actions)"

for _ in $(seq 1 "$EXTRA_CHAOS_ACTIONS"); do
    rand_choice a b; local_actor="$_RAND_RESULT"

    # Bias toward creates and deletes (60% vs 40% other)
    rand_int 1 100
    if [ "$_RAND_RESULT" -le 30 ]; then
        action="create"
    elif [ "$_RAND_RESULT" -le 50 ]; then
        action="delete"
    elif [ "$_RAND_RESULT" -le 60 ]; then
        action="restore"
    else
        select_action; action="$_CHAOS_SELECTED_ACTION"
    fi

    safe_exec "$action" "$local_actor"
    maybe_sync
done

_ok "Phase 3 complete: $EXTRA_CHAOS_ACTIONS extra actions"

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
# TOMBSTONE VERIFICATION
# Core test: deleted entities must remain deleted
# ============================================================
_step "Tombstone verification"

DB_A="$CLIENT_A_DIR/.todos/issues.db"
DB_B="$CLIENT_B_DIR/.todos/issues.db"

# Verify deleted entities are deleted on both clients
# Note: We check against CHAOS_DELETED_IDS (the chaos_lib state), not CDR_DELETED_IDS,
# because chaos_lib removes IDs from CHAOS_DELETED_IDS when they are restored.
# This way we only flag violations for issues that were NOT intentionally restored.
for deleted_id in "${CDR_DELETED_IDS[@]}"; do
    # Check if this issue was restored during Phase 3 chaos
    was_restored=false
    if ! is_chaos_deleted "$deleted_id"; then
        # Issue was restored via chaos actions - not a violation
        was_restored=true
    fi

    # Actually check if deleted_at is NOT null (meaning it's deleted)
    count_a=$(sqlite3 "$DB_A" "SELECT COUNT(*) FROM issues WHERE id='$deleted_id' AND deleted_at IS NULL;" 2>/dev/null || echo "1")
    count_b=$(sqlite3 "$DB_B" "SELECT COUNT(*) FROM issues WHERE id='$deleted_id' AND deleted_at IS NULL;" 2>/dev/null || echo "1")

    if [ "$was_restored" = "true" ]; then
        # Issue was intentionally restored - check it exists on both clients
        if [ "$count_a" -ge 1 ] && [ "$count_b" -ge 1 ]; then
            [ "$CHAOS_VERBOSE" = "true" ] && _ok "restored issue verified: $deleted_id exists on both clients"
        else
            [ "$CHAOS_VERBOSE" = "true" ] && _ok "restored issue sync pending: $deleted_id (A:$count_a B:$count_b)"
        fi
    elif [ "$count_a" -eq 0 ] && [ "$count_b" -eq 0 ]; then
        CDR_TOMBSTONE_VERIFIED=$((CDR_TOMBSTONE_VERIFIED + 1))
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "tombstone verified: $deleted_id stays deleted on both clients"
    else
        CDR_TOMBSTONE_VIOLATIONS=$((CDR_TOMBSTONE_VIOLATIONS + 1))
        _fail "TOMBSTONE VIOLATION: $deleted_id resurrected without restore (A:$count_a B:$count_b non-deleted)"
    fi
done

_ok "Tombstone verification: $CDR_TOMBSTONE_VERIFIED verified, $CDR_TOMBSTONE_VIOLATIONS violations"

# ============================================================
# NEW ENTITY VERIFICATION
# Recreated entities should be distinct and exist
# ============================================================
_step "New entity verification"

for recreated_id in "${CDR_RECREATED_IDS[@]}"; do
    # Check if this entity was deleted during Phase 3 chaos
    was_deleted_in_chaos=false
    if is_chaos_deleted "$recreated_id"; then
        was_deleted_in_chaos=true
    fi

    # Should exist and NOT be deleted (unless deleted during Phase 3)
    exists_a=$(sqlite3 "$DB_A" "SELECT COUNT(*) FROM issues WHERE id='$recreated_id' AND deleted_at IS NULL;" 2>/dev/null || echo "0")
    exists_b=$(sqlite3 "$DB_B" "SELECT COUNT(*) FROM issues WHERE id='$recreated_id' AND deleted_at IS NULL;" 2>/dev/null || echo "0")

    if [ "$was_deleted_in_chaos" = "true" ]; then
        # Entity was deleted during Phase 3 - not a test failure
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "new entity deleted in chaos: $recreated_id (expected)"
        CDR_NEW_ENTITY_VERIFIED=$((CDR_NEW_ENTITY_VERIFIED + 1))
    elif [ "$exists_a" -ge 1 ] && [ "$exists_b" -ge 1 ]; then
        CDR_NEW_ENTITY_VERIFIED=$((CDR_NEW_ENTITY_VERIFIED + 1))
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "new entity verified: $recreated_id exists on both clients"
    else
        _fail "NEW ENTITY MISSING: $recreated_id (A:$exists_a B:$exists_b)"
    fi
done

_ok "New entity verification: $CDR_NEW_ENTITY_VERIFIED verified"

# ============================================================
# CONVERGENCE VERIFICATION
# ============================================================
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
echo "  -- Create-Delete-Recreate Stats --"
echo "  Cycles completed:             $CYCLES"
echo "  CDR creates:                  $CDR_CREATES"
echo "  CDR deletes:                  $CDR_DELETES"
echo "  CDR recreates:                $CDR_RECREATES"
echo "  Concurrent conflicts:         $CDR_CONCURRENT_CONFLICTS"
echo "  Tombstones verified:          $CDR_TOMBSTONE_VERIFIED"
echo "  New entities verified:        $CDR_NEW_ENTITY_VERIFIED"
echo "  Tombstone violations:         $CDR_TOMBSTONE_VIOLATIONS"
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
  "test": "create_delete_recreate",
  "seed": $SEED,
  "pass": $([ "$CHAOS_UNEXPECTED_FAILURES" -eq 0 ] && [ "$CHAOS_CONVERGENCE_FAILED" -eq 0 ] && [ "$CDR_TOMBSTONE_VIOLATIONS" -eq 0 ] && echo "true" || echo "false"),
  "totals": {
    "actions": $CHAOS_ACTION_COUNT,
    "syncs": $CHAOS_SYNC_COUNT,
    "issues_created": ${#CHAOS_ISSUE_IDS[@]},
    "boards_created": ${#CHAOS_BOARD_NAMES[@]},
    "expected_failures": $CHAOS_EXPECTED_FAILURES,
    "unexpected_failures": $CHAOS_UNEXPECTED_FAILURES
  },
  "create_delete_recreate": {
    "cycles": $CYCLES,
    "creates": $CDR_CREATES,
    "deletes": $CDR_DELETES,
    "recreates": $CDR_RECREATES,
    "concurrent_conflicts": $CDR_CONCURRENT_CONFLICTS,
    "tombstones_verified": $CDR_TOMBSTONE_VERIFIED,
    "new_entities_verified": $CDR_NEW_ENTITY_VERIFIED,
    "tombstone_violations": $CDR_TOMBSTONE_VIOLATIONS
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

if [ "$CDR_TOMBSTONE_VIOLATIONS" -gt 0 ]; then
    _fail "$CDR_TOMBSTONE_VIOLATIONS tombstone violations (deleted entities resurrected)"
fi

report
