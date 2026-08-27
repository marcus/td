package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
)

// approveWithAttribution runs approve as the implementer session with the given
// flags and returns the output plus the resulting active approval row (nil if
// none was written).
func approveWithAttribution(t *testing.T, database *db.DB, issueID string, flags map[string]string) (string, *models.IssueReview, error) {
	t.Helper()
	out, err := runApproveCmd(t, []string{issueID}, flags)
	active, _ := database.GetActiveApprovalReview(issueID)
	return out, active, err
}

// TestApproveReviewedBy_ImplementerApprovesWithAttribution is the headline case
// of the whole epic: an orchestrator that implemented the work records the
// review its sub-agent performed, without claiming a self-review it did not do.
//
// The row must be true about both facts — self_review marks that an involved
// session wrote it, reviewed_by credits the actual reviewer.
func TestApproveReviewedBy_ImplementerApprovesWithAttribution(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	out, active, err := approveWithAttribution(t, database, issue.ID, map[string]string{
		"reviewed-by": "code-reviewer sub-agent",
	})
	if err != nil {
		t.Fatalf("approve --reviewed-by: %v\n%s", err, out)
	}
	if !strings.Contains(out, "reviewed by code-reviewer sub-agent") {
		t.Errorf("output should name the reviewer: %q", out)
	}
	if !strings.Contains(out, "recorded by "+implID) {
		t.Errorf("output should name the recording session: %q", out)
	}

	got, _ := database.GetIssue(issue.ID)
	if got.Status != models.StatusClosed {
		t.Fatalf("status = %s, want closed", got.Status)
	}
	if active == nil {
		t.Fatal("expected an approval review row")
	}
	if active.ReviewedBy != "code-reviewer sub-agent" {
		t.Errorf("ReviewedBy = %q, want the attribution", active.ReviewedBy)
	}
	if !active.SelfReview {
		t.Error("row was written by an involved session; self_review must be set for audit")
	}
	if active.ReviewerSession != implID {
		t.Errorf("ReviewerSession = %q, want the recording session %q", active.ReviewerSession, implID)
	}
}

// TestApproveReviewedBy_NoReasonRequired pins the decision that --reviewed-by
// does not require --reason. This is the default path for orchestrated work;
// forcing a second string here is friction on the common case, and the
// attribution is already the substance of the claim.
func TestApproveReviewedBy_NoReasonRequired(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	out, active, err := approveWithAttribution(t, database, issue.ID, map[string]string{
		"reviewed-by": "reviewer-2",
	})
	if err != nil {
		t.Fatalf("attributed approve without --reason should succeed: %v\n%s", err, out)
	}
	if active == nil {
		t.Fatal("expected an approval review row")
	}

	// The contrast: an unattributed self-review still demands a reason,
	// because nothing else vouches for it.
	issue2 := newInReviewIssueWithImpl(t, database, implID)
	out, active2, _ := approveWithAttribution(t, database, issue2.ID, map[string]string{
		"self-review": "true",
	})
	if !strings.Contains(out, "requires --reason") {
		t.Errorf("--self-review without --reason should be rejected, got %q", out)
	}
	if active2 != nil {
		t.Error("no review row should be written when the reason gate rejects")
	}
}

// TestApproveReviewedBy_MutuallyExclusiveWithSelfReview asserts the edge
// rejects the combination. It matters that this is caught at the CLI rather
// than left to the policy layer: the policy checks attribution first, so the
// pair would silently drop the --reason requirement and record an attributed
// row for a caller who also claimed a self-review.
func TestApproveReviewedBy_MutuallyExclusiveWithSelfReview(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	out, active, runErr := approveWithAttribution(t, database, issue.ID, map[string]string{
		"reviewed-by": "sub-agent",
		"self-review": "true",
		"reason":      "both flags",
	})
	if runErr == nil {
		t.Fatal("expected an error when both flags are passed")
	}
	if !strings.Contains(out, "mutually exclusive") {
		t.Errorf("expected a mutual-exclusion message, got %q", out)
	}
	if active != nil {
		t.Error("no review row should be written when validation rejects")
	}
	if got, _ := database.GetIssue(issue.ID); got.Status != models.StatusInReview {
		t.Errorf("status = %s, want in_review (nothing should have happened)", got.Status)
	}
}

// TestApproveReviewedBy_RejectsBlankAndOverlong covers the two ways an
// attribution can be worthless: naming nobody, and being used as a dumping
// ground for the review itself.
func TestApproveReviewedBy_RejectsBlankAndOverlong(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)

	cases := []struct {
		name    string
		value   string
		wantMsg string
	}{
		{"empty", "", "requires a name"},
		{"whitespace only", "   ", "requires a name"},
		{"over the length cap", strings.Repeat("x", reviewpolicy.MaxReviewedByLen+1), "limited to"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			issue := newInReviewIssueWithImpl(t, database, implID)
			out, active, runErr := approveWithAttribution(t, database, issue.ID, map[string]string{
				"reviewed-by": tc.value,
			})
			if runErr == nil {
				t.Fatalf("expected an error for %s, got output %q", tc.name, out)
			}
			if !strings.Contains(out, tc.wantMsg) {
				t.Errorf("expected %q in output, got %q", tc.wantMsg, out)
			}
			if active != nil {
				t.Error("no review row should be written when validation rejects")
			}
		})
	}

	// Exactly at the cap is accepted — the boundary belongs to the valid side.
	issue := newInReviewIssueWithImpl(t, database, implID)
	atCap := strings.Repeat("x", reviewpolicy.MaxReviewedByLen)
	if _, active, err := approveWithAttribution(t, database, issue.ID, map[string]string{
		"reviewed-by": atCap,
	}); err != nil {
		t.Errorf("attribution at exactly the cap should be accepted: %v", err)
	} else if active == nil || active.ReviewedBy != atCap {
		t.Errorf("attribution at the cap not persisted: %+v", active)
	}
}

// TestApproveReviewedBy_TrimsSurroundingWhitespace keeps the stored attribution
// clean, so `td show` and the monitor do not render ragged names.
func TestApproveReviewedBy_TrimsSurroundingWhitespace(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	_, active, err := approveWithAttribution(t, database, issue.ID, map[string]string{
		"reviewed-by": "  spaced reviewer  ",
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if active == nil || active.ReviewedBy != "spaced reviewer" {
		t.Errorf("ReviewedBy = %+v, want the trimmed name", active)
	}
}

// TestApproveReviewedBy_DoesNotGrantInDelegated is the security property at the
// CLI boundary: attribution is an audit record, never a permission. A project
// that pinned delegated mode did so for a mechanical independence boundary, and
// a free-text string must not dissolve it.
func TestApproveReviewedBy_DoesNotGrantInDelegated(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	out, active, _ := approveWithAttribution(t, database, issue.ID, map[string]string{
		"reviewed-by": "sub-agent that definitely reviewed it",
	})
	if active != nil {
		t.Fatal("delegated mode must not let an involved session approve, attribution or not")
	}
	if got, _ := database.GetIssue(issue.ID); got.Status != models.StatusInReview {
		t.Errorf("status = %s, want in_review", got.Status)
	}
	if !strings.Contains(out, "cannot approve") {
		t.Errorf("expected a rejection, got %q", out)
	}
}

// TestApproveReviewedBy_IndependentSessionMayCredit covers an independent
// reviewer crediting someone else — allowed, and never mistaken for a
// self-review since no involved session recorded it.
func TestApproveReviewedBy_IndependentSessionMayCredit(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	out, active, err := approveWithAttribution(t, database, issue.ID, map[string]string{
		"reviewed-by": "a human on the team",
	})
	if err != nil {
		t.Fatalf("independent session with attribution: %v\n%s", err, out)
	}
	if active == nil {
		t.Fatal("expected an approval review row")
	}
	if active.ReviewedBy != "a human on the team" {
		t.Errorf("ReviewedBy = %q, want the credit", active.ReviewedBy)
	}
	if active.SelfReview {
		t.Error("an independent session's row must not be stamped self_review")
	}
}

// TestApproveReviewedBy_RecordOnly composes attribution with --record-only:
// a sub-agent sharing the orchestrator's session can attest without closing.
func TestApproveReviewedBy_RecordOnly(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	out, active, err := approveWithAttribution(t, database, issue.ID, map[string]string{
		"record-only": "true",
		"reviewed-by": "code-reviewer sub-agent",
		"reason":      "reviewed the diff",
	})
	if err != nil {
		t.Fatalf("record-only with attribution: %v\n%s", err, out)
	}
	if !strings.Contains(out, "reviewed by code-reviewer sub-agent") {
		t.Errorf("output should name the reviewer: %q", out)
	}
	if active == nil || active.ReviewedBy != "code-reviewer sub-agent" {
		t.Fatalf("expected an attributed review row, got %+v", active)
	}
	if got, _ := database.GetIssue(issue.ID); got.Status != models.StatusInReview {
		t.Errorf("status = %s, want in_review (record-only must not close)", got.Status)
	}
}

// TestApproveReviewedBy_AttributedApprovalIsNotLoggedAsSecurity pins the audit
// behavior decided for this epic. An attributed approval is the normal
// orchestrated path, so it gets a plain progress log naming the reviewer —
// tagging every one of them SECURITY would drown the signal that tag exists
// for. A genuine self-review still gets the security treatment.
func TestApproveReviewedBy_AttributedApprovalIsNotLoggedAsSecurity(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)

	attributed := newInReviewIssueWithImpl(t, database, implID)
	if _, _, err := approveWithAttribution(t, database, attributed.ID, map[string]string{
		"reviewed-by": "code-reviewer sub-agent",
	}); err != nil {
		t.Fatalf("attributed approve: %v", err)
	}

	logs, err := database.GetLogs(attributed.ID, 0)
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	var sawAttribution bool
	for _, l := range logs {
		if l.Type == models.LogTypeSecurity {
			t.Errorf("attributed approval must not write a security-typed log: %q", l.Message)
		}
		if strings.Contains(l.Message, "reviewed by code-reviewer sub-agent") {
			sawAttribution = true
		}
	}
	if !sawAttribution {
		t.Error("expected a progress log naming the reviewer")
	}

	// The contrast: a genuine self-review still gets the security treatment.
	selfReviewed := newInReviewIssueWithImpl(t, database, implID)
	if _, _, err := approveWithAttribution(t, database, selfReviewed.ID, map[string]string{
		"self-review": "true",
		"reason":      "reviewed my own diff",
	}); err != nil {
		t.Fatalf("self-review approve: %v", err)
	}
	logs, _ = database.GetLogs(selfReviewed.ID, 0)
	var sawSecurity bool
	for _, l := range logs {
		if l.Type == models.LogTypeSecurity && strings.Contains(l.Message, "SELF-REVIEW") {
			sawSecurity = true
		}
	}
	if !sawSecurity {
		t.Error("a genuine self-review should still be logged as a security event")
	}
}

// TestApproveReviewedBy_TeachingMessageLeadsWithAttribution checks the message
// an agent actually hits when it is blocked. Order matters: an agent that reads
// only the start of the rejection should reach for attribution rather than
// claiming a self-review it did not perform.
func TestApproveReviewedBy_TeachingMessageLeadsWithAttribution(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	out, active, _ := approveWithAttribution(t, database, issue.ID, nil)
	if active != nil {
		t.Fatal("implementer with no attestation must not approve")
	}
	if !strings.Contains(out, "--reviewed-by") {
		t.Errorf("teaching message must offer --reviewed-by: %q", out)
	}
	if strings.Index(out, "--reviewed-by") > strings.Index(out, "--self-review") {
		t.Errorf("--reviewed-by must be offered before --self-review: %q", out)
	}
}

// TestApproveReviewedBy_JSONCarriesAttribution keeps the structured surface at
// parity with the human one — an agent parsing JSON must be able to see who the
// review was credited to without re-reading the issue.
func TestApproveReviewedBy_JSONCarriesAttribution(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	out, _, err := approveWithAttribution(t, database, issue.ID, map[string]string{
		"reviewed-by": "code-reviewer sub-agent",
		"json":        "true",
	})
	if err != nil {
		t.Fatalf("approve --json: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"reviewed_by"`) || !strings.Contains(out, "code-reviewer sub-agent") {
		t.Errorf("JSON output missing reviewed_by: %s", out)
	}
	if !strings.Contains(out, `"self_review"`) {
		t.Errorf("JSON output should also expose self_review so consumers can tell the row was recorded by an involved session: %s", out)
	}
}

// TestApproveReviewedBy_MinorIssuesAcceptAttribution — minor tasks bypass review
// in every mode, but a caller may still record who looked at one.
func TestApproveReviewedBy_MinorIssuesAcceptAttribution(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)
	issue.Minor = true
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	_, active, err := approveWithAttribution(t, database, issue.ID, map[string]string{
		"reviewed-by": "quick check by reviewer-1",
	})
	if err != nil {
		t.Fatalf("minor issue with attribution: %v", err)
	}
	if active == nil || active.ReviewedBy != "quick check by reviewer-1" {
		t.Errorf("attribution should be recorded on minor issues too: %+v", active)
	}
	if active.Decision != reviewpolicy.DecisionApproved {
		t.Errorf("decision = %s, want approved", active.Decision)
	}
}

// readSecurityEvents returns the raw contents of the project's
// security_events.jsonl, or "" when the file was never created.
func readSecurityEvents(t *testing.T, baseDir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(baseDir, ".todos", "security_events.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read security_events.jsonl: %v", err)
	}
	return string(b)
}

// TestApproveReviewedBy_WritesSecurityEventsEntry locks the compensating
// control for this epic's audit decision.
//
// Attributed approvals deliberately do NOT get a LogTypeSecurity issue log —
// they are the normal orchestrated path and tagging every one SECURITY would
// drown the signal. The .todos/security_events.jsonl entry is what remains: the
// only out-of-band record that an implementation-involved session approved the
// work. An independent review of td-9562f5 found that deleting the write left
// the whole suite green, which would let the follow-up story that rewrites
// audit behavior (td-ec8445) remove it silently.
func TestApproveReviewedBy_WritesSecurityEventsEntry(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	if _, _, err := approveWithAttribution(t, database, issue.ID, map[string]string{
		"reviewed-by": "code-reviewer sub-agent",
	}); err != nil {
		t.Fatalf("attributed approve: %v", err)
	}

	events := readSecurityEvents(t, dir)
	if events == "" {
		t.Fatal("attributed approval must still write a security_events.jsonl entry — it is the only out-of-band record that an involved session approved")
	}
	if !strings.Contains(events, issue.ID) {
		t.Errorf("security event should name the issue: %s", events)
	}
	if !strings.Contains(events, "attributed_review") {
		t.Errorf("security event should be classifiable as an attributed review: %s", events)
	}
	if !strings.Contains(events, "code-reviewer sub-agent") {
		t.Errorf("security event should name the credited reviewer: %s", events)
	}
	if !strings.Contains(events, implID) {
		t.Errorf("security event should name the recording session: %s", events)
	}
}

// TestApproveReviewedBy_IndependentApprovalWritesNoSecurityEvent is the
// counterpart: an independent session approving is unremarkable and must not
// pollute the audit file. Without this, the assertion above would pass just as
// well if td wrote a security event for every approval.
func TestApproveReviewedBy_IndependentApprovalWritesNoSecurityEvent(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	if _, _, err := approveWithAttribution(t, database, issue.ID, map[string]string{
		"reviewed-by": "a human on the team",
	}); err != nil {
		t.Fatalf("independent approve: %v", err)
	}

	if events := readSecurityEvents(t, dir); events != "" {
		t.Errorf("an independent session's approval must not write a security event: %s", events)
	}
}

// TestApproveReviewedBy_RecordOnlyLogNamesReviewer covers the record-only log
// message, which a mutation showed was unasserted. The issue log is where a
// human scanning `td show` learns who reviewed the work.
func TestApproveReviewedBy_RecordOnlyLogNamesReviewer(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	if _, _, err := approveWithAttribution(t, database, issue.ID, map[string]string{
		"record-only": "true",
		"reviewed-by": "code-reviewer sub-agent",
		"reason":      "reviewed the diff",
	}); err != nil {
		t.Fatalf("record-only with attribution: %v", err)
	}

	logs, err := database.GetLogs(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	var found bool
	for _, l := range logs {
		if strings.Contains(l.Message, "by code-reviewer sub-agent") {
			found = true
		}
	}
	if !found {
		t.Error("record-only log entry should name the credited reviewer")
	}
}

// TestApproveReviewedBy_ModeCRejectsAttribution pins that attribution is not
// silently swallowed when closing on an approval that already exists. No new
// review row is written on that path, so accepting the flag would let a caller
// believe it credited a reviewer when nothing was recorded.
func TestApproveReviewedBy_ModeCRejectsAttribution(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	// An independent session records the approval first.
	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	if _, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "reviewed the diff",
	}); err != nil {
		t.Fatalf("record-only: %v", err)
	}

	// The implementer now closes on it — and passing an attribution here is
	// meaningless, so it must be called out rather than dropped.
	t.Setenv("TD_SESSION_ID", "impl-agent")
	out, _ := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"reviewed-by": "someone else entirely",
		"reason":      "landing it",
	})
	if !strings.Contains(out, "ignored") {
		t.Errorf("expected --reviewed-by to be called out as ignored on the close path, got %q", out)
	}
	if got, _ := database.GetIssue(issue.ID); got.Status != models.StatusInReview {
		t.Errorf("status = %s, want in_review (the close should not have proceeded)", got.Status)
	}

	// Without the flag the same close succeeds.
	if _, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"reason": "landing it",
	}); err != nil {
		t.Fatalf("close on recorded approval: %v", err)
	}
	if got, _ := database.GetIssue(issue.ID); got.Status != models.StatusClosed {
		t.Errorf("status = %s, want closed", got.Status)
	}
	active, _ := database.GetActiveApprovalReview(issue.ID)
	if active == nil || active.ReviewedBy != "" {
		t.Errorf("the recorded approval must be untouched by the close: %+v", active)
	}
}

// TestApproveReviewedBy_RejectsControlCharacters closes a forgery vector: a
// newline in the attribution renders as extra entries in `td show`'s session
// log, so a name can fabricate convincing history. The cap on this field is
// justified by output readability, and stopping at length while allowing
// newlines would be half a check.
func TestApproveReviewedBy_RejectsControlCharacters(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)

	for _, bad := range []string{
		"reviewer\nApproved by: someone-else",
		"reviewer\roverwrite",
		"reviewer\x00null",
	} {
		issue := newInReviewIssueWithImpl(t, database, implID)
		out, active, runErr := approveWithAttribution(t, database, issue.ID, map[string]string{
			"reviewed-by": bad,
		})
		if runErr == nil {
			t.Errorf("expected rejection for %q, got output %q", bad, out)
		}
		if active != nil {
			t.Errorf("no review row should be written for %q", bad)
		}
	}
}

// TestApproveReviewedBy_LengthCapCountsRunes — the cap is advertised in
// characters, so a name in a non-Latin script must not be rejected for a byte
// count the caller cannot see.
func TestApproveReviewedBy_LengthCapCountsRunes(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	// 100 three-byte runes: 300 bytes, well under the 120-character cap.
	name := strings.Repeat("レ", 100)
	out, active, runErr := approveWithAttribution(t, database, issue.ID, map[string]string{
		"reviewed-by": name,
	})
	if runErr != nil {
		t.Fatalf("a 100-character name must be accepted regardless of byte length: %v\n%s", runErr, out)
	}
	if active == nil || active.ReviewedBy != name {
		t.Errorf("multi-byte attribution not persisted: %+v", active)
	}
}

// TestApproveReviewedBy_RecordOnlyIsAudited closes an audit hole that opening
// --record-only to trusted made reachable: an implementation-involved session
// could record an approval and then close it via Mode C, producing no
// security_events.jsonl entry at all — `td security` would report no exceptions
// for a review nobody independent ever performed.
//
// The rule under test is about WHO RECORDED the row, not which flag was used.
func TestApproveReviewedBy_RecordOnlyIsAudited(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)

	// Involved recorder, attributed: audited.
	attributed := newInReviewIssueWithImpl(t, database, implID)
	if _, _, err := approveWithAttribution(t, database, attributed.ID, map[string]string{
		"record-only": "true",
		"reviewed-by": "code-reviewer sub-agent",
		"reason":      "reviewed the diff",
	}); err != nil {
		t.Fatalf("record-only attributed: %v", err)
	}
	events := readSecurityEvents(t, dir)
	if !strings.Contains(events, "record_only") || !strings.Contains(events, "code-reviewer sub-agent") {
		t.Errorf("record-only by an involved session must be audited, got: %s", events)
	}

	// Involved recorder, self-review: also audited.
	selfReviewed := newInReviewIssueWithImpl(t, database, implID)
	if _, _, err := approveWithAttribution(t, database, selfReviewed.ID, map[string]string{
		"record-only": "true",
		"self-review": "true",
		"reason":      "reviewed my own diff",
	}); err != nil {
		t.Fatalf("record-only self-review: %v", err)
	}
	if events := readSecurityEvents(t, dir); !strings.Contains(events, "self_review") {
		t.Errorf("record-only self-review must be audited, got: %s", events)
	}
}

// TestApproveReviewedBy_IndependentRecordOnlyNotAudited is the counterpart —
// without it the assertion above would pass just as well if td audited every
// recorded review, which would make the file useless for its purpose.
func TestApproveReviewedBy_IndependentRecordOnlyNotAudited(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	if _, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "reviewed the diff",
	}); err != nil {
		t.Fatalf("independent record-only: %v", err)
	}
	if events := readSecurityEvents(t, dir); events != "" {
		t.Errorf("an independent session's recorded review must not be audited: %s", events)
	}
}
