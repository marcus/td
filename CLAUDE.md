# CLAUDE.md

## MANDATORY: Use td for Task Management

Run td usage --new-session at conversation start (or after /clear). This tells you what to work on next.

Sessions are automatic (based on terminal/agent context). Optional:
- td session "name" to label the current session
- td session --new to force a new session in the same context

Use td usage -q after first read.

## MANDATORY: Use `td` for Task Management

Run `td usage --new-session` at conversation start (or after /clear). This tells you what to work on next.

Sessions are automatic (based on your terminal/agent context). Optional:
- `td session "name"` to label the current session
- `td session --new` to force a new session in the same context

**Do NOT start a new session mid-work.** Sessions track implementers. A new session mid-task looks like a bypass of the review guardrails and breaks audit trails.

Use `td usage -q` after first read.

## Review Model (Delegated Review)

td's review guardrail protects against unchecked self-review, not against delegated closure. The rules:

- **Review must come from a session that did not participate in implementation.** You cannot review your own implementation, but you *can* close an issue after an independent review has been recorded.
- **Any session may perform the final close after approval.** Once an independent review exists, any session may run `td approve --reason "..."` to close. An independent review is required; the close itself may be delegated and is audited via `closed_by_session`.
- **Do NOT start a new session mid-work just to satisfy the review rules.** Use a real reviewer sub-agent or a separate agent context.

### Modes (`review_policy_mode`)

- `strict` — no prior involvement allowed; current default for existing installs.
- `balanced` — strict, plus a creator-approval exception with `--reason`. Legacy default for projects that set `balanced_review_policy=true`.
- `delegated` — review attestations + delegated close (opt-in now via `TD_FEATURE_REVIEW_POLICY_MODE=delegated` or `td feature set review_policy_mode delegated`; will become the default in a future release).

### Orchestrator / Sub-Agent Flow

Under `delegated` mode, the orchestrator submits the issue for review itself, delegates the review to a reviewer sub-agent, then closes once the approval is recorded:

```bash
# Orchestrator creates work
td add "Refactor auth" --type feature

# Implementer sub-agent (separate session) does the work
td start td-a1b2
td log "implemented auth refactor"
td handoff td-a1b2 --done "refactor" --remaining "none"

# Orchestrator submits for review (this sets review_requested_by_session)
td review td-a1b2

# Reviewer sub-agent (separate session) records approval without closing
td approve td-a1b2 --record-only --reason "Reviewed diff, tests pass"

# Orchestrator, implementer, or another session closes using the recorded approval
td approve td-a1b2 --reason "Closing after recorded independent approval"
```

The orchestrator does not need to own an issue role to close after approval. The reviewer must be independent; the closer is recorded separately for audit. If the closer is not the reviewer-of-record, pass `--reason`.

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

## Settings Persistence

Monitor settings stored in two places:
- **`config.json`**: pane heights, filter state (search, sort, type filter, include_closed)
- **Database**: last viewed board (`boards.last_viewed_at`), board view mode, board issue positions

Save pattern: async `tea.Cmd` via `saveFilterState()` / `savePaneHeightsAsync()` (fire-and-forget).

**Known issue**: `saveFilterState()` doesn't persist when td runs embedded in sidecar. The quit interceptor in `sidecar/internal/plugins/tdmonitor/plugin.go:241-250` wraps `tea.Batch` commands in a single `func() tea.Msg`, which may prevent Bubble Tea from dispatching batched sub-commands (like the config save alongside `fetchData`).

## Undo Support

Log actions via `database.LogAction()`. See `cmd/undo.go` for implementation.
