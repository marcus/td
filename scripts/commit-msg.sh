#!/usr/bin/env bash
# commit-msg hook for td
# Install: make install-hooks
set -euo pipefail

MSG_FILE="${1:-}"
ALLOWED_TYPES_DISPLAY='feat, fix, docs, test, chore, ci, perf, refactor, style, release'

if [[ -z "$MSG_FILE" || ! -f "$MSG_FILE" ]]; then
  echo "Usage: $0 <commit-message-file>" >&2
  exit 1
fi

trim_and_collapse() {
  printf '%s' "$1" | tr '\t' ' ' | sed -E 's/[[:space:]]+/ /g; s/^ //; s/ $//'
}

lowercase() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

is_allowed_type() {
  case "$1" in
    feat|fix|docs|test|chore|ci|perf|refactor|style|release)
      return 0
      ;;
  esac

  return 1
}

parse_subject() {
  local subject="$1"
  local pattern="$2"

  printf '%s\n' "$subject" | sed -En "s/$pattern/\\1|\\2|\\3/p" | head -n 1
}

is_exempt_subject() {
  local subject="$1"

  case "$subject" in
    Merge*|Merged*|merge*)
      return 0
      ;;
    Revert*|revert*)
      return 0
      ;;
    fixup!\ *|squash!\ *)
      return 0
      ;;
    release:*|release\(*\):*|Release:*|Release\(*\):*)
      return 0
      ;;
    Version*|Bump\ version*)
      return 0
      ;;
  esac

  printf '%s\n' "$subject" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z]+)?$' && return 0

  return 1
}

normalize_subject() {
  local subject="$1"
  local cleaned type scope summary
  local with_colon_regex='^([A-Za-z]+)(\([^)]+\))?[[:space:]]*:[[:space:]]*(.+)$'
  local scoped_missing_colon_regex='^([A-Za-z]+)(\([^)]+\))[[:space:]]+(.+)$'
  local unscoped_missing_colon_regex='^([A-Za-z]+)[[:space:]]+(.+)$'
  local malformed_scope_summary_regex='^\([^)]+\)(:|[[:space:]]|$)'

  cleaned="$(trim_and_collapse "$subject")"

  if [[ "$cleaned" =~ $with_colon_regex ]]; then
    type="$(lowercase "${BASH_REMATCH[1]}")"
    scope="${BASH_REMATCH[2]}"
    summary="${BASH_REMATCH[3]}"
    if is_allowed_type "$type"; then
      printf '%s' "$(trim_and_collapse "$type$scope: $summary")"
      return 0
    fi
  fi

  if [[ "$cleaned" =~ $scoped_missing_colon_regex ]]; then
    type="$(lowercase "${BASH_REMATCH[1]}")"
    scope="${BASH_REMATCH[2]}"
    summary="${BASH_REMATCH[3]}"
    if is_allowed_type "$type"; then
      printf '%s' "$(trim_and_collapse "$type$scope: $summary")"
      return 0
    fi
  fi

  if [[ "$cleaned" =~ $unscoped_missing_colon_regex ]]; then
    type="$(lowercase "${BASH_REMATCH[1]}")"
    summary="$(trim_and_collapse "${BASH_REMATCH[2]}")"
    if is_allowed_type "$type"; then
      if [[ "$summary" =~ $malformed_scope_summary_regex ]]; then
        printf '%s' "$cleaned"
        return 0
      fi
      printf '%s' "$(trim_and_collapse "$type: $summary")"
      return 0
    fi
  fi

  printf '%s' "$cleaned"
}

strip_trailing_refs() {
  local subject="$1"
  local stripped
  local trailing_ref_regex='^(.+)[[:space:]]+\((td-[[:alnum:]]+|#[0-9]+)\)$'
  local ref_only_regex='^\((td-[[:alnum:]]+|#[0-9]+)\)$'

  stripped="$(trim_and_collapse "$subject")"

  while [[ "$stripped" =~ $trailing_ref_regex ]]; do
    stripped="$(trim_and_collapse "${BASH_REMATCH[1]}")"
  done

  if [[ "$stripped" =~ $ref_only_regex ]]; then
    stripped=""
  fi

  printf '%s' "$stripped"
}

is_valid_subject() {
  local subject="$1"
  local parsed type scope summary

  [[ -n "$subject" ]] || return 1

  parsed="$(parse_subject "$subject" '^([a-z]+)(\([^)]+\))?:[[:space:]]+(.+)$')"
  if [[ -n "$parsed" ]]; then
    IFS='|' read -r type scope summary <<EOF
$parsed
EOF
    is_allowed_type "$type" || return 1
    summary="$(strip_trailing_refs "$summary")"
    [[ -n "$summary" ]] || return 1
    return 0
  fi

  return 1
}

rewrite_subject_line() {
  local new_subject="$1"
  local tmp_file

  tmp_file="$(mktemp "${TMPDIR:-/tmp}/td-commit-msg-XXXXXX")"
  {
    printf '%s\n' "$new_subject"
    sed '1d' "$MSG_FILE"
  } >"$tmp_file"
  mv "$tmp_file" "$MSG_FILE"
}

subject="$(sed -n '1p' "$MSG_FILE")"
trimmed_subject="$(trim_and_collapse "$subject")"

if is_exempt_subject "$trimmed_subject"; then
  exit 0
fi

normalized_subject="$(normalize_subject "$subject")"

if is_valid_subject "$normalized_subject"; then
  if [[ "$normalized_subject" != "$subject" ]]; then
    rewrite_subject_line "$normalized_subject"
    echo "Normalized commit subject: $normalized_subject"
  fi
  exit 0
fi

cat >&2 <<EOF
Commit subject must match: type(scope)?: summary
Allowed types: $ALLOWED_TYPES_DISPLAY
Optional trailing refs: (td-<id>) and/or (#123)

Examples:
  feat(cli): add commit message normalizer (td-a1b2c3)
  fix(review): preserve Nightshift trailers (#91)
  docs: document install-hooks behavior

Exempt flows:
  Merge..., Revert..., fixup!, squash!, release/version commits

Scopes must be attached directly to the type:
  fix(cli): preserve trailers
  not: fix (cli): preserve trailers

To fix the last commit message:
  git commit --amend
EOF
exit 1
