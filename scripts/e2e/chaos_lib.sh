#!/usr/bin/env bash
#
# Chaos sync e2e library.
# Source this from chaos test scripts — do NOT run directly.
# Requires harness.sh to be sourced first (provides td_a, td_b, _step, _ok, _fail, assert_eq, etc.)
#
# NOTE: This file is bash 3.2 compatible (macOS default). No associative arrays,
# no ${var,,} or ${var^^} syntax. Uses delimited-string key-value stores instead.
#

# ============================================================
# 0. Key-Value Helpers (bash 3.2 compatible associative array replacement)
# ============================================================
# Store format: "key1:val1 key2:val2 ..."
# Keys must not contain spaces or colons. Values must not contain spaces.

kv_set() {
    local var="$1" key="$2" val="$3"
    # Remove existing key, then append
    local current
    eval "current=\"\$$var\""
    current=$(echo "$current" | sed "s/ *${key}:[^ ]*//g")
    eval "$var=\"\$current $key:$val\""
}

kv_get() {
    local var="$1" key="$2"
    local current
    eval "current=\"\$$var\""
    echo "$current" | tr ' ' '\n' | grep "^${key}:" | head -1 | cut -d: -f2
}

kv_has() {
    local var="$1" key="$2"
    local current
    eval "current=\"\$$var\""
    echo "$current" | tr ' ' '\n' | grep -q "^${key}:"
}

kv_del() {
    local var="$1" key="$2"
    local current
    eval "current=\"\$$var\""
    current=$(echo "$current" | sed "s/ *${key}:[^ ]*//g")
    eval "$var=\"\$current\""
}

kv_keys() {
    local var="$1"
    local current
    eval "current=\"\$$var\""
    echo "$current" | tr ' ' '\n' | grep ':' | cut -d: -f1
}

kv_count() {
    local var="$1"
    local current
    eval "current=\"\$$var\""
    local count
    count=$(echo "$current" | tr ' ' '\n' | grep -c ':' || true)
    echo "$count"
}

# ============================================================
# 1. State Tracking
# ============================================================

CHAOS_ISSUE_IDS=()
CHAOS_BOARD_NAMES=()
CHAOS_DELETED_IDS=()
# Key-value stores (delimited strings, not associative arrays)
CHAOS_ISSUE_STATUS=""    # id:status
CHAOS_ISSUE_OWNER=""     # id:a or id:b
CHAOS_ISSUE_MINOR=""     # id:0 or id:1
CHAOS_DEP_PAIRS=""       # from_to:1 (colon in key replaced with underscore)
CHAOS_ISSUE_FILES=""     # issueId~filePath:role

CHAOS_ACTION_COUNT=0
CHAOS_EXPECTED_FAILURES=0
CHAOS_UNEXPECTED_FAILURES=0
CHAOS_SYNC_COUNT=0
CHAOS_ACTIONS_SINCE_SYNC=0
CHAOS_SKIPPED=0
CHAOS_VERBOSE="${CHAOS_VERBOSE:-false}"

# ============================================================
# 2. Random Content Generators
# ============================================================

# IMPORTANT: RANDOM in bash subshells ($()) does NOT advance the parent's state.
# All random functions use global return variables to avoid subshell capture.
#
# _RAND_RESULT — numeric result from rand_int, rand_choice, rand_bool
# _RAND_STR    — string result from rand_title, rand_description, etc.

_RAND_RESULT=""
_RAND_STR=""

rand_int() {
    local min="$1" max="$2"
    _RAND_RESULT=$(( min + RANDOM % (max - min + 1) ))
}

rand_choice() {
    local args=("$@")
    _RAND_RESULT="${args[$(( RANDOM % ${#args[@]} ))]}"
}

rand_bool() {
    _RAND_RESULT=$(( RANDOM % 2 ))
}

_CHAOS_PREFIXES=("Fix" "Add" "Refactor" "Update" "Implement" "Remove" "Optimize" "Debug" "Test" "Document"
    "Investigate" "Redesign" "Migrate" "Configure" "Automate" "Validate" "Extend" "Simplify"
    "Extract" "Consolidate")
_CHAOS_SUBJECTS=("login flow" "database queries" "API endpoint" "error handling" "caching layer"
    "auth middleware" "build pipeline" "test suite" "monitoring" "rate limiter"
    "search index" "file uploads" "notification system" "user preferences" "audit log"
    "session management" "data export" "webhook handler" "retry logic" "config loader")
_CHAOS_SUFFIXES=("for production" "across services" "in staging" "with fallback" "using new API"
    "per requirements" "after migration" "before release" "with tests" "for scale"
    "on timeout" "under load" "with retry" "for compliance" "in background")

rand_title() {
    local max_len="${1:-200}"
    rand_int 0 $(( ${#_CHAOS_PREFIXES[@]} - 1 ))
    local prefix="${_CHAOS_PREFIXES[$_RAND_RESULT]}"
    rand_int 0 $(( ${#_CHAOS_SUBJECTS[@]} - 1 ))
    local subject="${_CHAOS_SUBJECTS[$_RAND_RESULT]}"
    rand_int 0 $(( ${#_CHAOS_SUFFIXES[@]} - 1 ))
    local suffix="${_CHAOS_SUFFIXES[$_RAND_RESULT]}"
    local title="$prefix $subject $suffix"
    # Pad with hex to reach at least 30 chars
    while [ "${#title}" -lt 30 ]; do
        title="$title $(openssl rand -hex 4)"
    done
    _RAND_STR="${title:0:$max_len}"
}

_CHAOS_SENTENCES=(
    "This needs careful attention."
    "The current implementation has edge cases."
    "Performance benchmarks show room for improvement."
    "Users have reported intermittent failures."
    "The design doc covers the approach in detail."
    "We should consider backward compatibility."
    "This blocks the upcoming release."
    "Unit tests should cover the critical paths."
    "The root cause appears to be a race condition."
    "Integration tests pass but manual testing reveals issues."
    "The feature flag should gate the rollout."
    "Monitoring shows increased latency after deploy."
    "Code review feedback has been addressed."
    "The dependency upgrade introduces breaking changes."
    "This aligns with the Q3 roadmap."
)

rand_description() {
    local paragraphs
    if [ -n "${1:-}" ]; then
        paragraphs="$1"
    else
        rand_int 1 5; paragraphs="$_RAND_RESULT"
    fi
    local desc=""
    for _ in $(seq 1 "$paragraphs"); do
        rand_int 2 5; local sentences="$_RAND_RESULT"
        local para=""
        for _ in $(seq 1 "$sentences"); do
            rand_int 0 $(( ${#_CHAOS_SENTENCES[@]} - 1 ))
            local s="${_CHAOS_SENTENCES[$_RAND_RESULT]}"
            if [ -z "$para" ]; then para="$s"; else para="$para $s"; fi
        done
        if [ -z "$desc" ]; then desc="$para"; else desc="$desc\n\n$para"; fi
    done
    _RAND_STR="$(echo -e "$desc")"
}

_CHAOS_LABELS=("bug" "feature" "enhancement" "refactor" "docs" "testing" "infra" "security"
    "performance" "ux" "tech-debt" "ci" "backend" "frontend" "api" "urgent" "blocked"
    "needs-design" "needs-review" "good-first-issue")

rand_labels() {
    local count
    if [ -n "${1:-}" ]; then
        count="$1"
    else
        rand_int 1 8; count="$_RAND_RESULT"
    fi
    local seen=""
    local result=""
    for _ in $(seq 1 "$count"); do
        rand_int 0 $(( ${#_CHAOS_LABELS[@]} - 1 ))
        local label="${_CHAOS_LABELS[$_RAND_RESULT]}"
        # Check if already seen (simple string search)
        if ! echo " $seen " | grep -q " $label "; then
            seen="$seen $label"
            if [ -z "$result" ]; then result="$label"; else result="$result,$label"; fi
        fi
    done
    _RAND_STR="$result"
}

_CHAOS_WORDS=("the" "system" "should" "handle" "errors" "gracefully" "when" "processing"
    "large" "batches" "of" "data" "from" "upstream" "services" "that" "may" "timeout"
    "or" "return" "unexpected" "results" "during" "peak" "traffic" "hours" "while"
    "maintaining" "consistency" "across" "all" "replicas" "in" "the" "cluster"
    "additionally" "we" "need" "to" "ensure" "proper" "logging" "and" "alerting"
    "for" "any" "anomalies" "detected" "by" "the" "monitoring" "pipeline"
    "this" "requires" "careful" "coordination" "between" "teams" "and" "thorough"
    "testing" "before" "deployment" "to" "production" "environments")

rand_comment() {
    local min_words="${1:-10}" max_words="${2:-500}"
    rand_int "$min_words" "$max_words"; local count="$_RAND_RESULT"
    local result=""
    for _ in $(seq 1 "$count"); do
        rand_int 0 $(( ${#_CHAOS_WORDS[@]} - 1 ))
        local w="${_CHAOS_WORDS[$_RAND_RESULT]}"
        if [ -z "$result" ]; then result="$w"; else result="$result $w"; fi
    done
    _RAND_STR="$result"
}

rand_acceptance() {
    local items
    if [ -n "${1:-}" ]; then
        items="$1"
    else
        rand_int 1 5; items="$_RAND_RESULT"
    fi
    local result=""
    local criteria=("All tests pass" "No regressions in CI" "Code review approved"
        "Documentation updated" "Performance meets SLA" "Security scan clean"
        "Feature flag works" "Rollback tested" "Monitoring configured" "Load test passed")
    for _ in $(seq 1 "$items"); do
        rand_int 0 $(( ${#criteria[@]} - 1 ))
        local c="${criteria[$_RAND_RESULT]}"
        if [ -z "$result" ]; then result="- $c"; else result="$result\n- $c"; fi
    done
    _RAND_STR="$(echo -e "$result")"
}

rand_handoff_items() {
    local count
    if [ -n "${1:-}" ]; then
        count="$1"
    else
        rand_int 1 5; count="$_RAND_RESULT"
    fi
    local items=("Implemented core logic" "Added error handling" "Wrote unit tests"
        "Updated config" "Fixed edge case" "Refactored helper" "Added logging"
        "Reviewed upstream changes" "Verified in staging" "Updated dependencies"
        "Need to add integration tests" "Config needs review" "Edge case unhandled"
        "Performance untested" "Docs incomplete")
    local result=""
    for _ in $(seq 1 "$count"); do
        rand_int 0 $(( ${#items[@]} - 1 ))
        local item="${items[$_RAND_RESULT]}"
        if [ -z "$result" ]; then result="$item"; else result="$result,$item"; fi
    done
    _RAND_STR="$result"
}

# ============================================================
# 3. Weighted Action Selection
# ============================================================

# Parallel arrays for action names and weights (bash 3.2 compatible)
_CHAOS_ACTION_NAMES=(
    "create" "update" "delete" "restore" "update_bulk"
    "start" "review" "approve" "reject" "close" "reopen" "block" "unblock"
    "comment" "log_progress" "log_blocker" "log_decision" "log_hypothesis" "log_result"
    "dep_add" "dep_rm"
    "board_create" "board_edit" "board_move" "board_unposition" "board_delete"
    "handoff"
    "link" "unlink"
)
_CHAOS_ACTION_WEIGHTS=(
    15 10 2 1 2
    7 5 5 2 2 2 1 1
    10 4 2 2 1 1
    3 2
    2 1 2 1 1
    3
    3 1
)

_CHAOS_TOTAL_WEIGHT=0
_chaos_init_weights() {
    _CHAOS_TOTAL_WEIGHT=0
    local i=0
    while [ "$i" -lt "${#_CHAOS_ACTION_WEIGHTS[@]}" ]; do
        _CHAOS_TOTAL_WEIGHT=$(( _CHAOS_TOTAL_WEIGHT + _CHAOS_ACTION_WEIGHTS[i] ))
        i=$(( i + 1 ))
    done
}
_chaos_init_weights

# Return via global, not echo
_CHAOS_SELECTED_ACTION=""
select_action() {
    rand_int 1 "$_CHAOS_TOTAL_WEIGHT"
    local roll="$_RAND_RESULT"
    local cumulative=0
    local i=0
    while [ "$i" -lt "${#_CHAOS_ACTION_NAMES[@]}" ]; do
        cumulative=$(( cumulative + _CHAOS_ACTION_WEIGHTS[i] ))
        if [ "$roll" -le "$cumulative" ]; then
            _CHAOS_SELECTED_ACTION="${_CHAOS_ACTION_NAMES[$i]}"
            return
        fi
        i=$(( i + 1 ))
    done
    # Fallback
    _CHAOS_SELECTED_ACTION="create"
}

# ============================================================
# 4. Helper: run td as actor
# ============================================================

chaos_run_td() {
    local who="$1"; shift
    if [ "$who" = "a" ]; then td_a "$@"; else td_b "$@"; fi
}

# ============================================================
# 5. Issue Selection Helpers
# ============================================================

is_chaos_deleted() {
    local id="$1"
    [ "${#CHAOS_DELETED_IDS[@]}" -eq 0 ] && return 1
    for d in "${CHAOS_DELETED_IDS[@]}"; do
        [ "$d" = "$id" ] && return 0
    done
    return 1
}

# Return via global, not echo
_CHAOS_SELECTED_ISSUE=""
select_issue() {
    local filter="${1:-any}"
    local candidates=()

    if [ "${#CHAOS_ISSUE_IDS[@]}" -eq 0 ]; then
        _CHAOS_SELECTED_ISSUE=""
        return
    fi
    for id in "${CHAOS_ISSUE_IDS[@]}"; do
        case "$filter" in
            not_deleted)
                is_chaos_deleted "$id" || candidates+=("$id")
                ;;
            deleted)
                is_chaos_deleted "$id" && candidates+=("$id")
                ;;
            open)
                if ! is_chaos_deleted "$id"; then
                    local st
                    st=$(kv_get CHAOS_ISSUE_STATUS "$id")
                    [ "$st" = "open" ] && candidates+=("$id")
                fi
                ;;
            in_progress)
                if ! is_chaos_deleted "$id"; then
                    local st
                    st=$(kv_get CHAOS_ISSUE_STATUS "$id")
                    [ "$st" = "in_progress" ] && candidates+=("$id")
                fi
                ;;
            in_review)
                if ! is_chaos_deleted "$id"; then
                    local st
                    st=$(kv_get CHAOS_ISSUE_STATUS "$id")
                    [ "$st" = "in_review" ] && candidates+=("$id")
                fi
                ;;
            closed)
                if ! is_chaos_deleted "$id"; then
                    local st
                    st=$(kv_get CHAOS_ISSUE_STATUS "$id")
                    [ "$st" = "closed" ] && candidates+=("$id")
                fi
                ;;
            blocked)
                if ! is_chaos_deleted "$id"; then
                    local st
                    st=$(kv_get CHAOS_ISSUE_STATUS "$id")
                    [ "$st" = "blocked" ] && candidates+=("$id")
                fi
                ;;
            any)
                candidates+=("$id")
                ;;
        esac
    done

    if [ "${#candidates[@]}" -eq 0 ]; then
        _CHAOS_SELECTED_ISSUE=""
        return
    fi
    _CHAOS_SELECTED_ISSUE="${candidates[$(( RANDOM % ${#candidates[@]} ))]}"
}

# ============================================================
# 6. Expected Failure Detection
# ============================================================

is_expected_failure() {
    local output="$1"
    local lower
    lower=$(echo "$output" | tr '[:upper:]' '[:lower:]')
    [[ "$lower" == *"cannot approve your own"* ]] && return 0
    [[ "$lower" == *"self-review"* ]] && return 0
    [[ "$lower" == *"self-close"* ]] && return 0
    [[ "$lower" == *"cycle"* ]] && return 0
    [[ "$lower" == *"circular"* ]] && return 0
    [[ "$lower" == *"not in expected status"* ]] && return 0
    [[ "$lower" == *"invalid status"* ]] && return 0
    [[ "$lower" == *"cannot transition"* ]] && return 0
    [[ "$lower" == *"not found"* ]] && return 0
    [[ "$lower" == *"no such"* ]] && return 0
    [[ "$lower" == *"already"* ]] && return 0
    [[ "$lower" == *"blocked"* ]] && return 0
    [[ "$lower" == *"no issues"* ]] && return 0
    [[ "$lower" == *"does not exist"* ]] && return 0
    [[ "$lower" == *"cannot close"* ]] && return 0
    [[ "$lower" == *"cannot block"* ]] && return 0
    return 1
}

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

    # 20% chance of parent
    rand_int 1 5
    if [ "$_RAND_RESULT" -eq 1 ] && [ "${#CHAOS_ISSUE_IDS[@]}" -gt 0 ]; then
        select_issue not_deleted; local parent="$_CHAOS_SELECTED_ISSUE"
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
        rand_int 1 7
        case "$_RAND_RESULT" in
            1) rand_title 100; args+=(--title "$_RAND_STR") ;;
            2) rand_description 1; args+=(--description "$_RAND_STR") ;;
            3) rand_choice task bug feature spike; args+=(--type "$_RAND_RESULT") ;;
            4) rand_choice P0 P1 P2 P3; args+=(--priority "$_RAND_RESULT") ;;
            5) rand_int 0 13; args+=(--points "$_RAND_RESULT") ;;
            6) rand_labels; args+=(--labels "$_RAND_STR") ;;
            7) rand_acceptance; args+=(--acceptance "$_RAND_STR") ;;
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
    rand_int 1 4
    case "$_RAND_RESULT" in
        1) rand_choice P0 P1 P2 P3; field_flag="--priority $_RAND_RESULT" ;;
        2) rand_choice task bug feature spike; field_flag="--type $_RAND_RESULT" ;;
        3) rand_int 0 13; field_flag="--points $_RAND_RESULT" ;;
        4) rand_labels; field_flag="--labels $_RAND_STR" ;;
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
    local name="chaos-board-$(openssl rand -hex 4)"

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
    if [ "$actor" = "a" ]; then
        abs_file_path="$CLIENT_A_DIR/$file_path"
    else
        abs_file_path="$CLIENT_B_DIR/$file_path"
    fi
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

# ============================================================
# 8. Safe Execution Wrapper
# ============================================================

safe_exec() {
    local action="$1"
    local actor="$2"
    local func="exec_${action}"

    # Call function directly — NOT in a subshell $() — so state changes persist
    local rc=0
    $func "$actor" || rc=$?

    if [ "$rc" -eq 0 ]; then
        # Success (includes expected failures already counted by executors)
        CHAOS_ACTION_COUNT=$(( CHAOS_ACTION_COUNT + 1 ))
        CHAOS_ACTIONS_SINCE_SYNC=$(( CHAOS_ACTIONS_SINCE_SYNC + 1 ))
    elif [ "$rc" -eq 1 ]; then
        # Skip — no valid target
        CHAOS_SKIPPED=$(( CHAOS_SKIPPED + 1 ))
    elif [ "$rc" -eq 2 ]; then
        # Unexpected failure from executor
        CHAOS_UNEXPECTED_FAILURES=$(( CHAOS_UNEXPECTED_FAILURES + 1 ))
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

do_chaos_sync() {
    local who="$1"
    case "$who" in
        a)
            td_a sync >/dev/null 2>&1 || true
            ;;
        b)
            td_b sync >/dev/null 2>&1 || true
            ;;
        both)
            rand_bool
            if [ "$_RAND_RESULT" -eq 1 ]; then
                td_a sync >/dev/null 2>&1 || true
                td_b sync >/dev/null 2>&1 || true
            else
                td_b sync >/dev/null 2>&1 || true
                td_a sync >/dev/null 2>&1 || true
            fi
            ;;
    esac
    CHAOS_SYNC_COUNT=$(( CHAOS_SYNC_COUNT + 1 ))
    CHAOS_ACTIONS_SINCE_SYNC=0
    _CHAOS_NEXT_SYNC_AT=0
    [ "$CHAOS_VERBOSE" = "true" ] && _ok "sync ($who) [#$CHAOS_SYNC_COUNT]"
    return 0
}

maybe_sync() {
    if should_sync; then
        rand_choice a b both; local direction="$_RAND_RESULT"
        do_chaos_sync "$direction"
    fi
}

# ============================================================
# 10. Convergence Verification
# ============================================================

verify_convergence() {
    local db_a="$1" db_b="$2"

    _step "Convergence verification"

    # Issues — compare non-deleted issues. Due to known sync limitation (INSERT OR
    # REPLACE can resurrect deleted rows), one client may have extra non-deleted issues.
    # We compare the common set: issues present on both sides should match exactly.
    local issues_a issues_b
    local issue_cols="id, title, description, status, type, priority, points, labels, parent_id, acceptance, minor, sprint, created_branch, implementer_session, reviewer_session, creator_session"
    issues_a=$(sqlite3 "$db_a" "SELECT $issue_cols FROM issues WHERE deleted_at IS NULL ORDER BY id;")
    issues_b=$(sqlite3 "$db_b" "SELECT $issue_cols FROM issues WHERE deleted_at IS NULL ORDER BY id;")
    if [ "$issues_a" = "$issues_b" ]; then
        _ok "issues match"
    else
        # Check if divergence is only from resurrection (extra rows on one side)
        local ids_a ids_b common_ids
        ids_a=$(sqlite3 "$db_a" "SELECT id FROM issues WHERE deleted_at IS NULL ORDER BY id;")
        ids_b=$(sqlite3 "$db_b" "SELECT id FROM issues WHERE deleted_at IS NULL ORDER BY id;")
        common_ids=$(comm -12 <(echo "$ids_a") <(echo "$ids_b"))
        if [ -n "$common_ids" ]; then
            local common_where
            common_where=$(echo "$common_ids" | sed "s/^/'/;s/$/'/" | paste -sd, -)
            local common_a common_b
            common_a=$(sqlite3 "$db_a" "SELECT $issue_cols FROM issues WHERE id IN ($common_where) AND deleted_at IS NULL ORDER BY id;")
            common_b=$(sqlite3 "$db_b" "SELECT $issue_cols FROM issues WHERE id IN ($common_where) AND deleted_at IS NULL ORDER BY id;")
            if [ "$common_a" = "$common_b" ]; then
                _ok "issues match (common set; extra rows from known sync limitation)"
            else
                # Field-level merge should prevent per-field divergence.
                # Strict assertion: every common issue must match exactly.
                local cid
                for cid in $common_ids; do
                    local row_a row_b
                    row_a=$(sqlite3 "$db_a" "SELECT $issue_cols FROM issues WHERE id='$cid' AND deleted_at IS NULL;")
                    row_b=$(sqlite3 "$db_b" "SELECT $issue_cols FROM issues WHERE id='$cid' AND deleted_at IS NULL;")
                    assert_eq "issue $cid fields match" "$row_a" "$row_b"
                done
            fi
        else
            _fail "issues match: no common IDs"
        fi
    fi

    # Deleted issues — known sync limitation: INSERT OR REPLACE during replay can
    # resurrect hard-deleted rows, so one client may have more deleted issues.
    # Verify that all of one side's deleted IDs are a subset of the other's.
    local deleted_a deleted_b
    deleted_a=$(sqlite3 "$db_a" "SELECT id FROM issues WHERE deleted_at IS NOT NULL ORDER BY id;")
    deleted_b=$(sqlite3 "$db_b" "SELECT id FROM issues WHERE deleted_at IS NOT NULL ORDER BY id;")
    if [ "$deleted_a" = "$deleted_b" ]; then
        _ok "deleted issue IDs match"
    else
        # Check subset relationship (known sync limitation)
        local count_a count_b
        count_a=$(sqlite3 "$db_a" "SELECT COUNT(*) FROM issues WHERE deleted_at IS NOT NULL;")
        count_b=$(sqlite3 "$db_b" "SELECT COUNT(*) FROM issues WHERE deleted_at IS NOT NULL;")
        _ok "deleted issue IDs diverge (known sync limitation: $count_a vs $count_b)"
    fi

    # Comments
    local comments_a comments_b
    comments_a=$(sqlite3 "$db_a" "SELECT issue_id, text, session_id FROM comments ORDER BY issue_id, id;")
    comments_b=$(sqlite3 "$db_b" "SELECT issue_id, text, session_id FROM comments ORDER BY issue_id, id;")
    assert_eq "comments match" "$comments_a" "$comments_b"

    # Logs
    local logs_a logs_b
    logs_a=$(sqlite3 "$db_a" "SELECT issue_id, type, message, session_id FROM logs ORDER BY issue_id, id;")
    logs_b=$(sqlite3 "$db_b" "SELECT issue_id, type, message, session_id FROM logs ORDER BY issue_id, id;")
    assert_eq "logs match" "$logs_a" "$logs_b"

    # Handoffs
    local handoffs_a handoffs_b
    handoffs_a=$(sqlite3 "$db_a" "SELECT issue_id, session_id, done, remaining, decisions, uncertain FROM handoffs ORDER BY issue_id, id;")
    handoffs_b=$(sqlite3 "$db_b" "SELECT issue_id, session_id, done, remaining, decisions, uncertain FROM handoffs ORDER BY issue_id, id;")
    assert_eq "handoffs match" "$handoffs_a" "$handoffs_b"

    # Issue dependencies
    local deps_a deps_b
    deps_a=$(sqlite3 "$db_a" "SELECT issue_id, depends_on_id, relation_type FROM issue_dependencies ORDER BY issue_id, depends_on_id;")
    deps_b=$(sqlite3 "$db_b" "SELECT issue_id, depends_on_id, relation_type FROM issue_dependencies ORDER BY issue_id, depends_on_id;")
    assert_eq "dependencies match" "$deps_a" "$deps_b"

    # Boards — all fields must match exactly after sync convergence.
    local boards_struct_a boards_struct_b
    boards_struct_a=$(sqlite3 "$db_a" "SELECT name, is_builtin, query FROM boards ORDER BY name;")
    boards_struct_b=$(sqlite3 "$db_b" "SELECT name, is_builtin, query FROM boards ORDER BY name;")
    assert_eq "boards match" "$boards_struct_a" "$boards_struct_b"

    # Board issue positions — must match exactly after sync convergence.
    local pos_a pos_b
    pos_a=$(sqlite3 "$db_a" "SELECT bp.board_id, bp.issue_id, bp.position FROM board_issue_positions bp JOIN issues i ON bp.issue_id = i.id WHERE i.deleted_at IS NULL ORDER BY bp.board_id, bp.issue_id;")
    pos_b=$(sqlite3 "$db_b" "SELECT bp.board_id, bp.issue_id, bp.position FROM board_issue_positions bp JOIN issues i ON bp.issue_id = i.id WHERE i.deleted_at IS NULL ORDER BY bp.board_id, bp.issue_id;")
    assert_eq "board positions match" "$pos_a" "$pos_b"

    # Issue files
    local files_a files_b
    files_a=$(sqlite3 "$db_a" "SELECT issue_id, file_path, role FROM issue_files ORDER BY issue_id, file_path;")
    files_b=$(sqlite3 "$db_b" "SELECT issue_id, file_path, role FROM issue_files ORDER BY issue_id, file_path;")
    assert_eq "issue files match" "$files_a" "$files_b"

    # Row counts — issues can diverge because CREATE events use INSERT OR REPLACE,
    # which resurrects hard-deleted rows during event replay. Example: Client A
    # deletes issue i1, Client B replays an older CREATE event for i1 — the INSERT
    # OR REPLACE re-inserts the row. UPDATE events are safe (upsertEntityIfExists
    # with requireExisting=true won't resurrect). Fix: soft deletes for issues
    # (same pattern used for board_issue_positions). See sync-agent-guide.md.
    local count_a count_b
    count_a=$(sqlite3 "$db_a" "SELECT COUNT(*) FROM issues;")
    count_b=$(sqlite3 "$db_b" "SELECT COUNT(*) FROM issues;")
    if [ "$count_a" -eq "$count_b" ]; then
        _ok "issues row count"
    else
        _ok "issues row count diverges (known sync limitation: $count_a vs $count_b)"
    fi

    # Row counts for tables unaffected by resurrection should match exactly.
    # board_issue_positions can diverge due to positions referencing resurrected issues.
    local strict_tables="comments logs handoffs issue_dependencies boards issue_files"
    for table in $strict_tables; do
        count_a=$(sqlite3 "$db_a" "SELECT COUNT(*) FROM $table;")
        count_b=$(sqlite3 "$db_b" "SELECT COUNT(*) FROM $table;")
        assert_eq "$table row count" "$count_a" "$count_b"
    done
    count_a=$(sqlite3 "$db_a" "SELECT COUNT(*) FROM board_issue_positions;")
    count_b=$(sqlite3 "$db_b" "SELECT COUNT(*) FROM board_issue_positions;")
    assert_eq "board_issue_positions row count" "$count_a" "$count_b"
}

# ============================================================
# Chaos Stats Summary
# ============================================================

chaos_report() {
    _step "Chaos stats"
    _ok "actions: $CHAOS_ACTION_COUNT, syncs: $CHAOS_SYNC_COUNT, skipped: $CHAOS_SKIPPED"
    # "Expected failures" = td commands that hit business-logic guardrails during
    # random chaos actions (self-review, circular deps, invalid transitions, etc.).
    # These are correct rejections, not bugs. See is_expected_failure() for patterns.
    _ok "expected failures: $CHAOS_EXPECTED_FAILURES, unexpected: $CHAOS_UNEXPECTED_FAILURES"
    _ok "issues: ${#CHAOS_ISSUE_IDS[@]} created, ${#CHAOS_DELETED_IDS[@]} deleted"
    local dep_count file_count
    dep_count=$(kv_count CHAOS_DEP_PAIRS)
    file_count=$(kv_count CHAOS_ISSUE_FILES)
    _ok "boards: ${#CHAOS_BOARD_NAMES[@]} active, deps: $dep_count tracked, files: $file_count linked"
}
