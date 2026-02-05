#!/usr/bin/env bash
#
# Large payload sync stress test: exercises sync with oversized data.
#
# Tests:
# 1. Large descriptions (10K+ characters)
# 2. Many comments (50+ per issue)
# 3. Many dependencies (20+ relationships)
# 4. Large labels arrays
# 5. Combined stress (large desc + many comments + many deps)
#
# Verifies no truncation occurs and sync performance with large payloads.
#
set -euo pipefail
source "$(dirname "$0")/harness.sh"
source "$(dirname "$0")/chaos_lib.sh"

# ---- Defaults ----
SEED=$$
VERBOSE=false
PAYLOAD_SIZE="normal"  # normal, large, xlarge
DESC_MIN_CHARS=10000
DESC_MAX_CHARS=50000
COMMENT_COUNT=50
DEP_COUNT=20
LABEL_COUNT=50
JSON_REPORT=""
REPORT_FILE=""

# ---- Test-specific counters ----
LP_LARGE_DESC_CREATED=0
LP_LARGE_DESC_VERIFIED=0
LP_MANY_COMMENTS_CREATED=0
LP_MANY_COMMENTS_VERIFIED=0
LP_MANY_DEPS_CREATED=0
LP_MANY_DEPS_VERIFIED=0
LP_COMBO_CREATED=0
LP_COMBO_VERIFIED=0
LP_TRUNCATION_ERRORS=0
LP_SYNC_TIMES=()

# ---- Usage ----
usage() {
    cat <<EOF
Usage: bash scripts/e2e/test_large_payload.sh [OPTIONS]

Large payload sync stress test: verifies sync handles oversized data correctly.
Tests large descriptions, many comments, many dependencies, and combinations.

Options:
  --seed N              RANDOM seed for reproducibility (default: \$\$)
  --verbose             Detailed per-action output (default: false)
  --payload-size SIZE   Payload size preset: normal, large, xlarge (default: normal)
                        normal: 10K desc, 50 comments, 20 deps
                        large:  25K desc, 100 comments, 40 deps
                        xlarge: 50K desc, 200 comments, 80 deps
  --desc-chars N        Custom description size in chars (overrides preset)
  --comment-count N     Custom comment count (overrides preset)
  --dep-count N         Custom dependency count (overrides preset)
  --label-count N       Custom label count (default: 50)
  --json-report PATH    Write JSON summary to file
  --report-file PATH    Write text report to file
  -h, --help            Show this help

Examples:
  # Quick smoke test with default sizes
  bash scripts/e2e/test_large_payload.sh --verbose

  # Standard large payload test
  bash scripts/e2e/test_large_payload.sh

  # Extra large stress test
  bash scripts/e2e/test_large_payload.sh --payload-size xlarge

  # Custom sizes
  bash scripts/e2e/test_large_payload.sh --desc-chars 100000 --comment-count 500

  # Reproducible run
  bash scripts/e2e/test_large_payload.sh --seed 42
EOF
}

# ---- Parse args ----
while [[ $# -gt 0 ]]; do
    case "$1" in
        --seed)           SEED="$2"; shift 2 ;;
        --verbose)        VERBOSE=true; shift ;;
        --payload-size)   PAYLOAD_SIZE="$2"; shift 2 ;;
        --desc-chars)     DESC_MIN_CHARS="$2"; DESC_MAX_CHARS="$2"; shift 2 ;;
        --comment-count)  COMMENT_COUNT="$2"; shift 2 ;;
        --dep-count)      DEP_COUNT="$2"; shift 2 ;;
        --label-count)    LABEL_COUNT="$2"; shift 2 ;;
        --json-report)    JSON_REPORT="$2"; shift 2 ;;
        --report-file)    REPORT_FILE="$2"; shift 2 ;;
        -h|--help)        usage; exit 0 ;;
        *) echo "Unknown arg: $1" >&2; usage; exit 1 ;;
    esac
done

# ---- Apply payload size preset ----
case "$PAYLOAD_SIZE" in
    normal)
        : "${DESC_MIN_CHARS:=10000}"
        : "${DESC_MAX_CHARS:=50000}"
        : "${COMMENT_COUNT:=50}"
        : "${DEP_COUNT:=20}"
        ;;
    large)
        DESC_MIN_CHARS=25000
        DESC_MAX_CHARS=75000
        COMMENT_COUNT=100
        DEP_COUNT=40
        ;;
    xlarge)
        DESC_MIN_CHARS=50000
        DESC_MAX_CHARS=150000
        COMMENT_COUNT=200
        DEP_COUNT=80
        ;;
    *)
        echo "Unknown payload size: $PAYLOAD_SIZE" >&2
        exit 1
        ;;
esac

# ---- Setup ----
HARNESS_ACTORS=2
export HARNESS_ACTORS
setup

# ---- Seed RANDOM for reproducibility ----
RANDOM=$SEED

# ---- Configure chaos_lib ----
CHAOS_VERBOSE="$VERBOSE"
CHAOS_SYNC_MODE="adaptive"

# ---- Initial sync ----
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true

# ---- Config summary ----
_step "Large payload sync test (seed: $SEED)"
echo "  Payload size preset:    $PAYLOAD_SIZE"
echo "  Description size:       $DESC_MIN_CHARS - $DESC_MAX_CHARS chars"
echo "  Comment count:          $COMMENT_COUNT per issue"
echo "  Dependency count:       $DEP_COUNT relationships"
echo "  Label count:            $LABEL_COUNT labels"

CHAOS_TIME_START=$(date +%s)

# ============================================================
# Helpers: Generate large content
# ============================================================

# Generate a large random description of specified size
rand_large_description() {
    local min_chars="${1:-$DESC_MIN_CHARS}"
    local max_chars="${2:-$DESC_MAX_CHARS}"

    rand_int "$min_chars" "$max_chars"
    local target_len="$_RAND_RESULT"

    local result=""
    local paragraph_count=0

    while [ "${#result}" -lt "$target_len" ]; do
        # Generate a paragraph
        local para=""
        rand_int 3 8
        local sentence_count="$_RAND_RESULT"

        for _ in $(seq 1 "$sentence_count"); do
            rand_int 0 $(( ${#_CHAOS_SENTENCES[@]} - 1 ))
            local sentence="${_CHAOS_SENTENCES[$_RAND_RESULT]}"
            if [ -z "$para" ]; then
                para="$sentence"
            else
                para="$para $sentence"
            fi
        done

        if [ -z "$result" ]; then
            result="$para"
        else
            result="$result

$para"
        fi
        paragraph_count=$((paragraph_count + 1))

        # Safety valve: don't loop forever
        [ "$paragraph_count" -gt 500 ] && break
    done

    # Truncate to max if we overshot
    _RAND_STR="${result:0:$max_chars}"
}

# Generate many comments for an issue
add_many_comments() {
    local actor="$1"
    local issue_id="$2"
    local count="${3:-$COMMENT_COUNT}"

    local added=0
    for i in $(seq 1 "$count"); do
        rand_comment 20 200
        local text="Comment #$i: $_RAND_STR"

        local rc=0
        chaos_run_td "$actor" comments add "$issue_id" "$text" >/dev/null 2>&1 || rc=$?
        if [ "$rc" -eq 0 ]; then
            added=$((added + 1))
        fi

        # Progress every 10 comments
        if [ "$VERBOSE" = "true" ] && [ $((i % 10)) -eq 0 ]; then
            _ok "added $i/$count comments to $issue_id"
        fi
    done

    _RAND_RESULT="$added"
}

# Create many issues for dependency testing
create_dep_target_issues() {
    local actor="$1"
    local count="${2:-$DEP_COUNT}"

    local created_ids=()
    for i in $(seq 1 "$count"); do
        rand_title 50
        local title="Dep target #$i: $_RAND_STR"

        local output rc=0
        output=$(chaos_run_td "$actor" create "$title" --type task 2>&1) || rc=$?
        if [ "$rc" -eq 0 ]; then
            local issue_id
            issue_id=$(echo "$output" | grep -oE 'td-[0-9a-f]+' | head -n1)
            if [ -n "$issue_id" ]; then
                created_ids+=("$issue_id")
                CHAOS_ISSUE_IDS+=("$issue_id")
                kv_set CHAOS_ISSUE_STATUS "$issue_id" "open"
            fi
        fi
    done

    # Return via global array
    _LP_DEP_TARGET_IDS=("${created_ids[@]}")
}
_LP_DEP_TARGET_IDS=()

# Add many dependencies to an issue
add_many_deps() {
    local actor="$1"
    local issue_id="$2"
    local targets=("${@:3}")

    local added=0
    for target_id in "${targets[@]}"; do
        [ "$issue_id" = "$target_id" ] && continue

        local rc=0
        chaos_run_td "$actor" dep add "$issue_id" "$target_id" >/dev/null 2>&1 || rc=$?
        if [ "$rc" -eq 0 ]; then
            added=$((added + 1))
            kv_set CHAOS_DEP_PAIRS "${issue_id}_${target_id}" "1"
        fi
    done

    _RAND_RESULT="$added"
}

# Generate many labels
rand_many_labels() {
    local count="${1:-$LABEL_COUNT}"
    local result=""

    # Use chaos labels plus generated ones
    for i in $(seq 1 "$count"); do
        local label=""
        if [ "$i" -le "${#_CHAOS_LABELS[@]}" ]; then
            label="${_CHAOS_LABELS[$((i - 1))]}"
        else
            label="label-$i-$(openssl rand -hex 4)"
        fi

        if [ -z "$result" ]; then
            result="$label"
        else
            result="$result,$label"
        fi
    done

    _RAND_STR="$result"
}

# Measure sync time
measure_sync() {
    local start_time end_time elapsed
    start_time=$(date +%s)

    td_a sync >/dev/null 2>&1 || true
    td_b sync >/dev/null 2>&1 || true
    sleep 1
    td_b sync >/dev/null 2>&1 || true
    td_a sync >/dev/null 2>&1 || true

    end_time=$(date +%s)
    elapsed=$((end_time - start_time))
    LP_SYNC_TIMES+=("$elapsed")

    [ "$VERBOSE" = "true" ] && _ok "sync round completed in ${elapsed}s"
    CHAOS_SYNC_COUNT=$((CHAOS_SYNC_COUNT + 4))
}

# ============================================================
# PHASE 1: Large Description Test
# ============================================================
_step "Phase 1: Large description test (${DESC_MIN_CHARS}+ chars)"

# Create issue with large description
rand_large_description "$DESC_MIN_CHARS" "$DESC_MAX_CHARS"
LARGE_DESC="$_RAND_STR"
LARGE_DESC_LEN="${#LARGE_DESC}"

rand_title 100
LARGE_DESC_TITLE="Large desc test: $_RAND_STR"

local_output=""
local_rc=0
local_output=$(td_a create "$LARGE_DESC_TITLE" --type task 2>&1) || local_rc=$?

if [ "$local_rc" -eq 0 ]; then
    LARGE_DESC_ID=$(echo "$local_output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -n "$LARGE_DESC_ID" ]; then
        LP_LARGE_DESC_CREATED=1
        CHAOS_ISSUE_IDS+=("$LARGE_DESC_ID")
        kv_set CHAOS_ISSUE_STATUS "$LARGE_DESC_ID" "open"

        # Set the large description
        td_a update "$LARGE_DESC_ID" --description "$LARGE_DESC" >/dev/null 2>&1 || true

        _ok "created issue $LARGE_DESC_ID with ${LARGE_DESC_LEN}-char description"
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 2))
    fi
fi

# Sync and verify
measure_sync

# Verify description integrity on both clients
DB_A="$CLIENT_A_DIR/.todos/issues.db"
DB_B="$CLIENT_B_DIR/.todos/issues.db"

if [ -n "${LARGE_DESC_ID:-}" ]; then
    desc_a=$(sqlite3 "$DB_A" "SELECT LENGTH(description) FROM issues WHERE id='$LARGE_DESC_ID';")
    desc_b=$(sqlite3 "$DB_B" "SELECT LENGTH(description) FROM issues WHERE id='$LARGE_DESC_ID';")

    if [ "$desc_a" -ge "$DESC_MIN_CHARS" ] && [ "$desc_b" -ge "$DESC_MIN_CHARS" ]; then
        if [ "$desc_a" -eq "$desc_b" ]; then
            LP_LARGE_DESC_VERIFIED=1
            _ok "large description verified: ${desc_a} chars on both clients"
        else
            LP_TRUNCATION_ERRORS=$((LP_TRUNCATION_ERRORS + 1))
            _fail "description length mismatch: A=${desc_a} B=${desc_b}"
        fi
    else
        LP_TRUNCATION_ERRORS=$((LP_TRUNCATION_ERRORS + 1))
        _fail "description truncated: A=${desc_a} B=${desc_b} (expected >=${DESC_MIN_CHARS})"
    fi
fi

# ============================================================
# PHASE 2: Many Comments Test
# ============================================================
_step "Phase 2: Many comments test (${COMMENT_COUNT} comments)"

rand_title 100
MANY_COMMENTS_TITLE="Many comments test: $_RAND_STR"

local_output=""
local_rc=0
local_output=$(td_a create "$MANY_COMMENTS_TITLE" --type task 2>&1) || local_rc=$?

if [ "$local_rc" -eq 0 ]; then
    MANY_COMMENTS_ID=$(echo "$local_output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -n "$MANY_COMMENTS_ID" ]; then
        CHAOS_ISSUE_IDS+=("$MANY_COMMENTS_ID")
        kv_set CHAOS_ISSUE_STATUS "$MANY_COMMENTS_ID" "open"
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

        # Add many comments
        add_many_comments "a" "$MANY_COMMENTS_ID" "$COMMENT_COUNT"
        LP_MANY_COMMENTS_CREATED="$_RAND_RESULT"
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + LP_MANY_COMMENTS_CREATED))

        _ok "created issue $MANY_COMMENTS_ID with $LP_MANY_COMMENTS_CREATED comments"
    fi
fi

# Sync and verify
measure_sync

if [ -n "${MANY_COMMENTS_ID:-}" ]; then
    comments_a=$(sqlite3 "$DB_A" "SELECT COUNT(*) FROM comments WHERE issue_id='$MANY_COMMENTS_ID';")
    comments_b=$(sqlite3 "$DB_B" "SELECT COUNT(*) FROM comments WHERE issue_id='$MANY_COMMENTS_ID';")

    if [ "$comments_a" -ge "$COMMENT_COUNT" ] && [ "$comments_b" -ge "$COMMENT_COUNT" ]; then
        if [ "$comments_a" -eq "$comments_b" ]; then
            LP_MANY_COMMENTS_VERIFIED=1
            _ok "many comments verified: ${comments_a} comments on both clients"
        else
            LP_TRUNCATION_ERRORS=$((LP_TRUNCATION_ERRORS + 1))
            _fail "comment count mismatch: A=${comments_a} B=${comments_b}"
        fi
    else
        LP_TRUNCATION_ERRORS=$((LP_TRUNCATION_ERRORS + 1))
        _fail "comments missing: A=${comments_a} B=${comments_b} (expected >=${COMMENT_COUNT})"
    fi

    # Verify session IDs preserved
    session_ids_a=$(sqlite3 "$DB_A" "SELECT DISTINCT session_id FROM comments WHERE issue_id='$MANY_COMMENTS_ID' AND session_id IS NOT NULL;" | wc -l | tr -d ' ')
    session_ids_b=$(sqlite3 "$DB_B" "SELECT DISTINCT session_id FROM comments WHERE issue_id='$MANY_COMMENTS_ID' AND session_id IS NOT NULL;" | wc -l | tr -d ' ')

    if [ "$session_ids_a" -ge 1 ] && [ "$session_ids_b" -ge 1 ]; then
        _ok "session IDs preserved in comments"
    else
        _fail "session IDs missing in comments: A has ${session_ids_a}, B has ${session_ids_b}"
    fi
fi

# ============================================================
# PHASE 3: Many Dependencies Test
# ============================================================
_step "Phase 3: Many dependencies test (${DEP_COUNT} deps)"

# Create target issues for dependencies
create_dep_target_issues "a" "$DEP_COUNT"
DEP_TARGETS=("${_LP_DEP_TARGET_IDS[@]}")
CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + ${#DEP_TARGETS[@]}))

_ok "created ${#DEP_TARGETS[@]} dependency target issues"

# Create main issue with many deps
rand_title 100
MANY_DEPS_TITLE="Many deps test: $_RAND_STR"

local_output=""
local_rc=0
local_output=$(td_a create "$MANY_DEPS_TITLE" --type task 2>&1) || local_rc=$?

if [ "$local_rc" -eq 0 ]; then
    MANY_DEPS_ID=$(echo "$local_output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -n "$MANY_DEPS_ID" ]; then
        CHAOS_ISSUE_IDS+=("$MANY_DEPS_ID")
        kv_set CHAOS_ISSUE_STATUS "$MANY_DEPS_ID" "open"
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

        # Add many dependencies
        add_many_deps "a" "$MANY_DEPS_ID" "${DEP_TARGETS[@]}"
        LP_MANY_DEPS_CREATED="$_RAND_RESULT"
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + LP_MANY_DEPS_CREATED))

        _ok "created issue $MANY_DEPS_ID with $LP_MANY_DEPS_CREATED dependencies"
    fi
fi

# Sync and verify
measure_sync

if [ -n "${MANY_DEPS_ID:-}" ]; then
    deps_a=$(sqlite3 "$DB_A" "SELECT COUNT(*) FROM issue_dependencies WHERE issue_id='$MANY_DEPS_ID';")
    deps_b=$(sqlite3 "$DB_B" "SELECT COUNT(*) FROM issue_dependencies WHERE issue_id='$MANY_DEPS_ID';")

    if [ "$deps_a" -ge "$LP_MANY_DEPS_CREATED" ] && [ "$deps_b" -ge "$LP_MANY_DEPS_CREATED" ]; then
        if [ "$deps_a" -eq "$deps_b" ]; then
            LP_MANY_DEPS_VERIFIED=1
            _ok "many dependencies verified: ${deps_a} deps on both clients"
        else
            LP_TRUNCATION_ERRORS=$((LP_TRUNCATION_ERRORS + 1))
            _fail "dependency count mismatch: A=${deps_a} B=${deps_b}"
        fi
    else
        LP_TRUNCATION_ERRORS=$((LP_TRUNCATION_ERRORS + 1))
        _fail "dependencies missing: A=${deps_a} B=${deps_b} (expected >=${LP_MANY_DEPS_CREATED})"
    fi
fi

# ============================================================
# PHASE 4: Large Labels Test
# ============================================================
_step "Phase 4: Large labels test (${LABEL_COUNT} labels)"

rand_many_labels "$LABEL_COUNT"
MANY_LABELS="$_RAND_STR"
MANY_LABELS_COUNT=$(echo "$MANY_LABELS" | tr ',' '\n' | wc -l | tr -d ' ')

rand_title 100
MANY_LABELS_TITLE="Many labels test: $_RAND_STR"

local_output=""
local_rc=0
local_output=$(td_a create "$MANY_LABELS_TITLE" --type task --labels "$MANY_LABELS" 2>&1) || local_rc=$?

if [ "$local_rc" -eq 0 ]; then
    MANY_LABELS_ID=$(echo "$local_output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -n "$MANY_LABELS_ID" ]; then
        CHAOS_ISSUE_IDS+=("$MANY_LABELS_ID")
        kv_set CHAOS_ISSUE_STATUS "$MANY_LABELS_ID" "open"
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

        _ok "created issue $MANY_LABELS_ID with ${MANY_LABELS_COUNT} labels"
    fi
fi

# Sync and verify
measure_sync

if [ -n "${MANY_LABELS_ID:-}" ]; then
    labels_a=$(sqlite3 "$DB_A" "SELECT labels FROM issues WHERE id='$MANY_LABELS_ID';")
    labels_b=$(sqlite3 "$DB_B" "SELECT labels FROM issues WHERE id='$MANY_LABELS_ID';")

    labels_count_a=$(echo "$labels_a" | tr ',' '\n' | wc -l | tr -d ' ')
    labels_count_b=$(echo "$labels_b" | tr ',' '\n' | wc -l | tr -d ' ')

    if [ "$labels_count_a" -ge "$LABEL_COUNT" ] && [ "$labels_count_b" -ge "$LABEL_COUNT" ]; then
        if [ "$labels_a" = "$labels_b" ]; then
            _ok "many labels verified: ${labels_count_a} labels on both clients"
        else
            LP_TRUNCATION_ERRORS=$((LP_TRUNCATION_ERRORS + 1))
            _fail "labels mismatch: A has ${labels_count_a}, B has ${labels_count_b}"
        fi
    else
        LP_TRUNCATION_ERRORS=$((LP_TRUNCATION_ERRORS + 1))
        _fail "labels truncated: A=${labels_count_a} B=${labels_count_b} (expected >=${LABEL_COUNT})"
    fi
fi

# ============================================================
# PHASE 5: Combined Stress Test
# ============================================================
_step "Phase 5: Combined stress test (large desc + many comments + many deps)"

# Create issue with everything
rand_large_description "$DESC_MIN_CHARS" "$DESC_MAX_CHARS"
COMBO_DESC="$_RAND_STR"
COMBO_DESC_LEN="${#COMBO_DESC}"

rand_many_labels 20
COMBO_LABELS="$_RAND_STR"

rand_title 100
COMBO_TITLE="Combined stress test: $_RAND_STR"

local_output=""
local_rc=0
local_output=$(td_a create "$COMBO_TITLE" --type task --labels "$COMBO_LABELS" 2>&1) || local_rc=$?

if [ "$local_rc" -eq 0 ]; then
    COMBO_ID=$(echo "$local_output" | grep -oE 'td-[0-9a-f]+' | head -n1)
    if [ -n "$COMBO_ID" ]; then
        LP_COMBO_CREATED=1
        CHAOS_ISSUE_IDS+=("$COMBO_ID")
        kv_set CHAOS_ISSUE_STATUS "$COMBO_ID" "open"
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

        # Add large description
        td_a update "$COMBO_ID" --description "$COMBO_DESC" >/dev/null 2>&1 || true
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + 1))

        # Add many comments (half the normal count for speed)
        combo_comment_count=$((COMMENT_COUNT / 2))
        add_many_comments "a" "$COMBO_ID" "$combo_comment_count"
        COMBO_COMMENTS_ADDED="$_RAND_RESULT"
        CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + COMBO_COMMENTS_ADDED))

        # Add many deps (half the normal count for speed)
        combo_dep_count=$((DEP_COUNT / 2))
        if [ "${#DEP_TARGETS[@]}" -ge "$combo_dep_count" ]; then
            combo_targets=("${DEP_TARGETS[@]:0:$combo_dep_count}")
            add_many_deps "a" "$COMBO_ID" "${combo_targets[@]}"
            COMBO_DEPS_ADDED="$_RAND_RESULT"
            CHAOS_ACTION_COUNT=$((CHAOS_ACTION_COUNT + COMBO_DEPS_ADDED))
        else
            COMBO_DEPS_ADDED=0
        fi

        _ok "created combo issue $COMBO_ID: ${COMBO_DESC_LEN} char desc, ${COMBO_COMMENTS_ADDED} comments, ${COMBO_DEPS_ADDED} deps"
    fi
fi

# Sync and verify
measure_sync

if [ -n "${COMBO_ID:-}" ]; then
    # Verify description
    combo_desc_a=$(sqlite3 "$DB_A" "SELECT LENGTH(description) FROM issues WHERE id='$COMBO_ID';")
    combo_desc_b=$(sqlite3 "$DB_B" "SELECT LENGTH(description) FROM issues WHERE id='$COMBO_ID';")

    # Verify comments
    combo_comments_a=$(sqlite3 "$DB_A" "SELECT COUNT(*) FROM comments WHERE issue_id='$COMBO_ID';")
    combo_comments_b=$(sqlite3 "$DB_B" "SELECT COUNT(*) FROM comments WHERE issue_id='$COMBO_ID';")

    # Verify deps
    combo_deps_a=$(sqlite3 "$DB_A" "SELECT COUNT(*) FROM issue_dependencies WHERE issue_id='$COMBO_ID';")
    combo_deps_b=$(sqlite3 "$DB_B" "SELECT COUNT(*) FROM issue_dependencies WHERE issue_id='$COMBO_ID';")

    combo_pass=true

    if [ "$combo_desc_a" -ne "$combo_desc_b" ]; then
        _fail "combo: description mismatch (A=${combo_desc_a} B=${combo_desc_b})"
        combo_pass=false
    fi

    if [ "$combo_comments_a" -ne "$combo_comments_b" ]; then
        _fail "combo: comments mismatch (A=${combo_comments_a} B=${combo_comments_b})"
        combo_pass=false
    fi

    if [ "$combo_deps_a" -ne "$combo_deps_b" ]; then
        _fail "combo: deps mismatch (A=${combo_deps_a} B=${combo_deps_b})"
        combo_pass=false
    fi

    if [ "$combo_pass" = "true" ]; then
        LP_COMBO_VERIFIED=1
        _ok "combined stress test verified: desc=${combo_desc_a}, comments=${combo_comments_a}, deps=${combo_deps_a}"
    else
        LP_TRUNCATION_ERRORS=$((LP_TRUNCATION_ERRORS + 1))
    fi
fi

# ============================================================
# FINAL SYNC: Full round-robin for convergence
# ============================================================
_step "Final sync"
td_a sync >/dev/null 2>&1 || true
td_b sync >/dev/null 2>&1 || true
sleep 1
td_b sync >/dev/null 2>&1 || true
td_a sync >/dev/null 2>&1 || true

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
# PERFORMANCE SUMMARY
# ============================================================
_step "Performance summary"

# Calculate sync time stats
total_sync_time=0
min_sync_time=999999
max_sync_time=0
for t in "${LP_SYNC_TIMES[@]}"; do
    total_sync_time=$((total_sync_time + t))
    [ "$t" -lt "$min_sync_time" ] && min_sync_time="$t"
    [ "$t" -gt "$max_sync_time" ] && max_sync_time="$t"
done
avg_sync_time=0
if [ "${#LP_SYNC_TIMES[@]}" -gt 0 ]; then
    avg_sync_time=$((total_sync_time / ${#LP_SYNC_TIMES[@]}))
fi

echo "  Sync rounds:              ${#LP_SYNC_TIMES[@]}"
echo "  Total sync time:          ${total_sync_time}s"
echo "  Min sync time:            ${min_sync_time}s"
echo "  Max sync time:            ${max_sync_time}s"
echo "  Avg sync time:            ${avg_sync_time}s"

# ============================================================
# SUMMARY STATS
# ============================================================
_step "Summary"
echo "  Total actions:            $CHAOS_ACTION_COUNT"
echo "  Total syncs:              $CHAOS_SYNC_COUNT"
echo "  Issues created:           ${#CHAOS_ISSUE_IDS[@]}"
echo ""
echo "  -- Large Payload Stats --"
echo "  Large desc created:       $LP_LARGE_DESC_CREATED"
echo "  Large desc verified:      $LP_LARGE_DESC_VERIFIED"
echo "  Many comments created:    $LP_MANY_COMMENTS_CREATED"
echo "  Many comments verified:   $LP_MANY_COMMENTS_VERIFIED"
echo "  Many deps created:        $LP_MANY_DEPS_CREATED"
echo "  Many deps verified:       $LP_MANY_DEPS_VERIFIED"
echo "  Combo test created:       $LP_COMBO_CREATED"
echo "  Combo test verified:      $LP_COMBO_VERIFIED"
echo "  Truncation errors:        $LP_TRUNCATION_ERRORS"
echo ""
echo "  Seed:                     $SEED (use --seed $SEED to reproduce)"

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
  "test": "large_payload",
  "seed": $SEED,
  "pass": $([ "$LP_TRUNCATION_ERRORS" -eq 0 ] && [ "$CHAOS_CONVERGENCE_FAILED" -eq 0 ] && echo "true" || echo "false"),
  "config": {
    "payload_size": "$PAYLOAD_SIZE",
    "desc_min_chars": $DESC_MIN_CHARS,
    "desc_max_chars": $DESC_MAX_CHARS,
    "comment_count": $COMMENT_COUNT,
    "dep_count": $DEP_COUNT,
    "label_count": $LABEL_COUNT
  },
  "totals": {
    "actions": $CHAOS_ACTION_COUNT,
    "syncs": $CHAOS_SYNC_COUNT,
    "issues_created": ${#CHAOS_ISSUE_IDS[@]}
  },
  "large_payload": {
    "large_desc_created": $LP_LARGE_DESC_CREATED,
    "large_desc_verified": $LP_LARGE_DESC_VERIFIED,
    "many_comments_created": $LP_MANY_COMMENTS_CREATED,
    "many_comments_verified": $LP_MANY_COMMENTS_VERIFIED,
    "many_deps_created": $LP_MANY_DEPS_CREATED,
    "many_deps_verified": $LP_MANY_DEPS_VERIFIED,
    "combo_created": $LP_COMBO_CREATED,
    "combo_verified": $LP_COMBO_VERIFIED,
    "truncation_errors": $LP_TRUNCATION_ERRORS
  },
  "convergence": {
    "passed": $CHAOS_CONVERGENCE_PASSED,
    "failed": $CHAOS_CONVERGENCE_FAILED
  },
  "performance": {
    "sync_rounds": ${#LP_SYNC_TIMES[@]},
    "total_sync_seconds": $total_sync_time,
    "min_sync_seconds": $min_sync_time,
    "max_sync_seconds": $max_sync_time,
    "avg_sync_seconds": $avg_sync_time
  },
  "timing": {
    "wall_clock_seconds": $_json_wall_clock
  }
}
ENDJSON
    _ok "JSON report written to $JSON_REPORT"
fi

# ---- Final check ----
if [ "$LP_TRUNCATION_ERRORS" -gt 0 ]; then
    _fail "$LP_TRUNCATION_ERRORS truncation errors (data was lost during sync)"
fi

report
