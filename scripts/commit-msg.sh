#!/usr/bin/env bash
# commit-msg hook for td
# Install: make install-hooks  (or: ln -sf ../../scripts/commit-msg.sh .git/hooks/commit-msg)
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <commit-message-file>" >&2
  exit 1
fi

msg_file=$1
subject_pattern='^([[:alpha:]][[:alnum:]-]*)(\([^()]+\))?:[[:space:]]*(.+)$'
paren_task_pattern='^(.*[^[:space:]])[[:space:]]+\((td-[[:alnum:]]+)\)$'
bare_task_pattern='^(.*[^[:space:]])[[:space:]]+(td-[[:alnum:]]+)$'

trim() {
  local value=$1
  value=${value#"${value%%[![:space:]]*}"}
  value=${value%"${value##*[![:space:]]}"}
  printf '%s' "$value"
}

subject=""
if IFS= read -r subject <"$msg_file"; then
  :
fi
subject=$(trim "$subject")

if [[ -z "$subject" ]]; then
  echo "commit message subject is required" >&2
  exit 1
fi

# Preserve Git's generated subjects and the automated Homebrew tap bump commit.
if [[ "$subject" =~ ^(Merge|Revert|fixup\!|squash\!)\  ]]; then
  exit 0
fi
if [[ "$subject" =~ ^td:\ bump\ to\ v[0-9]+\.[0-9]+\.[0-9]+([.-][[:alnum:]]+)*$ ]]; then
  exit 0
fi

if [[ ! "$subject" =~ $subject_pattern ]]; then
  cat >&2 <<'EOF'
invalid commit message subject

Expected format:
  type: summary (td-<id>)
  type(scope): summary (td-<id>)

Example:
  feat(sync): persist cursor handling (td-a1b2)
EOF
  exit 1
fi

prefix=${BASH_REMATCH[1]}
scope=${BASH_REMATCH[2]:-}
rest=$(trim "${BASH_REMATCH[3]}")

summary=
task_ref=
if [[ "$rest" =~ $paren_task_pattern ]]; then
  summary=$(trim "${BASH_REMATCH[1]}")
  task_ref=${BASH_REMATCH[2]}
elif [[ "$rest" =~ $bare_task_pattern ]]; then
  summary=$(trim "${BASH_REMATCH[1]}")
  task_ref=${BASH_REMATCH[2]}
else
  cat >&2 <<'EOF'
missing trailing task reference in commit message subject

Expected format:
  type: summary (td-<id>)
  type(scope): summary (td-<id>)

Example:
  fix(db): preserve audit trail ordering (td-a1b2)
EOF
  exit 1
fi

if [[ -z "$summary" ]]; then
  echo "commit message summary cannot be empty" >&2
  exit 1
fi

normalized="${prefix}${scope}: ${summary} (${task_ref})"

if [[ "$normalized" == "$subject" ]]; then
  exit 0
fi

tmp_file=$(mktemp)
trap 'rm -f "$tmp_file"' EXIT

printf '%s\n' "$normalized" >"$tmp_file"
if [[ $(wc -l <"$msg_file") -gt 1 ]]; then
  tail -n +2 "$msg_file" >>"$tmp_file"
fi

mv "$tmp_file" "$msg_file"
