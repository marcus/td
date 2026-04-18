package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
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
			name:  "closed issue points to show",
			issue: &models.Issue{ID: "td-closed", Status: models.StatusClosed},
			want:  "  Already closed: td show td-closed",
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
			name:  "closed issue points to show",
			issue: &models.Issue{ID: "td-closed", Status: models.StatusClosed},
			want:  "  Already closed: td show td-closed",
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
			name:  "closed issue points to show",
			issue: &models.Issue{ID: "td-closed", Status: models.StatusClosed},
			want:  "  Already approved/closed: td show td-closed",
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

func TestRejectFollowupGuidance(t *testing.T) {
	tests := []struct {
		name  string
		issue *models.Issue
		want  string
	}{
		{
			name:  "open issue points to show as reopened",
			issue: &models.Issue{ID: "td-open", Status: models.StatusOpen},
			want:  "  Already reopened: td show td-open",
		},
		{
			name:  "in review issue points to reject",
			issue: &models.Issue{ID: "td-review", Status: models.StatusInReview},
			want:  "  Reject it: td reject td-review",
		},
		{
			name:  "closed issue points to show",
			issue: &models.Issue{ID: "td-closed", Status: models.StatusClosed},
			want:  "  Already closed: td show td-closed",
		},
		{
			name:  "nil issue falls back to reopened wording",
			issue: nil,
			want:  "  Already reopened: td show ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rejectFollowupGuidance(tt.issue)
			if got != tt.want {
				t.Fatalf("rejectFollowupGuidance() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDescribeStaleTransitionUpdate(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Closed elsewhere",
		Status: models.StatusClosed,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	msg := describeStaleTransitionUpdate(
		database,
		"approve",
		issue.ID,
		&db.StaleIssueStatusError{
			IssueID:  issue.ID,
			Expected: models.StatusInReview,
			Actual:   models.StatusClosed,
		},
		approveFollowupGuidance,
	)
	t.Log(msg)

	want := "cannot approve " + issue.ID + ": status changed from in_review to closed in another session\n  Current status: closed\n  Already approved/closed: td show " + issue.ID
	if msg != want {
		t.Fatalf("describeStaleTransitionUpdate() = %q, want %q", msg, want)
	}
}

func TestDescribeStaleTransitionUpdateIncludesRecentContext(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Reopened elsewhere",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if err := database.AddLog(&models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_reviewer",
		Message:   "Reopened",
		Type:      models.LogTypeProgress,
	}); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	msg := describeStaleTransitionUpdate(
		database,
		"reject",
		issue.ID,
		&db.StaleIssueStatusError{
			IssueID:  issue.ID,
			Expected: models.StatusInReview,
			Actual:   models.StatusOpen,
		},
		rejectFollowupGuidance,
	)
	t.Log(msg)

	if !strings.Contains(msg, "Current status: open") {
		t.Fatalf("expected current status context in %q", msg)
	}
	if !strings.Contains(msg, "Recent transition: reopened by ses_reviewer") {
		t.Fatalf("expected recent transition context in %q", msg)
	}
	if !strings.Contains(msg, "Already reopened: td show "+issue.ID) {
		t.Fatalf("expected reopened guidance in %q", msg)
	}
}

func TestDescribeStaleTransitionUpdatePrefersNewestWorkflowContext(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Newest transition wins",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if err := database.AddLog(&models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_impl",
		Message:   "Submitted for review",
		Type:      models.LogTypeProgress,
	}); err != nil {
		t.Fatalf("AddLog submitted failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := database.AddLog(&models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_reviewer",
		Message:   "Rejected",
		Type:      models.LogTypeProgress,
	}); err != nil {
		t.Fatalf("AddLog rejected failed: %v", err)
	}

	msg := describeStaleTransitionUpdate(
		database,
		"reject",
		issue.ID,
		&db.StaleIssueStatusError{
			IssueID:  issue.ID,
			Expected: models.StatusInReview,
			Actual:   models.StatusOpen,
		},
		rejectFollowupGuidance,
	)

	if !strings.Contains(msg, "Recent transition: rejected by ses_reviewer") {
		t.Fatalf("expected newest transition context in %q", msg)
	}
	if strings.Contains(msg, "Recent transition: submitted for review by ses_impl") {
		t.Fatalf("expected stale transition context to be ignored in %q", msg)
	}
}
