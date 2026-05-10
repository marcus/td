#!/usr/bin/env bash
set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
hook_script="$repo_root/scripts/commit-msg.sh"
tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/td-commit-msg-test.XXXXXX")

cleanup() {
  rm -rf "$tmp_dir"
}

fail() {
  printf 'test-commit-msg: %s\n' "$1" >&2
  exit 1
}

write_message() {
  local path=$1
  shift

  printf '%s\n' "$@" > "$path"
}

assert_file_equals() {
  local path=$1
  local expected=$2
  local actual

  actual=$(cat "$path")
  if [[ "$actual" != "$expected" ]]; then
    printf 'Expected:\n%s\n\nActual:\n%s\n' "$expected" "$actual" >&2
    fail "file contents did not match for $path"
  fi
}

run_success_case() {
  local name=$1
  local expected=$2
  shift 2

  local message_file="$tmp_dir/$name.txt"
  local stderr_file="$tmp_dir/$name.stderr"

  write_message "$message_file" "$@"

  if ! "$hook_script" "$message_file" > /dev/null 2> "$stderr_file"; then
    cat "$stderr_file" >&2
    fail "expected success for $name"
  fi

  assert_file_equals "$message_file" "$expected"
}

run_failure_case() {
  local name=$1
  local subject=$2
  local message_file="$tmp_dir/$name.txt"
  local stderr_file="$tmp_dir/$name.stderr"

  write_message "$message_file" "$subject"

  if "$hook_script" "$message_file" > /dev/null 2> "$stderr_file"; then
    cat "$stderr_file" >&2
    fail "expected failure for $name"
  fi

  grep -F "type: summary" "$stderr_file" > /dev/null || fail "expected guidance output for $name"
  assert_file_equals "$message_file" "$subject"
}

trap cleanup EXIT

run_success_case \
  valid-subject \
  "feat: add commit message normalizer (td-527bd4-0006)" \
  "feat: add commit message normalizer (td-527bd4-0006)"

run_success_case \
  normalize-simple \
  "docs: update changelog" \
  "Docs - Update changelog"

run_success_case \
  preserve-body \
  $'fix(sync): preserve trailers (td-527bd4-0006)\n\nBody line\n\nNightshift-Task: commit-normalize\nNightshift-Ref: https://github.com/marcus/nightshift' \
  "Fix sync - Preserve trailers (td-527bd4-0006)" \
  "" \
  "Body line" \
  "" \
  "Nightshift-Task: commit-normalize" \
  "Nightshift-Ref: https://github.com/marcus/nightshift"

run_success_case \
  normalize-release \
  "chore(homebrew): bump td to v1.2.3" \
  "Chore homebrew - Bump td to v1.2.3"

run_failure_case "invalid-td-empty" "feat: add normalizer (td-)"
run_failure_case "invalid-td-space" "feat: add normalizer (td 527bd4)"
run_failure_case "invalid-td-symbols" "feat: add normalizer (Td-???)"

printf 'commit-msg regression tests passed\n'
