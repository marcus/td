# Changelog

All notable changes to td are documented in this file.

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
