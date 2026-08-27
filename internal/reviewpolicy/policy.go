// Package reviewpolicy owns the shared review/close policy decisions that used
// to be duplicated across cmd/review_policy.go, internal/db/issues.go
// (ReviewableByFilter), pkg/monitor/actions.go, internal/serve/handlers_transitions.go,
// and internal/api/snapshot_query_source.go.
//
// Batch 1b goal: define the package and its surface without activating the
// new delegated-review behavior. Callers keep using their existing helpers;
// Batch 1c routes them through this package so all surfaces return identical
// decisions before Step 2 adds the record-only + close-after-review flow.
//
// The package is intentionally framework-free: it takes in plain data
// (issue + session booleans) and returns a decision. Database access,
// config loading, and logging happen in the callers — this keeps the policy
// logic pure and trivially testable.
package reviewpolicy

import (
	"fmt"

	"github.com/marcus/td/internal/models"
)

// Mode is the named-enum policy mode described in the plan under
// "Feature Flag and Compatibility Plan". Prefer this single string over
// stacked booleans so "what mode am I in?" is answerable from one config line.
type Mode string

const (
	// ModeStrict preserves the pre-balanced behavior: any prior involvement
	// (creator, implementer, or history) blocks approval and close.
	ModeStrict Mode = "strict"

	// ModeBalanced preserves the legacy creator-exception behavior. The
	// implementer still cannot self-approve, but a creator that did not
	// implement may approve work implemented by a different session. Step 5
	// made delegated the default; balanced is retained for projects that
	// explicitly opt in via review_policy_mode=balanced. New setups should
	// prefer delegated — it replaces the creator-exception workaround with
	// explicit review attestations that allow any involved role to close
	// after an independent review.
	ModeBalanced Mode = "balanced"

	// ModeDelegated is the new review-attestation model. Reviewer eligibility
	// is based solely on implementation independence (no started/unstarted
	// history, not the current implementer). Creator-only sessions are
	// eligible reviewers. Close-after-recorded-approval is wired in Step 2.
	ModeDelegated Mode = "delegated"

	// ModeTrusted is delegated plus an audited acknowledgement path for the
	// implementer. It keeps the delegated reviewer-independence norm — an
	// independent session is always the preferred reviewer — but lets a session
	// with implementation history record an approval and direct-close its own
	// work if it states, on the record, who reviewed it:
	//
	//   --reviewed-by "<who>"  the review is credited to a named party (a
	//                          sub-agent, a person, a tool). The expected path
	//                          for orchestrated work, where the orchestrator
	//                          holds the session but a sub-agent did the review.
	//   --self-review          the recorder reviewed their own work. Requires a
	//                          --reason, since nothing else vouches for it.
	//
	// Both stamp the review row for audit. Neither is verifiable by td: this is
	// an honesty guardrail, not a mechanical one, and that is deliberate. A
	// project that needs a mechanical independence boundary pins
	// review_policy_mode=delegated or strict, where an involved session cannot
	// approve at all. Without either acknowledgement, trusted behaves exactly
	// like delegated and rejects with a teaching message.
	ModeTrusted Mode = "trusted"
)

// ParseMode accepts the canonical string form and returns the corresponding
// Mode. Unknown values return an error rather than falling back to a default;
// callers that want a default should explicitly test for it.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeStrict, ModeBalanced, ModeDelegated, ModeTrusted:
		return Mode(s), nil
	case "":
		return "", fmt.Errorf("review_policy_mode is empty")
	default:
		return "", fmt.Errorf("unknown review_policy_mode %q (want strict|balanced|delegated|trusted)", s)
	}
}

// MaxReviewedByLen caps the --reviewed-by / reviewed_by attribution. It is a
// label for a human or an agent to read in review history, not a place to put
// the review itself — that belongs in the reason/summary. The cap keeps
// `td show` output and the monitor's review rows readable.
//
// It lives here rather than in a surface package so the CLI, the API, and the
// TUI cannot drift on what they accept.
const MaxReviewedByLen = 120

// CascadeFromParentApproval is the named exemption used when an epic-approval
// cascade closes descendants in bulk. See pkg/monitor/actions.go approval
// cascade. The new model records it as an issue_reviews row with this
// decision so audit output can tell cascaded closes from direct ones.
const CascadeFromParentApproval = "cascade_from_parent_approval"

// Decision values used in issue_reviews.decision. These are duplicated in
// internal/db/reviews.go as string literals today; centralizing them here
// lets Batch 1c switch call sites to named constants without introducing a
// models-package dependency cycle.
const (
	DecisionApproved                = "approved"
	DecisionChangesRequested        = "changes_requested"
	DecisionApprovedByParentCascade = "approved_by_parent_cascade"
)

// Rejection-reason constants. Callers format their own final messages on top
// of these base strings so surfaces can produce uniform error text without
// sharing sprintf templates.
const (
	ReasonImplementerCannotReview = "you cannot review your own implementation"

	// ReasonTrustedSelfReviewNeedsFlag is the teaching base string used when a
	// session that implemented (or has implementation history) tries to review
	// in trusted mode without saying who reviewed the work. It leads with
	// attribution because that is now the expected path for orchestrated work:
	// a sub-agent reviews, the orchestrator records who did it. Genuine
	// self-review is the second option, not the first. Callers append the
	// issue ID via the format helper below.
	ReasonTrustedSelfReviewNeedsFlag = "you implemented this issue, so approving it needs an attribution. If a sub-agent or another party reviewed it, name them: --reviewed-by \"<who>\". If you reviewed your own work, say so: --self-review --reason \"...\""

	ReasonPriorInvolvement = "you were involved with this issue (created, started, or previously worked on)"
	ReasonIssueNotInReview = "issue is not in review"
	ReasonNoActiveReview   = "no active approval review exists yet for this issue"
	ReasonNotAllowedCloser = "no active independent approval review exists for this issue"
	ReasonIssueNotFound    = "issue not found"
)

// ReviewerEligibilityInput is the full set of facts the policy layer needs
// to decide whether a session may record an approval review for an issue.
//
// Booleans are computed by the caller so the policy layer has no DB
// dependency. The caller is expected to use existing helpers like
// db.WasSessionImplementationInvolved to populate HasImplementationHistory.
type ReviewerEligibilityInput struct {
	Mode                     Mode
	Issue                    *models.Issue
	SessionID                string
	SessionIsImplementer     bool
	SessionIsCreator         bool
	HasImplementationHistory bool // WasSessionImplementationInvolved
	HasActiveApproval        bool // GetActiveApprovalReview != nil

	// SelfReviewAcknowledged is true when the caller passed --self-review (or
	// the UI equivalent). It only has an effect in trusted mode, where it
	// converts the implementer self-review rejection into a flag-gated,
	// audited allow.
	SelfReviewAcknowledged bool

	// AttributedTo names who actually performed the review, as asserted by the
	// recording session (--reviewed-by). It is deliberately unverifiable: td
	// cannot confirm that a named sub-agent exists or read anything. Its value
	// is that the audit record stops being false when an orchestrator records
	// a review a sub-agent performed.
	//
	// It NEVER grants permission on its own. In trusted mode it satisfies the
	// same involved-session acknowledgement that SelfReviewAcknowledged does;
	// in every other mode it is metadata the caller may still record, and the
	// eligibility decision ignores it.
	//
	// Callers are responsible for trimming and rejecting blank values before
	// populating this field — the policy layer treats any non-empty string as
	// a valid attestation.
	AttributedTo string

	// WasAnyInvolved mirrors the old WasSessionInvolved helper (any history
	// row at all, including created/reviewed). Required for strict mode
	// parity with the current cmd/review_policy.go:evaluateApproveEligibility
	// behavior. Balanced/delegated modes ignore it once the implementation
	// check already ruled the session out.
	//
	// NOTE: this is intentionally broader than HasImplementationHistory. That
	// helper counts only started/unstarted action rows; WasAnyInvolved counts
	// ANY row in issue_session_history, including `created` and `reviewed`.
	// Callers must not conflate the two: strict mode uses WasAnyInvolved to
	// preserve the "any prior involvement disqualifies" rule; balanced and
	// delegated modes use HasImplementationHistory to allow creator-only
	// approvals of work another session implemented.
	WasAnyInvolved bool
}

// ReviewerEligibility is the decision returned by EvaluateReviewerEligibility.
// CreatorException marks the balanced-mode path where the current behavior
// already required a reason.
type ReviewerEligibility struct {
	Allowed          bool
	CreatorException bool
	RequiresReason   bool
	RejectionMessage string

	// SelfReview marks a review recorded by a session that implemented the work
	// (or has implementation history). It is true for BOTH trusted-mode
	// acknowledgement paths — --self-review and --reviewed-by — because in both
	// cases the row was written by an involved session, which is the audit fact
	// the column has always recorded. AttributedTo is what distinguishes them:
	//
	//   SelfReview && AttributedTo == ""  -> the recorder reviewed their own work
	//   SelfReview && AttributedTo != ""  -> an involved recorder, review credited elsewhere
	//   !SelfReview                       -> recorded by an independent session
	SelfReview bool

	// AttributedTo echoes the caller's attribution back so every surface stamps
	// the review row from the policy decision rather than from raw input. An
	// independent session may also supply one (crediting a reviewer without
	// needing the acknowledgement), so this can be set with SelfReview false.
	AttributedTo string
}

// EvaluateReviewerEligibility decides whether the current session may record
// an approval review for the supplied issue. Batch 1b keeps this function
// behavior-equivalent to the existing cmd/review_policy.go logic for strict
// and balanced modes. Delegated mode defines the new permission-to-record
// check; it does NOT yet alter caller flow (that lands in Batch 1c / Step 2).
func EvaluateReviewerEligibility(in ReviewerEligibilityInput) ReviewerEligibility {
	if in.Issue == nil {
		return ReviewerEligibility{RejectionMessage: "cannot approve: " + ReasonIssueNotFound}
	}

	// Minor tasks intentionally bypass all self-review restrictions in every
	// policy mode. This mirrors the existing short-circuit. Attribution is
	// still echoed through: a minor task reviewed by a named party should say
	// so on the record.
	if in.Issue.Minor {
		return ReviewerEligibility{Allowed: true, AttributedTo: in.AttributedTo}
	}

	switch in.Mode {
	case ModeStrict:
		return evaluateReviewerStrict(in)
	case ModeBalanced:
		return evaluateReviewerBalanced(in)
	case ModeDelegated:
		return evaluateReviewerDelegated(in)
	case ModeTrusted:
		return evaluateReviewerTrusted(in)
	default:
		// Unknown modes behave like strict so a misconfigured system fails
		// closed rather than silently opening approval.
		return evaluateReviewerStrict(in)
	}
}

func evaluateReviewerStrict(in ReviewerEligibilityInput) ReviewerEligibility {
	// Strict: any prior involvement disqualifies. Mirrors the original
	// non-balanced branch at cmd/review_policy.go:51-59.
	if in.WasAnyInvolved || in.SessionIsCreator || in.SessionIsImplementer {
		return ReviewerEligibility{
			RejectionMessage: fmt.Sprintf("cannot approve: %s (%s)", ReasonPriorInvolvement, in.Issue.ID),
		}
	}
	return ReviewerEligibility{Allowed: true, AttributedTo: in.AttributedTo}
}

func evaluateReviewerBalanced(in ReviewerEligibilityInput) ReviewerEligibility {
	// Balanced still hard-blocks implementation self-approval. Mirrors
	// cmd/review_policy.go:61-86.
	if in.SessionIsImplementer || in.HasImplementationHistory {
		return ReviewerEligibility{
			RejectionMessage: fmt.Sprintf("cannot approve: %s of %s", ReasonImplementerCannotReview, in.Issue.ID),
		}
	}

	hasDifferentImplementer := in.Issue.ImplementerSession != "" && in.Issue.ImplementerSession != in.SessionID
	if in.SessionIsCreator && hasDifferentImplementer {
		return ReviewerEligibility{
			Allowed:          true,
			CreatorException: true,
			RequiresReason:   true,
			AttributedTo:     in.AttributedTo,
		}
	}

	if in.WasAnyInvolved {
		return ReviewerEligibility{
			RejectionMessage: fmt.Sprintf("cannot approve: %s (%s)", ReasonPriorInvolvement, in.Issue.ID),
		}
	}

	return ReviewerEligibility{Allowed: true, AttributedTo: in.AttributedTo}
}

func evaluateReviewerDelegated(in ReviewerEligibilityInput) ReviewerEligibility {
	// Delegated: the sole permission check for recording approval is
	// implementation independence. Creators who never implemented are
	// eligible reviewers (so orchestrators that never ran `td start` aren't
	// blocked). See plan section "Core Policy Rules > Reviewer eligibility".
	//
	// Batch 1b intentionally does NOT branch on HasActiveApproval — callers
	// that want to route to "close-using-recorded-approval" inspect that
	// field themselves. This keeps the reviewer predicate stable while
	// Step 2 wires the new flow.
	if in.SessionIsImplementer || in.HasImplementationHistory {
		return ReviewerEligibility{
			RejectionMessage: fmt.Sprintf("cannot approve: %s of %s", ReasonImplementerCannotReview, in.Issue.ID),
		}
	}
	return ReviewerEligibility{Allowed: true, AttributedTo: in.AttributedTo}
}

// evaluateReviewerTrusted implements the trusted-mode reviewer predicate. It is
// delegated with one difference: the delegated reject for an implementer (or a
// session with implementation history) becomes a flag-gated, audited allow.
//
// The trigger condition is intentionally identical to the delegated reject at
// evaluateReviewerDelegated (SessionIsImplementer || HasImplementationHistory) —
// we convert that exact reject into an allow-with-flag rather than inventing a
// looser predicate. A session that merely created or viewed the issue (no
// started history) still needs no flag, exactly as in delegated mode.
func evaluateReviewerTrusted(in ReviewerEligibilityInput) ReviewerEligibility {
	if !in.SessionIsImplementer && !in.HasImplementationHistory {
		// Independent session: eligible reviewer, no acknowledgement needed and
		// not a self-review. It may still credit a reviewer by name, so echo
		// any attribution through for the audit record.
		return ReviewerEligibility{Allowed: true, AttributedTo: in.AttributedTo}
	}

	// Attributed review: the recording session implemented the work but names
	// someone else as the reviewer. This is the expected path for an
	// orchestrator recording a sub-agent's review. The row is stamped
	// SelfReview (an involved session wrote it) AND AttributedTo (the review is
	// credited elsewhere) so the record is literally true about both facts.
	//
	// No reason is required: the attribution is the substance, and this is the
	// default path for orchestrated work — forcing a second string here is
	// friction on the common case.
	if in.AttributedTo != "" {
		return ReviewerEligibility{
			Allowed:      true,
			SelfReview:   true,
			AttributedTo: in.AttributedTo,
		}
	}

	if in.SelfReviewAcknowledged {
		// Acknowledged self-review: allow, mark it for audit, require a reason.
		// Unlike the attributed path this one asserts nobody else looked at the
		// work, so the reason carries what was actually checked.
		return ReviewerEligibility{
			Allowed:        true,
			SelfReview:     true,
			RequiresReason: true,
		}
	}

	// Involved session with no attestation at all: reject with a teaching
	// message that leads with attribution and offers self-review second.
	return ReviewerEligibility{
		RejectionMessage: fmt.Sprintf("cannot approve: %s (%s)", ReasonTrustedSelfReviewNeedsFlag, in.Issue.ID),
	}
}

// CloseEligibilityInput is the full set of facts the policy layer needs to
// decide whether a session may close an issue. In delegated mode, the active
// approval record is the safety gate; the caller's role is audit metadata,
// not a close permission.
type CloseEligibilityInput struct {
	Mode                      Mode
	Issue                     *models.Issue
	SessionID                 string
	SessionIsImplementer      bool
	SessionIsCreator          bool
	SessionIsReviewerOfRecord bool // session == issue.ReviewerSession, non-empty
	SessionIsReviewRequester  bool // session == issue.ReviewRequestedBySession, non-empty
	HasImplementationHistory  bool
	WasAnyInvolved            bool
	HasActiveApproval         bool

	// SelfReviewAcknowledged is true when the caller passed --self-review. It
	// only has an effect in trusted mode's direct review+close path (Case 2),
	// where it lets the implementer approve+close their own work in one action.
	SelfReviewAcknowledged bool

	// AttributedTo is the --reviewed-by attestation. Like
	// SelfReviewAcknowledged it only matters on the trusted direct
	// review+close path, where it satisfies the involved-session
	// acknowledgement without claiming the recorder did the review.
	AttributedTo string
}

// CloseEligibility is the decision returned by EvaluateCloseEligibility.
type CloseEligibility struct {
	Allowed           bool
	CreatorOpenBypass bool // preserves the existing balanced-mode self-created throwaway path
	RequiresReason    bool
	RejectionMessage  string
}

// EvaluateCloseEligibility decides whether the current session may close the
// supplied issue. Strict and balanced modes reproduce the existing behavior
// exactly (cmd/review_policy.go:evaluateCloseEligibility). Delegated mode
// defines the new close-after-review predicate but is not yet activated by
// callers — Batch 1c + Step 2 flip the call sites.
func EvaluateCloseEligibility(in CloseEligibilityInput) CloseEligibility {
	if in.Issue == nil {
		return CloseEligibility{RejectionMessage: "cannot close: " + ReasonIssueNotFound}
	}

	// Minor tasks bypass self-close restrictions in every mode. Preserved
	// from cmd/review_policy.go:99.
	if in.Issue.Minor {
		return CloseEligibility{Allowed: true}
	}

	switch in.Mode {
	case ModeStrict, ModeBalanced:
		return evaluateCloseStrictBalanced(in)
	case ModeDelegated:
		return evaluateCloseDelegated(in)
	case ModeTrusted:
		return evaluateCloseTrusted(in)
	default:
		return evaluateCloseStrictBalanced(in)
	}
}

// evaluateCloseStrictBalanced implements the existing (non-delegated) close
// rule. Strict and balanced modes produce the same close decision today —
// the current code path shares a single evaluateCloseEligibility helper and
// does not branch on balancedPolicy.
func evaluateCloseStrictBalanced(in CloseEligibilityInput) CloseEligibility {
	// Narrow bypass: creator-owned issue still open, no implementation
	// history by anyone.
	if in.SessionIsCreator && in.Issue.Status == models.StatusOpen &&
		!in.HasImplementationHistory && !in.SessionIsImplementer {
		return CloseEligibility{
			Allowed:           true,
			CreatorOpenBypass: true,
		}
	}

	if in.SessionIsImplementer || in.HasImplementationHistory {
		return CloseEligibility{
			RejectionMessage: fmt.Sprintf("cannot close own implementation: %s", in.Issue.ID),
		}
	}

	if in.SessionIsCreator {
		if in.HasImplementationHistory {
			return CloseEligibility{
				RejectionMessage: fmt.Sprintf("cannot close: %s has implementation history and requires review", in.Issue.ID),
			}
		}
		return CloseEligibility{
			RejectionMessage: fmt.Sprintf("cannot close: you created %s and it requires review", in.Issue.ID),
		}
	}

	if in.WasAnyInvolved {
		return CloseEligibility{
			RejectionMessage: fmt.Sprintf("cannot close: you previously worked on %s", in.Issue.ID),
		}
	}

	return CloseEligibility{Allowed: true}
}

// evaluateCloseDelegated implements the new review-attestation close rule.
// Batch 1b treats this as defining the predicate, not activating it — no
// caller routes through delegated mode yet.
func evaluateCloseDelegated(in CloseEligibilityInput) CloseEligibility {
	// Case 1: issue is in_review with an active approval review. Any session
	// may close because reviewer independence was already enforced when the
	// approval was recorded. If the closer differs from the reviewer, callers
	// require --reason and stamp closed_by_session for audit.
	if in.Issue.Status == models.StatusInReview && in.HasActiveApproval {
		return CloseEligibility{Allowed: true}
	}

	// Case 2: issue is in_review without an active approval. This is the
	// "direct review + close" fast path — reviewer eligibility IS close
	// eligibility. Reuse the delegated reviewer predicate so the two
	// decisions stay aligned.
	if in.Issue.Status == models.StatusInReview {
		rev := evaluateReviewerDelegated(ReviewerEligibilityInput{
			Mode:                     ModeDelegated,
			Issue:                    in.Issue,
			SessionID:                in.SessionID,
			SessionIsImplementer:     in.SessionIsImplementer,
			SessionIsCreator:         in.SessionIsCreator,
			HasImplementationHistory: in.HasImplementationHistory,
		})
		if rev.Allowed {
			return CloseEligibility{Allowed: true}
		}
		return CloseEligibility{RejectionMessage: "cannot close: " + rev.RejectionMessage}
	}

	// Case 3: issue is NOT in_review (still open/in_progress/blocked).
	// Delegated mode preserves the historical admin-only close gate: such
	// issues cannot be closed via EvaluateCloseEligibility unless the caller
	// matches the strict/balanced creator-open-bypass for a never-implemented
	// self-created throwaway. Falling through to reviewer-eligibility here
	// (as the first draft did) would let an uninvolved session close a still-
	// open issue it never looked at. Run through the strict/balanced predicate
	// so delegated mode never relaxes the non-in_review gate.
	return evaluateCloseStrictBalanced(in)
}

// evaluateCloseTrusted mirrors evaluateCloseDelegated. The only difference is
// Case 2 (in_review, no recorded approval): instead of the delegated reviewer
// predicate it routes through the trusted reviewer predicate, so an implementer
// who acknowledges the self-review with --self-review can direct approve+close
// their own work in one action.
func evaluateCloseTrusted(in CloseEligibilityInput) CloseEligibility {
	// Case 1: in_review with an active independent approval. Any session may
	// close — reviewer independence was already enforced when the approval was
	// recorded.
	if in.Issue.Status == models.StatusInReview && in.HasActiveApproval {
		return CloseEligibility{Allowed: true}
	}

	// Case 2: in_review without an active approval — the direct review+close
	// fast path. Reviewer eligibility IS close eligibility. Use the trusted
	// reviewer predicate so the self-review flag gates the implementer path and
	// the teaching message propagates on rejection.
	if in.Issue.Status == models.StatusInReview {
		rev := evaluateReviewerTrusted(ReviewerEligibilityInput{
			Mode:                     ModeTrusted,
			Issue:                    in.Issue,
			SessionID:                in.SessionID,
			SessionIsImplementer:     in.SessionIsImplementer,
			SessionIsCreator:         in.SessionIsCreator,
			HasImplementationHistory: in.HasImplementationHistory,
			SelfReviewAcknowledged:   in.SelfReviewAcknowledged,
			AttributedTo:             in.AttributedTo,
		})
		if rev.Allowed {
			// An unattributed self-review close requires a reason for audit. An
			// attributed close does not (the attribution is the substance), and
			// an independent direct close inherits the reviewer decision — in
			// both cases RequiresReason comes straight from the reviewer
			// predicate so the two decisions cannot drift.
			return CloseEligibility{Allowed: true, RequiresReason: rev.RequiresReason}
		}
		return CloseEligibility{RejectionMessage: "cannot close: " + rev.RejectionMessage}
	}

	// Case 3: not in_review — fall through to the strict/balanced gate exactly
	// like delegated, so trusted mode never relaxes the non-in_review close.
	return evaluateCloseStrictBalanced(in)
}

// IssueMutation describes the subset of an issue-update diff that is relevant
// to review freshness. Pure-metadata fields (due_date, labels, notes,
// comments, log entries) are intentionally excluded from the struct so new
// callers cannot accidentally widen the invalidation set.
type IssueMutation struct {
	DescriptionChanged bool
	TitleChanged       bool
	TypeChanged        bool
	PriorityChanged    bool
	MinorChanged       bool
	ParentIDChanged    bool

	// StatusChangedFromReviewNotClosing is true when an issue transitions
	// out of in_review to any status other than closed (i.e. rejected back
	// to open/in_progress/blocked). An in_review -> closed transition
	// should NOT flag this; that's the normal close path and must not
	// supersede its own approval.
	StatusChangedFromReviewNotClosing bool

	// Reopened is true when an issue leaves closed (today that is only
	// closed -> open). Close keeps its own approval active so
	// close-after-review still has a row to consult; reopen starts a new
	// review epoch and must not inherit that leftover gold stamp.
	Reopened bool

	LinkedFilesChanged     bool
	DependenciesChanged    bool
	WorkSessionTagsChanged bool

	// ReparentCascade is true when a parent-reparent cascade touched the
	// issue indirectly. Cascades that effectively re-scope the issue should
	// supersede any pending review.
	ReparentCascade bool
}

// IsReviewInvalidatingMutation returns true if any of the flagged changes
// should supersede an active approval review on the issue. Called from both
// the DB write path and the sync import path in Batch 1c.
func IsReviewInvalidatingMutation(m IssueMutation) bool {
	return m.DescriptionChanged ||
		m.TitleChanged ||
		m.TypeChanged ||
		m.PriorityChanged ||
		m.MinorChanged ||
		m.ParentIDChanged ||
		m.StatusChangedFromReviewNotClosing ||
		m.Reopened ||
		m.LinkedFilesChanged ||
		m.DependenciesChanged ||
		m.WorkSessionTagsChanged ||
		m.ReparentCascade
}
