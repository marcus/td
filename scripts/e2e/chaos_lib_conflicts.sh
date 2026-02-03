#!/usr/bin/env bash
#
# Chaos sync e2e library - Conflicts module.
# Contains: Field collision, delete-while-mutate, burst patterns, safe exec, sync scheduling.
# Requires: chaos_lib_executors.sh
#

# Source executors module (which sources core)
CHAOS_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$CHAOS_LIB_DIR/chaos_lib_executors.sh"

# ============================================================
# 7b. Field Collision (same-field conflict)
# ============================================================
# Both actors update the exact same field on the same issue simultaneously,
# stressing field-level merge. Values are guaranteed different for scalar fields.

exec_field_collision() {
    local id="$1"

    rand_int 1 7
    local field_num="$_RAND_RESULT"
    local field_name

    case "$field_num" in
        1)  field_name="title"
            rand_title 50; local title_a="$_RAND_STR"
            rand_title 50; local title_b="$_RAND_STR"
            chaos_run_td "a" update "$id" --title "$title_a" >/dev/null 2>&1 || true
            chaos_run_td "b" update "$id" --title "$title_b" >/dev/null 2>&1 || true
            ;;
        2)  field_name="description"
            rand_description 1; local desc_a="$_RAND_STR"
            rand_description 1; local desc_b="$_RAND_STR"
            chaos_run_td "a" update "$id" --description "$desc_a" >/dev/null 2>&1 || true
            chaos_run_td "b" update "$id" --description "$desc_b" >/dev/null 2>&1 || true
            ;;
        3)  field_name="priority"
            rand_choice P0 P1 P2 P3; local prio_a="$_RAND_RESULT"
            rand_choice P0 P1 P2 P3; local prio_b="$_RAND_RESULT"
            while [ "$prio_a" = "$prio_b" ]; do rand_choice P0 P1 P2 P3; prio_b="$_RAND_RESULT"; done
            chaos_run_td "a" update "$id" --priority "$prio_a" >/dev/null 2>&1 || true
            chaos_run_td "b" update "$id" --priority "$prio_b" >/dev/null 2>&1 || true
            ;;
        4)  field_name="points"
            rand_int 0 13; local pts_a="$_RAND_RESULT"
            rand_int 0 13; local pts_b="$_RAND_RESULT"
            while [ "$pts_a" = "$pts_b" ]; do rand_int 0 13; pts_b="$_RAND_RESULT"; done
            chaos_run_td "a" update "$id" --points "$pts_a" >/dev/null 2>&1 || true
            chaos_run_td "b" update "$id" --points "$pts_b" >/dev/null 2>&1 || true
            ;;
        5)  field_name="labels"
            rand_labels; local lbl_a="$_RAND_STR"
            rand_labels; local lbl_b="$_RAND_STR"
            chaos_run_td "a" update "$id" --labels "$lbl_a" >/dev/null 2>&1 || true
            chaos_run_td "b" update "$id" --labels "$lbl_b" >/dev/null 2>&1 || true
            ;;
        6)  field_name="sprint"
            rand_choice "sprint-1" "sprint-2" "sprint-3" "sprint-4"; local spr_a="$_RAND_RESULT"
            rand_choice "sprint-1" "sprint-2" "sprint-3" "sprint-4"; local spr_b="$_RAND_RESULT"
            while [ "$spr_a" = "$spr_b" ]; do rand_choice "sprint-1" "sprint-2" "sprint-3" "sprint-4"; spr_b="$_RAND_RESULT"; done
            chaos_run_td "a" update "$id" --sprint "$spr_a" >/dev/null 2>&1 || true
            chaos_run_td "b" update "$id" --sprint "$spr_b" >/dev/null 2>&1 || true
            ;;
        7)  field_name="acceptance"
            rand_acceptance; local acc_a="$_RAND_STR"
            rand_acceptance; local acc_b="$_RAND_STR"
            chaos_run_td "a" update "$id" --acceptance "$acc_a" >/dev/null 2>&1 || true
            chaos_run_td "b" update "$id" --acceptance "$acc_b" >/dev/null 2>&1 || true
            ;;
    esac

    CHAOS_FIELD_COLLISIONS=$((CHAOS_FIELD_COLLISIONS + 1))
    CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 2))
    CHAOS_ACTIONS_SINCE_SYNC=$((CHAOS_ACTIONS_SINCE_SYNC + 2))
    _chaos_inc_action_ok "field_collision"
    _chaos_inc_action_ok "field_collision"
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "field_collision: $field_name on $id"
    return 0
}

# ============================================================
# 7c. Delete-While-Mutate Conflict
# ============================================================
# Actor A deletes an issue; Actor B (unaware) performs 2-3 mutations on it.
# Classic tombstone-vs-mutation distributed systems edge case.

exec_delete_while_mutate() {
    local id="$1"

    # Actor A deletes the issue
    local output rc=0
    output=$(chaos_run_td "a" delete "$id" 2>&1) || rc=$?
    if [ $rc -ne 0 ]; then
        # Issue might already be deleted or invalid
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    fi

    # Track deletion in chaos state
    CHAOS_DELETED_IDS+=("$id")
    kv_set CHAOS_ISSUE_STATUS "$id" "deleted"

    # Actor B (unaware of deletion) performs 2-3 mutations
    rand_int 2 3; local mutation_count="$_RAND_RESULT"
    local mutations_done=0

    for _ in $(seq 1 "$mutation_count"); do
        rand_int 1 4
        case "$_RAND_RESULT" in
            1) # Comment
               rand_comment 5 20
               chaos_run_td "b" comments add "$id" "$_RAND_STR" >/dev/null 2>&1 || true ;;
            2) # Update field
               rand_title 50
               chaos_run_td "b" update "$id" --title "$_RAND_STR" >/dev/null 2>&1 || true ;;
            3) # Log progress
               rand_comment 5 15
               chaos_run_td "b" log --issue "$id" "$_RAND_STR" >/dev/null 2>&1 || true ;;
            4) # Try status change
               chaos_run_td "b" start "$id" --reason "chaos" >/dev/null 2>&1 || true ;;
        esac
        mutations_done=$((mutations_done + 1))
    done

    CHAOS_DELETE_MUTATE_CONFLICTS=$((CHAOS_DELETE_MUTATE_CONFLICTS + 1))
    CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1 + mutations_done))
    CHAOS_ACTIONS_SINCE_SYNC=$((CHAOS_ACTIONS_SINCE_SYNC + 1 + mutations_done))
    local _dwm_i=0
    while [ "$_dwm_i" -lt "$(( 1 + mutations_done ))" ]; do
        _chaos_inc_action_ok "delete_while_mutate"
        _dwm_i=$(( _dwm_i + 1 ))
    done
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "delete_while_mutate: $id ($mutations_done mutations after delete)"
    return 0
}

# ============================================================
# 7d. Burst-Without-Sync Pattern
# ============================================================
# One actor performs a rapid burst of 5-8 sequential mutations on a single
# issue without any intermediate syncs. Stresses event log ordering and
# replay of sequential local changes (create -> update -> start -> comment
# -> update priority -> review, all before syncing).

exec_burst() {
    local actor="$1"

    # Pick or create an issue
    local id=""
    if [ ${#CHAOS_ISSUE_IDS[@]} -gt 0 ] && [ $(( RANDOM % 2 )) -eq 0 ]; then
        select_issue not_deleted; id="$_CHAOS_SELECTED_ISSUE"
    fi

    if [ -z "$id" ]; then
        # Create a new issue for the burst
        rand_title; local title="$_RAND_STR"
        rand_choice task bug feature; local type_val="$_RAND_RESULT"
        local output rc=0
        output=$(chaos_run_td "$actor" create "$title" --type "$type_val" 2>&1) || rc=$?
        if [ "$rc" -ne 0 ]; then
            [ "$CHAOS_VERBOSE" = "true" ] && _ok "burst: create failed, skipping"
            return 0
        fi
        id=$(echo "$output" | grep -oE 'td-[a-f0-9]+' | head -1)
        if [ -z "$id" ]; then
            return 0
        fi
        CHAOS_ISSUE_IDS+=("$id")
        kv_set CHAOS_ISSUE_STATUS "$id" "open"
        kv_set CHAOS_ISSUE_OWNER "$id" "$actor"
    fi

    # Perform 5-8 sequential mutations without sync
    rand_int 5 8; local burst_size="$_RAND_RESULT"
    local actions_done=0

    for _ in $(seq 1 "$burst_size"); do
        rand_int 1 7
        case "$_RAND_RESULT" in
            1) # Update title
               rand_title 80
               chaos_run_td "$actor" update "$id" --title "$_RAND_STR" >/dev/null 2>&1 || true ;;
            2) # Update priority
               rand_choice P0 P1 P2 P3
               chaos_run_td "$actor" update "$id" --priority "$_RAND_RESULT" >/dev/null 2>&1 || true ;;
            3) # Add comment
               rand_comment 5 20
               chaos_run_td "$actor" comments add "$id" "$_RAND_STR" >/dev/null 2>&1 || true ;;
            4) # Log progress
               rand_comment 5 15
               chaos_run_td "$actor" log --issue "$id" "$_RAND_STR" >/dev/null 2>&1 || true ;;
            5) # Update labels
               rand_labels
               chaos_run_td "$actor" update "$id" --labels "$_RAND_STR" >/dev/null 2>&1 || true ;;
            6) # Update points
               rand_int 0 13
               chaos_run_td "$actor" update "$id" --points "$_RAND_RESULT" >/dev/null 2>&1 || true ;;
            7) # Update sprint
               rand_choice "sprint-1" "sprint-2" "sprint-3"
               chaos_run_td "$actor" update "$id" --sprint "$_RAND_RESULT" >/dev/null 2>&1 || true ;;
        esac
        actions_done=$((actions_done + 1))
    done

    CHAOS_BURST_COUNT=$((CHAOS_BURST_COUNT + 1))
    CHAOS_BURST_ACTIONS=$((CHAOS_BURST_ACTIONS + actions_done))
    CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + actions_done))
    CHAOS_ACTIONS_SINCE_SYNC=$((CHAOS_ACTIONS_SINCE_SYNC + actions_done))
    local _burst_i=0
    while [ "$_burst_i" -lt "$actions_done" ]; do
        _chaos_inc_action_ok "burst"
        _burst_i=$(( _burst_i + 1 ))
    done
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "burst: $actions_done actions on $id by $actor"
    return 0
}

# ============================================================
# 8. Safe Execution Wrapper
# ============================================================

safe_exec() {
    local action="$1"
    local actor="$2"
    local func="exec_${action}"

    # Call function directly — NOT in a subshell $() — so state changes persist
    _chaos_timer_start
    _CHAOS_CURRENT_ACTION="$action"
    local _expfail_before=$CHAOS_EXPECTED_FAILURES
    local rc=0
    $func "$actor" || rc=$?
    _chaos_timer_stop_mutate

    # Track expected failures that occurred inside the executor
    local _expfail_delta=$(( CHAOS_EXPECTED_FAILURES - _expfail_before ))
    if [ "$_expfail_delta" -gt 0 ]; then
        local _ef_i=0
        while [ "$_ef_i" -lt "$_expfail_delta" ]; do
            _chaos_inc_action_expfail "$action"
            _ef_i=$(( _ef_i + 1 ))
        done
    fi

    if [ "$rc" -eq 0 ]; then
        # Success (includes expected failures already counted by executors)
        CHAOS_ACTION_COUNT=$(( CHAOS_ACTION_COUNT + 1 ))
        CHAOS_ACTIONS_SINCE_SYNC=$(( CHAOS_ACTIONS_SINCE_SYNC + 1 ))
        _chaos_inc_action_ok "$action"
    elif [ "$rc" -eq 1 ]; then
        # Skip — no valid target
        CHAOS_SKIPPED=$(( CHAOS_SKIPPED + 1 ))
    elif [ "$rc" -eq 2 ]; then
        # Unexpected failure from executor
        CHAOS_UNEXPECTED_FAILURES=$(( CHAOS_UNEXPECTED_FAILURES + 1 ))
        _chaos_inc_action_unexpfail "$action"
    fi
}

# ============================================================
# 9. Sync Scheduling
# ============================================================

CHAOS_SYNC_MODE="${CHAOS_SYNC_MODE:-adaptive}"
CHAOS_SYNC_BATCH_MIN="${CHAOS_SYNC_BATCH_MIN:-3}"
CHAOS_SYNC_BATCH_MAX="${CHAOS_SYNC_BATCH_MAX:-10}"
_CHAOS_NEXT_SYNC_AT=0

should_sync() {
    case "$CHAOS_SYNC_MODE" in
        adaptive)
            if [ "$_CHAOS_NEXT_SYNC_AT" -eq 0 ]; then
                rand_int "$CHAOS_SYNC_BATCH_MIN" "$CHAOS_SYNC_BATCH_MAX"
                _CHAOS_NEXT_SYNC_AT="$_RAND_RESULT"
            fi
            [ "$CHAOS_ACTIONS_SINCE_SYNC" -ge "$_CHAOS_NEXT_SYNC_AT" ]
            ;;
        aggressive)
            return 0
            ;;
        random)
            rand_int 1 4
            [ "$_RAND_RESULT" -eq 1 ]
            ;;
        *)
            return 1
            ;;
    esac
}

_sync_one_actor() {
    # Sync a single actor, possibly injecting a partial failure.
    # Usage: _sync_one_actor <actor_letter>
    local actor="$1"
    if [ "$CHAOS_INJECT_FAILURES" = "true" ] && [ $(( RANDOM % 100 )) -lt "$CHAOS_INJECT_FAILURE_RATE" ]; then
        # Inject a partial sync: push-only or pull-only
        CHAOS_INJECTED_FAILURES=$(( CHAOS_INJECTED_FAILURES + 1 ))
        rand_bool
        if [ "$_RAND_RESULT" -eq 1 ]; then
            [ "$CHAOS_VERBOSE" = "true" ] && _ok "INJECTED FAILURE: push-only for actor $actor [#$CHAOS_INJECTED_FAILURES]"
            case "$actor" in
                a) td_a sync --push >/dev/null 2>&1 || true ;;
                b) td_b sync --push >/dev/null 2>&1 || true ;;
                c) td_c sync --push >/dev/null 2>&1 || true ;;
            esac
        else
            [ "$CHAOS_VERBOSE" = "true" ] && _ok "INJECTED FAILURE: pull-only for actor $actor [#$CHAOS_INJECTED_FAILURES]"
            case "$actor" in
                a) td_a sync --pull >/dev/null 2>&1 || true ;;
                b) td_b sync --pull >/dev/null 2>&1 || true ;;
                c) td_c sync --pull >/dev/null 2>&1 || true ;;
            esac
        fi
        return 0
    fi
    # Normal full sync
    case "$actor" in
        a) td_a sync >/dev/null 2>&1 || true ;;
        b) td_b sync >/dev/null 2>&1 || true ;;
        c) td_c sync >/dev/null 2>&1 || true ;;
    esac
}

do_chaos_sync() {
    local who="$1"
    _chaos_timer_start
    case "$who" in
        a|b|c)
            _sync_one_actor "$who"
            ;;
        both|all)
            # Randomize sync order among active actors
            if [ "${HARNESS_ACTORS:-2}" -ge 3 ]; then
                rand_int 1 6
                case "$_RAND_RESULT" in
                    1) _sync_one_actor a; _sync_one_actor b; _sync_one_actor c ;;
                    2) _sync_one_actor a; _sync_one_actor c; _sync_one_actor b ;;
                    3) _sync_one_actor b; _sync_one_actor a; _sync_one_actor c ;;
                    4) _sync_one_actor b; _sync_one_actor c; _sync_one_actor a ;;
                    5) _sync_one_actor c; _sync_one_actor a; _sync_one_actor b ;;
                    6) _sync_one_actor c; _sync_one_actor b; _sync_one_actor a ;;
                esac
            else
                rand_bool
                if [ "$_RAND_RESULT" -eq 1 ]; then
                    _sync_one_actor a
                    _sync_one_actor b
                else
                    _sync_one_actor b
                    _sync_one_actor a
                fi
            fi
            ;;
    esac
    _chaos_timer_stop_sync
    CHAOS_SYNC_COUNT=$(( CHAOS_SYNC_COUNT + 1 ))
    CHAOS_ACTIONS_SINCE_SYNC=0
    _CHAOS_NEXT_SYNC_AT=0
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "sync ($who) [#$CHAOS_SYNC_COUNT]"
    return 0
}

maybe_sync() {
    if should_sync; then
        if [ "${HARNESS_ACTORS:-2}" -ge 3 ]; then
            rand_choice a b c all; local direction="$_RAND_RESULT"
        else
            rand_choice a b both; local direction="$_RAND_RESULT"
        fi
        do_chaos_sync "$direction"
    fi
}
