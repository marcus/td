#!/usr/bin/env bash
#
# Run test_sync_real_data.sh across all projects listed in sidecar config.
#
# Usage:
#   bash scripts/e2e/test_sync_real_data_all_projects.sh
#   bash scripts/e2e/test_sync_real_data_all_projects.sh /path/to/config.json
#
set -u -o pipefail

CONFIG_PATH="${1:-$HOME/.config/sidecar/config.json}"
TEST_SCRIPT="$(cd "$(dirname "$0")" && pwd)/test_sync_real_data.sh"

if [ ! -f "$CONFIG_PATH" ]; then
  echo "FATAL: config not found: $CONFIG_PATH" >&2
  exit 1
fi
if [ ! -f "$TEST_SCRIPT" ]; then
  echo "FATAL: test script not found: $TEST_SCRIPT" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "FATAL: jq is required" >&2
  exit 1
fi

DB_REL=$(jq -r '.plugins["td-monitor"].dbPath // ".todos/issues.db"' "$CONFIG_PATH")

total=0
passed=0
failed=0
skipped=0

while IFS=$'\t' read -r name path; do
  [ -n "$path" ] || continue
  total=$((total + 1))

  if [ -z "$name" ] || [ "$name" = "null" ]; then
    name="$(basename "$path")"
  fi

  db_path="$DB_REL"
  if [[ "$DB_REL" != /* ]]; then
    db_path="${path%/}/$DB_REL"
  fi

  if [ ! -f "$db_path" ]; then
    echo "SKIP: $name ($db_path missing)"
    skipped=$((skipped + 1))
    continue
  fi

  echo ""
  echo "=== Project: $name ==="
  echo "DB: $db_path"

  if bash "$TEST_SCRIPT" "$db_path"; then
    echo "PASS: $name"
    passed=$((passed + 1))
  else
    echo "FAIL: $name"
    failed=$((failed + 1))
  fi
done < <(jq -r '.projects.list[]? | [.name, .path] | @tsv' "$CONFIG_PATH")

echo ""
echo "Summary: total=$total passed=$passed failed=$failed skipped=$skipped"

if [ "$failed" -ne 0 ]; then
  exit 1
fi
