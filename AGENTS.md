# AGENTS.md

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
go test ./...              # Test all
```

## Version & Release

```bash
# Commit changes with proper message
git add .
git commit -m "feat: description of changes

Details here

🤖 Generated with Codex

Co-Authored-By: Codex Haiku 4.5 <noreply@anthropic.com>"

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

## Settings Persistence

Monitor settings stored in two places:
- **`config.json`**: pane heights, filter state (search, sort, type filter, include_closed)
- **Database**: last viewed board (`boards.last_viewed_at`), board view mode, board issue positions

Save pattern: async `tea.Cmd` via `saveFilterState()` / `savePaneHeightsAsync()` (fire-and-forget).

**Known issue**: `saveFilterState()` doesn't persist when td runs embedded in sidecar. The quit interceptor in `sidecar/internal/plugins/tdmonitor/plugin.go:241-250` wraps `tea.Batch` commands in a single `func() tea.Msg`, which may prevent Bubble Tea from dispatching batched sub-commands (like the config save alongside `fetchData`).

## Undo Support

Log actions via `database.LogAction()`. See `cmd/undo.go` for implementation.
