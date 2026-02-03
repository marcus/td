#!/usr/bin/env bash
#
# Chaos sync e2e library - Executors module.
# Contains: All exec_* action executor functions.
# Requires: chaos_lib_core.sh
#

# Source core module
CHAOS_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$CHAOS_LIB_DIR/chaos_lib_core.sh"

# ============================================================
# 7. Action Executors
# ============================================================

exec_create() {
    local actor="$1"
    rand_title; local title="$_RAND_STR"
    rand_choice task bug feature spike; local type_val="$_RAND_RESULT"
    rand_choice P0 P1 P2 P3; local priority="$_RAND_RESULT"
    rand_int 0 13; local points="$_RAND_RESULT"
    rand_labels; local labels="$_RAND_STR"
    rand_description 1; local desc="$_RAND_STR"
    rand_acceptance; local acceptance="$_RAND_STR"

    local args=(create "$title" --type "$type_val" --priority "$priority" --points "$points" --labels "$labels")

    # 40% chance of parent
    local parent=""
    rand_int 1 5
    if [ "$_RAND_RESULT" -le 2 ] && [ "${#CHAOS_ISSUE_IDS[@]}" -gt 0 ]; then
        select_issue not_deleted; parent="$_CHAOS_SELECTED_ISSUE"
        if [ -n "$parent" ]; then
            args+=(--parent "$parent")
        fi
    fi

    local output rc=0
    output=$(chaos_run_td "$actor" "${args[@]}" 2>&1) || rc=$?
    if [ "$rc" -ne 0 ]; then
        if is_expected_failure "$output"; then
            CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
            return 0
        else
            [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected create: $output"
            return 2
        fi
    fi
    local issue_id
    issue_id=$(echo "$output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -z "$issue_id" ]; then
        return 2
    fi

    CHAOS_ISSUE_IDS+=("$issue_id")
    kv_set CHAOS_ISSUE_STATUS "$issue_id" "open"
    kv_set CHAOS_ISSUE_OWNER "$issue_id" "$actor"
    kv_set CHAOS_ISSUE_MINOR "$issue_id" "0"

    # Track parent-child relationship
    if [ -n "$parent" ]; then
        kv_set CHAOS_PARENT_CHILDREN "${parent}_${issue_id}" "1"
    fi

    # Set description and acceptance separately (they may contain special chars)
    chaos_run_td "$actor" update "$issue_id" --description "$desc" >/dev/null 2>&1 || true
    chaos_run_td "$actor" update "$issue_id" --acceptance "$acceptance" >/dev/null 2>&1 || true

    # 30% chance of marking minor
    rand_int 1 10
    if [ "$_RAND_RESULT" -le 3 ]; then
        chaos_run_td "$actor" update "$issue_id" --minor >/dev/null 2>&1 || true
        kv_set CHAOS_ISSUE_MINOR "$issue_id" "1"
    fi

    [ "$CHAOS_VERBOSE" = "true" ] && _ok "create: $issue_id by $actor ($type_val $priority)"
    return 0
}

exec_update() {
    local actor="$1"
    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    rand_int 1 3; local fields="$_RAND_RESULT"
    local args=(update "$id")
    for _ in $(seq 1 "$fields"); do
        rand_int 1 8
        case "$_RAND_RESULT" in
            1) rand_title 100; args+=(--title "$_RAND_STR") ;;
            2) rand_description 1; args+=(--description "$_RAND_STR") ;;
            3) rand_choice task bug feature spike; args+=(--type "$_RAND_RESULT") ;;
            4) rand_choice P0 P1 P2 P3; args+=(--priority "$_RAND_RESULT") ;;
            5) rand_int 0 13; args+=(--points "$_RAND_RESULT") ;;
            6) rand_labels; args+=(--labels "$_RAND_STR") ;;
            7) rand_acceptance; args+=(--acceptance "$_RAND_STR") ;;
            8) rand_choice "sprint-1" "sprint-2" "sprint-3" ""; args+=(--sprint "$_RAND_RESULT") ;;
        esac
    done

    local output rc=0
    output=$(chaos_run_td "$actor" "${args[@]}" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "update: $id by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected update: $output"
        return 2
    fi
}

exec_update_append() {
    local actor="$1"
    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    # Pick description or acceptance
    rand_int 1 2
    local field_flag
    if [ "$_RAND_RESULT" -eq 1 ]; then
        field_flag="--description"
    else
        field_flag="--acceptance"
    fi

    # Generate 1-3 random words as append text
    rand_int 1 3; local word_count="$_RAND_RESULT"
    local append_text=""
    for _ in $(seq 1 "$word_count"); do
        rand_int 0 $(( ${#_CHAOS_WORDS[@]} - 1 ))
        local w="${_CHAOS_WORDS[$_RAND_RESULT]}"
        if [ -z "$append_text" ]; then append_text="$w"; else append_text="$append_text $w"; fi
    done

    local output rc=0
    output=$(chaos_run_td "$actor" update "$id" --append "$field_flag" "$append_text" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "update_append: $id $field_flag '$append_text' by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected update_append: $output"
        return 2
    fi
}

exec_update_bulk() {
    local actor="$1"
    rand_int 2 3; local count="$_RAND_RESULT"
    local ids=()
    for _ in $(seq 1 "$count"); do
        select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
        if [ -n "$id" ]; then
            ids+=("$id")
        fi
    done
    [ "${#ids[@]}" -lt 2 ] && return 1

    local field_flag
    rand_int 1 5
    case "$_RAND_RESULT" in
        1) rand_choice P0 P1 P2 P3; field_flag="--priority $_RAND_RESULT" ;;
        2) rand_choice task bug feature spike; field_flag="--type $_RAND_RESULT" ;;
        3) rand_int 0 13; field_flag="--points $_RAND_RESULT" ;;
        4) rand_labels; field_flag="--labels $_RAND_STR" ;;
        5) rand_choice "sprint-1" "sprint-2" "sprint-3" ""; field_flag="--sprint $_RAND_RESULT" ;;
    esac

    local output
    # shellcheck disable=SC2086
    output=$(chaos_run_td "$actor" update "${ids[@]}" $field_flag 2>&1) || true
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "update_bulk: ${ids[*]} by $actor"
    return 0
}

exec_delete() {
    local actor="$1"
    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" delete "$id" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        CHAOS_DELETED_IDS+=("$id")
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "delete: $id by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected delete: $output"
        return 2
    fi
}

exec_restore() {
    local actor="$1"
    [ "${#CHAOS_DELETED_IDS[@]}" -eq 0 ] && return 1

    local id="${CHAOS_DELETED_IDS[$(( RANDOM % ${#CHAOS_DELETED_IDS[@]} ))]}"
    local output rc=0
    output=$(chaos_run_td "$actor" restore "$id" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        # Remove from deleted list
        local new_deleted=()
        for d in "${CHAOS_DELETED_IDS[@]}"; do
            [ "$d" != "$id" ] && new_deleted+=("$d")
        done
        CHAOS_DELETED_IDS=("${new_deleted[@]+"${new_deleted[@]}"}")
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "restore: $id by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected restore: $output"
        return 2
    fi
}

# --- Status transitions ---

exec_start() {
    local actor="$1"
    select_issue open; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" start "$id" --reason "chaos start" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        kv_set CHAOS_ISSUE_STATUS "$id" "in_progress"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "start: $id by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected start: $output"
        return 2
    fi
}

exec_unstart() {
    local actor="$1"
    select_issue in_progress; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" unstart "$id" --reason "chaos unstart" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        kv_set CHAOS_ISSUE_STATUS "$id" "open"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "unstart: $id by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected unstart: $output"
        return 2
    fi
}

exec_review() {
    local actor="$1"
    select_issue in_progress; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" review "$id" --reason "chaos review" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        kv_set CHAOS_ISSUE_STATUS "$id" "in_review"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "review: $id by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected review: $output"
        return 2
    fi
}

exec_approve() {
    local actor="$1"
    select_issue in_review; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    # Try to use OTHER actor to avoid self-approve on non-minor
    local approver="$actor"
    local owner
    owner=$(kv_get CHAOS_ISSUE_OWNER "$id")
    local is_minor
    is_minor=$(kv_get CHAOS_ISSUE_MINOR "$id")
    [ -z "$is_minor" ] && is_minor="0"
    if [ "$owner" = "$actor" ] && [ "$is_minor" != "1" ]; then
        if [ "$actor" = "a" ]; then approver="b"; else approver="a"; fi
    fi

    local output rc=0
    output=$(chaos_run_td "$approver" approve "$id" --reason "chaos approve" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        kv_set CHAOS_ISSUE_STATUS "$id" "closed"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "approve: $id by $approver"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$approver] unexpected approve: $output"
        return 2
    fi
}

exec_reject() {
    local actor="$1"
    select_issue in_review; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" reject "$id" --reason "chaos reject" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        kv_set CHAOS_ISSUE_STATUS "$id" "in_progress"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "reject: $id by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected reject: $output"
        return 2
    fi
}

exec_close() {
    local actor="$1"
    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1
    local status
    status=$(kv_get CHAOS_ISSUE_STATUS "$id")
    [ "$status" = "closed" ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" close "$id" --reason "chaos close" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        kv_set CHAOS_ISSUE_STATUS "$id" "closed"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "close: $id by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected close: $output"
        return 2
    fi
}

exec_reopen() {
    local actor="$1"
    select_issue closed; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" reopen "$id" --reason "chaos reopen" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        kv_set CHAOS_ISSUE_STATUS "$id" "open"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "reopen: $id by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected reopen: $output"
        return 2
    fi
}

# --- Bulk status transitions ---

exec_bulk_start() {
    local actor="$1"
    rand_int 2 4; local count="$_RAND_RESULT"
    local ids=()
    for _ in $(seq 1 "$count"); do
        select_issue open; local id="$_CHAOS_SELECTED_ISSUE"
        if [ -n "$id" ]; then
            # Avoid duplicates
            local dup=0
            for existing in "${ids[@]+"${ids[@]}"}"; do
                [ "$existing" = "$id" ] && dup=1 && break
            done
            [ "$dup" -eq 0 ] && ids+=("$id")
        fi
    done
    [ "${#ids[@]}" -lt 2 ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" start "${ids[@]}" --reason "chaos bulk start" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        for id in "${ids[@]}"; do
            kv_set CHAOS_ISSUE_STATUS "$id" "in_progress"
        done
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "bulk_start: ${ids[*]} by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected bulk_start: $output"
        return 2
    fi
}

exec_bulk_review() {
    local actor="$1"
    rand_int 2 4; local count="$_RAND_RESULT"
    local ids=()
    for _ in $(seq 1 "$count"); do
        select_issue in_progress; local id="$_CHAOS_SELECTED_ISSUE"
        if [ -n "$id" ]; then
            local dup=0
            for existing in "${ids[@]+"${ids[@]}"}"; do
                [ "$existing" = "$id" ] && dup=1 && break
            done
            [ "$dup" -eq 0 ] && ids+=("$id")
        fi
    done
    [ "${#ids[@]}" -lt 2 ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" review "${ids[@]}" --reason "chaos bulk review" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        for id in "${ids[@]}"; do
            kv_set CHAOS_ISSUE_STATUS "$id" "in_review"
        done
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "bulk_review: ${ids[*]} by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected bulk_review: $output"
        return 2
    fi
}

exec_bulk_close() {
    local actor="$1"
    rand_int 2 4; local count="$_RAND_RESULT"
    local ids=()
    for _ in $(seq 1 "$count"); do
        select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
        if [ -n "$id" ]; then
            local st
            st=$(kv_get CHAOS_ISSUE_STATUS "$id")
            if [ "$st" != "closed" ]; then
                local dup=0
                for existing in "${ids[@]+"${ids[@]}"}"; do
                    [ "$existing" = "$id" ] && dup=1 && break
                done
                [ "$dup" -eq 0 ] && ids+=("$id")
            fi
        fi
    done
    [ "${#ids[@]}" -lt 2 ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" close "${ids[@]}" --reason "chaos bulk close" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        for id in "${ids[@]}"; do
            kv_set CHAOS_ISSUE_STATUS "$id" "closed"
        done
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "bulk_close: ${ids[*]} by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected bulk_close: $output"
        return 2
    fi
}

exec_block() {
    local actor="$1"
    select_issue open; local id="$_CHAOS_SELECTED_ISSUE"
    if [ -z "$id" ]; then
        select_issue in_progress; id="$_CHAOS_SELECTED_ISSUE"
    fi
    [ -z "$id" ] && return 1

    local output
    output=$(chaos_run_td "$actor" block "$id" --reason "chaos block" 2>&1) || true
    if ! is_expected_failure "$output" && ! echo "$output" | grep -qi "error"; then
        kv_set CHAOS_ISSUE_STATUS "$id" "blocked"
    fi
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "block: $id by $actor"
    return 0
}

exec_unblock() {
    local actor="$1"
    select_issue blocked; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    local output
    output=$(chaos_run_td "$actor" unblock "$id" --reason "chaos unblock" 2>&1) || true
    if ! is_expected_failure "$output" && ! echo "$output" | grep -qi "error"; then
        kv_set CHAOS_ISSUE_STATUS "$id" "open"
    fi
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "unblock: $id by $actor"
    return 0
}

# --- Comments & logs ---

exec_comment() {
    local actor="$1"
    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    rand_comment 10 100; local text="$_RAND_STR"
    local output
    output=$(chaos_run_td "$actor" comments add "$id" "$text" 2>&1) || true
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "comment: $id by $actor"
    return 0
}

exec_log_progress() {
    local actor="$1"
    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    rand_comment 5 30; local msg="$_RAND_STR"
    chaos_run_td "$actor" log --issue "$id" "$msg" >/dev/null 2>&1 || true
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "log_progress: $id by $actor"
    return 0
}

exec_log_blocker() {
    local actor="$1"
    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    rand_comment 5 20; local msg="$_RAND_STR"
    chaos_run_td "$actor" log --issue "$id" --blocker "$msg" >/dev/null 2>&1 || true
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "log_blocker: $id by $actor"
    return 0
}

exec_log_decision() {
    local actor="$1"
    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    rand_comment 5 20; local msg="$_RAND_STR"
    chaos_run_td "$actor" log --issue "$id" --decision "$msg" >/dev/null 2>&1 || true
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "log_decision: $id by $actor"
    return 0
}

exec_log_hypothesis() {
    local actor="$1"
    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    rand_comment 5 20; local msg="$_RAND_STR"
    chaos_run_td "$actor" log --issue "$id" --hypothesis "$msg" >/dev/null 2>&1 || true
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "log_hypothesis: $id by $actor"
    return 0
}

exec_log_result() {
    local actor="$1"
    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    rand_comment 5 20; local msg="$_RAND_STR"
    chaos_run_td "$actor" log --issue "$id" --result "$msg" >/dev/null 2>&1 || true
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "log_result: $id by $actor"
    return 0
}

# --- Dependencies ---
# Dep pairs use underscore separator instead of colon in the compound key
# since the kv_* helpers use colon as key:value delimiter.
# Key format: "fromID_toID" -> value "1"

exec_dep_add() {
    local actor="$1"
    select_issue not_deleted; local id1="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id1" ] && return 1
    select_issue not_deleted; local id2="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id2" ] && return 1
    [ "$id1" = "$id2" ] && return 1

    # Already tracked?
    local dep_key="${id1}_${id2}"
    if kv_has CHAOS_DEP_PAIRS "$dep_key"; then
        return 1
    fi

    local output rc=0
    output=$(chaos_run_td "$actor" dep add "$id1" "$id2" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        kv_set CHAOS_DEP_PAIRS "$dep_key" "1"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "dep_add: $id1 -> $id2 by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected dep_add: $output"
        return 2
    fi
}

exec_dep_rm() {
    local actor="$1"
    # Get all dep pair keys
    local keys
    keys=$(kv_keys CHAOS_DEP_PAIRS)
    [ -z "$keys" ] && return 1

    # Convert to array for random selection
    local key_array=()
    local k
    for k in $keys; do
        key_array+=("$k")
    done
    [ "${#key_array[@]}" -eq 0 ] && return 1

    local pair="${key_array[$(( RANDOM % ${#key_array[@]} ))]}"
    # Split on underscore: fromID_toID
    # Issue IDs are like td-abcdef, so split on the underscore between two td- prefixed IDs
    # The key is "td-xxx_td-yyy", split by finding "_td-" pattern
    local id1 id2
    id1=$(echo "$pair" | sed 's/_td-.*$//')
    id2=$(echo "$pair" | sed 's/^[^_]*_//')

    local output rc=0
    output=$(chaos_run_td "$actor" dep rm "$id1" "$id2" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        kv_del CHAOS_DEP_PAIRS "$pair"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "dep_rm: $id1 -> $id2 by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected dep_rm: $output"
        return 2
    fi
}

# --- Boards ---

exec_board_create() {
    local actor="$1"
    local name
    # ~15% chance of edge-case board name
    if maybe_edge_data; then
        # Board names must be non-empty for tracking; use edge data as suffix
        if [ -z "$_RAND_STR" ]; then
            name="chaos-board-empty-$(openssl rand -hex 4)"
        else
            name="$_RAND_STR"
        fi
    else
        name="chaos-board-$(openssl rand -hex 4)"
    fi

    local args=(board create "$name")
    # 50% chance of query
    rand_bool
    if [ "$_RAND_RESULT" -eq 1 ]; then
        rand_choice "status = open" "priority = P0" "type = bug" "status != closed" "labels ~ urgent"
        local query="$_RAND_RESULT"
        args+=(-q "$query")
    fi

    local output rc=0
    output=$(chaos_run_td "$actor" "${args[@]}" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        CHAOS_BOARD_NAMES+=("$name")
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "board_create: $name by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected board_create: $output"
        return 2
    fi
}

exec_board_edit() {
    local actor="$1"
    [ "${#CHAOS_BOARD_NAMES[@]}" -eq 0 ] && return 1

    local name="${CHAOS_BOARD_NAMES[$(( RANDOM % ${#CHAOS_BOARD_NAMES[@]} ))]}"
    rand_choice "status = open" "priority = P0" "type = bug" "status != closed" "labels ~ urgent" "points > 3"
    local query="$_RAND_RESULT"

    local output
    output=$(chaos_run_td "$actor" board edit "$name" -q "$query" 2>&1) || true
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "board_edit: $name by $actor"
    return 0
}

exec_board_move() {
    local actor="$1"
    [ "${#CHAOS_BOARD_NAMES[@]}" -eq 0 ] && return 1

    local name="${CHAOS_BOARD_NAMES[$(( RANDOM % ${#CHAOS_BOARD_NAMES[@]} ))]}"
    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    rand_int 1 100; local pos="$_RAND_RESULT"
    local output rc=0
    output=$(chaos_run_td "$actor" board move "$name" "$id" "$pos" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "board_move: $name $id pos=$pos by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected board_move: $output"
        return 2
    fi
}

exec_board_unposition() {
    local actor="$1"
    [ "${#CHAOS_BOARD_NAMES[@]}" -eq 0 ] && return 1

    local name="${CHAOS_BOARD_NAMES[$(( RANDOM % ${#CHAOS_BOARD_NAMES[@]} ))]}"
    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    local output
    output=$(chaos_run_td "$actor" board unposition "$name" "$id" 2>&1) || true
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "board_unposition: $name $id by $actor"
    return 0
}

exec_board_delete() {
    local actor="$1"
    [ "${#CHAOS_BOARD_NAMES[@]}" -eq 0 ] && return 1

    local idx=$(( RANDOM % ${#CHAOS_BOARD_NAMES[@]} ))
    local name="${CHAOS_BOARD_NAMES[$idx]}"

    local output rc=0
    output=$(chaos_run_td "$actor" board delete "$name" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        # Remove from tracking
        local new_boards=()
        for b in "${CHAOS_BOARD_NAMES[@]}"; do
            [ "$b" != "$name" ] && new_boards+=("$b")
        done
        CHAOS_BOARD_NAMES=("${new_boards[@]+"${new_boards[@]}"}")
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "board_delete: $name by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected board_delete: $output"
        return 2
    fi
}

exec_board_view_mode() {
    local actor="$1"
    [ "${#CHAOS_BOARD_NAMES[@]}" -eq 0 ] && return 1

    local name="${CHAOS_BOARD_NAMES[$(( RANDOM % ${#CHAOS_BOARD_NAMES[@]} ))]}"
    rand_choice "swimlanes" "backlog"
    local mode="$_RAND_RESULT"

    local output rc=0
    output=$(chaos_run_td "$actor" board edit "$name" --view-mode "$mode" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "board_view_mode: $name → $mode by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected board_view_mode: $output"
        return 2
    fi
}

# --- Handoffs ---

exec_handoff() {
    local actor="$1"
    select_issue in_progress; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    rand_handoff_items; local done_items="$_RAND_STR"
    rand_handoff_items; local remaining_items="$_RAND_STR"
    rand_handoff_items 2; local decision_items="$_RAND_STR"
    rand_handoff_items 2; local uncertain_items="$_RAND_STR"

    local output rc=0
    output=$(chaos_run_td "$actor" handoff "$id" \
        --done "$done_items" \
        --remaining "$remaining_items" \
        --decision "$decision_items" \
        --uncertain "$uncertain_items" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "handoff: $id by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected handoff: $output"
        return 2
    fi
}

# --- Parent-child cascade actions ---

exec_create_child() {
    local actor="$1"
    # Need at least one existing issue to be a parent
    [ "${#CHAOS_ISSUE_IDS[@]}" -eq 0 ] && return 1

    select_issue not_deleted; local parent="$_CHAOS_SELECTED_ISSUE"
    [ -z "$parent" ] && return 1

    rand_title; local title="$_RAND_STR"
    rand_choice task bug feature spike; local type_val="$_RAND_RESULT"
    rand_choice P0 P1 P2 P3; local priority="$_RAND_RESULT"
    rand_int 0 13; local points="$_RAND_RESULT"

    local output rc=0
    output=$(chaos_run_td "$actor" create "$title" --type "$type_val" --priority "$priority" --points "$points" --parent "$parent" 2>&1) || rc=$?
    if [ "$rc" -ne 0 ]; then
        if is_expected_failure "$output"; then
            CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
            return 0
        else
            [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected create_child: $output"
            return 2
        fi
    fi
    local issue_id
    issue_id=$(echo "$output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -z "$issue_id" ]; then
        return 2
    fi

    CHAOS_ISSUE_IDS+=("$issue_id")
    kv_set CHAOS_ISSUE_STATUS "$issue_id" "open"
    kv_set CHAOS_ISSUE_OWNER "$issue_id" "$actor"
    kv_set CHAOS_ISSUE_MINOR "$issue_id" "0"
    kv_set CHAOS_PARENT_CHILDREN "${parent}_${issue_id}" "1"
    CHAOS_CASCADE_ACTIONS=$((CHAOS_CASCADE_ACTIONS + 1))

    [ "$CHAOS_VERBOSE" = "true" ] && _ok "create_child: $issue_id -> parent $parent by $actor"
    return 0
}

exec_cascade_handoff() {
    local actor="$1"
    # Find a parent that has children (scan CHAOS_PARENT_CHILDREN for a parent with in_progress status)
    local parent_id=""
    local keys
    keys=$(kv_keys CHAOS_PARENT_CHILDREN)
    for key in $keys; do
        local pid
        pid=$(echo "$key" | cut -d_ -f1)
        if ! is_chaos_deleted "$pid"; then
            local st
            st=$(kv_get CHAOS_ISSUE_STATUS "$pid")
            if [ "$st" = "in_progress" ]; then
                parent_id="$pid"
                break
            fi
        fi
    done
    [ -z "$parent_id" ] && return 1

    rand_handoff_items; local done_items="$_RAND_STR"
    rand_handoff_items; local remaining_items="$_RAND_STR"

    local output rc=0
    output=$(chaos_run_td "$actor" handoff "$parent_id" \
        --done "$done_items" \
        --remaining "$remaining_items" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        CHAOS_CASCADE_ACTIONS=$((CHAOS_CASCADE_ACTIONS + 1))
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "cascade_handoff: $parent_id by $actor (children should cascade)"
        # Verify children actually got handoffs
        for key in $keys; do
            local vpid vcid
            vpid=$(echo "$key" | cut -d_ -f1)
            vcid=$(echo "$key" | cut -d_ -f2-)
            if [ "$vpid" = "$parent_id" ] && ! is_chaos_deleted "$vcid"; then
                local vcst
                vcst=$(kv_get CHAOS_ISSUE_STATUS "$vcid")
                # Only check children that were in_progress (should cascade)
                if [ "$vcst" = "in_progress" ]; then
                    local child_out child_rc=0
                    child_out=$(chaos_run_td "$actor" show "$vcid" --json 2>&1) || child_rc=$?
                    if [ "$child_rc" -eq 0 ]; then
                        # Check if child has a handoff record in the output
                        if echo "$child_out" | grep -q '"handoff"'; then
                            [ "$CHAOS_VERBOSE" = "true" ] && _ok "cascade_handoff verified: child $vcid has handoff"
                        else
                            CHAOS_CASCADE_VERIFY_FAILURES=$((CHAOS_CASCADE_VERIFY_FAILURES + 1))
                            [ "$CHAOS_VERBOSE" = "true" ] && _fail "cascade_handoff NOT cascaded: child $vcid missing handoff"
                        fi
                    fi
                fi
            fi
        done
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected cascade_handoff: $output"
        return 2
    fi
}

exec_cascade_review() {
    local actor="$1"
    # Find a parent with in_progress children
    local parent_id=""
    local keys
    keys=$(kv_keys CHAOS_PARENT_CHILDREN)
    for key in $keys; do
        local pid cid
        pid=$(echo "$key" | cut -d_ -f1)
        cid=$(echo "$key" | cut -d_ -f2-)
        if ! is_chaos_deleted "$pid"; then
            local pst cst
            pst=$(kv_get CHAOS_ISSUE_STATUS "$pid")
            cst=$(kv_get CHAOS_ISSUE_STATUS "$cid")
            if [ "$pst" = "in_progress" ] && [ "$cst" = "in_progress" ]; then
                parent_id="$pid"
                break
            fi
        fi
    done
    [ -z "$parent_id" ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" review "$parent_id" --reason "cascade review test" 2>&1) || rc=$?
    if [ "$rc" -eq 0 ]; then
        kv_set CHAOS_ISSUE_STATUS "$parent_id" "in_review"
        # Verify children actually transitioned, only update tracker if confirmed
        for key in $keys; do
            local pid2 cid2
            pid2=$(echo "$key" | cut -d_ -f1)
            cid2=$(echo "$key" | cut -d_ -f2-)
            if [ "$pid2" = "$parent_id" ] && ! is_chaos_deleted "$cid2"; then
                local cst2
                cst2=$(kv_get CHAOS_ISSUE_STATUS "$cid2")
                if [ "$cst2" = "in_progress" ] || [ "$cst2" = "open" ]; then
                    # Verify actual state via CLI before updating tracker
                    local child_out child_rc=0
                    child_out=$(chaos_run_td "$actor" show "$cid2" --json 2>&1) || child_rc=$?
                    if [ "$child_rc" -eq 0 ]; then
                        local actual_status
                        actual_status=$(echo "$child_out" | grep -o '"status":"[^"]*"' | head -1 | cut -d'"' -f4)
                        if [ "$actual_status" = "in_review" ]; then
                            kv_set CHAOS_ISSUE_STATUS "$cid2" "in_review"
                            [ "$CHAOS_VERBOSE" = "true" ] && _ok "cascade_review verified: child $cid2 -> in_review"
                        else
                            CHAOS_CASCADE_VERIFY_FAILURES=$((CHAOS_CASCADE_VERIFY_FAILURES + 1))
                            [ "$CHAOS_VERBOSE" = "true" ] && _fail "cascade_review NOT cascaded: child $cid2 still $actual_status (expected in_review)"
                        fi
                    fi
                fi
            fi
        done
        CHAOS_CASCADE_ACTIONS=$((CHAOS_CASCADE_ACTIONS + 1))
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "cascade_review: $parent_id by $actor (children should cascade)"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected cascade_review: $output"
        return 2
    fi
}

# --- File links ---

exec_link() {
    local actor="$1"
    if [ ${#CHAOS_ISSUE_IDS[@]} -eq 0 ]; then
        CHAOS_SKIPPED=$((CHAOS_SKIPPED + 1))
        return 0
    fi

    # Pick random non-deleted issue
    select_issue not_deleted; local issue_id="$_CHAOS_SELECTED_ISSUE"
    if [ -z "$issue_id" ]; then
        CHAOS_SKIPPED=$((CHAOS_SKIPPED + 1))
        return 0
    fi

    # Generate random file path
    local dirs=("src" "tests" "docs" "internal" "cmd" "pkg")
    local exts=("go" "md" "sh" "yaml" "json")
    rand_choice "${dirs[@]}"; local dir="$_RAND_RESULT"
    rand_choice "${exts[@]}"; local ext="$_RAND_RESULT"
    rand_int 1 999; local num="$_RAND_RESULT"
    local file_path="${dir}_file_${num}.${ext}"

    # Pick random role
    rand_choice implementation test reference config
    local role="$_RAND_RESULT"

    # Create the file so td link can find it (td link requires files to exist)
    local abs_file_path
    case "$actor" in
        a) abs_file_path="$CLIENT_A_DIR/$file_path" ;;
        b) abs_file_path="$CLIENT_B_DIR/$file_path" ;;
        c) abs_file_path="$CLIENT_C_DIR/$file_path" ;;
        *) abs_file_path="$CLIENT_B_DIR/$file_path" ;;
    esac
    mkdir -p "$(dirname "$abs_file_path")"
    echo "chaos-generated" > "$abs_file_path"

    # Execute link command
    local output rc=0
    output=$(chaos_run_td "$actor" link "$issue_id" "$file_path" --role "$role" 2>&1) || rc=$?

    if [ "$rc" -eq 0 ]; then
        # Track in KV store using issue~filePath as key, role as value
        kv_set CHAOS_ISSUE_FILES "${issue_id}~${file_path}" "$role"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "link: $issue_id -> $file_path ($role) by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected link: $output"
        return 2
    fi
}

exec_unlink() {
    local actor="$1"

    # Need at least one linked file
    local count
    count=$(kv_count CHAOS_ISSUE_FILES)
    if [ "$count" -eq 0 ]; then
        CHAOS_SKIPPED=$((CHAOS_SKIPPED + 1))
        return 0
    fi

    # Pick a random linked file
    local keys
    keys=$(kv_keys CHAOS_ISSUE_FILES)
    local keys_arr=($keys)
    rand_int 0 $(( ${#keys_arr[@]} - 1 ))
    local picked_key="${keys_arr[$_RAND_RESULT]}"

    # Parse issue_id and file_path from key (separator is ~)
    local issue_id="${picked_key%%~*}"
    local file_path="${picked_key#*~}"

    # Execute unlink command
    local output rc=0
    output=$(chaos_run_td "$actor" unlink "$issue_id" "$file_path" 2>&1) || rc=$?

    if [ "$rc" -eq 0 ]; then
        kv_del CHAOS_ISSUE_FILES "$picked_key"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "unlink: $issue_id -> $file_path by $actor"
        return 0
    elif is_expected_failure "$output"; then
        # Unlink might fail if issue was deleted or file already unlinked by other actor
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected unlink: $output"
        return 2
    fi
}

# --- Work sessions ---

_get_active_ws() {
    case "$1" in
        a) echo "$CHAOS_ACTIVE_WS_A" ;;
        b) echo "$CHAOS_ACTIVE_WS_B" ;;
        c) echo "$CHAOS_ACTIVE_WS_C" ;;
    esac
}
_set_active_ws() {
    case "$1" in
        a) CHAOS_ACTIVE_WS_A="$2" ;;
        b) CHAOS_ACTIVE_WS_B="$2" ;;
        c) CHAOS_ACTIVE_WS_C="$2" ;;
    esac
}
_get_tagged_var() {
    case "$1" in
        a) echo "CHAOS_WS_TAGGED_A" ;;
        b) echo "CHAOS_WS_TAGGED_B" ;;
        c) echo "CHAOS_WS_TAGGED_C" ;;
    esac
}
_clear_ws_state() {
    case "$1" in
        a) CHAOS_ACTIVE_WS_A=""; CHAOS_WS_TAGGED_A="" ;;
        b) CHAOS_ACTIVE_WS_B=""; CHAOS_WS_TAGGED_B="" ;;
        c) CHAOS_ACTIVE_WS_C=""; CHAOS_WS_TAGGED_C="" ;;
    esac
}

exec_ws_start() {
    local actor="$1"
    local active_val
    active_val=$(_get_active_ws "$actor")

    # Can't start two sessions at once
    if [ -n "$active_val" ]; then
        CHAOS_SKIPPED=$((CHAOS_SKIPPED + 1))
        return 0
    fi

    rand_int 1 999; local name="chaos-ws-${_RAND_RESULT}"
    local output rc=0
    output=$(chaos_run_td "$actor" ws start "$name" 2>&1) || rc=$?
    if [ $rc -eq 0 ]; then
        _set_active_ws "$actor" "$name"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "ws_start: $name by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected ws_start: $output"
        return 2
    fi
}

exec_ws_tag() {
    local actor="$1"
    local active_val
    active_val=$(_get_active_ws "$actor")

    # Need active session
    [ -z "$active_val" ] && return 1

    select_issue not_deleted; local id="$_CHAOS_SELECTED_ISSUE"
    [ -z "$id" ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" ws tag "$id" --no-start 2>&1) || rc=$?
    if [ $rc -eq 0 ]; then
        local tv; tv=$(_get_tagged_var "$actor"); kv_set "$tv" "$id" "1"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "ws_tag: $id by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected ws_tag: $output"
        return 2
    fi
}

exec_ws_untag() {
    local actor="$1"
    local active_val tagged_var
    active_val=$(_get_active_ws "$actor")
    tagged_var=$(_get_tagged_var "$actor")

    # Need active session with tagged issues
    [ -z "$active_val" ] && return 1
    local count
    count=$(kv_count "$tagged_var")
    [ "$count" -eq 0 ] && return 1

    # Pick random tagged issue
    local keys
    keys=$(kv_keys "$tagged_var")
    local keys_arr=($keys)
    rand_int 0 $(( ${#keys_arr[@]} - 1 ))
    local id="${keys_arr[$_RAND_RESULT]}"

    local output rc=0
    output=$(chaos_run_td "$actor" ws untag "$id" 2>&1) || rc=$?
    if [ $rc -eq 0 ]; then
        kv_del "$tagged_var" "$id"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "ws_untag: $id by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected ws_untag: $output"
        return 2
    fi
}

exec_ws_end() {
    local actor="$1"
    local active_val
    active_val=$(_get_active_ws "$actor")

    # Need active session
    [ -z "$active_val" ] && return 1

    local output rc=0
    output=$(chaos_run_td "$actor" ws end 2>&1) || rc=$?
    if [ $rc -eq 0 ]; then
        _clear_ws_state "$actor"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "ws_end: by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected ws_end: $output"
        return 2
    fi
}

exec_ws_handoff() {
    local actor="$1"
    local active_val
    active_val=$(_get_active_ws "$actor")

    # Need active session
    [ -z "$active_val" ] && return 1

    rand_handoff_items; local done_items="$_RAND_STR"
    rand_handoff_items; local remaining_items="$_RAND_STR"
    rand_handoff_items 2; local decision_items="$_RAND_STR"
    rand_handoff_items 2; local uncertain_items="$_RAND_STR"

    local output rc=0
    output=$(chaos_run_td "$actor" ws handoff \
        --done "$done_items" \
        --remaining "$remaining_items" \
        --decision "$decision_items" \
        --uncertain "$uncertain_items" 2>&1) || rc=$?
    if [ $rc -eq 0 ]; then
        _clear_ws_state "$actor"
        [ "$CHAOS_VERBOSE" = "true" ] && _ok "ws_handoff: by $actor"
        return 0
    elif is_expected_failure "$output"; then
        CHAOS_EXPECTED_FAILURES=$((CHAOS_EXPECTED_FAILURES + 1))
        return 0
    else
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] unexpected ws_handoff: $output"
        return 2
    fi
}

# ============================================================
# 7c. Notes Executors
# ============================================================

# Helper to get db path for an actor
_chaos_get_db_path() {
    local actor="$1"
    case "$actor" in
        a) echo "$_CHAOS_DB_PATH_A" ;;
        b) echo "$_CHAOS_DB_PATH_B" ;;
        c) echo "$_CHAOS_DB_PATH_C" ;;
    esac
}

# Helper to get session id for an actor
_chaos_get_session_id() {
    local actor="$1"
    case "$actor" in
        a) echo "$_CHAOS_SESSION_A" ;;
        b) echo "$_CHAOS_SESSION_B" ;;
        c) echo "$_CHAOS_SESSION_C" ;;
    esac
}

exec_note_create() {
    local actor="$1"
    local db_path
    db_path=$(_chaos_get_db_path "$actor")
    [ -z "$db_path" ] && return 1

    # Generate note data
    local note_id="nt-$(openssl rand -hex 4)"
    rand_title 60; local title="$_RAND_STR"
    rand_description 3; local content="$_RAND_STR"
    local now
    now=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Create notes table if not exists
    sqlite3 "$db_path" "CREATE TABLE IF NOT EXISTS notes (
        id TEXT PRIMARY KEY,
        title TEXT NOT NULL,
        content TEXT NOT NULL,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL,
        pinned INTEGER DEFAULT 0,
        archived INTEGER DEFAULT 0,
        deleted_at TEXT
    );" 2>/dev/null || true

    # Insert note
    local rc=0
    sqlite3 "$db_path" "INSERT INTO notes (id, title, content, created_at, updated_at) VALUES ('$note_id', '$(echo "$title" | sed "s/'/''/g")', '$(echo "$content" | sed "s/'/''/g")', '$now', '$now');" 2>/dev/null || rc=$?

    if [ $rc -ne 0 ]; then
        [ "$CHAOS_VERBOSE" = "true" ] && _fail "[$actor] note_create failed"
        return 2
    fi

    # Create action_log entry
    local session_id
    session_id=$(_chaos_get_session_id "$actor")
    local al_id="al-$(openssl rand -hex 4)"
    local new_data="{\"id\":\"$note_id\",\"title\":\"$(echo "$title" | sed 's/"/\\"/g')\",\"content\":\"$(echo "$content" | sed 's/"/\\"/g')\"}"

    sqlite3 "$db_path" "INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp)
        VALUES ('$al_id', '$session_id', 'create', 'notes', '$note_id', '$new_data', '$now');" 2>/dev/null || true

    CHAOS_NOTE_IDS+=("$note_id")
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "note_create: $note_id by $actor"
    return 0
}

exec_note_update() {
    local actor="$1"

    # Need existing non-deleted notes
    [ "${#CHAOS_NOTE_IDS[@]}" -eq 0 ] && return 1

    # Filter out deleted notes
    local available=()
    for nid in "${CHAOS_NOTE_IDS[@]}"; do
        local is_deleted=0
        for did in "${CHAOS_DELETED_NOTE_IDS[@]}"; do
            [ "$nid" = "$did" ] && is_deleted=1 && break
        done
        [ "$is_deleted" -eq 0 ] && available+=("$nid")
    done
    [ "${#available[@]}" -eq 0 ] && return 1

    rand_int 0 $(( ${#available[@]} - 1 ))
    local note_id="${available[$_RAND_RESULT]}"

    local db_path
    db_path=$(_chaos_get_db_path "$actor")
    [ -z "$db_path" ] && return 1

    # Get current note data for previous_data
    local prev_data
    prev_data=$(sqlite3 "$db_path" "SELECT json_object('id', id, 'title', title, 'content', content) FROM notes WHERE id='$note_id';" 2>/dev/null || echo "{}")

    # Random update: title, content, or both
    rand_int 1 3
    local update_type="$_RAND_RESULT"
    local now
    now=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    local new_title="" new_content=""

    case "$update_type" in
        1) # Update title only
            rand_title 60; new_title="$_RAND_STR"
            sqlite3 "$db_path" "UPDATE notes SET title='$(echo "$new_title" | sed "s/'/''/g")', updated_at='$now' WHERE id='$note_id';" 2>/dev/null || return 2
            ;;
        2) # Update content only
            rand_description 3; new_content="$_RAND_STR"
            sqlite3 "$db_path" "UPDATE notes SET content='$(echo "$new_content" | sed "s/'/''/g")', updated_at='$now' WHERE id='$note_id';" 2>/dev/null || return 2
            ;;
        3) # Update both
            rand_title 60; new_title="$_RAND_STR"
            rand_description 3; new_content="$_RAND_STR"
            sqlite3 "$db_path" "UPDATE notes SET title='$(echo "$new_title" | sed "s/'/''/g")', content='$(echo "$new_content" | sed "s/'/''/g")', updated_at='$now' WHERE id='$note_id';" 2>/dev/null || return 2
            ;;
    esac

    # Create action_log entry
    local session_id
    session_id=$(_chaos_get_session_id "$actor")
    local al_id="al-$(openssl rand -hex 4)"
    local new_data
    new_data=$(sqlite3 "$db_path" "SELECT json_object('id', id, 'title', title, 'content', content) FROM notes WHERE id='$note_id';" 2>/dev/null || echo "{}")

    sqlite3 "$db_path" "INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp)
        VALUES ('$al_id', '$session_id', 'update', 'notes', '$note_id', '$prev_data', '$new_data', '$now');" 2>/dev/null || true

    [ "$CHAOS_VERBOSE" = "true" ] && _ok "note_update: $note_id by $actor"
    return 0
}

exec_note_delete() {
    local actor="$1"

    # Need existing non-deleted notes
    [ "${#CHAOS_NOTE_IDS[@]}" -eq 0 ] && return 1

    # Filter out deleted notes
    local available=()
    for nid in "${CHAOS_NOTE_IDS[@]}"; do
        local is_deleted=0
        for did in "${CHAOS_DELETED_NOTE_IDS[@]}"; do
            [ "$nid" = "$did" ] && is_deleted=1 && break
        done
        [ "$is_deleted" -eq 0 ] && available+=("$nid")
    done
    [ "${#available[@]}" -eq 0 ] && return 1

    rand_int 0 $(( ${#available[@]} - 1 ))
    local note_id="${available[$_RAND_RESULT]}"

    local db_path
    db_path=$(_chaos_get_db_path "$actor")
    [ -z "$db_path" ] && return 1

    local now
    now=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Soft delete
    sqlite3 "$db_path" "UPDATE notes SET deleted_at='$now' WHERE id='$note_id';" 2>/dev/null || return 2

    # Create action_log entry
    local session_id
    session_id=$(_chaos_get_session_id "$actor")
    local al_id="al-$(openssl rand -hex 4)"

    sqlite3 "$db_path" "INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp)
        VALUES ('$al_id', '$session_id', 'soft_delete', 'notes', '$note_id', '{}', '$now');" 2>/dev/null || true

    CHAOS_DELETED_NOTE_IDS+=("$note_id")
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "note_delete: $note_id by $actor"
    return 0
}
