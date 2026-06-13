#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
HOOK="$ROOT/scripts/commit-msg.sh"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

pass_count=0

run_success_case() {
  local name=$1
  local input=$2
  local expected=$3
  local file="$TMPDIR/${name}.txt"

  printf '%s' "$input" > "$file"
  "$HOOK" "$file"

  local actual
  actual=$(cat "$file")
  if [[ "$actual" != "$expected" ]]; then
    echo "FAIL: $name"
    echo "expected:"
    printf '%s\n' "$expected"
    echo "actual:"
    printf '%s\n' "$actual"
    exit 1
  fi

  pass_count=$((pass_count + 1))
}

run_failure_case() {
  local name=$1
  local input=$2
  local file="$TMPDIR/${name}.txt"

  printf '%s' "$input" > "$file"
  if "$HOOK" "$file" >/dev/null 2>&1; then
    echo "FAIL: $name"
    echo "expected hook failure"
    exit 1
  fi

  pass_count=$((pass_count + 1))
}

run_success_case \
  canonical \
  $'feat: add commit hook (td-a1b2)\n\nNightshift-Task: commit-normalize\nNightshift-Ref: https://github.com/marcus/nightshift\n' \
  $'feat: add commit hook (td-a1b2)\n\nNightshift-Task: commit-normalize\nNightshift-Ref: https://github.com/marcus/nightshift'

run_success_case \
  normalize_subject \
  $' Feat :   add commit hook   td-A1B2  \n' \
  $'feat: add commit hook (td-a1b2)'

run_success_case \
  preserve_body_and_trailers \
  $'fix: preserve trailers td-c3d4\n\nBody line\n\nNightshift-Task: commit-normalize\nNightshift-Ref: https://github.com/marcus/nightshift\n' \
  $'fix: preserve trailers (td-c3d4)\n\nBody line\n\nNightshift-Task: commit-normalize\nNightshift-Ref: https://github.com/marcus/nightshift'

run_success_case \
  keep_summary_task_mentions \
  $'feat: mention td-a1b2 parsing before final ref (td-c3d4)\n' \
  $'feat: mention td-a1b2 parsing before final ref (td-c3d4)'

run_success_case \
  release_exception \
  $'TD:  bump to   v1.2.3\n' \
  $'td: bump to v1.2.3'

run_failure_case \
  missing_task_id \
  $'feat: add commit hook\n'

run_failure_case \
  missing_summary \
  $'feat: td-a1b2\n'

run_failure_case \
  ambiguous_non_trailing_task_id \
  $'feat: mention td-a1b2 parsing before final ref\n'

echo "commit-msg hook tests passed: $pass_count"
