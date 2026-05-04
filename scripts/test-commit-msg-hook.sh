#!/usr/bin/env bash
set -euo pipefail

script_dir=$(
  cd "$(dirname "${BASH_SOURCE[0]}")" && pwd
)
hook="$script_dir/commit-msg.sh"
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

pass=0

assert_hook() {
  name="$1"
  input="$2"
  expected="$3"
  msg="$tmp_dir/$name.msg"
  want="$tmp_dir/$name.want"

  printf "%s" "$input" > "$msg"
  printf "%s" "$expected" > "$want"

  bash "$hook" "$msg"
  cmp -s "$msg" "$want"
  pass=$((pass + 1))
}

assert_rejects_unchanged() {
  name="$1"
  input="$2"
  msg="$tmp_dir/$name.msg"
  original="$tmp_dir/$name.original"
  err="$tmp_dir/$name.err"

  printf "%s" "$input" > "$msg"
  cp "$msg" "$original"

  if bash "$hook" "$msg" 2>"$err"; then
    echo "expected rejection for $name" >&2
    exit 1
  fi

  cmp -s "$msg" "$original"
  grep -q "invalid commit subject" "$err"
  pass=$((pass + 1))
}

assert_hook \
  "normalizes-whitespace-and-type" \
  "  FIX:   collapse   whitespace  " \
  "fix: collapse whitespace
"

assert_hook \
  "scoped-breaking" \
  "FEAT(cli)!: Add review guard

Body paragraph with  extra spacing.

Nightshift-Task: commit-normalize
Nightshift-Ref: https://github.com/marcus/nightshift
" \
  "feat(cli)!: Add review guard

Body paragraph with  extra spacing.

Nightshift-Task: commit-normalize
Nightshift-Ref: https://github.com/marcus/nightshift
"

for subject in \
  "Merge branch 'main'" \
  "Revert \"feat: add thing\"" \
  "fixup! feat: add thing" \
  "squash! fix: adjust thing"
do
  assert_hook \
    "passthrough-${subject//[^[:alnum:]]/-}" \
    "$subject

unchanged body
" \
    "$subject

unchanged body
"
done

assert_hook \
  "body-and-trailers-preserved" \
  "DOCS:   update   release notes

Keep this body exactly.

Nightshift-Task: commit-normalize
Nightshift-Ref: https://github.com/marcus/nightshift
" \
  "docs: update release notes

Keep this body exactly.

Nightshift-Task: commit-normalize
Nightshift-Ref: https://github.com/marcus/nightshift
"

assert_rejects_unchanged \
  "invalid-subject" \
  "Update the thing

Body should remain.
"

echo "commit-msg hook tests passed ($pass)"
