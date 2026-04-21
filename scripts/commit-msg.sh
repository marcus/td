#!/usr/bin/env bash
# commit-msg hook for td
# Install: make install-hooks (preferred)
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "commit-msg hook expects the commit message file path" >&2
  exit 1
fi

message_file=$1

if [[ ! -f "$message_file" ]]; then
  echo "commit-msg hook could not find commit message file: $message_file" >&2
  exit 1
fi

trim() {
  local value=$1
  value=${value#"${value%%[![:space:]]*}"}
  value=${value%"${value##*[![:space:]]}"}
  printf '%s' "$value"
}

lines=()
while IFS= read -r line || [[ -n $line ]]; do
  lines+=("$line")
done < "$message_file"

first_line_index=-1
for i in "${!lines[@]}"; do
  trimmed_line=$(trim "${lines[$i]%$'\r'}")
  if [[ -n "$trimmed_line" ]]; then
    first_line_index=$i
    break
  fi
done

if (( first_line_index < 0 )); then
  cat >&2 <<'EOF'
Invalid commit message.

Use one of:
  type: summary
  type: summary (td-<id>)
EOF
  exit 1
fi

subject=$(trim "${lines[$first_line_index]%$'\r'}")

if [[ ! $subject =~ ^([A-Za-z][A-Za-z0-9-]*)[[:space:]]*:?[[:space:]]*(.+)$ ]]; then
  cat >&2 <<EOF
Invalid commit subject: $subject

Use one of:
  type: summary
  type: summary (td-<id>)

Examples:
  feat: normalize commit messages (td-a1b2)
  chore: bump Homebrew formula to v1.2.3
EOF
  exit 1
fi

type_part=$(printf '%s' "${BASH_REMATCH[1]}" | tr '[:upper:]' '[:lower:]')
rest=$(trim "${BASH_REMATCH[2]}")

if [[ -z $rest ]]; then
  cat >&2 <<'EOF'
Invalid commit subject.

The summary cannot be empty.
Use one of:
  type: summary
  type: summary (td-<id>)
EOF
  exit 1
fi

task_suffix=""
summary=$rest

if [[ $rest == *" ("*")" ]]; then
  task_suffix="(${rest##* (}"
  summary=$(trim "${rest% $task_suffix}")
  if ! printf '%s\n' "$task_suffix" | grep -Eq '^\(td-[a-z0-9][a-z0-9-]*\)$'; then
    cat >&2 <<EOF
Invalid commit subject: $subject

The only allowed trailing parenthetical suffix is (td-<id>).
Use one of:
  type: summary
  type: summary (td-<id>)
EOF
    exit 1
  fi
fi

if [[ -z $summary ]]; then
  cat >&2 <<'EOF'
Invalid commit subject.

The summary cannot be empty.
Use one of:
  type: summary
  type: summary (td-<id>)
EOF
  exit 1
fi

normalized_subject="$type_part: $summary"
if [[ -n $task_suffix ]]; then
  normalized_subject+=" $task_suffix"
fi

last_non_empty_index=$(( ${#lines[@]} - 1 ))
while (( last_non_empty_index >= 0 )); do
  trimmed_line=$(trim "${lines[$last_non_empty_index]%$'\r'}")
  if [[ -n "$trimmed_line" ]]; then
    break
  fi
  ((last_non_empty_index--))
done

{
  printf '%s\n' "$normalized_subject"
  if (( first_line_index < last_non_empty_index )); then
    for ((i = first_line_index + 1; i <= last_non_empty_index; i++)); do
      printf '%s\n' "${lines[$i]%$'\r'}"
    done
  fi
} > "$message_file"
