# AGENTS.md

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

Work needs a review before it closes, and td asks who performed it. In the
default trusted mode an independent session approves with `td approve <id>
--reason "..."`; a reviewer can instead attest without closing via
`--record-only` and any session closes after. When you implemented the work
yourself, name the reviewer with `--reviewed-by "<who>"`, or acknowledge a
genuine self-review with `--self-review --reason "..."`.

`--reviewed-by` is an attestation td cannot verify. Naming a reviewer who did
not review is worse than an honest self-review, because it reads as independent
in the audit trail. Do not create a throwaway session to make a review appear
independent either. Pin `review_policy_mode=delegated|strict` when a project
needs a mechanical independence boundary rather than an honesty-based one.

## Development Approach

- Be pragmatic: `td` is a focused local tool, not an enterprise platform.
- Use worktrees for major features, risky migrations, or parallel work. Make
  quick localized fixes on the current branch when scope and verification are
  clear.
- One independent review and one rejection cycle is normally enough. Continue
  only for a genuine P0 data-loss/security finding; track other observations as
  follow-up work.
- Test likely failures and important boundaries, not exhaustive hypothetical
  states without a concrete use case.
- Surface friction after the first surprising blocker or material scope growth.
  Pause and explain it rather than silently turning a small fix into a subsystem.

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

## Settings Persistence

Monitor settings stored in two places:
- **`config.json`**: pane heights, filter state (search, sort, type filter, include_closed)
- **Database**: last viewed board (`boards.last_viewed_at`), board view mode, board issue positions

Save pattern: async `tea.Cmd` via `saveFilterState()` / `savePaneHeightsAsync()` (fire-and-forget).

**Known issue**: `saveFilterState()` doesn't persist when td runs embedded in sidecar. The quit interceptor in `sidecar/internal/plugins/tdmonitor/plugin.go:241-250` wraps `tea.Batch` commands in a single `func() tea.Msg`, which may prevent Bubble Tea from dispatching batched sub-commands (like the config save alongside `fetchData`).

## Undo Support

Log actions via `database.LogAction()`. See `cmd/undo.go` for implementation.
