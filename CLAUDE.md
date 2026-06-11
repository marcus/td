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

Sessions track implementers so the audit trail records who did what. Use a real reviewer sub-agent or a separate agent context for independent review — don't spin up throwaway sessions just to game the review check.

Use `td usage -q` after first read.

## Review Model (Trusted Review)

The default mode is now **`trusted`**. Trusted keeps the delegated review-attestation model — **prefer delegating review to an independent sub-agent** — and adds a flag-gated, audited self-review escape hatch for when delegation is not practical.

The rules:

- **Prefer an independent review.** A session that did not implement the issue reviews it (`td approve <id>`) or records an approval (`td approve <id> --record-only --reason "..."`). This is the norm; reach for it first.
- **Any session may perform the final close after approval.** Once an independent review exists, any session may run `td approve <id> --reason "..."` to close. The close is audited via `closed_by_session`; pass `--reason` if the closer is not the reviewer-of-record.
- **Self-review is allowed in trusted mode, but you must acknowledge it.** When you are the orchestrator/implementer and have already reviewed the diff yourself, approve+close with `td approve <id> --self-review --reason "..."`. The `--self-review` flag requires `--reason` and stamps `self_review` on the review row for audit. Do **not** fabricate a throwaway session to dodge the self-review acknowledgement — just acknowledge it.

### Modes (`review_policy_mode`)

Set with `td feature set review_policy_mode <mode>` (or `TD_FEATURE_REVIEW_POLICY_MODE=<mode>`).

- `trusted` — **default.** Delegated review-attestation plus a flag-gated, audited self-review escape hatch (`td approve --self-review --reason "..."`). Prefer delegation; self-review when delegation is impractical.
- `delegated` — review attestations + delegated close, with **no** self-review escape: the implementer cannot self-approve. Pin this for projects that want the hard wall.
- `balanced` — strict, plus a creator-approval exception with `--reason`. Legacy default for projects that set `balanced_review_policy=true`.
- `strict` — no prior involvement allowed at all.

### Orchestrator / Sub-Agent Flow

Preferred flow — orchestrator submits the issue for review, delegates the review to a reviewer sub-agent, then closes once the approval is recorded:

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

Trusted-mode shortcut — when you (orchestrator/implementer) have reviewed the diff yourself and delegation is not practical, acknowledge the self-review instead of spawning a reviewer session:

```bash
td review td-a1b2
td approve td-a1b2 --self-review --reason "Reviewed diff myself, tests pass"
```

The reviewer (when delegated) must be independent; the closer is recorded separately for audit. A self-review is recorded as such.

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
