#!/usr/bin/env bash
# commit-msg hook for td
# Install: make install-hooks  (recommended; resolves the active hooks path for linked worktrees too)
set -euo pipefail

message_file=${1:-}

trim() {
  local value=${1-}
  value=${value#"${value%%[![:space:]]*}"}
  value=${value%"${value##*[![:space:]]}"}
  printf '%s' "$value"
}

normalize_type() {
  local raw normalized

  raw=$(trim "${1-}")
  raw=$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]')
  raw=${raw//_/-}

  case "$raw" in
    feat|feature|features)
      normalized="feat"
      ;;
    fix|bug|bugfix|bug-fix|hotfix)
      normalized="fix"
      ;;
    docs|doc|documentation)
      normalized="docs"
      ;;
    chore|chores)
      normalized="chore"
      ;;
    build|ci|perf|refactor|revert|style)
      normalized="$raw"
      ;;
    test|tests|testing)
      normalized="test"
      ;;
    *)
      return 1
      ;;
  esac

  printf '%s' "$normalized"
}

normalize_scope() {
  local raw normalized

  raw=$(trim "${1-}")
  raw=${raw#[}
  raw=${raw%]}
  raw=$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]')
  normalized=$(printf '%s' "$raw" | sed -E 's/[[:space:]_]+/-/g; s/^-+//; s/-+$//; s/-+/-/g')

  if [[ -z "$normalized" || ! "$normalized" =~ ^[a-z0-9][a-z0-9._/-]*$ ]]; then
    return 1
  fi

  printf '%s' "$normalized"
}

normalize_summary() {
  local summary first_char

  summary=$(trim "${1-}")
  summary=$(printf '%s' "$summary" | sed -E 's/[[:space:]]+/ /g')

  if [[ "$summary" == *"." && "$summary" != *"..." ]]; then
    summary=${summary%.}
  fi

  if [[ "$summary" =~ ^([A-Z])([a-z].*)$ ]]; then
    first_char=$(printf '%s' "${BASH_REMATCH[1]}" | tr '[:upper:]' '[:lower:]')
    summary="${first_char}${BASH_REMATCH[2]}"
  fi

  printf '%s' "$summary"
}

print_error() {
  local subject=${1-}

  cat >&2 <<EOF
commit-msg: could not safely normalize this subject:
  $subject

Use one of:
  type: summary
  type(scope): summary

Optional suffix:
  type: summary (td-1234)

Examples:
  feat: add commit message normalizer
  fix(sync): preserve trailers (td-527bd4-0006)
  chore(homebrew): bump td to v1.2.3
EOF
}

is_valid_td_suffix() {
  local value

  value=$(trim "${1-}")
  [[ "$value" =~ ^[Tt][Dd]-[A-Za-z0-9._-]+$ ]]
}

is_td_like_suffix() {
  local value

  value=$(trim "${1-}")
  value=$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')

  [[ "$value" =~ ^td($|[^[:alpha:]].*) ]]
}

normalize_subject() {
  local subject trimmed base td_suffix type scope summary trailing_paren

  subject=${1-}
  trimmed=$(trim "$subject")

  if [[ -z "$trimmed" ]]; then
    print_error "$subject"
    return 1
  fi

  case "$trimmed" in
    Merge\ *|fixup!\ *|squash!\ *|Revert\ \"*\")
      printf '%s' "$trimmed"
      return 0
      ;;
  esac

  td_suffix=""
  base="$trimmed"
  if [[ "$base" =~ ^(.+)[[:space:]]+\(([^()]*)\)[[:space:]]*$ ]]; then
    trailing_paren=$(trim "${BASH_REMATCH[2]}")

    if ! is_valid_td_suffix "$trailing_paren" && is_td_like_suffix "$trailing_paren"; then
      print_error "$subject"
      return 1
    fi
  fi

  if [[ "$base" =~ ^(.+)[[:space:]]+\(([Tt][Dd]-[A-Za-z0-9._-]+)\)[[:space:]]*$ ]]; then
    base=$(trim "${BASH_REMATCH[1]}")
    td_suffix=" ($(printf '%s' "${BASH_REMATCH[2]}" | tr '[:upper:]' '[:lower:]'))"
  fi

  scope=""
  summary=""

  if [[ "$base" =~ ^([[:alpha:]][[:alnum:]_-]*)(\(([^()]*)\))?[[:space:]]*:[[:space:]]*(.+)$ ]]; then
    type=${BASH_REMATCH[1]}
    scope=${BASH_REMATCH[3]:-}
    summary=${BASH_REMATCH[4]}
  elif [[ "$base" =~ ^([[:alpha:]][[:alnum:]_-]*)(\(([^()]*)\))?[[:space:]]*-[[:space:]]*(.+)$ ]]; then
    type=${BASH_REMATCH[1]}
    scope=${BASH_REMATCH[3]:-}
    summary=${BASH_REMATCH[4]}
  elif [[ "$base" =~ ^([[:alpha:]][[:alnum:]_-]*)[[:space:]]+([[:alnum:]_./-]+)[[:space:]]*[:-][[:space:]]*(.+)$ ]]; then
    type=${BASH_REMATCH[1]}
    scope=${BASH_REMATCH[2]}
    summary=${BASH_REMATCH[3]}
  elif [[ "$base" =~ ^([[:alpha:]][[:alnum:]_-]*)[[:space:]]+(.+)$ ]]; then
    type=${BASH_REMATCH[1]}
    summary=${BASH_REMATCH[2]}
  else
    print_error "$subject"
    return 1
  fi

  if ! type=$(normalize_type "$type"); then
    print_error "$subject"
    return 1
  fi

  if [[ -n "$scope" ]]; then
    if ! scope=$(normalize_scope "$scope"); then
      print_error "$subject"
      return 1
    fi
  fi

  summary=$(normalize_summary "$summary")
  if [[ -z "$summary" ]]; then
    print_error "$subject"
    return 1
  fi

  if [[ -n "$scope" ]]; then
    printf '%s(%s): %s%s' "$type" "$scope" "$summary" "$td_suffix"
  else
    printf '%s: %s%s' "$type" "$summary" "$td_suffix"
  fi
}

if [[ -z "$message_file" || ! -f "$message_file" ]]; then
  echo "commit-msg: expected path to the commit message file" >&2
  exit 1
fi

lines=()
while IFS= read -r line || [[ -n "$line" ]]; do
  lines+=("$line")
done < "$message_file"
subject_index=-1

for i in "${!lines[@]}"; do
  line=${lines[$i]}
  if [[ -z $(trim "$line") || "$line" =~ ^# ]]; then
    continue
  fi
  subject_index=$i
  break
done

if (( subject_index < 0 )); then
  exit 0
fi

original_subject=${lines[$subject_index]}
normalized_subject=$(normalize_subject "$original_subject")

if [[ "$normalized_subject" != "$(trim "$original_subject")" ]]; then
  lines[$subject_index]=$normalized_subject
  printf '%s\n' "${lines[@]}" > "$message_file"
  echo "commit-msg: normalized subject to '$normalized_subject'" >&2
fi
