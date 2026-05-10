#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
HOOK="$ROOT/scripts/commit-msg.sh"

pass_count=0

make_msg_file() {
  local path=$1
  printf '%s' "$2" >"$path"
}

run_success_case() {
  local name=$1
  local input=$2
  local expected=$3
  local msg_file
  msg_file=$(mktemp)
  make_msg_file "$msg_file" "$input"

  "$HOOK" "$msg_file"

  local actual
  actual=$(cat "$msg_file")
  if [[ "$actual" != "$expected" ]]; then
    echo "FAIL: $name"
    echo "expected:"
    printf '%s\n' "$expected"
    echo "actual:"
    printf '%s\n' "$actual"
    rm -f "$msg_file"
    exit 1
  fi

  rm -f "$msg_file"
  pass_count=$((pass_count + 1))
}

run_failure_case() {
  local name=$1
  local input=$2
  local expected_stderr=$3
  local msg_file
  local err_file
  msg_file=$(mktemp)
  err_file=$(mktemp)
  make_msg_file "$msg_file" "$input"

  if "$HOOK" "$msg_file" 2>"$err_file"; then
    echo "FAIL: $name"
    echo "expected hook to fail"
    rm -f "$msg_file" "$err_file"
    exit 1
  fi

  if ! grep -Fq "$expected_stderr" "$err_file"; then
    echo "FAIL: $name"
    echo "expected stderr to contain: $expected_stderr"
    echo "actual stderr:"
    cat "$err_file"
    rm -f "$msg_file" "$err_file"
    exit 1
  fi

  rm -f "$msg_file" "$err_file"
  pass_count=$((pass_count + 1))
}

run_success_case \
  "normalizes plain subject" \
  $'feat: normalize hook output td-a1b2\n\nBody stays put.\n' \
  $'feat: normalize hook output (td-a1b2)\n\nBody stays put.'

run_success_case \
  "accepts canonical subject" \
  $'fix: preserve body content (td-a1b2)\n\nNightshift-Task: commit-normalize\n' \
  $'fix: preserve body content (td-a1b2)\n\nNightshift-Task: commit-normalize'

run_success_case \
  "normalizes scoped subject" \
  $'feat(sync): persist cursor td-a1b2' \
  $'feat(sync): persist cursor (td-a1b2)'

run_success_case \
  "uses trailing task token when summary mentions another td id" \
  $'feat: mention td-a1b2 parsing before final ref (td-c3d4)' \
  $'feat: mention td-a1b2 parsing before final ref (td-c3d4)'

run_success_case \
  "normalizes trailing task token when summary mentions another td id" \
  $'feat(sync): mention td-a1b2 parsing before final ref td-c3d4' \
  $'feat(sync): mention td-a1b2 parsing before final ref (td-c3d4)'

run_success_case \
  "allows automated release bump commits" \
  'td: bump to v1.2.3' \
  'td: bump to v1.2.3'

run_success_case \
  "allows merge subjects" \
  'Merge branch '\''feature/example'\''' \
  'Merge branch '\''feature/example'\'''

run_failure_case \
  "rejects missing task reference" \
  'feat: missing task reference' \
  'missing trailing task reference'

run_failure_case \
  "rejects malformed prefix" \
  'feat sync: malformed prefix td-a1b2' \
  'invalid commit message subject'

echo "commit-msg hook tests passed ($pass_count cases)"
