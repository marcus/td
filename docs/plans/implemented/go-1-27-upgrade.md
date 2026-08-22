# Plan: Upgrade to Go 1.27

## Goal

Move td from Go 1.25.8 to Go 1.27.0. This crosses two minor releases in one step; the risk is concentrated in the new default `encoding/json` implementation, which td leans on heavily for its JSONL logs, sync transport, serve API, and SQLite-backed JSON columns.

## Current state

| Concern | Today |
|---|---|
| go.mod directive | `go 1.25.8`, no `toolchain` directive |
| CI | `go-ci.yml` test job: `setup-go` with `go-version-file: go.mod`, then `env -u TD_FEATURE_SYNC_AUTOSYNC -u TD_FEATURE_SYNC_CLI go test ./...`; no lint gate by design (~2100 pre-existing findings, documented in the workflow) |
| Release | `release.yml`: same `go-version-file` lever, goreleaser builds; Homebrew tap distributes |
| Local gate | `scripts/pre-commit.sh` (gofmt on staged files) |
| Plans convention | this directory (`docs/plans/active`) |

Because both workflows read the version from go.mod, the directive is the single CI lever.

## What Go 1.26/1.27 change that touches td

- **encoding/json/v2 is the default implementation as of 1.27** (escape hatch: `GOEXPERIMENT=nojsonv2`). Expect differences in error strings and edge behaviors around unknown fields, trailing data, and case-insensitive matching — exactly the surfaces a sync protocol and a serve API exercise.
- **stdversion vet** runs by default under `go test` from 1.27; satisfied once directives are current.
- Generic methods and embedded-field-selector struct literals become legal syntax (additive).
- `compress/flate` output bytes changed — matters only if any test asserts exact compressed output.
- darwin builds require macOS 13+ (adopted at 1.26; Homebrew users are unaffected in practice).

## Work sequence

1. **Directives**: `go.mod` → `go 1.27.0`. Consider whether the jump deserves an intermediate `go 1.26.x` landing commit for bisectability; if tests pass at 1.27 directly, one commit is fine.
2. **Tidy**: `go mod tidy`; review the diff for any dependency that raises its own `go` directive as a consequence.
3. **JSON pass**: run the full suite plus `scripts/e2e-sync-test.sh`. Any failure gets bisected with `GOEXPERIMENT=nojsonv2` first to attribute it to json/v2 before touching code. Watch specifically: sync payload round-trips, serve API responses, log-line encode/decode, and tests asserting error message text.
4. **Serve smoke**: start `td serve` against a scratch project, drive a couple of reads/writes through the HTTP surface.
5. **Pre-commit sanity**: hook script still passes on a real commit.

## Coordination

sidecar pins released td versions (`td v0.62.0` today). Bumping this directive does not affect sidecar until sidecar's next pin bump, and sidecar carries its own upgrade plan — land these independently. The recommended overall order across the trio is td → tasks → sidecar, so sidecar's workspace bump never trails a member requirement.

## Verification & acceptance evidence

Recorded 2026-08-22, all at Go 1.27.0 (darwin/arm64):

- Full `env -u TD_FEATURE_SYNC_AUTOSYNC -u TD_FEATURE_SYNC_CLI GOWORK=off go test -count=1 ./...` — clean, cache busted.
- `scripts/e2e-sync-test.sh` — green.
- `scripts/e2e/run-all.sh` — 16 passed, 3 skipped (`--full` only), 0 failed. See below.
- Serve smoke against a scratch store — `GET /health`, `GET /v1/issues`, and `POST /v1/issues` round-trip correctly; a deliberately malformed body returns a precise json/v2 validation error.
- `goreleaser release --snapshot --clean` — all four targets (darwin amd64/arm64, linux amd64/arm64) build and archive.
- CHANGELOG entry landed under `[Unreleased]`.

No json/v2 attribution pass was needed: nothing failed for a JSON reason, so `GOEXPERIMENT=nojsonv2` was never required.

### What step 3 turned up (unrelated to the upgrade)

Running the shell sync suite surfaced a pre-existing break rather than a 1.27 regression: all 16 `scripts/e2e` tests had been failing since 2026-06-19 (`fb1327a`), which migrated `test/e2e/harness.go` and `scripts/e2e-sync-test.sh` to the device PKCE login flow but missed `scripts/e2e/harness.sh`. That harness kept calling the retired `/v1/auth/login/start` + `/auth/verify` endpoints, which now answer 410 Gone, so every test died in shared setup — silently, because `curl -sf` under `set -e` exits with no diagnostic. Fixed in `td-cb8ba9` and landed alongside this upgrade.

Worth noting for the next plan: this suite is not part of `go test ./...` and is not run by CI, which is precisely why it rotted unnoticed for two months. Wiring it into CI is a separate decision (it is slow — each script builds binaries and starts a server).

Out of scope: introducing golangci-lint (needs its own remediation pass), dependency upgrades beyond tidy, adopting json/v2-specific APIs.
