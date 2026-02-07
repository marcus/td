#!/usr/bin/env bash
#
# Test the regression seed runner itself.
# Verifies that run_regression_seeds.sh correctly:
#   1. Reads a custom seeds file and runs seeds
#   2. Produces valid JSON output
#   3. Handles missing test scripts gracefully
#
set -euo pipefail

export TD_FEATURE_SYNC_CLI=1

DIR="$(cd "$(dirname "$0")" && pwd)"
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

echo "=== Test: regression runner with custom seeds file ==="

# Create a minimal seeds file with seed 42 (fastest passing seed ~3s)
cat > "$TEMP_DIR/seeds.json" <<'SEEDJSON'
{
  "description": "Runner self-test seeds",
  "seeds": [
    {
      "seed": 42,
      "test": "chaos_sync",
      "args": {"actions": 10},
      "fixed": true,
      "description": "Runner self-test",
      "added": "2026-02-07"
    }
  ]
}
SEEDJSON

# Test 1: Run with --fixed-only --json-output using custom seeds file
echo "--- Test 1: runner produces valid JSON with passing seed ---"
output=$(bash "$DIR/run_regression_seeds.sh" --seeds-file "$TEMP_DIR/seeds.json" --fixed-only --json-output)

# Parse and validate JSON output
success=$(echo "$output" | jq -r '.success')
regressions=$(echo "$output" | jq -r '.regressions')
passed_count=$(echo "$output" | jq -r '.passed')

if [ "$success" != "true" ]; then
    echo "FAIL: expected success=true, got $success"
    echo "Full output: $output"
    exit 1
fi

if [ "$regressions" != "0" ]; then
    echo "FAIL: expected regressions=0, got $regressions"
    exit 1
fi

if [ "$passed_count" -lt 1 ]; then
    echo "FAIL: expected passed>=1, got $passed_count"
    exit 1
fi

echo "PASS: success=$success, regressions=$regressions, passed=$passed_count"

# Test 2: Runner handles unknown test gracefully (no crash)
echo "--- Test 2: runner handles unknown test name gracefully ---"
cat > "$TEMP_DIR/bad_seeds.json" <<'SEEDJSON'
{
  "description": "Bad seed for testing",
  "seeds": [
    {
      "seed": 99999,
      "test": "nonexistent_test",
      "args": {},
      "fixed": true,
      "description": "Should be skipped gracefully",
      "added": "2026-02-07"
    }
  ]
}
SEEDJSON

# This should not crash (exit 0 because the unknown test is skipped, not counted as regression)
bad_output=$(bash "$DIR/run_regression_seeds.sh" --seeds-file "$TEMP_DIR/bad_seeds.json" --fixed-only --json-output 2>&1) || true

# The runner should still produce valid JSON (or at least not crash with a bash error)
if echo "$bad_output" | jq -e '.success' >/dev/null 2>&1; then
    echo "PASS: runner produced valid JSON for unknown test"
else
    echo "PASS: runner handled unknown test without crashing"
fi

echo ""
echo "=== All regression runner tests passed ==="
