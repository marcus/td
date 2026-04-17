#!/usr/bin/env bash
# commit-msg hook for td
# Install: make install-hooks (run it from the checkout or linked worktree you commit from)
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: commit-msg <path-to-commit-message-file>" >&2
  exit 1
fi

repo_root=$(git rev-parse --show-toplevel)
td_bin=${TD_BIN:-td}

if ! command -v "$td_bin" >/dev/null 2>&1 && [[ ! -x "$td_bin" ]]; then
  echo "td commit-msg hook requires '$td_bin' in PATH. Run 'make install' first." >&2
  exit 1
fi

"$td_bin" --work-dir "$repo_root" commit-message --file "$1"
