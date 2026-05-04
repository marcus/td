#!/usr/bin/env bash
# commit-msg hook for td.
set -euo pipefail

msg_file="${1:-}"
if [[ -z "$msg_file" || ! -f "$msg_file" ]]; then
  echo "commit-msg: expected path to commit message file" >&2
  exit 1
fi

first_line=""
IFS= read -r first_line < "$msg_file" || true

trimmed=$(
  printf "%s\n" "$first_line" | awk '{$1=$1; print}'
)

case "$trimmed" in
  Merge\ *|Revert\ *|fixup!\ *|squash!\ *)
    exit 0
    ;;
esac

if [[ ! "$trimmed" =~ ^([[:alpha:]]+)(\([^()[:space:]]+\))?(!)?:[[:space:]](.+)$ ]]; then
  cat >&2 <<EOF
commit-msg: invalid commit subject:
  $first_line

Expected: type: subject, type(scope): subject, type!: subject, or type(scope)!: subject
Allowed types: feat, fix, docs, test, refactor, chore, ci, build, perf, style, td
Pass-through subjects: Merge, Revert, fixup!, squash!
EOF
  exit 1
fi

type="${BASH_REMATCH[1]}"
lower_type=$(
  printf "%s" "$type" | tr '[:upper:]' '[:lower:]'
)

case "$lower_type" in
  feat|fix|docs|test|refactor|chore|ci|build|perf|style|td)
    ;;
  *)
    cat >&2 <<EOF
commit-msg: invalid commit type: $type

Allowed types: feat, fix, docs, test, refactor, chore, ci, build, perf, style, td
Expected subject format: type: subject
EOF
    exit 1
    ;;
esac

normalized="${lower_type}${trimmed:${#type}}"

if [[ "$normalized" != "$first_line" ]]; then
  tmp_file=$(mktemp "${msg_file}.XXXXXX")
  {
    printf "%s\n" "$normalized"
    sed '1d' "$msg_file"
  } > "$tmp_file"
  mv "$tmp_file" "$msg_file"
fi
