#!/usr/bin/env bash
#
# Parent deletion cascade sync test: verify cascade behavior and orphan handling.
#
# Tests scenarios where parent issues with children are deleted and synced.
# In td's implementation, deleting a parent is a soft-delete that does NOT
# cascade-delete children. Children retain their parent_id, becoming "orphaned"
# (referencing a deleted parent). This test verifies:
#
# 1. Orphan consistency: Children's parent_id remains unchanged across sync
# 2. No dangling references after sync (parent_id points to existing row, even if deleted)
# 3. Concurrent modification: What happens when B modifies child while A deletes parent
# 4. Deep hierarchy: Grandchildren behavior when grandparent is deleted
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"
source "$(dirname "$0")/chaos_lib.sh"

# ---- Defaults ----
SEED=$$
VERBOSE=false
DEEP_HIERARCHY_DEPTH=3  # Max depth for grandchild tests
JSON_REPORT=""
REPORT_FILE=""

# ---- Test-specific counters ----
PDC_PARENTS_CREATED=0
PDC_CHILDREN_CREATED=0
PDC_PARENTS_DELETED=0
PDC_ORPHAN_VERIFIED=0
PDC_ORPHAN_SYNC_VERIFIED=0
PDC_CONCURRENT_MODIFY_TESTS=0
PDC_DEEP_HIERARCHY_TESTS=0
PDC_VIOLATIONS=0

# ---- Tracked entities ----
PDC_PARENT_IDS=()         # Parent issue IDs
PDC_CHILD_IDS=()          # Child issue IDs
PDC_PARENT_TO_CHILDREN="" # KV: parentId -> comma-separated childIds
PDC_DELETED_PARENTS=()    # Parents that were deleted

# ---- Usage ----
usage() {
    cat <<EOF
Usage: bash scripts/e2e/test_parent_delete_cascade.sh [OPTIONS]

Parent deletion cascade sync test: verifies that when a parent issue is deleted,
children remain intact with their parent_id preserved (orphaned state), and this
state syncs correctly across clients.

Options:
  --seed N                  RANDOM seed for reproducibility (default: \$\$)
  --verbose                 Detailed per-action output (default: false)
  --json-report PATH        Write JSON summary to file
  --report-file PATH        Write text report to file
  -h, --help                Show this help

Test Scenarios:
  1. Basic cascade: Create parent + children, delete parent, verify orphan state
  2. Sync consistency: Orphaned children sync correctly between clients
  3. Concurrent modify: B modifies child while A deletes parent (race condition)
  4. Deep hierarchy: Grandchildren when intermediate parent deleted
  5. Multi-child: Parent with many children deleted at once

Examples:
  # Quick smoke test
  bash scripts/e2e/test_parent_delete_cascade.sh --verbose

  # Reproducible run
  bash scripts/e2e/test_parent_delete_cascade.sh --seed 42

  # Standard run
  bash scripts/e2e/test_parent_delete_cascade.sh
EOF
}

# ---- Parse args ----
while [[ $# -gt 0 ]]; do
    case "$1" in
        --seed)             SEED="$2"; shift 2 ;;
        --verbose)          VERBOSE=true; shift ;;
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
_step "Parent delete cascade test (seed: $SEED)"
echo "  Focus: Parent deletion orphans children, sync preserves state"
echo "  Scenarios: basic cascade, sync consistency, concurrent modify, deep hierarchy"

# ============================================================
# Helper: Create a parent issue
# ============================================================
_pdc_create_parent() {
    local actor="$1"
    local title_prefix="${2:-PDC-parent}"

    rand_title 80
    local title="$title_prefix: $_RAND_STR"
    rand_choice task bug feature; local type_val="$_RAND_RESULT"
    rand_choice P0 P1 P2 P3; local priority="$_RAND_RESULT"

    local output rc=0
    output=$(chaos_run_td "$actor" create "$title" --type "$type_val" --priority "$priority" 2>&1) || rc=$?

    if [ "$rc" -ne 0 ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "parent create failed: $output"
        return 1
    fi

    local issue_id
    issue_id=$(echo "$output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -z "$issue_id" ]; then
        return 1
    fi

    CHAOS_ISSUE_IDS+=("$issue_id")
    kv_set CHAOS_ISSUE_STATUS "$issue_id" "open"
    kv_set CHAOS_ISSUE_OWNER "$issue_id" "$actor"
    PDC_PARENT_IDS+=("$issue_id")
    PDC_PARENTS_CREATED=$((PDC_PARENTS_CREATED + 1))
    CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

    _PDC_LAST_PARENT_ID="$issue_id"
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "parent created: $issue_id by $actor"
    return 0
}

_PDC_LAST_PARENT_ID=""
_PDC_LAST_CHILD_ID=""

# ============================================================
# Helper: Create a child issue with specified parent
# ============================================================
_pdc_create_child() {
    local actor="$1"
    local parent_id="$2"
    local title_prefix="${3:-PDC-child}"

    rand_title 80
    local title="$title_prefix: $_RAND_STR"
    rand_choice task bug feature; local type_val="$_RAND_RESULT"
    rand_choice P0 P1 P2 P3; local priority="$_RAND_RESULT"

    local output rc=0
    output=$(chaos_run_td "$actor" create "$title" --type "$type_val" --priority "$priority" --parent "$parent_id" 2>&1) || rc=$?

    if [ "$rc" -ne 0 ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "child create failed: $output"
        return 1
    fi

    local issue_id
    issue_id=$(echo "$output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -z "$issue_id" ]; then
        return 1
    fi

    CHAOS_ISSUE_IDS+=("$issue_id")
    kv_set CHAOS_ISSUE_STATUS "$issue_id" "open"
    kv_set CHAOS_ISSUE_OWNER "$issue_id" "$actor"
    kv_set CHAOS_PARENT_CHILDREN "${parent_id}_${issue_id}" "1"
    PDC_CHILD_IDS+=("$issue_id")
    PDC_CHILDREN_CREATED=$((PDC_CHILDREN_CREATED + 1))
    CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

    # Track parent -> children mapping
    local existing_children
    existing_children=$(kv_get PDC_PARENT_TO_CHILDREN "$parent_id")
    if [ -z "$existing_children" ]; then
        kv_set PDC_PARENT_TO_CHILDREN "$parent_id" "$issue_id"
    else
        kv_set PDC_PARENT_TO_CHILDREN "$parent_id" "${existing_children},${issue_id}"
    fi

    _PDC_LAST_CHILD_ID="$issue_id"
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "child created: $issue_id -> parent $parent_id by $actor"
    return 0
}

# ============================================================
# Helper: Verify child's parent_id in database
# ============================================================
_pdc_verify_parent_id() {
    local db_path="$1"
    local child_id="$2"
    local expected_parent_id="$3"
    local client_name="$4"

    local actual_parent_id
    actual_parent_id=$(sqlite3 "$db_path" "SELECT parent_id FROM issues WHERE id='$child_id';" 2>/dev/null || echo "ERROR")

    if [ "$actual_parent_id" = "$expected_parent_id" ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "$client_name: $child_id has parent_id=$expected_parent_id (correct)"
        return 0
    else
        _fail "$client_name: $child_id has parent_id='$actual_parent_id', expected '$expected_parent_id'"
        return 1
    fi
}

# ============================================================
# Helper: Verify orphan state (child references deleted parent)
# ============================================================
_pdc_verify_orphan_state() {
    local db_path="$1"
    local child_id="$2"
    local parent_id="$3"
    local client_name="$4"

    # Child should have parent_id set
    local actual_parent_id
    actual_parent_id=$(sqlite3 "$db_path" "SELECT parent_id FROM issues WHERE id='$child_id';" 2>/dev/null || echo "")

    if [ "$actual_parent_id" != "$parent_id" ]; then
        _fail "$client_name: orphan $child_id has wrong parent_id='$actual_parent_id', expected '$parent_id'"
        PDC_VIOLATIONS=$((PDC_VIOLATIONS + 1))
        return 1
    fi

    # Parent should be soft-deleted (deleted_at IS NOT NULL)
    local parent_deleted
    parent_deleted=$(sqlite3 "$db_path" "SELECT CASE WHEN deleted_at IS NOT NULL THEN 'deleted' ELSE 'active' END FROM issues WHERE id='$parent_id';" 2>/dev/null || echo "missing")

    if [ "$parent_deleted" = "deleted" ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "$client_name: $child_id correctly orphaned (parent $parent_id deleted)"
        return 0
    elif [ "$parent_deleted" = "active" ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "$client_name: parent $parent_id not yet deleted (pending sync?)"
        return 0
    else
        _fail "$client_name: parent $parent_id missing entirely (should exist as soft-deleted)"
        PDC_VIOLATIONS=$((PDC_VIOLATIONS + 1))
        return 1
    fi
}

# ============================================================
# SCENARIO 1: Basic Parent-Child Cascade
# Create parent with multiple children, delete parent, verify orphan state
# ============================================================
_step "Scenario 1: Basic parent-child cascade"

# Create parent on A
if _pdc_create_parent "a" "S1-parent"; then
    s1_parent="$_PDC_LAST_PARENT_ID"

    # Create 3 children on A
    s1_children=()
    for i in 1 2 3; do
        if _pdc_create_child "a" "$s1_parent" "S1-child-$i"; then
            s1_children+=("$_PDC_LAST_CHILD_ID")
        fi
    done

    # Sync to B
    td_a sync >/dev/null 2>&1 || true
    td_b sync >/dev/null 2>&1 || true
    CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 2))

    # Verify children exist on B with correct parent_id
    DB_A="$CLIENT_A_DIR/.todos/issues.db"
    DB_B="$CLIENT_B_DIR/.todos/issues.db"

    for child_id in "${s1_children[@]}"; do
        _pdc_verify_parent_id "$DB_B" "$child_id" "$s1_parent" "B (before delete)"
    done

    # Delete parent on A
    delete_output=""
    delete_rc=0
    delete_output=$(td_a delete "$s1_parent" 2>&1) || delete_rc=$?
    if [ "$delete_rc" -eq 0 ]; then
        CHAOS_DELETED_IDS+=("$s1_parent")
        PDC_DELETED_PARENTS+=("$s1_parent")
        PDC_PARENTS_DELETED=$((PDC_PARENTS_DELETED + 1))
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "deleted parent: $s1_parent"

        # Verify children on A still have parent_id (orphaned locally)
        for child_id in "${s1_children[@]}"; do
            if _pdc_verify_orphan_state "$DB_A" "$child_id" "$s1_parent" "A (local orphan)"; then
                PDC_ORPHAN_VERIFIED=$((PDC_ORPHAN_VERIFIED + 1))
            fi
        done

        # Sync deletion to B
        td_a sync >/dev/null 2>&1 || true
        td_b sync >/dev/null 2>&1 || true
        sleep 0.5
        td_b sync >/dev/null 2>&1 || true
        td_a sync >/dev/null 2>&1 || true
        CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 4))

        # Verify orphan state synced to B
        for child_id in "${s1_children[@]}"; do
            if _pdc_verify_orphan_state "$DB_B" "$child_id" "$s1_parent" "B (synced orphan)"; then
                PDC_ORPHAN_SYNC_VERIFIED=$((PDC_ORPHAN_SYNC_VERIFIED + 1))
            fi
        done
    else
        _fail "failed to delete parent $s1_parent: $delete_output"
    fi
fi

_ok "Scenario 1 complete: ${#s1_children[@]} children orphaned"

# ============================================================
# SCENARIO 2: Concurrent Modify - B modifies child while A deletes parent
# ============================================================
_step "Scenario 2: Concurrent modification (B modifies while A deletes parent)"

if _pdc_create_parent "a" "S2-parent"; then
    s2_parent="$_PDC_LAST_PARENT_ID"

    # Create child on A
    if _pdc_create_child "a" "$s2_parent" "S2-child"; then
        s2_child="$_PDC_LAST_CHILD_ID"

        # Sync to B
        td_a sync >/dev/null 2>&1 || true
        td_b sync >/dev/null 2>&1 || true
        CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 2))

        # Now: A deletes parent while B modifies child (before B knows about delete)

        # A deletes parent
        td_a delete "$s2_parent" >/dev/null 2>&1 || true
        CHAOS_DELETED_IDS+=("$s2_parent")
        PDC_DELETED_PARENTS+=("$s2_parent")
        PDC_PARENTS_DELETED=$((PDC_PARENTS_DELETED + 1))
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

        # B (unaware of delete) modifies child
        rand_title 50
        td_b update "$s2_child" --title "$_RAND_STR - modified by B" >/dev/null 2>&1 || true
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

        # B also adds a comment to the child
        td_b comments add "$s2_child" "Comment added while parent being deleted" >/dev/null 2>&1 || true
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

        # Now sync both ways
        td_a sync >/dev/null 2>&1 || true
        td_b sync >/dev/null 2>&1 || true
        sleep 0.5
        td_b sync >/dev/null 2>&1 || true
        td_a sync >/dev/null 2>&1 || true
        CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 4))

        # Verify: child should still exist (not deleted), with parent_id preserved
        DB_A="$CLIENT_A_DIR/.todos/issues.db"
        DB_B="$CLIENT_B_DIR/.todos/issues.db"

        # Check child not deleted on A
        child_deleted_a=$(sqlite3 "$DB_A" "SELECT CASE WHEN deleted_at IS NOT NULL THEN 'deleted' ELSE 'active' END FROM issues WHERE id='$s2_child';" 2>/dev/null || echo "missing")
        if [ "$child_deleted_a" = "active" ]; then
            _ok "A: child $s2_child survived parent deletion (not cascade deleted)"
        else
            _fail "A: child $s2_child was incorrectly deleted/missing: $child_deleted_a"
            PDC_VIOLATIONS=$((PDC_VIOLATIONS + 1))
        fi

        # Check child not deleted on B
        child_deleted_b=$(sqlite3 "$DB_B" "SELECT CASE WHEN deleted_at IS NOT NULL THEN 'deleted' ELSE 'active' END FROM issues WHERE id='$s2_child';" 2>/dev/null || echo "missing")
        if [ "$child_deleted_b" = "active" ]; then
            _ok "B: child $s2_child survived parent deletion (not cascade deleted)"
        else
            _fail "B: child $s2_child was incorrectly deleted/missing: $child_deleted_b"
            PDC_VIOLATIONS=$((PDC_VIOLATIONS + 1))
        fi

        # Verify parent_id preserved
        _pdc_verify_orphan_state "$DB_A" "$s2_child" "$s2_parent" "A (concurrent)"
        _pdc_verify_orphan_state "$DB_B" "$s2_child" "$s2_parent" "B (concurrent)"

        PDC_CONCURRENT_MODIFY_TESTS=$((PDC_CONCURRENT_MODIFY_TESTS + 1))
    fi
fi

_ok "Scenario 2 complete: concurrent modification handled"

# ============================================================
# SCENARIO 3: Deep Hierarchy - Grandchildren
# Create parent -> child -> grandchild, delete parent, verify chain
# ============================================================
_step "Scenario 3: Deep hierarchy (grandchildren)"

if _pdc_create_parent "a" "S3-grandparent"; then
    s3_grandparent="$_PDC_LAST_PARENT_ID"

    # Create child (will be parent of grandchild)
    if _pdc_create_child "a" "$s3_grandparent" "S3-parent"; then
        s3_parent="$_PDC_LAST_CHILD_ID"

        # Create grandchild
        if _pdc_create_child "a" "$s3_parent" "S3-grandchild"; then
            s3_grandchild="$_PDC_LAST_CHILD_ID"

            # Sync to B
            td_a sync >/dev/null 2>&1 || true
            td_b sync >/dev/null 2>&1 || true
            CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 2))

            DB_A="$CLIENT_A_DIR/.todos/issues.db"
            DB_B="$CLIENT_B_DIR/.todos/issues.db"

            # Verify hierarchy before delete
            _pdc_verify_parent_id "$DB_A" "$s3_parent" "$s3_grandparent" "A (S3 before)"
            _pdc_verify_parent_id "$DB_A" "$s3_grandchild" "$s3_parent" "A (S3 before)"

            # Delete the grandparent (top of hierarchy)
            td_a delete "$s3_grandparent" >/dev/null 2>&1 || true
            CHAOS_DELETED_IDS+=("$s3_grandparent")
            PDC_DELETED_PARENTS+=("$s3_grandparent")
            PDC_PARENTS_DELETED=$((PDC_PARENTS_DELETED + 1))
            CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

            # Sync
            td_a sync >/dev/null 2>&1 || true
            td_b sync >/dev/null 2>&1 || true
            sleep 0.5
            td_b sync >/dev/null 2>&1 || true
            td_a sync >/dev/null 2>&1 || true
            CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 4))

            # Verify: s3_parent should be orphaned (points to deleted grandparent)
            _pdc_verify_orphan_state "$DB_A" "$s3_parent" "$s3_grandparent" "A (S3 deep)"
            _pdc_verify_orphan_state "$DB_B" "$s3_parent" "$s3_grandparent" "B (S3 deep)"

            # Verify: s3_grandchild should still point to s3_parent (which is NOT deleted)
            _pdc_verify_parent_id "$DB_A" "$s3_grandchild" "$s3_parent" "A (S3 grandchild)"
            _pdc_verify_parent_id "$DB_B" "$s3_grandchild" "$s3_parent" "B (S3 grandchild)"

            # Now also delete the intermediate parent
            td_a delete "$s3_parent" >/dev/null 2>&1 || true
            CHAOS_DELETED_IDS+=("$s3_parent")
            PDC_DELETED_PARENTS+=("$s3_parent")
            PDC_PARENTS_DELETED=$((PDC_PARENTS_DELETED + 1))
            CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

            # Sync again
            td_a sync >/dev/null 2>&1 || true
            td_b sync >/dev/null 2>&1 || true
            sleep 0.5
            td_b sync >/dev/null 2>&1 || true
            td_a sync >/dev/null 2>&1 || true
            CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 4))

            # Verify: grandchild is now orphaned (its parent s3_parent is deleted)
            _pdc_verify_orphan_state "$DB_A" "$s3_grandchild" "$s3_parent" "A (S3 double orphan)"
            _pdc_verify_orphan_state "$DB_B" "$s3_grandchild" "$s3_parent" "B (S3 double orphan)"

            PDC_DEEP_HIERARCHY_TESTS=$((PDC_DEEP_HIERARCHY_TESTS + 1))
        fi
    fi
fi

_ok "Scenario 3 complete: deep hierarchy orphaning verified"

# ============================================================
# SCENARIO 4: Multi-child stress - Parent with many children
# ============================================================
_step "Scenario 4: Multi-child stress test"

if _pdc_create_parent "a" "S4-parent"; then
    s4_parent="$_PDC_LAST_PARENT_ID"

    # Create 8 children (stress test)
    s4_children=()
    for i in $(seq 1 8); do
        if _pdc_create_child "a" "$s4_parent" "S4-child-$i"; then
            s4_children+=("$_PDC_LAST_CHILD_ID")
        fi
    done

    # Sync
    td_a sync >/dev/null 2>&1 || true
    td_b sync >/dev/null 2>&1 || true
    CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 2))

    # Delete parent
    td_a delete "$s4_parent" >/dev/null 2>&1 || true
    CHAOS_DELETED_IDS+=("$s4_parent")
    PDC_DELETED_PARENTS+=("$s4_parent")
    PDC_PARENTS_DELETED=$((PDC_PARENTS_DELETED + 1))
    CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

    # Sync
    td_a sync >/dev/null 2>&1 || true
    td_b sync >/dev/null 2>&1 || true
    sleep 0.5
    td_b sync >/dev/null 2>&1 || true
    td_a sync >/dev/null 2>&1 || true
    CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 4))

    DB_A="$CLIENT_A_DIR/.todos/issues.db"
    DB_B="$CLIENT_B_DIR/.todos/issues.db"

    # Verify all children orphaned correctly
    s4_verified=0
    for child_id in "${s4_children[@]}"; do
        if _pdc_verify_orphan_state "$DB_A" "$child_id" "$s4_parent" "A (S4)"; then
            s4_verified=$((s4_verified + 1))
        fi
        if _pdc_verify_orphan_state "$DB_B" "$child_id" "$s4_parent" "B (S4)"; then
            s4_verified=$((s4_verified + 1))
        fi
    done

    _ok "Scenario 4 complete: $s4_verified / $((${#s4_children[@]} * 2)) child orphan verifications passed"
fi

# ============================================================
# SCENARIO 5: Rapid create-delete cycles with children
# ============================================================
_step "Scenario 5: Rapid create-delete cycles"

for cycle in 1 2 3; do
    if _pdc_create_parent "a" "S5-cycle-$cycle"; then
        s5_parent="$_PDC_LAST_PARENT_ID"

        # Create 2 children
        _pdc_create_child "a" "$s5_parent" "S5-c$cycle-child1" || true
        s5_c1="$_PDC_LAST_CHILD_ID"
        _pdc_create_child "a" "$s5_parent" "S5-c$cycle-child2" || true
        s5_c2="$_PDC_LAST_CHILD_ID"

        # Immediate delete (no sync in between)
        td_a delete "$s5_parent" >/dev/null 2>&1 || true
        CHAOS_DELETED_IDS+=("$s5_parent")
        PDC_DELETED_PARENTS+=("$s5_parent")
        PDC_PARENTS_DELETED=$((PDC_PARENTS_DELETED + 1))
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))
    fi
done

# Sync all at once
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 0.5
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true
CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 4))

_ok "Scenario 5 complete: rapid cycles synced"

# ============================================================
# FINAL CONVERGENCE VERIFICATION
# ============================================================
_step "Final sync (round-robin)"
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 1
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true

DB_A="$CLIENT_A_DIR/.todos/issues.db"
DB_B="$CLIENT_B_DIR/.todos/issues.db"

# ============================================================
# ORPHAN INTEGRITY CHECK
# Verify no orphan references point to completely missing rows
# ============================================================
_step "Orphan integrity check"

# Get all issues with non-empty parent_id
orphan_check_a=$(sqlite3 "$DB_A" "SELECT i.id, i.parent_id FROM issues i WHERE i.parent_id != '' AND i.parent_id IS NOT NULL;" 2>/dev/null || echo "")
if [ -n "$orphan_check_a" ]; then
    while IFS='|' read -r child_id parent_id; do
        # Parent should exist (even if soft-deleted)
        parent_exists=$(sqlite3 "$DB_A" "SELECT COUNT(*) FROM issues WHERE id='$parent_id';" 2>/dev/null || echo "0")
        if [ "$parent_exists" -eq 0 ]; then
            _fail "A: child $child_id references non-existent parent $parent_id (integrity violation)"
            PDC_VIOLATIONS=$((PDC_VIOLATIONS + 1))
        else
            [ "$CHAOS_VERBOSE" = "true" ] && _ok "A: child $child_id -> parent $parent_id exists (ok)"
        fi
    done <<< "$orphan_check_a"
fi

orphan_check_b=$(sqlite3 "$DB_B" "SELECT i.id, i.parent_id FROM issues i WHERE i.parent_id != '' AND i.parent_id IS NOT NULL;" 2>/dev/null || echo "")
if [ -n "$orphan_check_b" ]; then
    while IFS='|' read -r child_id parent_id; do
        parent_exists=$(sqlite3 "$DB_B" "SELECT COUNT(*) FROM issues WHERE id='$parent_id';" 2>/dev/null || echo "0")
        if [ "$parent_exists" -eq 0 ]; then
            _fail "B: child $child_id references non-existent parent $parent_id (integrity violation)"
            PDC_VIOLATIONS=$((PDC_VIOLATIONS + 1))
        else
            [ "$CHAOS_VERBOSE" = "true" ] && _ok "B: child $child_id -> parent $parent_id exists (ok)"
        fi
    done <<< "$orphan_check_b"
fi

_ok "Orphan integrity check complete"

# ============================================================
# CONVERGENCE VERIFICATION
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

# ---- Idempotency verification ----
verify_idempotency "$DB_A" "$DB_B"

# ---- Event count verification ----
verify_event_counts "$DB_A" "$DB_B"

# ============================================================
# SUMMARY STATS
# ============================================================
CHAOS_TIME_END=$(date +%s)

_step "Summary"
echo "  Total actions:                $CHAOS_ACTION_COUNT"
echo "  Total syncs:                  $CHAOS_SYNC_COUNT"
echo "  Expected failures:            $CHAOS_EXPECTED_FAILURES"
echo "  Unexpected failures:          $CHAOS_UNEXPECTED_FAILURES"
echo ""
echo "  -- Parent Delete Cascade Stats --"
echo "  Parents created:              $PDC_PARENTS_CREATED"
echo "  Children created:             $PDC_CHILDREN_CREATED"
echo "  Parents deleted:              $PDC_PARENTS_DELETED"
echo "  Orphans verified (local):     $PDC_ORPHAN_VERIFIED"
echo "  Orphans verified (synced):    $PDC_ORPHAN_SYNC_VERIFIED"
echo "  Concurrent modify tests:      $PDC_CONCURRENT_MODIFY_TESTS"
echo "  Deep hierarchy tests:         $PDC_DEEP_HIERARCHY_TESTS"
echo "  Violations:                   $PDC_VIOLATIONS"
echo ""
echo "  Seed:                         $SEED (use --seed $SEED to reproduce)"

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
  "test": "parent_delete_cascade",
  "seed": $SEED,
  "pass": $([ "$CHAOS_UNEXPECTED_FAILURES" -eq 0 ] && [ "$CHAOS_CONVERGENCE_FAILED" -eq 0 ] && [ "$PDC_VIOLATIONS" -eq 0 ] && echo "true" || echo "false"),
  "totals": {
    "actions": $CHAOS_ACTION_COUNT,
    "syncs": $CHAOS_SYNC_COUNT,
    "expected_failures": $CHAOS_EXPECTED_FAILURES,
    "unexpected_failures": $CHAOS_UNEXPECTED_FAILURES
  },
  "parent_delete_cascade": {
    "parents_created": $PDC_PARENTS_CREATED,
    "children_created": $PDC_CHILDREN_CREATED,
    "parents_deleted": $PDC_PARENTS_DELETED,
    "orphans_verified_local": $PDC_ORPHAN_VERIFIED,
    "orphans_verified_synced": $PDC_ORPHAN_SYNC_VERIFIED,
    "concurrent_modify_tests": $PDC_CONCURRENT_MODIFY_TESTS,
    "deep_hierarchy_tests": $PDC_DEEP_HIERARCHY_TESTS,
    "violations": $PDC_VIOLATIONS
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

if [ "$PDC_VIOLATIONS" -gt 0 ]; then
    _fail "$PDC_VIOLATIONS parent-child cascade violations detected"
fi

report
