#!/usr/bin/env bash
# commit-msg hook for td
# Install: make install-hooks
set -euo pipefail

fail() {
  echo "error: $*" >&2
  exit 1
}

trim() {
  printf '%s' "$1" | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//'
}

normalize_type() {
  local canonical
  canonical=$(trim "$1")
  canonical=$(printf '%s' "$canonical" | tr '[:upper:]' '[:lower:]')
  canonical=$(printf '%s' "$canonical" | sed -E \
    -e 's/[[:space:]_]+/-/g' \
    -e 's/[^a-z0-9-]+/-/g' \
    -e 's/-+/-/g' \
    -e 's/^-+//' \
    -e 's/-+$//')

  [[ -n "$canonical" ]] || return 1

  case "$canonical" in
    feat|feature|features)
      canonical='feat'
      ;;
    fix|bug|bugfix|bugfixes|hotfix)
      canonical='fix'
      ;;
    docs|doc|documentation)
      canonical='docs'
      ;;
    chore|chores|maintenance|maint)
      canonical='chore'
      ;;
    refactor|refactoring)
      canonical='refactor'
      ;;
    test|tests|testing)
      canonical='test'
      ;;
    perf|performance)
      canonical='perf'
      ;;
    build|dep|deps|dependency|dependencies)
      canonical='build'
      ;;
    ci)
      canonical='ci'
      ;;
    style)
      canonical='style'
      ;;
  esac

  [[ "$canonical" =~ ^[a-z][a-z0-9-]*$ ]] || return 1
  printf '%s' "$canonical"
}

normalize_scope() {
  local scope
  scope=$(trim "$1")
  scope=$(printf '%s' "$scope" | tr '[:upper:]' '[:lower:]')
  scope=$(printf '%s' "$scope" | sed -E \
    -e 's/[[:space:]_]+/-/g' \
    -e 's/[^a-z0-9./-]+/-/g' \
    -e 's/-+/-/g' \
    -e 's#/+#/#g' \
    -e 's#(^[./-]+|[./-]+$)##g')

  [[ -n "$scope" ]] || return 1
  [[ "$scope" =~ ^[a-z0-9][a-z0-9./-]*$ ]] || return 1

  printf '%s' "$scope"
}

normalize_summary() {
  local summary
  summary=$(trim "$1")
  summary=$(printf '%s' "$summary" | tr -s '[:space:]' ' ')
  summary=$(printf '%s' "$summary" | sed -E \
    -e 's/[[:space:]]+([,.;!?])/\1/g' \
    -e 's/[[:space:]]+$//' \
    -e 's/\.$//')

  [[ -n "$summary" ]] || return 1

  if [[ "$summary" =~ ^[A-Z][a-z] ]]; then
    summary="$(printf '%s' "${summary:0:1}" | tr '[:upper:]' '[:lower:]')${summary:1}"
  fi

  printf '%s' "$summary"
}

is_valid_td_id() {
  [[ "$1" =~ ^td-[a-z0-9]+(-[a-z0-9]+)*$ ]]
}

is_git_generated_subject() {
  local subject="$1"

  if [[ "$subject" == "fixup! "* || "$subject" == "squash! "* ]]; then
    return 0
  fi

  if [[ "$subject" =~ ^Merge\ (branch|remote-tracking\ branch|tag)\ \'[^\']+\'([[:space:]]+into[[:space:]].+)?$ ]]; then
    return 0
  fi

  if [[ "$subject" =~ ^Merge\ pull\ request\ #[0-9]+\ from\ .+$ ]]; then
    return 0
  fi

  if [[ "$subject" =~ ^Revert\ \".+\"$ ]]; then
    return 0
  fi

  return 1
}

find_subject_line() {
  local file="$1"
  local comment_char="$2"
  local line
  local line_number=0

  SUBJECT_LINE_NUMBER=""
  SUBJECT_LINE=""

  while IFS= read -r line || [[ -n "$line" ]]; do
    line_number=$((line_number + 1))
    line="${line%$'\r'}"

    if [[ -z "$(trim "$line")" ]]; then
      continue
    fi

    if [[ "$line" == "$comment_char"* ]]; then
      continue
    fi

    SUBJECT_LINE_NUMBER="$line_number"
    SUBJECT_LINE="$line"
    return 0
  done < "$file"

  return 1
}

rewrite_subject_line() {
  local file="$1"
  local line_number="$2"
  local new_subject="$3"

  LINE_NUMBER="$line_number" NEW_SUBJECT="$new_subject" perl -i -pe '
    if ($. == $ENV{LINE_NUMBER}) {
      my $replacement = $ENV{NEW_SUBJECT};
      s/^[^\r\n]*/$replacement/;
    }
  ' "$file"
}

normalize_subject() {
  local subject="$1"
  local core_subject
  local td_suffix=""
  local td_id=""
  local malformed_td_suffix_regex='[(][[:space:]]*[Tt][Dd][^)]*[)][[:space:]]*$'
  local ambiguous_td_suffix_regex='[[:space:]][Tt][Dd]-[[:alnum:]-]+[[:space:]]*$'
  local type_raw
  local scope_raw
  local summary_raw
  local type
  local scope
  local summary

  subject=$(trim "$subject")

  if [[ "$subject" =~ ^(.*[^[:space:]])[[:space:]]*\([[:space:]]*([Tt][Dd]-[[:alnum:]-]+)[[:space:]]*\)[[:space:]]*$ ]]; then
    core_subject="${BASH_REMATCH[1]}"
    td_id=$(printf '%s' "${BASH_REMATCH[2]}" | tr '[:upper:]' '[:lower:]')
    is_valid_td_id "$td_id" || fail "malformed td suffix; use trailing ' (td-<id>)'"
    td_suffix=" ($td_id)"
  else
    core_subject="$subject"

    if [[ "$subject" =~ $malformed_td_suffix_regex ]]; then
      fail "malformed td suffix; use trailing ' (td-<id>)'"
    fi

    if [[ "$subject" =~ $ambiguous_td_suffix_regex ]]; then
      fail "ambiguous td suffix; wrap it as ' (td-<id>)'"
    fi
  fi

  core_subject=$(trim "$core_subject")

  if [[ "$core_subject" =~ ^([[:alpha:]][[:alnum:]_-]*)[[:space:]]*\(([[:alnum:][:space:]_./-]+)\)[[:space:]]*[:-][[:space:]]*(.+)$ ]]; then
    type_raw="${BASH_REMATCH[1]}"
    scope_raw="${BASH_REMATCH[2]}"
    summary_raw="${BASH_REMATCH[3]}"
    type=$(normalize_type "$type_raw") || fail "invalid commit type '$type_raw'"
    scope=$(normalize_scope "$scope_raw") || fail "invalid commit scope '$scope_raw'"
    summary=$(normalize_summary "$summary_raw") || fail "commit summary cannot be empty"
    NORMALIZED_SUBJECT="${type}(${scope}): ${summary}${td_suffix}"
    return 0
  fi

  if [[ "$core_subject" =~ ^([[:alpha:]][[:alnum:]_-]*)[[:space:]]*[:-][[:space:]]*(.+)$ ]]; then
    type_raw="${BASH_REMATCH[1]}"
    summary_raw="${BASH_REMATCH[2]}"
    type=$(normalize_type "$type_raw") || fail "invalid commit type '$type_raw'"
    summary=$(normalize_summary "$summary_raw") || fail "commit summary cannot be empty"
    NORMALIZED_SUBJECT="${type}: ${summary}${td_suffix}"
    return 0
  fi

  fail "commit subject must match 'type: summary' or 'type(scope): summary'"
}

main() {
  local commit_msg_file="${1:-}"
  local comment_char

  [[ -n "$commit_msg_file" ]] || fail "usage: $0 <commit-msg-file>"
  [[ -f "$commit_msg_file" ]] || fail "commit message file not found: $commit_msg_file"

  comment_char=$(git config --get core.commentChar 2>/dev/null || true)
  if [[ -z "$comment_char" || "$comment_char" == "auto" ]]; then
    comment_char="#"
  fi
  comment_char="${comment_char:0:1}"

  if ! find_subject_line "$commit_msg_file" "$comment_char"; then
    exit 0
  fi

  if is_git_generated_subject "$SUBJECT_LINE"; then
    exit 0
  fi

  normalize_subject "$SUBJECT_LINE"

  if [[ "$NORMALIZED_SUBJECT" != "$SUBJECT_LINE" ]]; then
    rewrite_subject_line "$commit_msg_file" "$SUBJECT_LINE_NUMBER" "$NORMALIZED_SUBJECT"
  fi
}

main "$@"
