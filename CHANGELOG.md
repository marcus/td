# Changelog

All notable changes to td are documented in this file.

## [Unreleased]

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
