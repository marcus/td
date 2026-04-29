#!/usr/bin/env bash
# commit-msg hook for td
# Install: make install-hooks
set -euo pipefail

allowed_types='feat|fix|docs|test|refactor|chore|ci|build|perf|style|td'
passthrough='^(Merge( |$)|Revert( |$)|fixup!|squash!)'

usage() {
  echo "usage: commit-msg <path-to-commit-message-file>" >&2
}

invalid() {
  cat >&2 <<EOF
Invalid commit subject: $1

Use Conventional Commit format:
  type: subject
  type(scope): subject

Allowed types: feat, fix, docs, test, refactor, chore, ci, build, perf, style, td
EOF
  exit 1
}

if [[ $# -ne 1 ]]; then
  usage
  exit 1
fi

msg_file=$1
if [[ ! -f "$msg_file" ]]; then
  echo "commit message file not found: $msg_file" >&2
  exit 1
fi

IFS= read -r first_line < "$msg_file" || first_line=""

if [[ "$first_line" =~ $passthrough ]]; then
  exit 0
fi

subject=$(printf '%s' "$first_line" | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//; s/[[:space:]]+/ /g')

shopt -s nocasematch
if [[ "$subject" =~ ^($allowed_types)(\([A-Za-z0-9._/-]+\))?(!)?[[:space:]]*:[[:space:]]*(.+)$ ]]; then
  type=$(printf '%s' "${BASH_REMATCH[1]}" | tr '[:upper:]' '[:lower:]')
  scope=${BASH_REMATCH[2]}
  bang=${BASH_REMATCH[3]}
  summary=$(printf '%s' "${BASH_REMATCH[4]}" | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//; s/[[:space:]]+/ /g')
  [[ -n "$summary" ]] || invalid "$first_line"
  normalized="${type}${scope}${bang}: ${summary}"
else
  invalid "$first_line"
fi
shopt -u nocasematch

if [[ "$normalized" == "$first_line" ]]; then
  exit 0
fi

tmp=$(mktemp "${msg_file}.XXXXXX")
trap 'rm -f "$tmp"' EXIT

printf '%s\n' "$normalized" > "$tmp"
tail -n +2 "$msg_file" >> "$tmp" || true
mv "$tmp" "$msg_file"
trap - EXIT
