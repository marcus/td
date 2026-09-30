#!/usr/bin/env bash
set -euo pipefail

# A tag push must not bypass the checks enforced by `make release`.
version=${RELEASE_VERSION:?RELEASE_VERSION is required}
if [[ ! $version =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo "Error: RELEASE_VERSION must be vX.Y.Z without leading zeroes" >&2
  exit 1
fi

head=$(git rev-parse HEAD)
tag=$(git rev-parse "refs/tags/$version^{commit}")
remote_head=$(git ls-remote --exit-code origin refs/heads/main | awk '{print $1}')
if [[ $head != "$tag" || $head != "$remote_head" ]]; then
  echo "Error: release tag and HEAD must match live origin/main" >&2
  exit 1
fi
if ! grep -Fq "## [$version] - " CHANGELOG.md; then
  echo "Error: CHANGELOG.md has no $version release entry" >&2
  exit 1
fi

# Deliberately read-only and fail-closed. `make release` dispatches missing CI
# before tagging; publication itself cannot silently skip unavailable checks.
runs=$(gh run list --workflow=go-ci.yml --branch main --commit "$head" --limit 1 \
  --json status,conclusion)
if ! jq -e '.[0].status == "completed" and .[0].conclusion == "success"' <<<"$runs" >/dev/null; then
  echo "Error: Go CI must be completed and successful on $head before publication" >&2
  exit 1
fi
echo "Release $version matches live main and successful Go CI ($head)"
