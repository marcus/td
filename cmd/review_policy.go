package cmd

// The cmd-layer review-policy wrapper keeps the user-facing rejection
// messages that cmd/review_policy_test.go, cmd/review_test.go, and
// cmd/approve_test.go assert on. internal/reviewpolicy owns the decision;
// this file maps it into the approveEligibility / closeEligibility shapes
// that the rest of cmd/*.go consumes. If a rejection string changes, update
// both this file and the corresponding test file in the same change.

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
)

type approveEligibility struct {
	Allowed          bool
	CreatorException bool
	RequiresReason   bool
	RejectionMessage string

	// SelfReview is true when the trusted-mode self-review path applied: the
	// caller is the implementer (or has implementation history) and passed
	// --self-review. Callers stamp it on the recorded review row.
	SelfReview bool
}

type closeEligibility struct {
	Allowed           bool
	CreatorOpenBypass bool
	RequiresReason    bool
	RejectionMessage  string
}

func balancedReviewPolicyEnabled(baseDir string) bool {
	// Step 5: balanced mode is active only when it is the EFFECTIVE resolved
	// mode. This keeps ListIssuesOptions.BalancedReviewPolicy consistent with
	// features.ResolveReviewPolicyMode — both return ModeBalanced only when
	// the user explicitly opts in (via review_policy_mode=balanced or the
	// legacy balanced_review_policy=true flag). A default/fresh project now
	// resolves to delegated, so this helper returns false there.
	mode, err := resolveReviewPolicyMode(baseDir)
	if err != nil {
		return false
	}
	return mode == reviewpolicy.ModeBalanced
}

// resolveReviewPolicyMode returns the effective review policy mode for a
// baseDir. Step 5 delegates the decision to features.ResolveReviewPolicyMode
// so cmd/* and monitor/serve/api surfaces never diverge: a fresh project
// resolves to delegated, an explicit review_policy_mode= wins, and the
// legacy balanced_review_policy flag is honored only when set explicitly
// (with a one-time deprecation warning from the features layer).
func resolveReviewPolicyMode(baseDir string) (reviewpolicy.Mode, error) {
	return features.ResolveReviewPolicyMode(baseDir)
}

func reviewableByOptions(baseDir, sessionID string) db.ListIssuesOptions {
	mode, err := resolveReviewPolicyMode(baseDir)
	modeString := ""
	balanced := false
	if err == nil {
		modeString = string(mode)
		balanced = mode == reviewpolicy.ModeBalanced
	}
	return db.ListIssuesOptions{
		ReviewableBy:         sessionID,
		BalancedReviewPolicy: balanced,
		ReviewPolicyMode:     modeString,
	}
}

func readyToCloseByOptions(baseDir, sessionID string) db.ListIssuesOptions {
	mode, err := resolveReviewPolicyMode(baseDir)
	modeString := ""
	balanced := false
	if err == nil {
		modeString = string(mode)
		balanced = mode == reviewpolicy.ModeBalanced
	}
	return db.ListIssuesOptions{
		ReadyToCloseBy:       sessionID,
		BalancedReviewPolicy: balanced,
		ReviewPolicyMode:     modeString,
	}
}

func approvalCandidateIssues(database *db.DB, baseDir, sessionID string, includeReadyToClose bool) ([]models.Issue, error) {
	reviewable, err := database.ListIssues(reviewableByOptions(baseDir, sessionID))
	if err != nil {
		return nil, err
	}
	if !includeReadyToClose {
		return reviewable, nil
	}

	seen := make(map[string]bool, len(reviewable))
	for _, issue := range reviewable {
		seen[issue.ID] = true
	}

	ready, err := database.ListIssues(readyToCloseByOptions(baseDir, sessionID))
	if err != nil {
		return nil, err
	}
	for _, issue := range ready {
		if seen[issue.ID] {
			continue
		}
		reviewable = append(reviewable, issue)
		seen[issue.ID] = true
	}
	return reviewable, nil
}

// evaluateApproveEligibility is the cmd-layer wrapper that routes through
// internal/reviewpolicy while keeping the legacy rejection strings that
// cmd/*_test.go depend on.
//
// Existing callers pass a boolean `balancedPolicy` flag; we translate it to
// the named-enum mode here so the wrapper signature stays stable for
// consumers in review.go / list.go / status.go. The boolean flag is still
// resolved upstream via balancedReviewPolicyEnabled(baseDir).
//
// When the caller already has the resolved mode (e.g. delegated-mode record-
// only or direct approve paths), prefer evaluateApproveEligibilityWithMode
// so prior-reviewer repeat eligibility is honored under delegated mode.
func evaluateApproveEligibility(issue *models.Issue, sessionID string, wasInvolved, wasImplementationInvolved, balancedPolicy bool) approveEligibility {
	mode := reviewpolicy.ModeStrict
	if balancedPolicy {
		mode = reviewpolicy.ModeBalanced
	}
	return evaluateApproveEligibilityWithMode(issue, sessionID, wasInvolved, wasImplementationInvolved, mode, false)
}

// evaluateApproveEligibilityWithMode is the mode-aware variant used by the
// Step 2 record-only and close-after-review paths. It preserves the legacy
// balanced/strict rejection strings while letting delegated mode apply its
// own rule (prior reviewers may re-review).
func evaluateApproveEligibilityWithMode(issue *models.Issue, sessionID string, wasInvolved, wasImplementationInvolved bool, mode reviewpolicy.Mode, selfReview bool) approveEligibility {
	balancedPolicy := mode == reviewpolicy.ModeBalanced

	in := reviewpolicy.ReviewerEligibilityInput{
		Mode:                     mode,
		Issue:                    issue,
		SessionID:                sessionID,
		SessionIsImplementer:     issue != nil && issue.ImplementerSession != "" && issue.ImplementerSession == sessionID,
		SessionIsCreator:         issue != nil && issue.CreatorSession != "" && issue.CreatorSession == sessionID,
		HasImplementationHistory: wasImplementationInvolved,
		WasAnyInvolved:           wasInvolved,
		SelfReviewAcknowledged:   selfReview,
	}
	decision := reviewpolicy.EvaluateReviewerEligibility(in)

	// Translate reviewpolicy's decision back to the current cmd strings.
	// We DO NOT forward decision.RejectionMessage because reviewpolicy wording
	// differs slightly (e.g. "you cannot review your own implementation" vs
	// the cmd's historical "you were involved with implementation of ...").
	if decision.Allowed {
		return approveEligibility{
			Allowed:          true,
			CreatorException: decision.CreatorException,
			RequiresReason:   decision.RequiresReason,
			SelfReview:       decision.SelfReview,
		}
	}

	if issue == nil {
		return approveEligibility{
			Allowed:          false,
			RejectionMessage: "cannot approve: issue not found",
		}
	}

	// Trusted mode owns a teaching rejection (names both the preferred
	// independent-review norm and the --self-review escape hatch). Forward it
	// verbatim so agents see the actionable guidance instead of the generic
	// "you were involved" string.
	if mode == reviewpolicy.ModeTrusted && decision.RejectionMessage != "" {
		return approveEligibility{
			Allowed:          false,
			RejectionMessage: decision.RejectionMessage,
		}
	}

	// Preserve the historical rejection wording exactly.
	if balancedPolicy {
		if in.SessionIsImplementer || in.HasImplementationHistory {
			return approveEligibility{
				Allowed:          false,
				RejectionMessage: fmt.Sprintf("cannot approve: you were involved with implementation of %s", issue.ID),
			}
		}
	}
	return approveEligibility{
		Allowed:          false,
		RejectionMessage: fmt.Sprintf("cannot approve: you were involved with %s (created, started, or previously worked on)", issue.ID),
	}
}

// evaluateCloseEligibility is the cmd-layer wrapper around
// reviewpolicy.EvaluateCloseEligibility. The same wording-preservation note
// from evaluateApproveEligibility applies: the policy package owns the
// decision, this wrapper owns the user-facing string.
//
// Step 2: this wrapper keeps the legacy 5-arg signature (strict/balanced
// semantics) for existing callers. Use evaluateCloseEligibilityForBaseDir
// when the caller has baseDir available and wants delegated-mode resolution.
func evaluateCloseEligibility(issue *models.Issue, sessionID string, wasInvolved, wasImplementationInvolved, hasImplementationHistory bool) closeEligibility {
	// The cmd-layer `td close` flow is strict/balanced today; Step 2 wires
	// delegated-aware closers via evaluateCloseEligibilityForBaseDir. The
	// legacy signature preserves the pre-batch behavior where a post-decision
	// veto handles the issue-wide impl-history gate.
	mode := reviewpolicy.ModeStrict
	return evaluateCloseEligibilityWithMode(issue, sessionID, wasInvolved, wasImplementationInvolved, hasImplementationHistory, mode, false, false)
}

// evaluateCloseEligibilityForBaseDir resolves the project review_policy_mode
// and routes through reviewpolicy with the right mode. Callers that have
// baseDir access should prefer this wrapper so delegated-mode closers (see
// Step 2 CLI changes) work without needing a separate code path.
//
// hasActiveApproval lets the caller signal that an active approval review
// already exists on the issue — required for the delegated close-using-
// recorded-approval path. Strict/balanced modes ignore it.
func evaluateCloseEligibilityForBaseDir(
	baseDir string,
	issue *models.Issue,
	sessionID string,
	wasInvolved, wasImplementationInvolved, hasImplementationHistory bool,
	hasActiveApproval bool,
	selfReview bool,
) closeEligibility {
	mode, err := resolveReviewPolicyMode(baseDir)
	if err != nil {
		// Fail-closed: unknown mode configurations drop to strict rules.
		mode = reviewpolicy.ModeStrict
	}
	return evaluateCloseEligibilityWithMode(issue, sessionID, wasInvolved, wasImplementationInvolved, hasImplementationHistory, mode, hasActiveApproval, selfReview)
}

// evaluateCloseEligibilityWithMode is the shared implementation behind
// evaluateCloseEligibility (legacy) and evaluateCloseEligibilityForBaseDir.
// The issue-wide impl-history veto (wantCLICloseBlockedByIssueVeto in parity
// tests) is applied under strict/balanced only; delegated mode owns the
// full rule in reviewpolicy and does not need the post-decision veto.
func evaluateCloseEligibilityWithMode(
	issue *models.Issue, sessionID string,
	wasInvolved, wasImplementationInvolved, hasImplementationHistory bool,
	mode reviewpolicy.Mode, hasActiveApproval bool, selfReview bool,
) closeEligibility {
	in := reviewpolicy.CloseEligibilityInput{
		Mode:                      mode,
		Issue:                     issue,
		SessionID:                 sessionID,
		SessionIsImplementer:      issue != nil && issue.ImplementerSession != "" && issue.ImplementerSession == sessionID,
		SessionIsCreator:          issue != nil && issue.CreatorSession != "" && issue.CreatorSession == sessionID,
		SessionIsReviewerOfRecord: issue != nil && issue.ReviewerSession != "" && issue.ReviewerSession == sessionID,
		SessionIsReviewRequester:  issue != nil && issue.ReviewRequestedBySession != "" && issue.ReviewRequestedBySession == sessionID,
		HasImplementationHistory:  wasImplementationInvolved,
		WasAnyInvolved:            wasInvolved,
		HasActiveApproval:         hasActiveApproval,
		SelfReviewAcknowledged:    selfReview,
	}

	decision := reviewpolicy.EvaluateCloseEligibility(in)

	// Post-check: under strict/balanced the old cmd layer had a stricter
	// creator-open-bypass gate than reviewpolicy — it disabled the bypass
	// whenever the ISSUE (not just the caller) had any implementation
	// history. reviewpolicy only looks at the caller's flag, so we veto
	// the bypass here when the issue-wide hasImplementationHistory flag
	// is set. Delegated mode owns the full rule in reviewpolicy and does
	// not need the post-decision veto.
	if mode != reviewpolicy.ModeDelegated &&
		decision.Allowed && decision.CreatorOpenBypass && hasImplementationHistory {
		// Creator trying to close an open issue with impl history — reject
		// with the legacy message.
		if issue != nil {
			return closeEligibility{
				Allowed:          false,
				RejectionMessage: fmt.Sprintf("cannot close: %s has implementation history and requires review", issue.ID),
			}
		}
	}

	if decision.Allowed {
		return closeEligibility{
			Allowed:           true,
			CreatorOpenBypass: decision.CreatorOpenBypass,
			RequiresReason:    decision.RequiresReason,
		}
	}

	// Preserve legacy rejection messages verbatim.
	if issue == nil {
		return closeEligibility{
			Allowed:          false,
			RejectionMessage: "cannot close: issue not found",
		}
	}
	if in.SessionIsImplementer || wasImplementationInvolved {
		return closeEligibility{
			Allowed:          false,
			RejectionMessage: fmt.Sprintf("cannot close own implementation: %s", issue.ID),
		}
	}
	if in.SessionIsCreator {
		if hasImplementationHistory {
			return closeEligibility{
				Allowed:          false,
				RejectionMessage: fmt.Sprintf("cannot close: %s has implementation history and requires review", issue.ID),
			}
		}
		return closeEligibility{
			Allowed:          false,
			RejectionMessage: fmt.Sprintf("cannot close: you created %s and it requires review", issue.ID),
		}
	}
	if wasInvolved {
		return closeEligibility{
			Allowed:          false,
			RejectionMessage: fmt.Sprintf("cannot close: you previously worked on %s", issue.ID),
		}
	}
	return closeEligibility{
		Allowed:          false,
		RejectionMessage: decision.RejectionMessage,
	}
}
