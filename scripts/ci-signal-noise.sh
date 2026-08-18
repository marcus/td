#!/usr/bin/env bash
# ci-signal-noise.sh — score GitHub Actions signal-to-noise ratio.
#
# Classifies recent workflow run failures as either "signal" (real code defect)
# or "noise" (flaky, infra, timeout, cancelled, succeeded-on-retry), then
# computes a signal-to-noise score for the repository.
#
# Usage: ci-signal-noise.sh [--limit N] [--workflow NAME] [--since DURATION] [--json] [--help]

set -euo pipefail

LIMIT=50
WORKFLOW=""
SINCE=""
JSON=0

usage() {
  cat <<'EOF'
ci-signal-noise.sh — score GitHub Actions signal-to-noise ratio.

Options:
  --limit N         Number of recent runs to inspect (default 50)
  --workflow NAME   Only consider runs from this workflow (name or filename)
  --since DURATION  Only include runs created after this ISO-8601 duration
                    relative to now (e.g. 7d, 24h). Implemented via gh's
                    --created filter; pass "7d" -> ">=YYYY-MM-DD".
  --json            Emit machine-readable JSON instead of a table
  -h, --help        Show this help

Heuristics for "noise":
  - conclusion in {cancelled, timed_out, skipped, neutral, stale}
  - run_attempt > 1 AND final conclusion == success (succeeded on retry)
  - failure job logs match common infra/network keywords (ECONNRESET, 429,
    503, "could not resolve host", "runner lost", "rate limit", "network",
    "timeout", etc.)

Everything else with conclusion=failure is "signal".

Score = signal_failures / (signal_failures + noise_failures).
Thresholds: >=0.8 healthy, 0.5-0.8 warn, <0.5 noisy.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --limit) LIMIT="$2"; shift 2;;
    --workflow) WORKFLOW="$2"; shift 2;;
    --since) SINCE="$2"; shift 2;;
    --json) JSON=1; shift;;
    -h|--help) usage; exit 0;;
    *) echo "unknown arg: $1" >&2; usage >&2; exit 2;;
  esac
done

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI not found; install from https://cli.github.com/" >&2
  exit 0
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq not found; required for parsing gh output" >&2
  exit 0
fi

# Compute --created filter from --since (best-effort; supports Nd / Nh).
CREATED_FILTER=""
if [[ -n "$SINCE" ]]; then
  if [[ "$SINCE" =~ ^([0-9]+)d$ ]]; then
    DAYS="${BASH_REMATCH[1]}"
    if date -v-1d +%Y-%m-%d >/dev/null 2>&1; then
      CUTOFF=$(date -v-"${DAYS}"d +%Y-%m-%d)
    else
      CUTOFF=$(date -d "${DAYS} days ago" +%Y-%m-%d)
    fi
    CREATED_FILTER=">=${CUTOFF}"
  elif [[ "$SINCE" =~ ^([0-9]+)h$ ]]; then
    HOURS="${BASH_REMATCH[1]}"
    if date -v-1H +%Y-%m-%dT%H:%M:%SZ >/dev/null 2>&1; then
      CUTOFF=$(date -u -v-"${HOURS}"H +%Y-%m-%dT%H:%M:%SZ)
    else
      CUTOFF=$(date -u -d "${HOURS} hours ago" +%Y-%m-%dT%H:%M:%SZ)
    fi
    CREATED_FILTER=">=${CUTOFF}"
  else
    echo "--since must be Nd or Nh (got: $SINCE)" >&2
    exit 2
  fi
fi

GH_ARGS=(run list --limit "$LIMIT" --json databaseId,name,conclusion,status,event,headBranch,attempt,createdAt,workflowName,displayTitle)
if [[ -n "$WORKFLOW" ]]; then
  GH_ARGS+=(--workflow "$WORKFLOW")
fi
if [[ -n "$CREATED_FILTER" ]]; then
  GH_ARGS+=(--created "$CREATED_FILTER")
fi

RUNS_JSON=$(gh "${GH_ARGS[@]}" 2>/dev/null || true)

if [[ -z "$RUNS_JSON" || "$RUNS_JSON" == "[]" ]]; then
  if [[ "$JSON" -eq 1 ]]; then
    echo '{"total":0,"score":null,"message":"no runs found"}'
  else
    echo "No GitHub Actions runs found (repo may have no CI history)."
  fi
  exit 0
fi

NOISE_KEYWORDS='ECONNRESET|could not resolve host|runner lost|rate limit|429 Too Many|503 Service|network is unreachable|connection reset|i/o timeout|context deadline exceeded|temporary failure|the runner has received a shutdown signal'

classify_run() {
  local id="$1" conclusion="$2" attempt="$3"
  case "$conclusion" in
    success)
      if [[ "$attempt" -gt 1 ]]; then echo "noise:retry-succeeded"; else echo "pass"; fi
      return;;
    cancelled) echo "noise:cancelled"; return;;
    timed_out) echo "noise:timed_out"; return;;
    skipped|neutral|stale) echo "noise:${conclusion}"; return;;
    failure|startup_failure|action_required) ;;
    *) echo "pass"; return;;
  esac

  # Fetch a small slice of the failed run log; classify by keyword.
  local log
  log=$(gh run view "$id" --log-failed 2>/dev/null | head -c 200000 || true)
  if [[ -n "$log" ]] && echo "$log" | grep -Eiq "$NOISE_KEYWORDS"; then
    echo "noise:infra"
  else
    echo "signal"
  fi
}

TOTAL=0; PASSES=0; SIGNAL=0; NOISE=0
DETAILS=""

while IFS=$'\t' read -r id conclusion attempt name title; do
  [[ -z "$id" ]] && continue
  TOTAL=$((TOTAL+1))
  verdict=$(classify_run "$id" "$conclusion" "$attempt")
  case "$verdict" in
    pass) PASSES=$((PASSES+1));;
    signal) SIGNAL=$((SIGNAL+1));;
    noise:*) NOISE=$((NOISE+1));;
  esac
  if [[ "$JSON" -eq 1 ]]; then
    DETAILS+=$(jq -nc --arg id "$id" --arg c "$conclusion" --arg a "$attempt" \
                     --arg n "$name" --arg t "$title" --arg v "$verdict" \
                     '{id:$id,conclusion:$c,attempt:($a|tonumber),workflow:$n,title:$t,verdict:$v}')
    DETAILS+=$'\n'
  fi
done < <(echo "$RUNS_JSON" | jq -r '.[] | [.databaseId, .conclusion // "null", .attempt // 1, .workflowName // .name, .displayTitle // ""] | @tsv')

FAILS=$((SIGNAL+NOISE))
if [[ "$FAILS" -eq 0 ]]; then
  SCORE="1.000"
else
  SCORE=$(awk -v s="$SIGNAL" -v f="$FAILS" 'BEGIN{ printf "%.3f", s/f }')
fi

verdict_label() {
  awk -v s="$1" 'BEGIN{
    if (s+0 >= 0.8) print "healthy";
    else if (s+0 >= 0.5) print "warn";
    else print "noisy";
  }'
}
LABEL=$(verdict_label "$SCORE")

if [[ "$JSON" -eq 1 ]]; then
  jq -n --argjson total "$TOTAL" --argjson passes "$PASSES" \
        --argjson signal "$SIGNAL" --argjson noise "$NOISE" \
        --arg score "$SCORE" --arg label "$LABEL" \
        --argjson runs "$(printf '%s' "$DETAILS" | jq -s '.')" \
        '{total:$total,passes:$passes,signal_failures:$signal,noise_failures:$noise,score:($score|tonumber),verdict:$label,runs:$runs}'
else
  echo "CI Signal-to-Noise — last $TOTAL runs"
  echo "----------------------------------------"
  printf "  Passes:           %d\n" "$PASSES"
  printf "  Signal failures:  %d\n" "$SIGNAL"
  printf "  Noise failures:   %d\n" "$NOISE"
  printf "  Score:            %s  (%s)\n" "$SCORE" "$LABEL"
  echo
  case "$LABEL" in
    healthy) echo "CI looks healthy. Most failures appear to be real signal.";;
    warn)    echo "Mixed signal. Investigate flaky tests and infra timeouts.";;
    noisy)   echo "CI is noisy. Most failures look like infra/flake — fix or quarantine.";;
  esac
fi
