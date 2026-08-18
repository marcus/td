# CI Signal-to-Noise Scorer

`scripts/ci-signal-noise.sh` is a small diagnostic that uses the `gh` CLI to
pull recent GitHub Actions runs for the current repo and classifies each
failure as **signal** (real code defect) or **noise** (flake, infra,
timeout, cancelled, or succeeded-on-retry).

## Usage

```bash
scripts/ci-signal-noise.sh                  # last 50 runs, human output
scripts/ci-signal-noise.sh --limit 100
scripts/ci-signal-noise.sh --workflow ci.yml --since 7d
scripts/ci-signal-noise.sh --json | jq .score
```

Flags:

- `--limit N` — runs to inspect (default 50)
- `--workflow NAME` — filter to a single workflow file or name
- `--since Nd` / `--since Nh` — only runs created in the last N days/hours
- `--json` — machine-readable output

Requires `gh` and `jq`. If `gh` isn't installed or the repo has no Actions
history, the script prints a message and exits 0.

## Heuristics

A run is classified as **noise** when any of:

- `conclusion` ∈ {`cancelled`, `timed_out`, `skipped`, `neutral`, `stale`}
- `run_attempt > 1` and final conclusion is `success` (succeeded on retry)
- failure logs match infra keywords: `ECONNRESET`, `could not resolve host`,
  `runner lost`, `rate limit`, `429`, `503`, `network is unreachable`,
  `connection reset`, `i/o timeout`, `context deadline exceeded`, etc.

Everything else with `conclusion=failure` is **signal**.

## Score

```
score = signal_failures / (signal_failures + noise_failures)
```

- `>= 0.8` — **healthy** (most failures are real bugs to fix)
- `0.5 – 0.8` — **warn** (mixed; investigate flake/infra)
- `< 0.5` — **noisy** (CI is mostly lying; fix or quarantine the flakes)

If there are zero failures, the score is reported as `1.000` (healthy).
