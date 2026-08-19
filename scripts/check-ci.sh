#!/usr/bin/env bash
set -euo pipefail

# Go CI must be green on the commit being released.
#
# Fails closed if CI is red, still running, or has not started. Skips with a
# warning if `gh` cannot resolve a GitHub repo here (no origin, or origin is
# not github.com/marcus/td).
#
# The commit being released is normally docs-only — the changelog stamp — and
# go-ci.yml's path filters skip it, so a strict "find a run for this SHA" gate
# finds nothing and blocks the release forever. The workflow carries a
# workflow_dispatch trigger for exactly this case; dispatch it here and wait
# rather than leaving the operator to remember.
#
#   RELEASE_CI_WAIT=0       fail fast instead of dispatching and polling
#   RELEASE_CI_TIMEOUT=N    seconds to wait for the run (default 1800)

if ! command -v gh >/dev/null 2>&1 || ! gh repo view >/dev/null 2>&1; then
  echo "Warning: gh unavailable or origin is not a resolvable GitHub repo; skipping automated Go CI status check" >&2
  exit 0
fi

head=$(git rev-parse origin/main)
ci_wait=${RELEASE_CI_WAIT:-1}

runs_for_head() {
  gh run list --workflow=go-ci.yml --branch main --limit 20 \
    --json headSha,status,conclusion -q \
    "[.[] | select(.headSha == \"$head\")]" 2>/dev/null || echo '[]'
}

runs=$(runs_for_head)
if [[ $(jq 'length' <<<"$runs") == 0 ]]; then
  if [[ $ci_wait == 0 ]]; then
    echo "Error: no Go CI run found for $head yet; wait for it to start" >&2
    exit 1
  fi
  echo "No Go CI run for $head (path filters skip docs-only commits); dispatching one..." >&2
  if ! gh workflow run go-ci.yml --ref main >/dev/null 2>&1; then
    echo "Error: could not dispatch Go CI for $head; run it manually and retry" >&2
    exit 1
  fi
fi

deadline=$((SECONDS + ${RELEASE_CI_TIMEOUT:-1800}))
while :; do
  runs=$(runs_for_head)
  status=$(jq -r '.[0].status // "missing"' <<<"$runs")
  conclusion=$(jq -r '.[0].conclusion // ""' <<<"$runs")
  [[ $status == completed ]] && break
  if [[ $ci_wait == 0 ]]; then
    echo "Error: Go CI is still $status on $head; wait for it to finish" >&2
    exit 1
  fi
  if ((SECONDS >= deadline)); then
    echo "Error: timed out waiting for Go CI on $head (last status: $status)" >&2
    exit 1
  fi
  echo "  Go CI on $head is $status; waiting..." >&2
  sleep 20
done

if [[ $conclusion != success ]]; then
  echo "Error: Go CI is $conclusion on $head; fix it before releasing" >&2
  exit 1
fi
echo "Go CI is green on $head"
