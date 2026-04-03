#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
hook="$repo_root/scripts/commit-msg.sh"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

pass_count=0

assert_file_equals() {
  local name="$1"
  local input="$2"
  local expected="$3"
  local file="$tmpdir/$name.txt"

  printf '%s' "$input" > "$file"
  "$hook" "$file"

  if ! diff -u <(printf '%s' "$expected") "$file"; then
    echo "FAILED: $name"
    exit 1
  fi

  pass_count=$((pass_count + 1))
}

assert_file_equals \
  "pass-through" \
  $'feat(cli): add commit normalizer (td-a1b2)\n\nBody line\nNightshift-Task: example\n' \
  $'feat(cli): add commit normalizer (td-a1b2)\n\nBody line\nNightshift-Task: example\n'

assert_file_equals \
  "capitalized-prefix" \
  $'Docs: Update changelog for v0.43.0\n' \
  $'docs: Update changelog for v0.43.0\n'

assert_file_equals \
  "legacy-bracketed-prefix" \
  $'[td-527bd4] Clean up Dispatch worktrees page search, filters, and row density\n' \
  $'chore: Clean up Dispatch worktrees page search, filters, and row density (td-527bd4)\n'

assert_file_equals \
  "preserve-ticket-suffix" \
  $'Feat(api): add release endpoint (td-9FA24F)\n' \
  $'feat(api): add release endpoint (td-9fa24f)\n'

assert_file_equals \
  "preserve-body-and-trailers" \
  $'Update README links\n\nExpanded body stays here.\nNightshift-Task: commit-normalize\nNightshift-Ref: https://github.com/marcus/nightshift\n' \
  $'chore: Update README links\n\nExpanded body stays here.\nNightshift-Task: commit-normalize\nNightshift-Ref: https://github.com/marcus/nightshift\n'

assert_file_equals \
  "bypass-merge" \
  $'Merge pull request #91 from marcus/dispatch/td-527bd4-0006\n\nMerge body\n' \
  $'Merge pull request #91 from marcus/dispatch/td-527bd4-0006\n\nMerge body\n'

assert_file_equals \
  "bypass-revert" \
  $'Revert "fix: add release endpoint"\n\nThis reverts commit abcdef.\n' \
  $'Revert "fix: add release endpoint"\n\nThis reverts commit abcdef.\n'

assert_file_equals \
  "bypass-fixup" \
  $'fixup! feat(cli): add commit normalizer\n' \
  $'fixup! feat(cli): add commit normalizer\n'

echo "commit-msg tests passed ($pass_count cases)"
