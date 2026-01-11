# Changelog

All notable changes to td are documented in this file.

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
