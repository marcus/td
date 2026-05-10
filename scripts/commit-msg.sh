#!/usr/bin/env bash
# commit-msg hook for td
# Install: make install-hooks  (or: ln -sf ../../scripts/commit-msg.sh .git/hooks/commit-msg)
set -euo pipefail

MESSAGE_FILE="${1:-}"
ALLOWED_TYPES_REGEX='^(feat|fix|chore|docs|refactor|test|ci|build|perf|style|revert)$'

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

normalize_subject() {
  local original="$1"
  local working prefix raw_type remainder type task_suffix normalized

  working="$(trim "$original")"
  prefix=""

  if [[ "$working" =~ ^((fixup|squash)!\ )(.*)$ ]]; then
    prefix="${BASH_REMATCH[1]}"
    working="${BASH_REMATCH[3]}"
  fi

  if [[ "$working" == Merge\ * || "$working" == Revert\ \"* ]]; then
    printf '%s%s\n' "$prefix" "$working"
    return 0
  fi

  if [[ "$working" =~ ^([[:alpha:]]+)[[:space:]]*:[[:space:]]*(.+)$ ]]; then
    raw_type="${BASH_REMATCH[1]}"
    remainder="${BASH_REMATCH[2]}"
  elif [[ "$working" =~ ^([[:alpha:]]+)[[:space:]]+(.+)$ ]]; then
    raw_type="${BASH_REMATCH[1]}"
    remainder="${BASH_REMATCH[2]}"
  else
    return 1
  fi

  type="$(printf '%s' "$raw_type" | tr '[:upper:]' '[:lower:]')"
  if ! [[ "$type" =~ $ALLOWED_TYPES_REGEX ]]; then
    return 1
  fi

  remainder="$(trim "$remainder")"
  task_suffix=""
  if [[ "$remainder" =~ ^(.*)[[:space:]]+\(([Tt][Dd]-[[:alnum:]][[:alnum:]-]*)\)$ ]]; then
    remainder="$(trim "${BASH_REMATCH[1]}")"
    task_suffix="$(printf '%s' "${BASH_REMATCH[2]}" | tr '[:upper:]' '[:lower:]')"
  fi

  if [[ -z "$remainder" ]]; then
    return 1
  fi

  normalized="${prefix}${type}: ${remainder}"
  if [[ -n "$task_suffix" ]]; then
    normalized="${normalized} (${task_suffix})"
  fi

  printf '%s\n' "$normalized"
}

if [[ -z "$MESSAGE_FILE" || ! -f "$MESSAGE_FILE" ]]; then
  echo "commit-msg: expected the commit message file path as the first argument." >&2
  exit 1
fi

lines=()
while IFS= read -r line || [[ -n "$line" ]]; do
  lines+=("$line")
done < "$MESSAGE_FILE"
subject_index=-1
subject_line=""

for i in "${!lines[@]}"; do
  line="${lines[$i]}"
  trimmed_line="$(trim "$line")"
  if [[ -z "$trimmed_line" || "$trimmed_line" == \#* ]]; then
    continue
  fi
  subject_index="$i"
  subject_line="$line"
  break
done

if [[ "$subject_index" -lt 0 ]]; then
  exit 0
fi

if ! normalized_subject="$(normalize_subject "$subject_line")"; then
  cat >&2 <<'EOF'
Commit message must use one of these formats:
  type: summary (td-<id>)   # normal development work
  type: summary             # automation/release work without a td task

Examples:
  feat: add session analytics (td-a1b2)
  chore: bump homebrew formula to v0.2.0

Allowed types: feat, fix, chore, docs, refactor, test, ci, build, perf, style, revert
EOF
  exit 1
fi

if [[ "$(trim "$subject_line")" != "$normalized_subject" ]]; then
  lines[$subject_index]="$normalized_subject"
  printf '%s\n' "${lines[@]}" > "$MESSAGE_FILE"
  echo "Normalized commit subject to: $normalized_subject"
fi
