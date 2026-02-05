# Notes + Sync Gated Merge Plan

## Goal

Merge `sidecar-notes` (td) and `notes` (sidecar) into each repo's `main` without exposing unfinished notes UX or sync flows to all end users.

## Strategy

1. Land backend compatibility and migrations first.
2. Keep all user-visible entry points default-off behind feature flags.
3. Require explicit opt-in for notes sync transport in td and notes UI in sidecar.

## Branch Integration Approach

Both source branches are not safe to merge directly (history divergence/rebases would cause noisy or regressive merges). Use clean integration branches from `main` and port only intended end-state changes.

## td Changes (branch: `codex/merge-sidecar-notes-gated`)

- Add notes entity support in sync internals:
  - `internal/api/sync.go` accepts `notes` entity type.
  - `internal/sync/client.go` normalizes `note|notes -> notes`.
  - `internal/sync/backfill.go` includes notes table for orphan backfill.
- Add new feature flag:
  - `sync_notes` (default `false`) in `internal/features/features.go`.
- Gate outbound/inbound notes sync paths:
  - `cmd/sync.go` validator now allows `notes` only when `sync_notes=true`.
  - `cmd/sync.go` and `cmd/autosync.go` filter pending events through validator before push.
  - Pull/apply already validates entity types through same validator.
- Gate map updated in `internal/features/sync_gate_map.go`.
- Add dedicated e2e script:
  - `scripts/e2e/test_notes_sync.sh`.

## sidecar Changes (branch: `codex/merge-notes-gated`)

- Merge notes plugin implementation files under `internal/plugins/notes/`.
- Register plugin only when feature is enabled:
  - `cmd/sidecar/main.go` checks `features.IsEnabled("notes_plugin")`.
- Add feature flag:
  - `notes_plugin` default `false` in `internal/features/features.go`.
- Keep notes config/state/keymap paths present but inert when plugin is disabled.
- Public website notes docs intentionally not included in this gated merge.

## Flag Matrix (Default Rollout State)

- td:
  - `sync_cli=false`
  - `sync_autosync=false`
  - `sync_monitor_prompt=false`
  - `sync_notes=false`
- sidecar:
  - `notes_plugin=false`

This keeps notes hidden and non-syncing unless explicitly enabled.

## Test Matrix

1. Default-off smoke:
   - td sync commands hidden unless existing sync flags enabled.
   - sidecar starts without notes tab.
2. Opt-in tester path:
   - Enable `sync_notes` in td + `notes_plugin` in sidecar.
   - Run `scripts/e2e/test_notes_sync.sh` and normal `go test ./...`.
3. Mixed-mode safety:
   - sidecar `notes_plugin=true`, td `sync_notes=false` should not crash; notes remain local-only.

## Merge Order

1. Merge td integration branch first (schema/sync transport ready, still default-off).
2. Merge sidecar integration branch second (UI still default-off).
3. Enable flags only for tester cohort.

