package cmd

import (
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/models"
)

func TestEvaluateApproveEligibility(t *testing.T) {
	issue := &models.Issue{
		ID:                 "td-test",
		CreatorSession:     "ses_creator",
		ImplementerSession: "ses_impl",
		Status:             models.StatusInReview,
	}

	tests := []struct {
		name                      string
		sessionID                 string
		wasInvolved               bool
		wasImplementationInvolved bool
		balanced                  bool
		minor                     bool
		noImplementer             bool
		wantAllowed               bool
		wantCreatorException      bool
		wantRequiresReason        bool
	}{
		{
			name:                      "strict blocks creator-only approval",
			sessionID:                 "ses_creator",
			wasInvolved:               true,
			wasImplementationInvolved: false,
			balanced:                  false,
			wantAllowed:               false,
		},
		{
			name:                      "balanced allows creator-only approval",
			sessionID:                 "ses_creator",
			wasInvolved:               true,
			wasImplementationInvolved: false,
			balanced:                  true,
			wantAllowed:               true,
			wantCreatorException:      true,
			wantRequiresReason:        true,
		},
		{
			name:                      "balanced blocks creator who implemented",
			sessionID:                 "ses_creator",
			wasInvolved:               true,
			wasImplementationInvolved: true,
			balanced:                  true,
			wantAllowed:               false,
		},
		{
			name:                      "balanced blocks implementer",
			sessionID:                 "ses_impl",
			wasInvolved:               true,
			wasImplementationInvolved: true,
			balanced:                  true,
			wantAllowed:               false,
		},
		{
			name:                      "balanced allows unrelated reviewer",
			sessionID:                 "ses_reviewer",
			wasInvolved:               false,
			wasImplementationInvolved: false,
			balanced:                  true,
			wantAllowed:               true,
		},
		{
			name:                      "balanced blocks involved non-creator",
			sessionID:                 "ses_reviewer",
			wasInvolved:               true,
			wasImplementationInvolved: false,
			balanced:                  true,
			wantAllowed:               false,
		},
		{
			name:                      "minor always allowed",
			sessionID:                 "ses_impl",
			wasInvolved:               true,
			wasImplementationInvolved: true,
			balanced:                  false,
			minor:                     true,
			wantAllowed:               true,
		},
		{
			name:                      "balanced blocks creator when no implementer set",
			sessionID:                 "ses_creator",
			wasInvolved:               true,
			wasImplementationInvolved: false,
			balanced:                  true,
			noImplementer:             true,
			wantAllowed:               false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := *issue
			i.Minor = tt.minor
			if tt.noImplementer {
				i.ImplementerSession = ""
			}
			got := evaluateApproveEligibility(&i, tt.sessionID, tt.wasInvolved, tt.wasImplementationInvolved, tt.balanced)
			if got.Allowed != tt.wantAllowed {
				t.Fatalf("Allowed=%v, want %v", got.Allowed, tt.wantAllowed)
			}
			if got.CreatorException != tt.wantCreatorException {
				t.Fatalf("CreatorException=%v, want %v", got.CreatorException, tt.wantCreatorException)
			}
			if got.RequiresReason != tt.wantRequiresReason {
				t.Fatalf("RequiresReason=%v, want %v", got.RequiresReason, tt.wantRequiresReason)
			}
		})
	}
}

func TestReviewableByOptions_UsesBalancedReviewPolicyFlag(t *testing.T) {
	baseDir := t.TempDir()
	sessionID := "ses_test"

	// Default is ON.
	opts := reviewableByOptions(baseDir, sessionID)
	if !opts.BalancedReviewPolicy {
		t.Fatalf("BalancedReviewPolicy should default to true")
	}

	// Local config override OFF.
	if err := config.SetFeatureFlag(baseDir, features.BalancedReviewPolicy.Name, false); err != nil {
		t.Fatalf("SetFeatureFlag failed: %v", err)
	}
	opts = reviewableByOptions(baseDir, sessionID)
	if opts.BalancedReviewPolicy {
		t.Fatalf("BalancedReviewPolicy should be false when overridden in config")
	}

	// Env override ON should win over config OFF.
	t.Setenv("TD_FEATURE_BALANCED_REVIEW_POLICY", "true")
	opts = reviewableByOptions(baseDir, sessionID)
	if !opts.BalancedReviewPolicy {
		t.Fatalf("BalancedReviewPolicy should be true when env override is set")
	}
}

func TestEvaluateCloseEligibility(t *testing.T) {
	issue := &models.Issue{
		ID:                 "td-close",
		CreatorSession:     "ses_creator",
		ImplementerSession: "ses_impl",
		Status:             models.StatusInReview,
	}

	tests := []struct {
		name                      string
		sessionID                 string
		status                    models.Status
		implementer               string
		setImplementer            bool
		wasInvolved               bool
		wasImplementationInvolved bool
		hasImplementationHistory  bool
		minor                     bool
		wantAllowed               bool
		wantCreatorOpenBypass     bool
		wantMessage               string
	}{
		{
			name:                      "minor always allowed",
			sessionID:                 "ses_impl",
			wasInvolved:               true,
			wasImplementationInvolved: true,
			hasImplementationHistory:  true,
			minor:                     true,
			wantAllowed:               true,
		},
		{
			name:                      "creator can close own open throwaway issue",
			sessionID:                 "ses_creator",
			status:                    models.StatusOpen,
			implementer:               "",
			setImplementer:            true,
			wasInvolved:               true,
			wasImplementationInvolved: false,
			hasImplementationHistory:  false,
			wantAllowed:               true,
			wantCreatorOpenBypass:     true,
		},
		{
			name:                      "creator cannot close once issue has implementation history",
			sessionID:                 "ses_creator",
			status:                    models.StatusOpen,
			implementer:               "",
			setImplementer:            true,
			wasInvolved:               true,
			wasImplementationInvolved: false,
			hasImplementationHistory:  true,
			wantMessage:               "cannot close: td-close has implementation history and requires review",
		},
		{
			name:                      "implementer cannot close own work",
			sessionID:                 "ses_impl",
			wasInvolved:               true,
			wasImplementationInvolved: true,
			hasImplementationHistory:  true,
			wantMessage:               "cannot close own implementation: td-close",
		},
		{
			name:                      "previously involved non-implementer cannot close",
			sessionID:                 "ses_helper",
			implementer:               "",
			setImplementer:            true,
			wasInvolved:               true,
			wasImplementationInvolved: false,
			hasImplementationHistory:  false,
			wantMessage:               "cannot close: you previously worked on td-close",
		},
		{
			name:                      "uninvolved session can close",
			sessionID:                 "ses_other",
			wasInvolved:               false,
			wasImplementationInvolved: false,
			hasImplementationHistory:  true,
			wantAllowed:               true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := *issue
			if tt.status != "" {
				i.Status = tt.status
			}
			i.Minor = tt.minor
			if tt.setImplementer {
				i.ImplementerSession = tt.implementer
			}

			got := evaluateCloseEligibility(&i, tt.sessionID, tt.wasInvolved, tt.wasImplementationInvolved, tt.hasImplementationHistory)
			if got.Allowed != tt.wantAllowed {
				t.Fatalf("Allowed=%v, want %v", got.Allowed, tt.wantAllowed)
			}
			if got.CreatorOpenBypass != tt.wantCreatorOpenBypass {
				t.Fatalf("CreatorOpenBypass=%v, want %v", got.CreatorOpenBypass, tt.wantCreatorOpenBypass)
			}
			if got.RejectionMessage != tt.wantMessage {
				t.Fatalf("RejectionMessage=%q, want %q", got.RejectionMessage, tt.wantMessage)
			}
		})
	}
}

func TestCloseFollowupGuidance(t *testing.T) {
	tests := []struct {
		name  string
		issue *models.Issue
		want  string
	}{
		{
			name:  "open issue points to review",
			issue: &models.Issue{ID: "td-open", Status: models.StatusOpen},
			want:  "  Submit for review: td review td-open",
		},
		{
			name:  "in progress issue points to review",
			issue: &models.Issue{ID: "td-progress", Status: models.StatusInProgress},
			want:  "  Submit for review: td review td-progress",
		},
		{
			name:  "in review issue points to approve",
			issue: &models.Issue{ID: "td-review", Status: models.StatusInReview},
			want:  "  Already in review: td approve td-review",
		},
		{
			name:  "nil issue falls back to review wording",
			issue: nil,
			want:  "  Submit for review: td review ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := closeFollowupGuidance(tt.issue)
			if got != tt.want {
				t.Fatalf("closeFollowupGuidance() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReviewFollowupGuidance(t *testing.T) {
	tests := []struct {
		name  string
		issue *models.Issue
		want  string
	}{
		{
			name:  "open issue points to review",
			issue: &models.Issue{ID: "td-open", Status: models.StatusOpen},
			want:  "  Submit for review: td review td-open",
		},
		{
			name:  "in progress issue points to review",
			issue: &models.Issue{ID: "td-progress", Status: models.StatusInProgress},
			want:  "  Submit for review: td review td-progress",
		},
		{
			name:  "in review issue points to approve",
			issue: &models.Issue{ID: "td-review", Status: models.StatusInReview},
			want:  "  Already in review: td approve td-review",
		},
		{
			name:  "nil issue falls back to review wording",
			issue: nil,
			want:  "  Submit for review: td review ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reviewFollowupGuidance(tt.issue)
			if got != tt.want {
				t.Fatalf("reviewFollowupGuidance() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApproveFollowupGuidance(t *testing.T) {
	tests := []struct {
		name  string
		issue *models.Issue
		want  string
	}{
		{
			name:  "in review issue points to approve",
			issue: &models.Issue{ID: "td-review", Status: models.StatusInReview},
			want:  "  Approve it: td approve td-review",
		},
		{
			name:  "open issue points to review first",
			issue: &models.Issue{ID: "td-open", Status: models.StatusOpen},
			want:  "  Submit for review first: td review td-open",
		},
		{
			name:  "nil issue falls back to review wording",
			issue: nil,
			want:  "  Submit for review first: td review ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := approveFollowupGuidance(tt.issue)
			if got != tt.want {
				t.Fatalf("approveFollowupGuidance() = %q, want %q", got, tt.want)
			}
		})
	}
}
