package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/models"
)

type approveEligibility struct {
	Allowed          bool
	CreatorException bool
	RequiresReason   bool
	RejectionMessage string
}

type closeEligibility struct {
	Allowed           bool
	CreatorOpenBypass bool
	RejectionMessage  string
}

func balancedReviewPolicyEnabled(baseDir string) bool {
	return features.IsEnabled(baseDir, features.BalancedReviewPolicy.Name)
}

func reviewableByOptions(baseDir, sessionID string) db.ListIssuesOptions {
	return db.ListIssuesOptions{
		ReviewableBy:         sessionID,
		BalancedReviewPolicy: balancedReviewPolicyEnabled(baseDir),
	}
}

func evaluateApproveEligibility(issue *models.Issue, sessionID string, wasInvolved, wasImplementationInvolved, balancedPolicy bool) approveEligibility {
	if issue == nil {
		return approveEligibility{
			Allowed:          false,
			RejectionMessage: "cannot approve: issue not found",
		}
	}

	// Minor tasks intentionally bypass all self-review restrictions.
	if issue.Minor {
		return approveEligibility{Allowed: true}
	}

	isCreator := issue.CreatorSession != "" && issue.CreatorSession == sessionID
	isImplementer := issue.ImplementerSession != "" && issue.ImplementerSession == sessionID

	if !balancedPolicy {
		if wasInvolved || isCreator || isImplementer {
			return approveEligibility{
				Allowed:          false,
				RejectionMessage: fmt.Sprintf("cannot approve: you were involved with %s (created, started, or previously worked on)", issue.ID),
			}
		}
		return approveEligibility{Allowed: true}
	}

	// Balanced policy still hard-blocks implementation self-approval.
	if isImplementer || wasImplementationInvolved {
		return approveEligibility{
			Allowed:          false,
			RejectionMessage: fmt.Sprintf("cannot approve: you were involved with implementation of %s", issue.ID),
		}
	}

	// Creator-only exception: creator can approve if a different session implemented.
	hasDifferentImplementer := issue.ImplementerSession != "" && issue.ImplementerSession != sessionID
	if isCreator && hasDifferentImplementer {
		return approveEligibility{
			Allowed:          true,
			CreatorException: true,
			RequiresReason:   true,
		}
	}

	// Non-creator sessions still require zero prior involvement.
	if wasInvolved {
		return approveEligibility{
			Allowed:          false,
			RejectionMessage: fmt.Sprintf("cannot approve: you were involved with %s (created, started, or previously worked on)", issue.ID),
		}
	}

	return approveEligibility{Allowed: true}
}

func evaluateCloseEligibility(issue *models.Issue, sessionID string, wasInvolved, wasImplementationInvolved, hasImplementationHistory bool) closeEligibility {
	if issue == nil {
		return closeEligibility{
			Allowed:          false,
			RejectionMessage: "cannot close: issue not found",
		}
	}

	// Minor tasks intentionally bypass self-close restrictions.
	if issue.Minor {
		return closeEligibility{Allowed: true}
	}

	isCreator := issue.CreatorSession != "" && issue.CreatorSession == sessionID
	isImplementer := issue.ImplementerSession != "" && issue.ImplementerSession == sessionID

	// Narrow bypass for self-created throwaway tasks:
	// only creator-owned issues that are still open and have never entered
	// implementation flow by any session.
	if isCreator && issue.Status == models.StatusOpen && !hasImplementationHistory && !wasImplementationInvolved {
		return closeEligibility{
			Allowed:           true,
			CreatorOpenBypass: true,
		}
	}

	// Once the current session has any implementation history, direct close is blocked.
	if isImplementer || wasImplementationInvolved {
		return closeEligibility{
			Allowed:          false,
			RejectionMessage: fmt.Sprintf("cannot close own implementation: %s", issue.ID),
		}
	}

	if isCreator {
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

	return closeEligibility{Allowed: true}
}
