#!/usr/bin/env bash
#
# Chaos sync e2e library - Core module.
# Contains: KV helpers, state tracking, random generators, action selection, issue selection.
# Source this from other chaos_lib modules.
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
CHAOS_PARENT_CHILDREN="" # parentId_childId:1
CHAOS_ISSUE_FILES=""     # issueId~filePath:role
CHAOS_ACTIVE_WS_A=""     # Active work session name for actor a
CHAOS_ACTIVE_WS_B=""     # Active work session name for actor b
CHAOS_ACTIVE_WS_C=""     # Active work session name for actor c
CHAOS_WS_TAGGED_A=""     # KV: issueId:1 for issues tagged in actor a's session
CHAOS_WS_TAGGED_B=""     # KV: issueId:1 for issues tagged in actor b's session
CHAOS_WS_TAGGED_C=""     # KV: issueId:1 for issues tagged in actor c's session
CHAOS_NOTE_IDS=()        # Array of note IDs
CHAOS_DELETED_NOTE_IDS=() # Array of soft-deleted note IDs

CHAOS_ACTION_COUNT=0
CHAOS_EXPECTED_FAILURES=0
CHAOS_UNEXPECTED_FAILURES=0
CHAOS_SYNC_COUNT=0
CHAOS_ACTIONS_SINCE_SYNC=0
CHAOS_SKIPPED=0
CHAOS_FIELD_COLLISIONS=0
CHAOS_DELETE_MUTATE_CONFLICTS=0
CHAOS_BURST_COUNT=0
CHAOS_BURST_ACTIONS=0
CHAOS_CASCADE_ACTIONS=0
CHAOS_INJECTED_FAILURES=0
CHAOS_INJECT_FAILURES="${CHAOS_INJECT_FAILURES:-false}"
CHAOS_INJECT_FAILURE_RATE="${CHAOS_INJECT_FAILURE_RATE:-7}"  # percentage (5-10% range)
CHAOS_VERBOSE="${CHAOS_VERBOSE:-false}"

# Per-action-type counters (KV stores: actionName:count)
CHAOS_PER_ACTION_OK=""         # action_name:count
CHAOS_PER_ACTION_EXPFAIL=""    # action_name:count
CHAOS_PER_ACTION_UNEXPFAIL=""  # action_name:count

# Timing tracking (epoch seconds)
CHAOS_TIME_START=0
CHAOS_TIME_END=0
CHAOS_TIME_SYNCING=0           # cumulative seconds spent in sync
CHAOS_TIME_MUTATING=0          # cumulative seconds spent in mutations
_CHAOS_PHASE_START=0           # temp: start of current phase

# Convergence results (populated by verify_convergence_tracked)
CHAOS_CONVERGENCE_RESULTS=""   # table_name:pass or table_name:fail
CHAOS_CONVERGENCE_PASSED=0
CHAOS_CONVERGENCE_FAILED=0
CHAOS_CASCADE_VERIFY_FAILURES=0

# Mid-test convergence checks (periodic verification during chaos)
CHAOS_MID_TEST_CONVERGENCE_CHECKS=0
CHAOS_MID_TEST_CONVERGENCE_PASSES=0
CHAOS_MID_TEST_CONVERGENCE_FAILURES=0
CHAOS_MID_TEST_CHECK_INTERVAL="${CHAOS_MID_TEST_CHECK_INTERVAL:-25}"  # check every N actions
CHAOS_MID_TEST_CHECKS_ENABLED="${CHAOS_MID_TEST_CHECKS_ENABLED:-true}"
_CHAOS_LAST_CONVERGENCE_CHECK_AT=0

# ============================================================
# 1b. Per-Action & Timing Helpers
# ============================================================

_chaos_inc_action_ok() {
    local action="$1"
    local cur
    cur=$(kv_get CHAOS_PER_ACTION_OK "$action" || true)
    cur=${cur:-0}
    kv_set CHAOS_PER_ACTION_OK "$action" "$(( cur + 1 ))"
}

_chaos_inc_action_expfail() {
    local action="$1"
    local cur
    cur=$(kv_get CHAOS_PER_ACTION_EXPFAIL "$action" || true)
    cur=${cur:-0}
    kv_set CHAOS_PER_ACTION_EXPFAIL "$action" "$(( cur + 1 ))"
}

_chaos_inc_action_unexpfail() {
    local action="$1"
    local cur
    cur=$(kv_get CHAOS_PER_ACTION_UNEXPFAIL "$action" || true)
    cur=${cur:-0}
    kv_set CHAOS_PER_ACTION_UNEXPFAIL "$action" "$(( cur + 1 ))"
}

_chaos_timer_start() {
    _CHAOS_PHASE_START=$(date +%s)
}

_chaos_timer_stop_sync() {
    local now
    now=$(date +%s)
    CHAOS_TIME_SYNCING=$(( CHAOS_TIME_SYNCING + now - _CHAOS_PHASE_START ))
}

_chaos_timer_stop_mutate() {
    local now
    now=$(date +%s)
    CHAOS_TIME_MUTATING=$(( CHAOS_TIME_MUTATING + now - _CHAOS_PHASE_START ))
}

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

# ---- Edge-case data for adversarial testing ----
# These strings exercise serialization, SQL escaping, unicode handling, and boundary conditions.
# NOTE: null bytes (\x00) are omitted because bash cannot store them in variables.

_CHAOS_EDGE_STRINGS=()

# Empty and minimal
_CHAOS_EDGE_STRINGS+=("")                          # empty string
_CHAOS_EDGE_STRINGS+=("x")                         # single character

# Very long string (1200 chars of 'A')
_CHAOS_LONG_STR="$(printf 'A%.0s' $(seq 1 1200))"
_CHAOS_EDGE_STRINGS+=("$_CHAOS_LONG_STR")

# Unicode: emoji
_CHAOS_EDGE_STRINGS+=("🔥🐛✅🚀💀🎉")

# Unicode: CJK characters
_CHAOS_EDGE_STRINGS+=("测试中文数据处理")

# Unicode: RTL (Arabic)
_CHAOS_EDGE_STRINGS+=("مرحبا بالعالم")

# Strings with newlines
_CHAOS_EDGE_STRINGS+=("line one
line two
line three")

# Single quotes
_CHAOS_EDGE_STRINGS+=("it's a test with 'single quotes'")

# Double quotes
_CHAOS_EDGE_STRINGS+=('she said "hello world"')

# Backslashes
_CHAOS_EDGE_STRINGS+=('path\\to\\file and \\n not a newline')

# SQL injection attempt
_CHAOS_EDGE_STRINGS+=("'; DROP TABLE issues; --")

# More SQL special chars
_CHAOS_EDGE_STRINGS+=('Robert"); DELETE FROM sync_events WHERE ("1"="1')

# Mixed unicode + special chars
_CHAOS_EDGE_STRINGS+=("emoji🔥 with 'quotes' and \"doubles\" and \\backslash")

# Whitespace variants: tabs and trailing spaces
_CHAOS_EDGE_STRINGS+=("	tabs	and   spaces   ")

# Percent and format strings
_CHAOS_EDGE_STRINGS+=("%s %d %x %n %%")

# HTML/XML-like content
_CHAOS_EDGE_STRINGS+=("<script>alert('xss')</script>")

# JSON-like content
_CHAOS_EDGE_STRINGS+=('{"key": "value", "nested": {"a": 1}}')

# Counter for edge-case usage in stats
CHAOS_EDGE_DATA_USED=0

# maybe_edge_data: ~15% chance of replacing _RAND_STR with an edge-case string.
# Call after any rand_* function to potentially override its output.
maybe_edge_data() {
    local roll=$(( RANDOM % 100 ))
    if [ "$roll" -lt 15 ]; then
        local idx=$(( RANDOM % ${#_CHAOS_EDGE_STRINGS[@]} ))
        _RAND_STR="${_CHAOS_EDGE_STRINGS[$idx]}"
        CHAOS_EDGE_DATA_USED=$((CHAOS_EDGE_DATA_USED + 1))
        return 0
    fi
    return 1
}

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
    # ~15% chance of edge-case data
    if maybe_edge_data; then
        # For titles, ensure non-empty (some commands reject empty titles)
        if [ -z "$_RAND_STR" ]; then
            _RAND_STR="edge-case-empty-$(( RANDOM % 1000 ))"
        fi
        _RAND_STR="${_RAND_STR:0:$max_len}"
        return
    fi
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
    # ~15% chance of edge-case data
    if maybe_edge_data; then return; fi
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
    # ~15% chance of edge-case data
    if maybe_edge_data; then return; fi
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
    # ~15% chance of edge-case data
    if maybe_edge_data; then return; fi
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
    # ~15% chance of edge-case data
    if maybe_edge_data; then return; fi
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
    "create" "update" "update_append" "delete" "restore" "update_bulk"
    "start" "unstart" "review" "approve" "reject" "close" "reopen" "bulk_start" "bulk_review" "bulk_close" "block" "unblock"
    "comment" "log_progress" "log_blocker" "log_decision" "log_hypothesis" "log_result"
    "dep_add" "dep_rm"
    "board_create" "board_edit" "board_move" "board_unposition" "board_delete" "board_view_mode"
    "handoff"
    "link" "unlink"
    "ws_start" "ws_tag" "ws_untag" "ws_end" "ws_handoff"
    "create_child" "cascade_handoff" "cascade_review"
    "note_create" "note_update" "note_delete"
)
_CHAOS_ACTION_WEIGHTS=(
    15 10 2 2 1 2
    7 1 5 5 2 2 2 1 1 1 1 1
    10 4 2 2 1 1
    3 2
    2 1 2 1 1 1
    3
    3 1
    2 3 1 1 1
    4 2 2
    5 3 1
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
    case "$who" in
        a) td_a "$@" ;;
        b) td_b "$@" ;;
        c) td_c "$@" ;;
        *) td_b "$@" ;;
    esac
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
    [[ "$lower" == *"no active"* ]] && return 0
    [[ "$lower" == *"no work session"* ]] && return 0
    [[ "$lower" == *"session"*"not found"* ]] && return 0
    return 1
}
