#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
hook="$script_dir/commit-msg.sh"
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

pass_count=0
fail_count=0

run_case() {
  local name=$1
  local expected=$2
  local content=$3
  local msg_file="$tmpdir/msg.txt"
  local out_file="$tmpdir/out.txt"

  printf '%s\n' "$content" >"$msg_file"

  if "$hook" "$msg_file" >"$out_file" 2>&1; then
    status=0
  else
    status=$?
  fi

  if [[ $status -eq $expected ]]; then
    printf "ok   %s\n" "$name"
    pass_count=$((pass_count + 1))
  else
    printf "FAIL %s (expected exit %s, got %s)\n" "$name" "$expected" "$status"
    sed 's/^/  /' "$out_file"
    fail_count=$((fail_count + 1))
  fi
}

run_case "accepts standard task commit" 0 "feat: normalize commit messages (td-abc123)"
run_case "accepts scoped standard task commit" 0 "fix(parser): handle empty commit subject (td-9f2e1a)"
run_case "accepts trailers in body" 0 "$(cat <<'EOF'
docs: explain commit hook usage (td-00cafe)

Nightshift-Task: commit-normalize
Nightshift-Ref: https://github.com/marcus/nightshift
EOF
)"
run_case "accepts changelog exception" 0 "docs: Update changelog for v1.2.3"
run_case "accepts release bump exception" 0 "td: bump to v1.2.3"
run_case "accepts merge subject" 0 "Merge branch 'main' into feat/commit-message-normalizer-task"
run_case "accepts fixup subject" 0 "fixup! feat: normalize commit messages (td-abc123)"
run_case "accepts revert type" 0 "revert: back out broken normalization rule (td-abc123)"
run_case "rejects missing task id" 1 "feat: normalize commit messages"
run_case "rejects missing type prefix" 1 "normalize commit messages (td-abc123)"
run_case "rejects uppercase task id" 1 "feat: normalize commit messages (TD-abc123)"

if [[ $fail_count -gt 0 ]]; then
  printf "\n%s commit-msg regression check(s) failed.\n" "$fail_count"
  exit 1
fi

printf "\nAll %s commit-msg regression checks passed.\n" "$pass_count"
