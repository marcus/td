# CLAUDE.md

<!-- td-agent-instructions:start -->
<!-- td-agent-instructions:version=2 -->

## Working with td

td keeps task context durable across agent sessions. At the start of a new context, run `td usage --new-session -q` to see the current work.

Use your judgment about how much tracking the task needs. For substantive work, use `td start <id>`, record useful progress or decisions with `td log`, hand off unfinished work with `td handoff <id>`, and submit completed work with `td review <id>`.

Prefer an independent review when practical. In the default trusted mode, self-review is allowed and audited:

`td approve <id> --self-review --reason "..."`

Run `td usage` for workflow guidance or `td <command> --help` for details.

<!-- td-agent-instructions:end -->

## Review Model (Trusted Review)

In trusted mode, an independent reviewer approves and closes with `td approve
<id> --reason "..."`; self-review requires `--self-review --reason "..."`.
Delegated mode supports `--record-only` review followed by closure from another
session. Pin `review_policy_mode=delegated|strict` when a project needs a hard
independence boundary. Do not create a throwaway session to make a review appear
independent.

For delegated reviews, give each sub-agent context its own `TD_CONTEXT_ID`; see
[docs/multi-agent-sessions.md](docs/multi-agent-sessions.md).

## Build & Install

```bash
go build -o td .           # Build locally
go test ./...              # Test all
```

## Version & Release

```bash
# Commit changes with proper message
git add .
git commit -m "feat: description of changes

Details here

🤖 Generated with Claude Code

Co-Authored-By: Claude Haiku 4.5 <noreply@anthropic.com>"

# Create version tag (bump from current version, e.g., v0.2.0 → v0.3.0)
git tag -a v0.3.0 -m "Release v0.3.0: description"

# Push commit and tag
git push origin main
git push origin v0.3.0

# Install locally with version
go install -ldflags "-X main.Version=v0.3.0" ./...

# Verify installation
td version
```

## Architecture

- `cmd/` - Cobra commands
- `internal/db/` - SQLite (schema.go). DB stored at `<project>/.todos/issues.db`
- `internal/models/` - Issue, Log, Handoff, WorkSession
- `internal/session/` - Session management (DB-backed, scoped by branch + agent)
- `internal/reviewpolicy/` - Shared review / close eligibility policy
- `pkg/monitor/` - TUI monitor (see [docs/modal-system.md](docs/modal-system.md) for modal architecture)

Issue lifecycle: open → in_progress → in_review → closed (or blocked)

## Sync Enablement (per-project model)

**Autosync is enabled per project by setting it up.** Once a project has a usable `sync_state` (via `td login` + `td sync init`/`link`), autosync just works — there is **no feature flag to flip** to turn sync on. The `sync_autosync` feature flag and the `config.json` `sync.autosync` switch are **optional overrides / a global kill-switch**, not the on-switch.

Gate precedence (see `cmd/feature_gate.go:autosyncGateOpen`):

1. **Global kill-switch** — `config.json` `sync.autosync: false` (or `TD_FEATURE_SYNC_AUTOSYNC=false` / `TD_SYNC_AUTO=false`) closes the gate everywhere. `td sync disable` / `td sync enable` write this tri-state field; an explicit `true` only *clears* the kill (does not force-enable an unconfigured project).
2. **Explicit `sync_autosync` feature flag** (env or project config) decides outright.
3. **Per-project configured (default)** — a configured project autosyncs; an unconfigured one does not.

`TD_FEATURE_SYNC_AUTOSYNC` is now an **override, not the on-switch** — normal use should not depend on it. If you do set it, put it in `~/.zshenv` (sourced by all shells) **not** `~/.zshrc` (interactive-only), or non-interactive agent subshells silently strand changes with no error.

**Diagnostics:** `td sync status` is always available (even when the sync CLI is gated) — run it first when sync seems stuck. It reports gate state + source, configured, authenticated, pending event count, and last sync. `td doctor` also covers sync.

**Migration:** the gate reads only `sync.autosync`, never the legacy `sync.enabled` — a stale `sync.enabled: false` does not silently kill sync. Already-authenticated projects with a `sync_state` are automatically "configured"; no re-login needed.

Full details: [docs/sync-client-guide.md](docs/sync-client-guide.md#enabling-autosync-the-per-project-model).

## Settings Persistence

Monitor settings stored in two places:
- **`config.json`**: pane heights, filter state (search, sort, type filter, include_closed)
- **Database**: last viewed board (`boards.last_viewed_at`), board view mode, board issue positions

Save pattern: async `tea.Cmd` via `saveFilterState()` / `savePaneHeightsAsync()` (fire-and-forget).

**Known issue**: `saveFilterState()` doesn't persist when td runs embedded in sidecar. The quit interceptor in `sidecar/internal/plugins/tdmonitor/plugin.go:241-250` wraps `tea.Batch` commands in a single `func() tea.Msg`, which may prevent Bubble Tea from dispatching batched sub-commands (like the config save alongside `fetchData`).

## Undo Support

Log actions via `database.LogAction()`. See `cmd/undo.go` for implementation.
