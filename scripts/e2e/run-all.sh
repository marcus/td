#!/usr/bin/env bash
#
# Run all e2e test scripts in this directory.
# Usage:
#   bash scripts/e2e/run-all.sh                # core tests only
#   bash scripts/e2e/run-all.sh --full         # core + real-data tests
#   bash scripts/e2e/run-all.sh --regression   # core + regression seed suite
#   bash scripts/e2e/run-all.sh --full --regression  # all of the above
#
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

FULL=false
REGRESSION=false
for arg in "$@"; do
    case "$arg" in
        --full) FULL=true ;;
        --regression) REGRESSION=true ;;
    esac
done

# Tests that require external data or are slow — only run with --full
FULL_ONLY="test_sync_real_data test_sync_real_data_all_projects test_monitor_autosync"

passed=0
failed=0
skipped=0
failures=()

for test_script in "$DIR"/test_*.sh; do
    name=$(basename "$test_script" .sh)

    if [ "$FULL" = "false" ] && [[ " $FULL_ONLY " == *" $name "* ]]; then
        echo -e "${CYAN}${BOLD}>>> $name${NC} (skipped — use --full)"
        skipped=$((skipped + 1))
        echo ""
        continue
    fi

    echo -e "${CYAN}${BOLD}>>> $name${NC}"

    if bash "$test_script"; then
        passed=$((passed + 1))
    else
        failed=$((failed + 1))
        failures+=("$name")
    fi
    echo ""
done

echo -e "${BOLD}========================================${NC}"
if [ "$failed" -eq 0 ]; then
    msg="All $passed tests passed."
    if [ "$skipped" -gt 0 ]; then
        msg="$passed passed, $skipped skipped."
    fi
    echo -e "${GREEN}${BOLD}$msg${NC}"
else
    echo -e "${GREEN}$passed passed${NC}, ${RED}$failed failed${NC}"
    for f in "${failures[@]}"; do
        echo -e "  ${RED}FAIL:${NC} $f"
    done
    exit 1
fi

# Run regression seed suite if requested
if [ "$REGRESSION" = "true" ]; then
    echo ""
    echo -e "${CYAN}${BOLD}>>> regression seed suite${NC}"
    if TD_FEATURE_SYNC_CLI=1 bash "$DIR/run_regression_seeds.sh" --fixed-only; then
        echo -e "  ${GREEN}Regression suite passed${NC}"
    else
        echo -e "  ${RED}FAIL:${NC} regression seed suite"
        exit 1
    fi
fi
