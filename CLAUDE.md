# CLAUDE.md

<!-- td-agent-instructions:start -->
<!-- td-agent-instructions:version=3 -->

## Working with td

td keeps task context durable across sessions. In a new context, run `td usage --new-session -q` to see current work.

Use your judgment about how much tracking a task needs. For substantive work: `td start <id>`, record progress with `td log`, hand off with `td handoff <id>`, then `td review <id>`.

Closing needs a review. Say who did it (default trusted mode; delegated/strict allow only the first):

- independent session: `td approve <id> --reason "..."`
- a sub-agent: `td approve <id> --reviewed-by "<who>"`
- you: `td approve <id> --self-review --reason "..."`

Prefer a reviewer with its own `TD_CONTEXT_ID`; never name one who did not review.

Run `td usage` or `td <command> --help`.

<!-- td-agent-instructions:end -->

## Review Model (Trusted Review)

Work still needs a review before it closes. What changed is that td now asks you
to say **who** reviewed it, rather than assuming the reviewing agent and the
recording session are the same thing.

In the default trusted mode:

- An independent session reviews and closes: `td approve <id> --reason "..."`
- A reviewer attests without closing and anyone closes after:
  `td approve <id> --record-only --reason "..."`, then `td approve <id> --reason "..."`
- You implemented it and a sub-agent reviewed it: `td approve <id> --reviewed-by "<who>"`
- You implemented it and reviewed it yourself: `td approve <id> --self-review --reason "..."`

Prefer the first two. When a reviewing sub-agent has its own `TD_CONTEXT_ID` its
independence is mechanically verified rather than asserted — see
[docs/multi-agent-sessions.md](docs/multi-agent-sessions.md).

`--reviewed-by` is an attestation td cannot verify. It exists so an orchestrator
recording a sub-agent's review writes a true record instead of claiming a
self-review it did not perform. Do not use it to launder an unreviewed change:
naming a reviewer who did not review is worse than `--self-review`, because it
reads as independent in the audit trail. Do not create a throwaway session to
make a review appear independent either.

Pin `review_policy_mode=delegated|strict` when a project needs a mechanical
independence boundary rather than an honesty-based one. In those modes an
involved session cannot approve at all, and `--reviewed-by` records the
attribution without granting anything.

## Build & Install

```bash
go build -o td .           # Build locally
make test                  # Test with the release-safe environment
```

## Version & Release

```bash
# Update and commit CHANGELOG.md first, then:
git push origin main
make release VERSION=vX.Y.Z

# Verify the release workflow, assets, Homebrew tap, and installed version.
```

See [docs/guides/releasing-new-version.md](docs/guides/releasing-new-version.md)
for the non-interactive runbook and complete verification checklist.

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
