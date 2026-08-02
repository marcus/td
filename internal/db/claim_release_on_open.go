package db

import "github.com/marcus/td/internal/models"

// releaseClaimOnOpen enforces one invariant on every logged issue write: an
// issue whose next state is `open` carries no implementer claim.
//
// Open means unclaimed work. An issue that is open AND held is a state no
// surface can act on correctly: `td unstart <id>` reads the status alone and
// reports "already unstarted" while the claim is still there, `td next` offers
// the issue to a second agent, and the sweeps only reclaim it after the fact.
//
// This lives in the shared write path — updateIssueAndLogFromPreviousStore and
// its review-metadata twin, through which every logged issue mutation on every
// surface passes — deliberately. The same leak was fixed three times at the
// call site (td-c45f99 for `td reopen`, td-d2e612 for `td unblock` and the
// auto-unblock cascade) and each fix left the other producers untouched:
// `td update --status open`, the API's /unblock and /reopen handlers, the TUI
// reopen action, and the TUI edit form. Clearing here means a surface cannot
// leak the claim by forgetting to, and a new transition to open inherits the
// release without knowing it exists.
//
// It mutates the caller's issue in place, so the struct the caller goes on to
// render, return as JSON, or serialize into action_log NewData shows the same
// released state the row does.
//
// Scope: the LOGGED write path only. The unlogged UpdateIssue / UpsertIssueRaw
// / ReplaceIssueRaw paths exist to apply state authored elsewhere (sync
// receiver, `td system import`) and must stay faithful to what they are given.
// db.ReleaseClaims writes its own guarded UPDATE and already releases.
//
// Returns whether it changed anything, for tests and future callers.
func releaseClaimOnOpen(next *models.Issue) bool {
	if next == nil || next.Status != models.StatusOpen || next.ImplementerSession == "" {
		return false
	}
	next.ImplementerSession = ""
	return true
}
