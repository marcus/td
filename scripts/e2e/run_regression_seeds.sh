#!/usr/bin/env bash
#
# Run regression seeds for deterministic e2e test replay.
#
# This script reads regression_seeds.json and replays each seed with its
# associated test script and arguments. Use it to verify bug fixes and
# prevent regressions.
#
# USAGE:
#   bash scripts/e2e/run_regression_seeds.sh                  # Run all seeds
#   bash scripts/e2e/run_regression_seeds.sh --fixed-only     # Run only fixed=true seeds (regression check)
#   bash scripts/e2e/run_regression_seeds.sh --unfixed-only   # Run only fixed=false seeds (check if bugs persist)
#   bash scripts/e2e/run_regression_seeds.sh --verbose        # Show detailed output
#   bash scripts/e2e/run_regression_seeds.sh --json-output    # Output JSON summary
#   bash scripts/e2e/run_regression_seeds.sh --seeds-file F   # Use custom seeds file
#
# EXIT CODES:
#   0 - All seeds passed as expected (fixed=true pass, fixed=false allowed to fail)
#   1 - Regression detected (fixed=true seed failed)
#
# ADDING NEW SEEDS:
#   When a bug is found via chaos/fuzz testing:
#   1. Note the seed number from the failing test output
#   2. Add an entry to regression_seeds.json:
#      {
#        "seed": <seed-number>,
#        "test": "<test_name>",           # e.g., "chaos_sync", "network_partition"
#        "description": "td-XXXX: brief bug description",
#        "added": "YYYY-MM-DD",
#        "args": {"arg": value},          # test-specific arguments
#        "fixed": false                   # set true after bug is fixed
#      }
#   3. Run with --unfixed-only to confirm the bug reproduces
#   4. Fix the bug, run again, set fixed=true when passing
#
set -euo pipefail

export TD_FEATURE_SYNC_CLI=1

DIR="$(cd "$(dirname "$0")" && pwd)"
SEEDS_FILE="$DIR/regression_seeds.json"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

# Defaults
FILTER=""  # "fixed", "unfixed", or "" for all
VERBOSE=false
JSON_OUTPUT=false

usage() {
    cat <<EOF
Usage: bash scripts/e2e/run_regression_seeds.sh [OPTIONS]

Run regression seeds for deterministic e2e test replay.

Options:
  --fixed-only       Run only fixed=true seeds (regression check for CI)
  --unfixed-only     Run only fixed=false seeds (verify bugs still fail)
  --verbose          Show full test output
  --json-output      Output JSON summary instead of text
  --seeds-file PATH  Use custom seeds file (default: regression_seeds.json)
  -h, --help         Show this help

Exit codes:
  0 - All seeds passed as expected
  1 - Regression detected (a fixed seed failed)

EOF
}

# Parse args
while [[ $# -gt 0 ]]; do
    case "$1" in
        --fixed-only)   FILTER="fixed"; shift ;;
        --unfixed-only) FILTER="unfixed"; shift ;;
        --verbose)      VERBOSE=true; shift ;;
        --json-output)  JSON_OUTPUT=true; shift ;;
        --seeds-file)   SEEDS_FILE="$2"; shift; shift ;;
        -h|--help)      usage; exit 0 ;;
        *) echo "Unknown arg: $1" >&2; usage; exit 1 ;;
    esac
done

# Check dependencies
if ! command -v jq &>/dev/null; then
    echo -e "${RED}Error: jq is required but not installed${NC}" >&2
    exit 1
fi

if [ ! -f "$SEEDS_FILE" ]; then
    echo -e "${RED}Error: $SEEDS_FILE not found${NC}" >&2
    exit 1
fi

# Map test names to scripts
test_script_for() {
    local test_name="$1"
    case "$test_name" in
        chaos_sync)        echo "$DIR/test_chaos_sync.sh" ;;
        network_partition) echo "$DIR/test_network_partition.sh" ;;
        server_restart)    echo "$DIR/test_server_restart.sh" ;;
        late_join)         echo "$DIR/test_late_join.sh" ;;
        *)
            echo -e "${RED}Unknown test: $test_name${NC}" >&2
            return 1
            ;;
    esac
}

# Build command-line args from JSON object
args_to_cli() {
    local args_json="$1"
    local cli_args=""

    # Parse each key-value pair
    for key in $(echo "$args_json" | jq -r 'keys[]' 2>/dev/null); do
        local value
        value=$(echo "$args_json" | jq -r ".[\"$key\"]")
        cli_args="$cli_args --$key $value"
    done

    echo "$cli_args"
}

# Read seeds
seeds_json=$(cat "$SEEDS_FILE")
seed_count=$(echo "$seeds_json" | jq '.seeds | length')

if [ "$seed_count" -eq 0 ]; then
    echo -e "${YELLOW}No seeds found in $SEEDS_FILE${NC}"
    exit 0
fi

# Filter seeds based on flags
filtered_seeds=""
if [ "$FILTER" = "fixed" ]; then
    filtered_seeds=$(echo "$seeds_json" | jq -c '.seeds | map(select(.fixed == true))')
elif [ "$FILTER" = "unfixed" ]; then
    filtered_seeds=$(echo "$seeds_json" | jq -c '.seeds | map(select(.fixed == false))')
else
    filtered_seeds=$(echo "$seeds_json" | jq -c '.seeds')
fi

filtered_count=$(echo "$filtered_seeds" | jq 'length')

if [ "$filtered_count" -eq 0 ]; then
    msg="No seeds to run"
    [ -n "$FILTER" ] && msg="$msg (filter: --${FILTER}-only)"
    echo -e "${YELLOW}$msg${NC}"
    exit 0
fi

# Track results
passed=0
failed=0
regressions=0
results_json="[]"

if [ "$JSON_OUTPUT" = "false" ]; then
    echo -e "${BOLD}Running $filtered_count regression seeds${NC}"
    [ -n "$FILTER" ] && echo -e "${DIM}(filter: --${FILTER}-only)${NC}"
    echo ""
fi

# Run each seed
for i in $(seq 0 $((filtered_count - 1))); do
    seed_entry=$(echo "$filtered_seeds" | jq -c ".[$i]")

    seed=$(echo "$seed_entry" | jq -r '.seed')
    test_name=$(echo "$seed_entry" | jq -r '.test')
    description=$(echo "$seed_entry" | jq -r '.description')
    is_fixed=$(echo "$seed_entry" | jq -r '.fixed')
    args_json=$(echo "$seed_entry" | jq -c '.args // {}')

    # Get test script
    test_script=""
    if ! test_script=$(test_script_for "$test_name"); then
        continue
    fi

    if [ ! -f "$test_script" ]; then
        if [ "$JSON_OUTPUT" = "false" ]; then
            echo -e "${RED}SKIP${NC} seed=$seed test=$test_name (script not found: $test_script)"
        fi
        continue
    fi

    # Build CLI args
    cli_args=$(args_to_cli "$args_json")

    if [ "$JSON_OUTPUT" = "false" ]; then
        echo -e -n "${CYAN}>>> seed=$seed test=$test_name${NC}"
        [ "$is_fixed" = "true" ] && echo -e -n " ${DIM}(fixed)${NC}"
        echo ""
        echo -e "${DIM}    $description${NC}"
    fi

    # Run test
    start_time=$(date +%s)
    test_output=""
    test_exit_code=0

    if [ "$VERBOSE" = "true" ]; then
        if bash "$test_script" --seed "$seed" $cli_args; then
            test_exit_code=0
        else
            test_exit_code=$?
        fi
    else
        if test_output=$(bash "$test_script" --seed "$seed" $cli_args 2>&1); then
            test_exit_code=0
        else
            test_exit_code=$?
        fi
    fi

    end_time=$(date +%s)
    duration=$((end_time - start_time))

    # Evaluate result
    result_status=""
    is_regression=false

    if [ "$test_exit_code" -eq 0 ]; then
        result_status="pass"
        passed=$((passed + 1))
        if [ "$JSON_OUTPUT" = "false" ]; then
            echo -e "    ${GREEN}PASS${NC} (${duration}s)"
        fi
    else
        result_status="fail"
        failed=$((failed + 1))

        if [ "$is_fixed" = "true" ]; then
            # This is a regression!
            is_regression=true
            regressions=$((regressions + 1))
            if [ "$JSON_OUTPUT" = "false" ]; then
                echo -e "    ${RED}FAIL - REGRESSION${NC} (${duration}s)"
                if [ "$VERBOSE" = "false" ] && [ -n "$test_output" ]; then
                    echo -e "${DIM}${test_output}${NC}" | tail -20
                fi
            fi
        else
            # Expected failure (bug not yet fixed)
            if [ "$JSON_OUTPUT" = "false" ]; then
                echo -e "    ${YELLOW}FAIL (expected - not fixed)${NC} (${duration}s)"
            fi
        fi
    fi

    # Add to results JSON
    results_json=$(echo "$results_json" | jq \
        --argjson seed "$seed" \
        --arg test "$test_name" \
        --arg status "$result_status" \
        --argjson duration "$duration" \
        --argjson fixed "$is_fixed" \
        --argjson regression "$is_regression" \
        '. + [{seed: $seed, test: $test, status: $status, duration: $duration, fixed: $fixed, regression: $regression}]')

    if [ "$JSON_OUTPUT" = "false" ]; then
        echo ""
    fi
done

# Summary
if [ "$JSON_OUTPUT" = "true" ]; then
    # Output JSON summary
    cat <<ENDJSON
{
  "total": $filtered_count,
  "passed": $passed,
  "failed": $failed,
  "regressions": $regressions,
  "filter": $([ -n "$FILTER" ] && echo "\"$FILTER\"" || echo "null"),
  "success": $([ "$regressions" -eq 0 ] && echo "true" || echo "false"),
  "results": $results_json
}
ENDJSON
else
    echo -e "${BOLD}========================================${NC}"
    if [ "$regressions" -gt 0 ]; then
        echo -e "${RED}${BOLD}REGRESSION DETECTED${NC}"
        echo -e "  $regressions fixed seed(s) failed"
        echo -e "  Total: $passed passed, $failed failed"
    elif [ "$failed" -gt 0 ] && [ "$FILTER" != "fixed" ]; then
        echo -e "${YELLOW}${BOLD}$passed passed, $failed failed (expected - unfixed bugs)${NC}"
    else
        echo -e "${GREEN}${BOLD}All $passed seeds passed${NC}"
    fi
fi

# Exit code: 0 if no regressions, 1 if regression detected
if [ "$regressions" -gt 0 ]; then
    exit 1
fi
exit 0
