#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
hook="$root/scripts/commit-msg.sh"
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_file() {
  local got=$1
  local want=$2
  if ! cmp -s "$got" "$want"; then
    diff -u "$want" "$got" >&2 || true
    fail "file content differed"
  fi
}

msg="$tmpdir/normalize-msg"
want="$tmpdir/normalize-want"
cat > "$msg" <<'EOF'
  Feat  :   Add   commit    hook

Body  keeps   its spacing.

Nightshift-Task: commit-normalize
Nightshift-Ref: https://github.com/marcus/nightshift
EOF
cat > "$want" <<'EOF'
feat: Add commit hook

Body  keeps   its spacing.

Nightshift-Task: commit-normalize
Nightshift-Ref: https://github.com/marcus/nightshift
EOF
"$hook" "$msg"
assert_file "$msg" "$want"

msg="$tmpdir/scope-msg"
printf 'FIX(api)!:  preserve    scope\n' > "$msg"
printf 'fix(api)!: preserve scope\n' > "$want"
"$hook" "$msg"
assert_file "$msg" "$want"

for subject in \
  "Merge branch 'main'" \
  'Revert "feat: add thing"' \
  'fixup! feat: add thing' \
  'squash! feat: add thing'
do
  msg="$tmpdir/pass-through-msg"
  printf '%s\n\nunchanged body\n' "$subject" > "$msg"
  cp "$msg" "$want"
  "$hook" "$msg"
  assert_file "$msg" "$want"
done

msg="$tmpdir/invalid-msg"
printf 'Update docs\n\nNightshift-Task: commit-normalize\n' > "$msg"
if "$hook" "$msg" 2> "$tmpdir/invalid-err"; then
  fail "invalid subject unexpectedly passed"
fi
grep -q "Invalid commit subject" "$tmpdir/invalid-err" || fail "missing invalid-subject error"
grep -q "Nightshift-Task: commit-normalize" "$msg" || fail "invalid message trailers were rewritten"

echo "commit-msg hook tests passed"
