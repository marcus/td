#!/usr/bin/env bash
# commit-msg hook for td
# Install: make install-hooks
#   or: install -m 0755 scripts/commit-msg.sh "$(git rev-parse --git-path hooks)/commit-msg"
set -euo pipefail

msg_file=${1:?usage: commit-msg.sh <commit-message-file>}

trim() {
  sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//'
}

fail() {
  cat >&2 <<'EOF'
Commit subject must use one of these canonical formats:
  type: summary
  type: summary (td-<id>)

The hook only normalizes safe prefix inconsistencies such as type casing or
spacing around the colon. Any other trailing parenthetical suffix is rejected.

Examples:
  feat: normalize commit messages (td-2b41b2)
  docs: update changelog for v0.40.0
EOF
  exit 1
}

if [[ ! -f "$msg_file" ]]; then
  echo "commit-msg hook could not read $msg_file" >&2
  exit 1
fi

if IFS= read -r subject <"$msg_file"; then
  :
else
  subject=""
fi

trimmed_subject=$(printf '%s' "$subject" | trim)

if [[ -z "$trimmed_subject" ]]; then
  fail
fi

# Preserve Git workflow subjects that rely on special prefixes.
if [[ "$trimmed_subject" =~ ^(fixup\!\ |squash\!\ |Merge |Revert ) ]]; then
  exit 0
fi

if [[ ! "$trimmed_subject" =~ ^([[:alpha:]][[:alnum:]-]*)[[:space:]]*:[[:space:]]*(.+)$ ]]; then
  fail
fi

type_part=${BASH_REMATCH[1]}
summary_part=${BASH_REMATCH[2]}

normalized_type=$(printf '%s' "$type_part" | tr '[:upper:]' '[:lower:]')
normalized_summary=$(printf '%s' "$summary_part" | trim)

if [[ -z "$normalized_summary" ]]; then
  fail
fi

if [[ "$normalized_summary" =~ ^(.*)[[:space:]]+\(([^()]*)\)$ ]]; then
  summary_without_suffix=$(printf '%s' "${BASH_REMATCH[1]}" | trim)
  suffix=${BASH_REMATCH[2]}

  if [[ ! "$suffix" =~ ^td-[a-f0-9]+$ ]]; then
    fail
  fi
  if [[ -z "$summary_without_suffix" ]]; then
    fail
  fi

  normalized_summary="${summary_without_suffix} (${suffix})"
fi

normalized_subject="${normalized_type}: ${normalized_summary}"

if [[ "$normalized_subject" == "$subject" ]]; then
  exit 0
fi

tmp_file=$(mktemp)
trap 'rm -f "$tmp_file"' EXIT

printf '%s\n' "$normalized_subject" >"$tmp_file"
if [[ $(wc -l <"$msg_file") -gt 1 ]]; then
  tail -n +2 "$msg_file" >>"$tmp_file"
fi

mv "$tmp_file" "$msg_file"
trap - EXIT
