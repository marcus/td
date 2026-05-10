#!/usr/bin/env bash
# commit-msg hook for td
# Install all git hooks: make install-hooks
set -euo pipefail

MSG_FILE=${1:-}

if [[ -z "$MSG_FILE" || ! -f "$MSG_FILE" ]]; then
  echo "commit-msg: missing commit message file" >&2
  exit 1
fi

subject=$(
  sed '/^[[:space:]]*#/d; /^[[:space:]]*$/d; q' "$MSG_FILE" |
    sed 's/^[[:space:]]*//; s/[[:space:]]*$//'
)

if [[ -z "$subject" ]]; then
  echo "commit-msg: subject must not be empty" >&2
  exit 1
fi

case "$subject" in
  Merge\ *|Revert\ *|fixup!\ *|squash!\ *|amend!\ *)
    exit 0
    ;;
esac

if (( ${#subject} > 72 )); then
  echo "commit-msg: subject must be 72 characters or less" >&2
  exit 1
fi

type_pattern='(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test)'
if ! [[ "$subject" =~ ^${type_pattern}(\([a-z0-9._-]+\))?!?:[[:space:]][^[:space:]].* ]]; then
  echo "commit-msg: subject must use Conventional Commit format: type: summary" >&2
  echo "commit-msg: allowed types: build, chore, ci, docs, feat, fix, perf, refactor, revert, style, test" >&2
  exit 1
fi

nightshift_signal=0
if [[ -n "${NIGHTSHIFT_TASK:-}" || -n "${NIGHTSHIFT_REF:-}" || -n "${NIGHTSHIFT:-}" ]]; then
  nightshift_signal=1
elif grep -Eq '^Nightshift-(Task|Ref):' "$MSG_FILE"; then
  nightshift_signal=1
fi

if (( nightshift_signal )); then
  if ! grep -Eq '^Nightshift-Task: [^[:space:]].*$' "$MSG_FILE"; then
    echo "commit-msg: Nightshift work requires trailer: Nightshift-Task: <task-id>" >&2
    exit 1
  fi
  if ! grep -Eq '^Nightshift-Ref: https://github\.com/marcus/nightshift$' "$MSG_FILE"; then
    echo "commit-msg: Nightshift work requires trailer: Nightshift-Ref: https://github.com/marcus/nightshift" >&2
    exit 1
  fi
fi
