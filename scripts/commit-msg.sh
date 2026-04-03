#!/usr/bin/env bash
# commit-msg hook for td
# Install: make install-hooks
set -euo pipefail

msg_file=${1:-}
canonical_subject_re='^[a-z][a-z0-9-]*(\([[:alnum:]_.-]+\))?:[[:space:]].+([[:space:]]\(td-[a-z0-9]+\))?$'
bracketed_ticket_re='^\[([Tt][Dd]-[A-Za-z0-9]+)\][[:space:]]*(.+)$'
trailing_ticket_re='^(.+)[[:space:]]+\((td-[A-Za-z0-9]+)\)$'
type_with_colon_re='^([A-Za-z][A-Za-z0-9-]*)(\([^)]+\))?:[[:space:]]+(.+)$'
type_with_dash_re='^([A-Za-z][A-Za-z0-9-]*)(\([^)]+\))?[[:space:]]*-[[:space:]]+(.+)$'
type_with_space_re='^([A-Za-z][A-Za-z0-9-]*)(\([^)]+\))?[[:space:]]+(.+)$'

if [[ -z "$msg_file" || ! -f "$msg_file" ]]; then
  exit 0
fi

lower() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

canonical_type() {
  local raw
  raw="$(lower "$1")"
  case "$raw" in
    feat|feature)
      printf 'feat'
      ;;
    fix|bugfix|hotfix)
      printf 'fix'
      ;;
    docs|doc)
      printf 'docs'
      ;;
    chore|refactor|perf|test|build|ci|style)
      printf '%s' "$raw"
      ;;
    *)
      return 1
      ;;
  esac
}

is_bypass_subject() {
  local subject="$1"
  [[ -z "$subject" ]] && return 0
  [[ "$subject" =~ ^Merge([[:space:]]|$) ]] && return 0
  [[ "$subject" =~ ^Revert([[:space:]]|$) ]] && return 0
  [[ "$subject" =~ ^(fixup!|squash!|amend!|reword!) ]] && return 0
  return 1
}

is_canonical_subject() {
  local subject="$1"
  [[ "$subject" =~ $canonical_subject_re ]]
}

normalize_subject() {
  local subject="$1"
  local ticket_suffix=""
  local type_part=""
  local scope_part=""
  local summary=""
  local mapped_type=""

  if is_bypass_subject "$subject"; then
    printf '%s' "$subject"
    return 0
  fi

  if [[ "$subject" =~ $bracketed_ticket_re ]]; then
    ticket_suffix="$(lower "${BASH_REMATCH[1]}")"
    subject="${BASH_REMATCH[2]}"
  fi

  if [[ "$subject" =~ $trailing_ticket_re ]]; then
    subject="${BASH_REMATCH[1]}"
    if [[ -z "$ticket_suffix" ]]; then
      ticket_suffix="$(lower "${BASH_REMATCH[2]}")"
    fi
  fi

  subject="$(trim "$subject")"

  if is_canonical_subject "$subject"; then
    summary="$subject"
  elif [[ "$subject" =~ $type_with_colon_re ]]; then
    type_part="${BASH_REMATCH[1]}"
    scope_part="${BASH_REMATCH[2]}"
    summary="${BASH_REMATCH[3]}"
    if mapped_type="$(canonical_type "$type_part" 2>/dev/null)"; then
      summary="${mapped_type}${scope_part}: ${summary}"
    else
      summary="chore: ${subject}"
    fi
  elif [[ "$subject" =~ $type_with_dash_re ]]; then
    type_part="${BASH_REMATCH[1]}"
    scope_part="${BASH_REMATCH[2]}"
    summary="${BASH_REMATCH[3]}"
    if mapped_type="$(canonical_type "$type_part" 2>/dev/null)"; then
      summary="${mapped_type}${scope_part}: ${summary}"
    else
      summary="chore: ${subject}"
    fi
  elif [[ "$subject" =~ $type_with_space_re ]]; then
    type_part="${BASH_REMATCH[1]}"
    scope_part="${BASH_REMATCH[2]}"
    summary="${BASH_REMATCH[3]}"
    if mapped_type="$(canonical_type "$type_part" 2>/dev/null)"; then
      summary="${mapped_type}${scope_part}: ${summary}"
    else
      summary="chore: ${subject}"
    fi
  else
    summary="chore: ${subject}"
  fi

  if [[ -n "$ticket_suffix" && ! "$summary" =~ \(td-[a-z0-9]+\)$ ]]; then
    summary="${summary} (${ticket_suffix})"
  fi

  printf '%s' "$summary"
}

first_line=""
IFS= read -r first_line < "$msg_file" || true

normalized_subject="$(normalize_subject "$first_line")"

if [[ "$normalized_subject" == "$first_line" ]]; then
  exit 0
fi

tmp_file="$(mktemp)"
trap 'rm -f "$tmp_file"' EXIT

printf '%s\n' "$normalized_subject" > "$tmp_file"
if [[ $(wc -l < "$msg_file") -gt 1 ]]; then
  tail -n +2 "$msg_file" >> "$tmp_file"
fi

mv "$tmp_file" "$msg_file"
