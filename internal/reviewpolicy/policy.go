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
)

// ParseMode accepts the canonical string form and returns the corresponding
// Mode. Unknown values return an error rather than falling back to a default;
// callers that want a default should explicitly test for it.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeStrict, ModeBalanced, ModeDelegated:
		return Mode(s), nil
	case "":
		return "", fmt.Errorf("review_policy_mode is empty")
	default:
		return "", fmt.Errorf("unknown review_policy_mode %q (want strict|balanced|delegated)", s)
	}
}

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
	ReasonPriorInvolvement        = "you were involved with this issue (created, started, or previously worked on)"
	ReasonIssueNotInReview        = "issue is not in review"
	ReasonNoActiveReview          = "no active approval review exists yet for this issue"
	ReasonNotAllowedCloser        = "you are not an allowed closer for this issue (must be creator, implementer, reviewer, or review-requester)"
	ReasonIssueNotFound           = "issue not found"
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
	// policy mode. This mirrors the existing short-circuit.
	if in.Issue.Minor {
		return ReviewerEligibility{Allowed: true}
	}

	switch in.Mode {
	case ModeStrict:
		return evaluateReviewerStrict(in)
	case ModeBalanced:
		return evaluateReviewerBalanced(in)
	case ModeDelegated:
		return evaluateReviewerDelegated(in)
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
	return ReviewerEligibility{Allowed: true}
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
		}
	}

	if in.WasAnyInvolved {
		return ReviewerEligibility{
			RejectionMessage: fmt.Sprintf("cannot approve: %s (%s)", ReasonPriorInvolvement, in.Issue.ID),
		}
	}

	return ReviewerEligibility{Allowed: true}
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
	return ReviewerEligibility{Allowed: true}
}

// CloseEligibilityInput is the full set of facts the policy layer needs to
// decide whether a session may close an issue. The four "SessionIs*" role
// booleans correspond to the four allowed closer roles in the plan:
// creator, implementer, review-requester, reviewer-of-record.
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
	// Case 1: issue is in_review with an active approval review. Any of the
	// four allowed roles may close.
	if in.Issue.Status == models.StatusInReview && in.HasActiveApproval {
		if in.SessionIsCreator || in.SessionIsImplementer ||
			in.SessionIsReviewerOfRecord || in.SessionIsReviewRequester {
			return CloseEligibility{Allowed: true}
		}
		return CloseEligibility{
			RejectionMessage: fmt.Sprintf("cannot close: %s (%s)", ReasonNotAllowedCloser, in.Issue.ID),
		}
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
		m.LinkedFilesChanged ||
		m.DependenciesChanged ||
		m.WorkSessionTagsChanged ||
		m.ReparentCascade
}
