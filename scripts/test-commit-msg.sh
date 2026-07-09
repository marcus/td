#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK="$SCRIPT_DIR/commit-msg.sh"

PASS=0
FAIL=0

cleanup() {
  if [[ -n "${TEST_TMPDIR:-}" && -d "${TEST_TMPDIR:-}" ]]; then
    rm -rf "$TEST_TMPDIR"
  fi
}

trap cleanup EXIT

TEST_TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/td-commit-msg-tests-XXXXXX")"

run_hook() {
  local input="$1"
  local message_file output_file status

  message_file="$(mktemp "$TEST_TMPDIR/message-XXXXXX")"
  output_file="$(mktemp "$TEST_TMPDIR/output-XXXXXX")"

  printf '%s' "$input" >"$message_file"

  if "$HOOK" "$message_file" >"$output_file" 2>&1; then
    status=0
  else
    status=$?
  fi

  LAST_STATUS="$status"
  LAST_MESSAGE_FILE="$message_file"
  LAST_OUTPUT="$(cat "$output_file")"
}

pass_case() {
  local name="$1"
  echo "ok   $name"
  PASS=$((PASS + 1))
}

fail_case() {
  local name="$1"
  local details="$2"
  echo "not ok   $name"
  echo "$details" | sed 's/^/  /'
  FAIL=$((FAIL + 1))
}

expect_pass() {
  local name="$1"
  local input="$2"
  local expected_message="$3"
  local expected_file diff_output

  run_hook "$input"

  if [[ "$LAST_STATUS" -ne 0 ]]; then
    fail_case "$name" "expected success, got exit $LAST_STATUS\n$LAST_OUTPUT"
    return
  fi

  expected_file="$(mktemp "$TEST_TMPDIR/expected-XXXXXX")"
  printf '%s' "$expected_message" >"$expected_file"

  if ! cmp -s "$expected_file" "$LAST_MESSAGE_FILE"; then
    diff_output="$(diff -u "$expected_file" "$LAST_MESSAGE_FILE" || true)"
    fail_case "$name" "message mismatch\n$diff_output"
    return
  fi

  pass_case "$name"
}

expect_reject() {
  local name="$1"
  local input="$2"
  local expected_output_fragment="$3"

  run_hook "$input"

  if [[ "$LAST_STATUS" -eq 0 ]]; then
    fail_case "$name" "expected rejection, but hook succeeded"
    return
  fi

  if [[ "$LAST_OUTPUT" != *"$expected_output_fragment"* ]]; then
    fail_case "$name" "expected output to contain: $expected_output_fragment\nactual:\n$LAST_OUTPUT"
    return
  fi

  pass_case "$name"
}

expect_pass "valid pass-through subject" \
  $'fix: keep commit bodies unchanged\n\nNightshift-Task: commit-normalize\n' \
  $'fix: keep commit bodies unchanged\n\nNightshift-Task: commit-normalize\n'

expect_pass "normalizes capitalization and missing colon" \
  $'Fix add commit message hook\n' \
  $'fix: add commit message hook\n'

expect_pass "normalizes scoped subject missing colon" \
  $'Fix(cli) add commit message hook\n' \
  $'fix(cli): add commit message hook\n'

expect_pass "normalizes scoped subject and whitespace" \
  $'Docs(api)   :   add usage examples   (#12)\n' \
  $'docs(api): add usage examples (#12)\n'

expect_pass "accepts scoped commit with td and PR suffixes" \
  $'feat(cli): add commit normalizer (td-a1b2c3) (#91)\n' \
  $'feat(cli): add commit normalizer (td-a1b2c3) (#91)\n'

expect_pass "exempts merge commits" \
  $'Merge pull request #91 from marcus/dispatch/td-527bd4-0006\n' \
  $'Merge pull request #91 from marcus/dispatch/td-527bd4-0006\n'

expect_pass "exempts revert commits" \
  $'Revert "feat(sync): add encryption tables and key_id column"\n\nThis reverts commit deadbeef.\n' \
  $'Revert "feat(sync): add encryption tables and key_id column"\n\nThis reverts commit deadbeef.\n'

expect_pass "exempts fixup commits" \
  $'fixup! feat(cli): add commit normalizer\n' \
  $'fixup! feat(cli): add commit normalizer\n'

expect_pass "exempts squash commits" \
  $'squash! fix(cli): preserve trailers\n' \
  $'squash! fix(cli): preserve trailers\n'

expect_pass "exempts release subjects" \
  $'release: v0.44.0\n' \
  $'release: v0.44.0\n'

expect_pass "exempts bare version subjects" \
  $'v0.44.0\n' \
  $'v0.44.0\n'

expect_pass "preserves body and trailers when normalizing" \
  $'Test(sync)   preserve trailers   (td-a1b2c3)\n\nBody line\n\nNightshift-Task: commit-normalize\nNightshift-Ref: https://github.com/marcus/nightshift\n' \
  $'test(sync): preserve trailers (td-a1b2c3)\n\nBody line\n\nNightshift-Task: commit-normalize\nNightshift-Ref: https://github.com/marcus/nightshift\n'

expect_reject "rejects unknown subjects" \
  $'Add pre-commit hook and make install-hooks target\n' \
  'type(scope)?: summary'

expect_reject "rejects malformed spaced scope with colon" \
  $'fix (cli): broken parser\n' \
  'fix(cli): preserve trailers'

expect_reject "rejects malformed spaced scope without colon" \
  $'docs (api) typo fix\n' \
  'fix(cli): preserve trailers'

expect_reject "rejects missing summary" \
  $'fix:\n' \
  'Examples:'

expect_reject "rejects pr-only summary" \
  $'docs(scope): (#12)\n' \
  'Examples:'

expect_reject "rejects td-only summary" \
  $'docs(scope): (td-a1b2c3)\n' \
  'Examples:'

expect_reject "rejects ref-only summary chain" \
  $'docs(scope): (td-a1b2c3) (#12)\n' \
  'Examples:'

if [[ "$FAIL" -gt 0 ]]; then
  echo ""
  echo "Failed $FAIL commit-msg hook test(s); passed $PASS."
  exit 1
fi

echo ""
echo "Passed $PASS commit-msg hook tests."
