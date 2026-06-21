# Changelog

All notable changes to td are documented in this file.

## [Unreleased]

## [v0.51.0] - 2026-06-20

### Sessions and worktrees
- **Current focus and active work sessions are now session/worktree scoped.** CLI and serve-mode current-state paths use the local-only `session_state` table instead of shared config-file focus, so separate agents and worktrees can keep independent current work without clobbering each other. This includes the CLI focus/log/handoff/review/work-session paths and the serve API used by embedded clients.
- **Session identity is safer for sub-agents and alternate worktrees.** Sessions now carry worktree metadata and context-aware identity so independent agent contexts can be tracked separately while local-only fields are kept out of sync payloads.
- **`td context`/resume reliability improved.** Resume now correctly calls the show path so context lookup returns the expected issue details.

### Sync
- **Auto-sync failures are bounded and visible.** Push retries now use a bounded backoff and warn when local changes remain pending instead of silently stranding them.
- **`td handoff` no longer waits on a startup pull and avoids redundant pulls after failed pushes.** Handoff still records local progress and attempts the post-mutation push, but an unhealthy sync endpoint now exits after the bounded push window with a pending-sync warning instead of paying multiple network timeouts.
- **Local-only worktree/session metadata is scrubbed from sync events and conflicts.** Worktree identity remains useful locally without leaking or conflicting across devices.

### Workflow
- **Auto-review handoffs can be synthesized from session logs.** Review submission can preserve a useful handoff trail even when an explicit handoff was missing.

## [v0.50.1] - 2026-06-20

### Sync
- **Fixed: `td sync` no longer warns about phantom conflicts from your own events.** A sync deliberately re-pulls the client's own just-pushed events to keep sequence numbers convergent, and replaying them in `server_seq` order can transiently overwrite newer local state (identical-payload log entries; an issue's creation event landing on top of a later local close) before a subsequent event in the same batch restores it. These self-replays were being flagged as overwrites, producing a spurious `Warning: N local records overwritten by remote changes:` and writing phantom rows to `sync_conflicts`, even on a single device with no other writers. `ApplyRemoteEvents` now gates conflict detection on `ev.DeviceID != myDeviceID` (wiring up a parameter that was already passed but unused), so only changes authored by *another* device are reported. Genuine cross-device conflicts still warn exactly as before. Regression test included.

## [v0.50.0] - 2026-06-20

### Sync server
- **First-class project slugs.** Projects now have a stored, globally-unique `slug` (migration v6: `slug` column + unique index). Slugs are generated on create from the project name (`slugify`, falling back to the project id for names that produce an empty slug, with `-2`/`-3` suffixes on collision) and an idempotent startup backfill assigns slugs to pre-existing projects, ordered deterministically by `created_at`. Soft-deleted projects are skipped so they don't consume slug namespace. The slug is exposed as the `slug` JSON field on the user project API (`GET /v1/projects`, `GET /v1/projects/{id}`) and the admin project API. Stored slugs are stable canonical identifiers — they are intentionally **not** updated on rename. This enables clean, deep-linkable, guessable `/projects/<slug>` URLs in td-watch (with the opaque `p_…` id still resolving and redirecting to the canonical slug).
- **Project invitations (backend).** Invite users to a project by email with a role; accept/decline flow with token-hashed invitations, plus invited-user signup support.
- **Web signup via magic links.** Email magic-link signup, including for invited users.
- **Sync-scope enforcement on project routes.** Project routes now require the `sync` scope while preserving the admin proxy path; added a `HasAnyScope` helper and a scope-enforcement test matrix covering admin proxy paths.

### CLI / JSON output
- **Consistent `--json` across every command, including all mutating ones.** `--json` is now a global (persistent) flag registered on the root command, so it works uniformly on reads *and* mutations (`create`/`add`, `update`, `start`, `unstart`, `log`, `handoff`, `defer`, `block`/`unblock`/`reopen`, `link`, `note add`/`edit`/`delete`, `approve`, `review`, `reject`, `close`). Previously many mutating commands had no JSON mode or registered their own ad-hoc local `--json` flag.
- **Shared success envelopes.** Issue-affecting commands emit `{"id","status","action","issue":{...full issue...}}` (plus command-specific extras like `session`, `reason`, or cascade counts); non-issue mutations emit `{"action", ...}` (e.g. `log` -> `{"action":"logged","id","log":{...}}`, `handoff` -> `{"action":"handoff_recorded","id","handoff":{...}}`). Produced by the new `output.EmitIssue` / `output.EmitResult` helpers. Bulk operations emit one JSON object per id (NDJSON).
- **`td add --json` now returns the new issue id** (and full issue), making `id=$(td add "..." --json | jq -r .id)` a reliable scripting idiom.
- **Structured JSON error envelopes.** Errors in JSON mode emit `{"error":{"code":"...","message":"..."}}` on stdout with exit code 1, via `output.JSONError`. `JSONError` now encodes through the `json` package so messages containing quotes, backslashes, or newlines remain valid, parseable single-line JSON.
- **Fixed: `td epic create` was broken** and now correctly delegates to the create path (emitting an `epic`-typed issue, including under `--json`).
- Exceptions documented: `td query` continues to use `--output table|json|ids|count`; the JSONL commands (`errors`, `security`, and the stats error/security views) emit their own line-delimited JSON; `show` additionally supports the legacy `--format json`.

### Documentation
- New "JSON output (`--json`)" section in `docs/guides/cli-commands-guide.md` documenting the global flag, both success envelopes, the error envelope, NDJSON bulk output, the exceptions, and a scripting tip, with real captured examples. Added a JSON output pointer to `README.md`.

## [v0.47.2] - 2026-06-17

### Bug Fixes
- **Fixed: `td handoff`, `td review`, and `td ws tag` failed with `FOREIGN KEY constraint failed (787)` when given a bare issue id (e.g. `94e0fd` instead of `td-94e0fd`).** These write paths persisted the raw argument as `issue_id`; since issue PKs always carry the `td-` prefix, a bare id never matched `issues(id)` and the constraint fired once foreign-key enforcement was enabled in v0.44.0. The lookup itself normalized internally, so `td start`/`td show` worked on the bare id while the audit-trail write failed — making the failure look like a session/sync problem (it was not; nothing references the `sessions` table by FK). `AddHandoff`, `AddComment`, `AddGitSnapshot`, and `TagIssueToWorkSession`/`UntagIssueFromWorkSession` now normalize `issue_id` before writing, mirroring how `GetIssue` and `CreateIssueReview` already canonicalize ids. Normalizing tag and untag together also closes a latent mismatch where the two computed different deterministic row ids from differing id forms. A regression test exercises bare-id writes under FK enforcement.

## [v0.47.1] - 2026-06-17

### Monitor
- **Fixed: capital-letter keyboard shortcuts stopped working after the v0.45.0 Charmbracelet v2 migration.** `Y` (copy issue ID), and every other shift-bound shortcut (`C`, `F`, `G`, `H`, `I`, `J`, `K`, `N`, `O`, `R`, `S`, `T`, `V`, `W`), silently did nothing — most visibly in sidecar's embedded monitor. In Bubble Tea v2 a shifted printable key arrives as the unshifted code plus a shift modifier (e.g. `shift+y` => code `y` + `ModShift`), so the keymap's `KeyToString` rendered it as `"shift+y"` and never matched bindings written in textual form (`"Y"`). It now uses `Key.String()`, which returns the printable text (`"Y"`, `"?"`) and falls back to the keystroke form for special keys (`"tab"`, `"ctrl+c"`, `"shift+tab"`). Regression tests now exercise realistic shifted-key input.

## [v0.47.0] - 2026-06-15

### Monitor
- **Getting Started modal now shows only on the first open of a project**, not on every launch. Previously the "Welcome to td!" modal reappeared each time the monitor opened in any project that had not installed td agent instructions, which nagged repeatedly when agents declined the install. A per-project `getting_started_seen` flag is now stored in `.todos/config.json` and stamped the moment the modal is first displayed; the modal shows only when the project has no instructions **and** has not been seen before. The `H`-key manual reopen path is unchanged.

### Documentation
- Release guide rewritten to be agent-friendly: documents the `make release` workflow, clarifies that the Homebrew tap is bumped by the dedicated `update-homebrew-tap` GitHub Actions job (building from source, updating URL + tarball sha256) rather than by GoReleaser, distinguishes the hand-written `CHANGELOG.md` from GoReleaser's auto-generated GitHub release notes, and adds an agent-oriented non-interactive runbook. Corrected the stale "manually maintained" comment in `.goreleaser.yml`.

## [v0.46.0] - 2026-06-11

### Review policy
- **`trusted` is now the default `review_policy_mode`.** A fresh install with no explicit configuration resolves to `trusted`, which keeps the delegated review-attestation model (prefer an independent reviewer; any session may close once an approval is recorded) and adds a flag-gated, audited self-review escape hatch. When you have reviewed your own diff and delegation is impractical, approve+close with `td approve <id> --self-review --reason "..."` (stamps `self_review` on the review row for audit). Explicit `review_policy_mode` settings, the `TD_FEATURE_REVIEW_POLICY_MODE` env override, and the legacy `balanced_review_policy` mapping are all unchanged — only the unconfigured default flips. Pin `review_policy_mode=delegated|strict` to keep the hard no-self-review wall.
- **`td feature {get,set,unset,list}` now manage the string-valued `review_policy_mode`** (e.g. `td feature set review_policy_mode trusted`), validating values via `reviewpolicy.ParseMode`. Previously the command only accepted boolean flags and rejected `review_policy_mode` as unknown.

## [v0.45.0] - 2026-06-08

### Dependencies
- **Charmbracelet v2 migration**: the monitor TUI and all Charm dependencies moved to the `charm.land` v2 stack — lipgloss v2.0.3, bubbletea v2.0.7, bubbles v2.1.0, glamour v2.0.0, huh v2.0.3 — plus x/ansi v0.11.7 and x/cellbuf v0.0.15. Bubble Tea v2 ships a faster renderer; glamour v2 renders OSC 8 clickable links in markdown.
- Key and mouse handling migrated to the v2 message model (`tea.KeyPressMsg`, `tea.MouseClickMsg`/`MouseWheelMsg`, etc.); colors moved to `image/color.Color`.

### API
- `monitor.Model.View()` now returns `tea.View` (Bubble Tea v2). A `monitor.Model.ViewString()` accessor is preserved so embedders (sidecar's tdmonitor plugin) can render the monitor as a plain string. The exported `modal`/`mouse` handler signatures (`HandleKey`, `HandleMouse`) are unchanged — they take the v2 message interfaces.

## [v0.44.0] - 2026-04-18

### Features
- `td doctor fk`: hidden diagnostic that reports orphan-row counts across every declared foreign-key relation (gated behind `TD_FEATURE_SYNC_CLI=1`)
- Monitor DB-pool diagnostics: set `TD_MONITOR_DBPOOL_DEBUG=1` to trace `getSharedDB`/`releaseSharedDB` with refcounts and caller — helps detect connection leaks in embedded monitors

### Database
- **Foreign-key enforcement enabled** on the CLI `issues.db` (previously off despite the schema declaring ~12 FK relationships)
- Migration 30 cleans up pre-existing orphan rows and adds schema-level `ON DELETE CASCADE` to child relations (handoffs, git_snapshots, issue_dependencies, issue_files, work_session_issues, comments, issue_session_history, board_issue_positions)
- Centralized SQLite connection opener (`internal/db/conn.go` — `OpenSQLite`) applies uniform pragmas (WAL, `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON`, `MaxOpenConns=1`) across the CLI, API server, per-project event DB, and snapshot DB paths
- WAL checkpoint on `Close` switched from `TRUNCATE` to `PASSIVE` to avoid blocking concurrent readers (the snapshot-build path still uses `TRUNCATE` before file copy, as intended)
- Manual cascade emulation in `internal/sync/events.go` removed where schema cascades now handle it; runtime `parent_id` cleanup retained (no FK on `issues.parent_id` due to `''` sentinel semantics)

### Improvements
- `withWriteLock` scope documented explicitly in source (serializes CLI writes only; does not coordinate with the API server's separate DBs)
- `td import` tolerates forward-referencing dependencies (issue A depending on issue B that appears later in the JSON) by disabling FK enforcement inside the import transaction; run `td doctor fk` after a large import to surface any remaining orphans

## [v0.43.0] - 2026-03-24

### Bug Fixes
- **Atomic lossless import** — `td import --json` now imports all associated data (logs, handoffs, dependencies, files) in a single transaction; backward-compat `UnmarshalJSON` handles old `handoff` singular / `[]string` deps format; `GetHandoffs` and `GetIssueDependencyRelations` added to DB layer (#65)
- **`UpdateIssue` missing fields** — `creator_session`, `minor`, and `created_branch` were not updated by `UpdateIssue` / `updateIssueAndLog`; patches silently dropped these fields (#70)
- **Timezone-aware defer/due filtering** — temporal queries used `date('now')` (UTC) instead of `date('now','localtime')`; deferred/overdue/due-soon filters returned wrong results in non-UTC zones (#70)
- **`RemoveDependencyLogged` wrong depID** — hardcoded `"depends_on"` in `DependencyID` call even for `"blocks"` relations; undo data was corrupted for non-`depends_on` relations (#70)
- **`DeleteBoardLogged` not atomic** — position updates, action_log inserts, and board delete ran outside a transaction; partial failure left inconsistent state (#70)
- **RateLimiter goroutine leak** — background cleanup goroutine used `time.Sleep` loop with no cancellation; `Stop()` added, called in `Server.Shutdown()` (#70)
- **CORS missing methods** — PATCH, PUT, DELETE not in `Access-Control-Allow-Methods`; browser pre-flight checks failed for mutating requests (#70)
- **Snapshot stat error ignored** — `f.Stat()` error swallowed; now returns 500 with proper error message (#70)
- **DB connection leak on init failure** — `Open` and `Initialize` did not close `conn` on migration or schema errors (#70)
- **Form scroll over-run** — `FormScrollOffset` could exceed content height; now clamped to `formScrollToBottom()` on wheel-down (#70)
- **Modal click detection** — section line bounds (`BlockedBy*`, `Blocks*`) computed during render (wrong: built incrementally); extracted to `computeModalSectionLines()`, called before click handling (#70)
- **In-progress panel header count** — used `len(m.InProgress)` including focused duplicate; now counts `inProgressVisible` to avoid spurious header when all items are hidden (#70)
- **RFC3339Nano timestamp parsing** — sync pull events with sub-second precision failed with strict `RFC3339`; now tries `RFC3339Nano` first with `RFC3339` fallback in both `autoSyncPull` and `runPull` (#69)
- **`sess != nil` guard in delete/restore** — `DeleteIssueLogged` / `RestoreIssueLogged` called with `sess.ID` without nil check; now uses empty string fallback (#69)
- **`escapeJSON` incomplete escaping** — manual string replacement missed `\r`, `\b`, `\f`, NUL, and other control characters; replaced with `json.Marshal` (#69)
- **Stdin pipe read without size check** — `stat.Size() > 0` guard on piped stdin in `log` and `handoff` commands silently dropped content from pipes that report 0 size; guard removed (#69)
- **Trusted proxy XFF spoofing** — `clientIP` trusted `X-Forwarded-For` unconditionally; attackers could spoof client IP for rate limit bypass; now only trusts XFF from configured `TrustedProxies` (#69)
- **CreateUser admin TOCTOU race** — `SELECT COUNT(*)` + `INSERT` without transaction allowed concurrent requests to both become admin; wrapped in a transaction (#69)
- **Backfill `anyEventSetsStatus` false positive** — LIKE pre-filter on `"status":"open"` matched nested fields and similar-named statuses (`"reopened"`); added `statusMatches` post-filter; extracted `checkCreateEventStatus` so `rows.Close()` fires before next query (#69)
- **Autosync pull transaction leak** — `defer tx.Rollback()` accumulated across loop iterations; extracted to `autoSyncApplyPullBatch` so defer fires per batch (#69)
- **Singleflight snapshot dedup** — concurrent snapshot requests for same project triggered redundant builds; now deduplicated with `singleflight.Group` (#69)

## [v0.42.2] - 2026-03-21

### Bug Fixes
- **SSE nil-validator panic** — `ApplyRemoteEvents` was called with `nil` validator, causing guaranteed panic on any non-empty SSE event batch; now passes an allow-all validator (#68)
- **`work_session_issues` never synced** — missing from `baseSyncableEntities`, silently dropping events on push/pull; added to sync entity map (#68)
- **Non-atomic undo of delete** — `RestoreIssue` + `LogAction` as separate locked operations had a crash window; replaced with atomic `RestoreIssueLogged` (#68)
- **Timestamp parse mismatch** — `GetRecentConflicts` used rigid format that failed on Go `time.Time.String()` output, breaking `td sync conflicts`; now uses flexible `parseTimestamp` with monotonic clock stripping (#68)
- **`rows.Err()` unchecked** — ~30 query functions returned silent partial results on driver errors; all now check and propagate `rows.Err()` (#68)
- **Non-transactional migration** — `migrateFilePathsToRelative` crash left partial data; now runs inside a transaction with proper rollback (#68)
- **Snapshot serve race** — only copy of snapshot was deleted on cache rename failure; `servePath` now updated immediately before second rename (#68)
- **StatusFilter data race** — map reference captured in goroutine shared underlying data; now deep-copied before capture (#68)
- **Board editor data race** — `BoardEditorBoard` pointer mutated from save goroutine while Update loop may read it; now copies struct before mutation (#68)
- **Stale syncState** — push updates `last_sync_at` in DB but in-memory struct was stale; pull now reloads syncState after push for correct conflict detection (#68)
- **CLI reject from wrong states** — rejected issues in `in_progress`/`blocked`/`closed`; now restricted to `in_review` only, matching HTTP API behavior (#68)
- **HelpFilter backspace UTF-8** — byte slicing split multi-byte runes; now uses `[]rune` conversion (#68)
- **Board editor preview count** — showed capped "6" instead of "5+"; uses sentinel `-1` to signal overflow (#68)
- **`copyFile` sync durability** — backup file not flushed to disk; now calls `out.Sync()` after copy (#68)

## [v0.42.1] - 2026-03-20

### Bug Fixes
- fix: `td import` now restores all issue fields and associated data (logs, handoffs, dependencies, files) — lossless round-trip (#64)

## [v0.42.0] - 2026-03-09

### Bug Fixes
- Fix closed_at timestamp to use current time on approve/close (#55)
- Fix mobile navbar sidebar hidden behind secondary panel (#54)

## [v0.41.0] - 2026-03-01

### Bug Fixes
- Fix premature title truncation in task list panel: overhead calculation in `formatIssueShort` was overestimating by 3 chars due to phantom leading spaces in tag width and a hardcoded type icon width. Task titles now display 3 more characters before truncating, giving more readable output in both `td monitor` and sidecar's embedded td view (sidecar#215)

## [v0.40.0] - 2026-02-27

### Features
- Add search/filter to help modal (press `/` to filter) (#25)
- Add scroll support to form modal
- Add `balanced_review_policy` feature flag (default on)
  - Allows creator-only approvals when a different session implemented the issue
  - Requires `--reason` for creator-exception approvals and logs them to security audit
  - Keeps implementer/self-approval blocked for non-minor issues

### Improvements
- Align `reviewable`/`in-review`/`status` reviewability hints with actual policy check

### Documentation
- Document balanced review policy in core workflow and references

## [v0.39.0] - 2026-02-26

### Features
- `td serve`: HTTP API server for programmatic access to td projects
  - Full CRUD for issues, comments, dependencies, boards, and focus
  - Status transition endpoints (start, review, approve, reject, close, reopen)
  - SSE event stream for real-time updates
  - Port file management and session bootstrap
  - Response envelope, DTOs, and validation helpers

### Fixes
- Support full agent file family (GEMINI.md, CLAUDE.local.md, etc) (#49)
- `td reject` resets issues to open instead of in_progress (#45, #47)
- Normalize action_log timestamp writes to RFC3339Nano UTC (#43)
- Exclude tasks with open dependencies from ready/next (#34)
- Prevent dependency divergence from phantom deletes and double normalization

### Documentation
- HTTP API documentation for `td serve`
- Improved sync setup guides based on user feedback (#39)
- Mention 100 character limit in title flag help text

## [v0.38.0] - 2026-02-19

### Fixes
- Fix approveIssue action in board/swimlanes view (#35)

## [v0.35.0] - 2026-02-14

### Features
- GTD-style deferral system: `td defer` and `td due` commands for managing temporal visibility
- `--defer` and `--due` flags on `td create` and `td update` for inline date assignment
- List temporal filters: `--deferred`, `--overdue`, `--surfacing`, `--due-soon` for focused views
- Monitor TUI modal displays defer/due dates with smart relative formatting
- Natural date parsing: `+7d`, `+2w`, `monday`, `tomorrow`, `next-week`, and more

### Documentation
- New deferral docs page covering GTD deferral concepts and usage
- Updated command reference with defer/due flags and temporal filters
- Updated monitor docs with defer/due date display

## [v0.34.0] - 2026-02-10

### Features
- `--work-dir` / `-w` global flag and `TD_WORK_DIR` env var for pointing td at a different project directory
  - Integrates with `.td-root` and git worktree resolution (unlike bypassing it)
  - Priority: `--work-dir` flag > `TD_WORK_DIR` env > cwd
  - Accepts path to project dir or directly to `.todos` dir
- Event taxonomy normalizer: centralized validation and normalization of entity and action types
  - Backward-compatible: accepts both singular/plural entity names and legacy action types
  - Comprehensive validation for all entity+action combinations in the sync/API layer

## [v0.33.0] - 2026-02-09

### Features
- Notes CLI: full CRUD via `td note` (add, list, show, edit, delete, pin, unpin, archive, unarchive)
- Notes CRUD database layer with soft-delete, undo support, and list filtering
- TDQ note query support: `note.` cross-entity fields (title, content, created, updated, pinned, archived)

### Bug Fixes
- Remove accidentally committed test artifacts
- Fix time parsing for TEXT timestamp columns in notes DB methods

## [v0.32.0] - 2026-02-08

### Features
- Admin API: server overview, config, and rate-limit-violations endpoints
- Admin API: user/auth endpoints — users list, detail, keys, auth events
- Admin API: project, events, and snapshots endpoints
- TDQ-powered snapshot query endpoint with server-side execution
- Integration test harness with fluent builder and assertion helpers
- Error code constants for consistent API responses

### Improvements
- Homebrew formula now builds from source (avoids macOS Gatekeeper warnings)

## [v0.31.0] - 2026-02-07

### Features
- Complete regression seed suite with verified seeds and runner integration
- Enable notes entity sync by default with feature flag

### Bug Fixes
- Resolve .todos in main repo for external git worktrees (gh pr checkout, Claude Code)
- Fix .todos lookup when td/sidecar launched from non-project-root directory
- Add sync feature flags to bash e2e harness (matching Go harness)
- Remove redundant notes schema from e2e test (latent schema mismatch)

## [v0.30.0] - 2026-02-06

### Features
- Sync engine: full multi-client sync with auto-sync, snapshot bootstrap, field-level merge, and conflict recording
- Sync CLI: `td sync init` guided setup wizard, `td sync tail` live activity view, `td config set/get/list`
- Notes entity support in sync
- Sync feature-flag framework with gated entity rollout
- Chaos sync test oracle with weighted random actions, convergence verification, and CLI runner
- Sparse board positioning with `ComputeInsertPosition` and automatic re-spacing
- Logged mutation layer (`*Logged` variants) for full undo/sync coverage
- Sync history tracking and pruning
- Multi-environment deployment system
- Nightshift added to sister projects

### Bug Fixes
- Field-level LWW merge prevents cross-field divergence
- Soft-delete board positions to prevent sync resurrection
- Cascade board position soft-deletes in sync receiver
- Map issue delete to soft_delete in sync protocol
- Prevent NULL points from sync partial update
- Handle NULL session columns after sync
- Backfill stale issues and handle undone creates in sync
- Detect dependency cycles during sync event application
- Drop UNIQUE(name) on boards to prevent sync data loss
- File locking and atomic writes for config
- Monitor periodic sync uses independent goroutine instead of BubbleTea Cmd

### Testing
- Comprehensive e2e sync test suite: chaos, convergence, clock skew, network partition, server restart, late-joiner, soak mode
- Syncharness test infrastructure for board delete cascades, server migration, and real-data scenarios
- Unit tests for sparse positioning and all logged mutation variants

### Documentation
- Sync setup and client guides
- Package-level godoc comments across 15 packages

## [v0.29.0] - 2026-02-02

### Bug Fixes
- Fix form width for text wrapping in issue modal
- Fix cross-entity query OR logic and blocks() wrong DB call
- Stop clipboard tests from clobbering system clipboard

## [v0.28.1] - 2026-01-31

### Bug Fixes
- Fix scan error on databases with unmigrated integer primary keys (CAST id AS TEXT in all SELECT queries)

## [v0.28.0] - 2026-01-30

### Features
- Primary key migration to enable future sync support
- GoReleaser binary releases and Homebrew formula

### Improvements
- Accessibility improvements
- Minor fixes from code review
- Transactional PK migration for safety

### Documentation
- Release guide wording fixes (cask → tap)

## [v0.27.0] - 2026-01-30

### Features
- GoReleaser binary releases and Homebrew formula
- Session migration to database

### Bug Fixes
- Revert URI DSN for modernc.org/sqlite, extract openConn helper
- Repair sessions table for DBs where v13 migration didn't apply

## [v0.26.0] - 2026-01-29

### Features
- Case-insensitive enum values in TDQ query language
- Much improved board editor modal
- ContextForm added to sidecar context map

### Bug Fixes
- Epic field query matched all issues instead of descendants
- Query language bug fixes
- Code review bug fix

## [v0.25.0] - 2026-01-28

### Features
- Exported `OpenIssueByIDMsg` for embedding contexts to programmatically open issue detail modals by ID

## [v0.24.0] - 2026-01-28

### Features
- Auto-unblock dependents when blocker is approved/closed
- OG image for rich link previews
- Redesigned marketing site hero and workflow sections

### Bug Fixes
- TUI actions now capture PreviousData/NewData for undo support
- TUI markForReview sets ImplementerSession when empty (matching CLI)
- TUI reopenIssue clears ReviewerSession (matching CLI)

## [v0.23.0] - 2026-01-26

### Features
- Group view controls in footer (view mode, show closed toggle, sort order)
- Split docs for easier navigation

### Bug Fixes
- Better focus handling for edit modal

## [v0.22.2] - 2026-01-26

### Bug Fixes
- Make list section a single tab stop instead of per-item (Tab now cycles list → buttons, not item1 → item2 → ... → buttons)

## [v0.22.1] - 2026-01-26

### Bug Fixes
- Fix board picker and handoffs modal navigation (j/k/up/down) not updating cursor due to value receiver semantics with declarative modal list pointers

## [v0.22.0] - 2026-01-25

### Features
- Migrate multiple modals to declarative library (Statistics, Handoffs, Board Picker, Delete/Close Confirmation)
- Add Getting Started modal for new users
- Improve monitor screenshot and workflow section styling
- Update marketing copy and redesign workflow sections
- Add Fraunces serif font for section headers

### Documentation
- Update modal inventory after declarative modal migrations

## [v0.21.0] - 2026-01-23

### Features
- Improve agent DX based on error pattern analysis

### Bug Fixes
- Fix agent fingerprint cache to only cache expensive process tree walk
- Add indices to schema for frequent queries to improve performance
- Fix critical path queries

### Documentation
- Update docs structure and marketing site

## [v0.20.0] - 2026-01-21

### Features
- Shorten issue IDs from 8 to 6 hex characters for easier typing
- Add collision retry logic for ID generation

## [v0.19.0] - 2026-01-21

### Features
- Include full task markdown when yanking epics (copies epic + all child stories)

### Bug Fixes
- Fix database connection leak in embedded monitor (connection pool singleton prevents FD accumulation)

## [v0.18.0] - 2026-01-20

### Features
- Add configurable title length limits via config (TitleMinLength, TitleMaxLength)
- Default max title length of 100 chars prevents description-as-title abuse

## [v0.17.0] - 2026-01-19

### Bug Fixes
- Add missing ESCAPE clause to label() SQL query for proper wildcard escaping
- Add error handling for is_ready()/has_open_deps() pre-fetch queries

## [v0.16.0] - 2026-01-19

### Features
- Add `epic.labels` field for query expressions
- Add `is_ready()` query function to find issues with no open dependencies
- Add `has_open_deps()` query function to check dependency status

### Bug Fixes
- Fix board refresh when query functions change
- Fix monitor panel header styling and row alignment
- Stabilize activity table column widths
- Fix activity table scrolling

## [v0.15.1] - 2026-01-19

### Bug Fixes
- Fix `--filter` flag validation: error if provided but empty
- Escape SQL wildcards in label() queries to prevent injection
- Use actual function name in label()/labels() error messages

## [v0.15.0] - 2026-01-17

### Bug Fixes
- Make SyntaxTheme actually apply Chroma themes in sidecar

## [v0.14.0] - 2026-01-17

### Features
- Add markdown theme support with custom chroma style builder
- Support hex color palettes and syntax themes for markdown rendering in monitor
- Allow embedders (sidecar) to customize theme via MarkdownThemeConfig

## [v0.13.0] - 2026-01-17

### Features
- Add send-to-worktree command for sidecar integration
- Add ctrl+K/ctrl+J shortcuts for move to top/bottom in board mode

### Bug Fixes
- ws handoff --review now uses proper review flow

## [v0.12.3] - 2026-01-14

### Features
- Persist filter state across sessions (search query, sort mode, type filter, include closed)
- Active search query now highlighted in orange for better visibility

### Documentation
- Add remote sync options research spec (Turso, rqlite, CR-SQLite analysis)

## [v0.12.2] - 2026-01-14

### Features
- Title validation for issue creation (min 20 chars, rejects generic titles)
- Cascade status changes to descendant issues (review, close, approve now cascade down)
- Epic task keybindings (O/R/C) in modal task section
- Created/closed timestamps shown in modal view
- Focus TaskList panel with cursor on first result after search

### Bug Fixes
- Modal actions (review, close, reopen) now work on focused epic tasks
- Modal refresh behavior instead of auto-close after status changes

## [v0.12.1] - 2026-01-14

### Bug Fixes
- Fix off-by-one mouse click bug in Current Work panel when no focused issue

## [v0.12.0] - 2026-01-14

### Features
- Sidecar worktree integration
- Mouse support for board picker
- CLI interface improvements

### Bug Fixes
- Fix modals when embedded in Sidecar
- Add panel checks to cursor commands when board mode active
- Fix for opening issues in top panel of td monitor
- Epic list consistency improvements

### Refactoring
- Split db.go into smaller files for maintainability

## [v0.11.0] - 2026-01-13

### Features
- Add gradient borders to sidecar panel

### Bug Fixes
- Fix session action recording and file locking for analytics
- Apply type filter correctly in board/backlog view
- Fix gradient border rendering issues

## [v0.10.0] - 2026-01-13

### Features
- Add board view with swimlanes in `td monitor`
  - New `td board` command for board operations
  - Toggle between swimlanes and backlog views
  - Keyboard navigation for board mode
  - Status-based swimlane organization
- Configurable keymap bindings system
- Improved blocked issue calculation and display

### Bug Fixes
- Fix line truncation issue in monitor view
- Fix mode switching in td monitor
- Respect sort order in swimlanes view
- Fix board movement issues
- Fix keyboard shortcuts in center panel

### Documentation
- Add board swimlanes and issue boards v2 specifications

## [v0.9.0] - 2026-01-10

### Features
- Add `rework()` query function for finding rejected issues awaiting rework
  - Query with `td query "rework()"` to find issues needing fixes
  - Efficient caching - fetches rework IDs once before filtering
- Show full log text in monitor task modal
  - No more truncation - long messages wrap properly
  - Uses cellbuf.Wrap for correct display-width handling
- Add Submit and Cancel buttons to form modal
  - Tab/Shift+Tab navigation between form fields and buttons
  - Mouse hover and click support for buttons

## [v0.8.0] - 2026-01-10

### Features
- Add issue state machine with workflow guards
  - Formal state transitions (open → in_progress → in_review → closed)
  - Validation guards prevent invalid state changes
  - New `td workflow` command for state diagnostics
- Add "needs rework" indicator for rejected in_progress issues
- Improved modal system documentation

### Bug Fixes
- Consolidate analytics logging to avoid double logging
- Add safe fallback for rejected issue detection errors

## [v0.7.0] - 2026-01-08

### Features
- Add local CLI analytics tracking (`td stats analytics`)
  - Track command usage, flags, duration, success/failure
  - Bar charts for most used commands and flags
  - List of least used and never used commands
  - Daily activity visualization
  - Session activity tracking
  - Toggle with `TD_ANALYTICS=false` env var
- Add unified `td stats` command with subcommands:
  - `td stats analytics` - Command usage statistics
  - `td stats security` - Security exception audit log
  - `td stats errors` - Failed command attempts

## [v0.6.0] - 2026-01-07

### Features
- Auto-handoff when submitting issues for review

### Bug Fixes
- Fix mouse offset issue when filtering or sorting in td monitor
- Remove self-close from close guidance

### Tests
- Additional test coverage

## [v0.5.0] - 2026-01-07

### Features
- Improved shortcuts panel for standalone `td` command
- Search field improvements
- Add `td security` command for viewing self-close exception audit logs

### Tests
- Add comprehensive modal scroll boundary tests
- Add comprehensive editor integration tests
- Add security command and review tests

## [v0.4.26] - 2026-01-06

### Bug Fixes
- ReviewableBy query now properly excludes issues where session is creator or in session history (not just implementer)
- Session migration now cleans up old session files after successful migration to agent-scoped format

### Tests
- Added `TestReviewableByFilter` with comprehensive scenarios covering creator, implementer, and session history bypass prevention
- Added tests for `ExplicitID` in agent fingerprint `String()` method

### Documentation
- Added release guide at `docs/guides/releasing-new-version.md` with step-by-step instructions
- Moved completed feature specifications to `docs/implemented/`

## [v0.4.25] - 2025-12-20

### Bug Fixes
- Epic create command now correctly sets issue type to epic

## [v0.4.24] - 2025-12-20

### Documentation
- Added warnings in developer guides about not starting new sessions mid-work (bypasses review)

## [v0.4.23] - 2025-12-19

### Bug Fixes
- Fixed mouse scroll and click offset issues in monitor TaskList

## [v0.4.22] - 2025-12-19

### Bug Fixes
- Removed dead code related to self-close enforcement

### Documentation
- Updated docs for self-close exception workflow

## [v0.4.21] - 2025-12-18

### Changed
- Updated review workflow process

## [v0.4.20] - 2025-12-17

### Features
- Improved agent-friendly interface with better CLI messages

### UI
- Enhanced td monitor modal styling and interactions

---

## Release Process

When releasing a new version:

1. **Update CHANGELOG.md** with new version at the top
2. **Follow semver** (Major.Minor.Patch):
   - Major: Breaking changes
   - Minor: New features (backward compatible)
   - Patch: Bug fixes only
3. **Create annotated git tag**: `git tag -a vX.Y.Z -m "Release vX.Y.Z: description"`
4. **Push commits and tag**: `git push origin main && git push origin vX.Y.Z`
5. **Create GitHub release** with release notes (can auto-generate from commits)
6. **Install with version**: `go install -ldflags "-X main.Version=vX.Y.Z" ./...`

See `docs/guides/releasing-new-version.md` for detailed instructions.
