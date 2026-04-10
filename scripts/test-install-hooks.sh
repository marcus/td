#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"

TMPDIR_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/td-install-hooks-test-XXXX")
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_file_matches() {
  local label="$1"
  local expected="$2"
  local actual="$3"

  if ! cmp -s "$expected" "$actual"; then
    echo "FAIL: $label" >&2
    diff -u "$expected" "$actual" >&2 || true
    exit 1
  fi
}

TEST_REPO="$TMPDIR_ROOT/repo"
mkdir -p "$TEST_REPO/scripts"
git init -q "$TEST_REPO"

cp "$REPO_DIR/Makefile" "$TEST_REPO/Makefile"
cp "$REPO_DIR/scripts/pre-commit.sh" "$TEST_REPO/scripts/pre-commit.sh"
cp "$REPO_DIR/scripts/commit-msg.sh" "$TEST_REPO/scripts/commit-msg.sh"
chmod +x "$TEST_REPO/scripts/pre-commit.sh" "$TEST_REPO/scripts/commit-msg.sh"

cp "$TEST_REPO/scripts/pre-commit.sh" "$TMPDIR_ROOT/pre-commit.expected"
cp "$TEST_REPO/scripts/commit-msg.sh" "$TMPDIR_ROOT/commit-msg.expected"

ln -s ../../scripts/pre-commit.sh "$TEST_REPO/.git/hooks/pre-commit"
ln -s ../../scripts/commit-msg.sh "$TEST_REPO/.git/hooks/commit-msg"

make -C "$TEST_REPO" install-hooks >/dev/null

PRE_COMMIT_HOOK="$TEST_REPO/$(cd "$TEST_REPO" && git rev-parse --git-path hooks/pre-commit)"
COMMIT_MSG_HOOK="$TEST_REPO/$(cd "$TEST_REPO" && git rev-parse --git-path hooks/commit-msg)"

[[ ! -L "$PRE_COMMIT_HOOK" ]] || fail "pre-commit hook should be a wrapper file, not a symlink"
[[ ! -L "$COMMIT_MSG_HOOK" ]] || fail "commit-msg hook should be a wrapper file, not a symlink"

assert_file_matches "scripts/pre-commit.sh was preserved" \
  "$TMPDIR_ROOT/pre-commit.expected" \
  "$TEST_REPO/scripts/pre-commit.sh"
assert_file_matches "scripts/commit-msg.sh was preserved" \
  "$TMPDIR_ROOT/commit-msg.expected" \
  "$TEST_REPO/scripts/commit-msg.sh"

cat > "$TMPDIR_ROOT/pre-commit.wrapper.expected" <<'EOF'
#!/bin/sh
set -eu
repo_root=$(git rev-parse --show-toplevel)
exec "$repo_root/scripts/pre-commit.sh" "$@"
EOF

cat > "$TMPDIR_ROOT/commit-msg.wrapper.expected" <<'EOF'
#!/bin/sh
set -eu
repo_root=$(git rev-parse --show-toplevel)
exec "$repo_root/scripts/commit-msg.sh" "$@"
EOF

assert_file_matches "pre-commit wrapper content" \
  "$TMPDIR_ROOT/pre-commit.wrapper.expected" \
  "$PRE_COMMIT_HOOK"
assert_file_matches "commit-msg wrapper content" \
  "$TMPDIR_ROOT/commit-msg.wrapper.expected" \
  "$COMMIT_MSG_HOOK"

echo "PASS: install-hooks safely replaced legacy symlinks without clobbering tracked scripts"
