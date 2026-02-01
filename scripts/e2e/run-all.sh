#!/usr/bin/env bash
#
# Run all e2e test scripts in this directory.
# Usage: bash scripts/e2e/run-all.sh
#
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

passed=0
failed=0
failures=()

for test_script in "$DIR"/test_*.sh; do
    name=$(basename "$test_script" .sh)
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
    echo -e "${GREEN}${BOLD}All $passed tests passed.${NC}"
else
    echo -e "${GREEN}$passed passed${NC}, ${RED}$failed failed${NC}"
    for f in "${failures[@]}"; do
        echo -e "  ${RED}FAIL:${NC} $f"
    done
    exit 1
fi
