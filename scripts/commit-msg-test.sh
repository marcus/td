#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
HOOK="$ROOT/scripts/commit-msg.sh"
TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/td-commit-msg-test-XXXXXX")
trap 'rm -rf "$TMPDIR"' EXIT

pass=0
fail=0

run_case() {
  local name=$1
  local expected=$2
  local env_mode=$3
  local file="$TMPDIR/$name.msg"
  shift 3

  printf "%s" "$*" > "$file"

  set +e
  if [[ "$env_mode" == "nightshift" ]]; then
    NIGHTSHIFT_TASK=commit-normalize "$HOOK" "$file" >"$TMPDIR/$name.out" 2>&1
  else
    "$HOOK" "$file" >"$TMPDIR/$name.out" 2>&1
  fi
  local status=$?
  set -e

  if [[ "$expected" == "pass" && $status -eq 0 ]] || [[ "$expected" == "fail" && $status -ne 0 ]]; then
    echo "ok - $name"
    pass=$((pass + 1))
  else
    echo "not ok - $name"
    sed 's/^/  /' "$TMPDIR/$name.out"
    fail=$((fail + 1))
  fi
}

run_case "valid-conventional" pass none "feat: normalize commit messages

Add the hook.
"

run_case "valid-with-td-reference" pass none "fix: tighten review flow (td-a1b2)
"

run_case "valid-nightshift-topic-without-trailers" pass none "fix: handle nightshift scheduler crash
"

run_case "valid-nightshift-trailers" pass nightshift "chore: normalize commit examples

Nightshift-Task: commit-normalize
Nightshift-Ref: https://github.com/marcus/nightshift
"

run_case "invalid-subject" fail none "normalize commit examples
"

run_case "invalid-overlong-subject" fail none "feat: this subject is intentionally far too long for the repository commit message hook
"

run_case "missing-nightshift-trailers" fail nightshift "chore: normalize commit examples
"

run_case "exempt-merge" pass none "Merge branch 'main' into feature
"

run_case "exempt-revert" pass none "Revert \"feat: normalize commit messages\"
"

run_case "exempt-fixup" pass none "fixup! feat: normalize commit messages
"

run_case "exempt-squash" pass none "squash! feat: normalize commit messages
"

echo ""
if [[ $fail -gt 0 ]]; then
  echo "$fail commit-msg test(s) failed"
  exit 1
fi

echo "$pass commit-msg test(s) passed"
