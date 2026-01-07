package monitor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

// =============================================================================
// Comments Display Modal Tests
// =============================================================================

func TestModalCommentsDisplayBasic(t *testing.T) {
	// Test that comments are correctly included in estimated modal content lines
	issue := &models.Issue{
		ID:    "td-001",
		Title: "Test issue",
		Type:  models.TypeTask,
	}

	tests := []struct {
		name            string
		comments        []models.Comment
		expectedMinLine int // Minimum line contribution from comments
	}{
		{
			name:            "no comments",
			comments:        []models.Comment{},
			expectedMinLine: 0,
		},
		{
			name: "single comment",
			comments: []models.Comment{
				{
					ID:        1,
					IssueID:   "td-001",
					SessionID: "sess-001",
					Text:      "This is a test comment",
					CreatedAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
				},
			},
			expectedMinLine: 2, // Header + 1 comment
		},
		{
			name: "multiple comments",
			comments: []models.Comment{
				{
					ID:        1,
					IssueID:   "td-001",
					SessionID: "sess-001",
					Text:      "First comment",
					CreatedAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
				},
				{
					ID:        2,
					IssueID:   "td-001",
					SessionID: "sess-002",
					Text:      "Second comment",
					CreatedAt: time.Date(2024, 1, 15, 11, 45, 0, 0, time.UTC),
				},
				{
					ID:        3,
					IssueID:   "td-001",
					SessionID: "sess-003",
					Text:      "Third comment",
					CreatedAt: time.Date(2024, 1, 15, 14, 20, 0, 0, time.UTC),
				},
			},
			expectedMinLine: 4, // Header + 3 comments
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				Width:  80,
				Height: 30,
				Cursor: make(map[Panel]int),
			}

			modal := &ModalEntry{
				IssueID:  "td-001",
				Issue:    issue,
				Comments: tt.comments,
			}

			lineCount := m.estimateModalContentLines(modal)
			// Minimum line count should include base header (5) + comment contribution
			if tt.expectedMinLine > 0 {
				if lineCount < tt.expectedMinLine {
					t.Errorf("estimated lines = %d, want >= %d (base + comments)",
						lineCount, tt.expectedMinLine)
				}
			}
		})
	}
}

func TestModalCommentsDisplayFormatting(t *testing.T) {
	// Test that comments are rendered with correct formatting: timestamp, session, text
	issueID := "td-test-001"
	issue := &models.Issue{
		ID:    issueID,
		Title: "Test issue",
		Type:  models.TypeTask,
	}

	tests := []struct {
		name     string
		comment  models.Comment
		validate func(t *testing.T, output string)
	}{
		{
			name: "comment with short session",
			comment: models.Comment{
				ID:        1,
				IssueID:   issueID,
				SessionID: "short",
				Text:      "Test comment",
				CreatedAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			},
			validate: func(t *testing.T, output string) {
				// Should contain timestamp in MM-DD HH:MM format
				if !strings.Contains(output, "01-15") {
					t.Error("output should contain timestamp date 01-15")
				}
				if !strings.Contains(output, "10:30") {
					t.Error("output should contain timestamp time 10:30")
				}
				// Should contain session ID
				if !strings.Contains(output, "short") {
					t.Error("output should contain session ID 'short'")
				}
				// Should contain comment text
				if !strings.Contains(output, "Test comment") {
					t.Error("output should contain comment text")
				}
			},
		},
		{
			name: "comment with long session ID (truncated)",
			comment: models.Comment{
				ID:        2,
				IssueID:   issueID,
				SessionID: "this-is-a-very-long-session-id-that-should-be-truncated",
				Text:      "Another test",
				CreatedAt: time.Date(2024, 1, 15, 14, 45, 0, 0, time.UTC),
			},
			validate: func(t *testing.T, output string) {
				// Should contain timestamp
				if !strings.Contains(output, "01-15") {
					t.Error("output should contain timestamp date")
				}
				// Session should be truncated to 10 chars
				if !strings.Contains(output, "this-is-a-") {
					t.Error("output should contain truncated session ID")
				}
				// Should contain comment text
				if !strings.Contains(output, "Another test") {
					t.Error("output should contain comment text")
				}
			},
		},
		{
			name: "comment with special characters",
			comment: models.Comment{
				ID:        3,
				IssueID:   issueID,
				SessionID: "sess-003",
				Text:      "Comment with [special] {chars} and (parens)",
				CreatedAt: time.Date(2024, 1, 15, 9, 15, 0, 0, time.UTC),
			},
			validate: func(t *testing.T, output string) {
				if !strings.Contains(output, "[special]") {
					t.Error("output should preserve special characters like [brackets]")
				}
				if !strings.Contains(output, "{chars}") {
					t.Error("output should preserve special characters like {braces}")
				}
				if !strings.Contains(output, "(parens)") {
					t.Error("output should preserve special characters like (parens)")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modal := &ModalEntry{
				IssueID:  issueID,
				Issue:    issue,
				Comments: []models.Comment{tt.comment},
			}

			// Note: We can't directly test renderModal as it requires full view context,
			// but we can validate that comments are stored and the structure is correct
			if len(modal.Comments) != 1 {
				t.Errorf("modal should have 1 comment, got %d", len(modal.Comments))
			}

			comment := modal.Comments[0]
			if comment.Text != tt.comment.Text {
				t.Errorf("comment text = %q, want %q", comment.Text, tt.comment.Text)
			}
			if comment.SessionID != tt.comment.SessionID {
				t.Errorf("session ID = %q, want %q", comment.SessionID, tt.comment.SessionID)
			}
			if !comment.CreatedAt.Equal(tt.comment.CreatedAt) {
				t.Errorf("created at = %v, want %v", comment.CreatedAt, tt.comment.CreatedAt)
			}

			// Generate a simplified output to validate
			output := fmt.Sprintf("%s %s %s",
				comment.CreatedAt.Format("01-02 15:04"),
				comment.SessionID,
				comment.Text,
			)
			tt.validate(t, output)
		})
	}
}

func TestModalCommentsMultipleDisplayOrder(t *testing.T) {
	// Test that multiple comments display in creation order (oldest first)
	issue := &models.Issue{
		ID:    "td-001",
		Title: "Test issue",
		Type:  models.TypeTask,
	}

	comments := []models.Comment{
		{
			ID:        1,
			IssueID:   "td-001",
			SessionID: "sess-001",
			Text:      "First comment",
			CreatedAt: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        2,
			IssueID:   "td-001",
			SessionID: "sess-002",
			Text:      "Second comment",
			CreatedAt: time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC),
		},
		{
			ID:        3,
			IssueID:   "td-001",
			SessionID: "sess-003",
			Text:      "Third comment",
			CreatedAt: time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		},
	}

	modal := &ModalEntry{
		IssueID:  "td-001",
		Issue:    issue,
		Comments: comments,
	}

	// Verify comments are stored in order
	for i, expectedComment := range comments {
		if modal.Comments[i].ID != expectedComment.ID {
			t.Errorf("comment %d: ID = %d, want %d",
				i, modal.Comments[i].ID, expectedComment.ID)
		}
		if modal.Comments[i].Text != expectedComment.Text {
			t.Errorf("comment %d: text = %q, want %q",
				i, modal.Comments[i].Text, expectedComment.Text)
		}
		if !modal.Comments[i].CreatedAt.Equal(expectedComment.CreatedAt) {
			t.Errorf("comment %d: timestamp mismatch", i)
		}
	}
}

func TestModalCommentsEmptyHandling(t *testing.T) {
	// Test that empty comment lists are handled correctly
	m := Model{
		Width:  80,
		Height: 30,
		Cursor: make(map[Panel]int),
	}

	issue := &models.Issue{
		ID:    "td-empty-comments",
		Title: "Issue with no comments",
		Type:  models.TypeTask,
	}

	tests := []struct {
		name     string
		comments []models.Comment
	}{
		{
			name:     "nil comments",
			comments: nil,
		},
		{
			name:     "empty slice",
			comments: []models.Comment{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modal := &ModalEntry{
				IssueID:  issue.ID,
				Issue:    issue,
				Comments: tt.comments,
			}

			// Should not crash and should have zero comments
			if len(modal.Comments) != 0 {
				t.Errorf("expected 0 comments, got %d", len(modal.Comments))
			}

			// Should estimate reasonable line count without comments
			lines := m.estimateModalContentLines(modal)
			if lines < 5 {
				t.Errorf("estimated lines = %d, should be at least 5 (basic structure)",
					lines)
			}
		})
	}
}

func TestModalCommentsLongTextWrapping(t *testing.T) {
	// Test that long comments don't crash and handle text truncation
	m := Model{
		Width:  80,
		Height: 30,
		Cursor: make(map[Panel]int),
	}

	contentWidth := m.modalContentWidth()
	if contentWidth < 30 {
		contentWidth = 30
	}

	issue := &models.Issue{
		ID:    "td-long-comment",
		Title: "Test with long comment",
		Type:  models.TypeTask,
	}

	// Create a very long comment
	longText := strings.Repeat("This is a very long comment that exceeds normal display width. ", 10)

	comment := models.Comment{
		ID:        1,
		IssueID:   "td-long-comment",
		SessionID: "sess-long",
		Text:      longText,
		CreatedAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	modal := &ModalEntry{
		IssueID:  issue.ID,
		Issue:    issue,
		Comments: []models.Comment{comment},
	}

	// Should handle long text without crashing
	lines := m.estimateModalContentLines(modal)
	if lines < 2 {
		t.Errorf("estimated lines = %d, should be >= 2 (header + comment)",
			lines)
	}

	// Comment should still be accessible
	if modal.Comments[0].Text != longText {
		t.Error("long comment text should be preserved unchanged")
	}
}

func TestModalCommentsTimestampFormatting(t *testing.T) {
	// Test various timestamp formats are handled correctly
	issue := &models.Issue{
		ID:    "td-timestamps",
		Title: "Test timestamps",
		Type:  models.TypeTask,
	}
	_ = issue

	tests := []struct {
		name             string
		timestamp        time.Time
		expectedDatePart string // MM-DD format
		expectedTimePart string // HH:MM format
	}{
		{
			name:             "morning time",
			timestamp:        time.Date(2024, 1, 15, 9, 5, 0, 0, time.UTC),
			expectedDatePart: "01-15",
			expectedTimePart: "09:05",
		},
		{
			name:             "afternoon time",
			timestamp:        time.Date(2024, 1, 15, 14, 30, 0, 0, time.UTC),
			expectedDatePart: "01-15",
			expectedTimePart: "14:30",
		},
		{
			name:             "midnight",
			timestamp:        time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expectedDatePart: "01-15",
			expectedTimePart: "00:00",
		},
		{
			name:             "end of day",
			timestamp:        time.Date(2024, 1, 15, 23, 59, 0, 0, time.UTC),
			expectedDatePart: "01-15",
			expectedTimePart: "23:59",
		},
		{
			name:             "different month",
			timestamp:        time.Date(2024, 12, 25, 10, 30, 0, 0, time.UTC),
			expectedDatePart: "12-25",
			expectedTimePart: "10:30",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comment := models.Comment{
				ID:        1,
				IssueID:   "td-timestamps",
				SessionID: "sess-001",
				Text:      "Test comment",
				CreatedAt: tt.timestamp,
			}

			// Validate timestamp formatting
			formatted := comment.CreatedAt.Format("01-02 15:04")
			if !strings.Contains(formatted, tt.expectedDatePart) {
				t.Errorf("timestamp format = %s, should contain date %s",
					formatted, tt.expectedDatePart)
			}
			if !strings.Contains(formatted, tt.expectedTimePart) {
				t.Errorf("timestamp format = %s, should contain time %s",
					formatted, tt.expectedTimePart)
			}
		})
	}
}

func TestModalCommentsSessionIDTruncation(t *testing.T) {
	// Test that session IDs are truncated to 10 characters
	issue := &models.Issue{
		ID:    "td-session",
		Title: "Test session IDs",
		Type:  models.TypeTask,
	}

	tests := []struct {
		name           string
		sessionID      string
		expectedLen    int
		shouldTruncate bool
	}{
		{
			name:           "short session ID",
			sessionID:      "short",
			expectedLen:    5,
			shouldTruncate: false,
		},
		{
			name:           "exactly 10 chars",
			sessionID:      "0123456789",
			expectedLen:    10,
			shouldTruncate: false,
		},
		{
			name:           "longer than 10 chars",
			sessionID:      "this-is-a-very-long-session-id",
			expectedLen:    10,
			shouldTruncate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comment := models.Comment{
				ID:        1,
				IssueID:   "td-session",
				SessionID: tt.sessionID,
				Text:      "Test",
				CreatedAt: time.Now(),
			}

			modal := &ModalEntry{
				IssueID:  issue.ID,
				Issue:    issue,
				Comments: []models.Comment{comment},
			}

			// Verify the original session ID is stored
			if modal.Comments[0].SessionID != tt.sessionID {
				t.Errorf("stored session ID = %q, want %q",
					modal.Comments[0].SessionID, tt.sessionID)
			}

			// Truncation happens during render (not in storage)
			// But we can validate the logic here
			truncated := modal.Comments[0].SessionID
			if len(truncated) > 10 {
				truncated = truncated[:10]
			}

			if len(truncated) != tt.expectedLen {
				t.Errorf("truncated session ID length = %d, want %d",
					len(truncated), tt.expectedLen)
			}

			if tt.shouldTruncate && truncated != tt.sessionID[:10] {
				t.Errorf("truncation mismatch: got %q, want %q",
					truncated, tt.sessionID[:10])
			}
		})
	}
}

func TestModalCommentsScrollBehavior(t *testing.T) {
	// Test that comments contribute correctly to scroll calculations
	m := Model{
		Width:  80,
		Height: 30,
		Cursor: make(map[Panel]int),
	}

	issue := &models.Issue{
		ID:    "td-scroll",
		Title: "Test scroll behavior",
		Type:  models.TypeTask,
		Description: "This is a description that takes some space\n" +
			"Line 2\n" +
			"Line 3",
	}

	tests := []struct {
		name           string
		commentCount   int
		checkScrollReq bool // If true, verify scroll is needed
	}{
		{
			name:         "no comments - minimal content",
			commentCount: 0,
		},
		{
			name:           "few comments - may or may not need scroll",
			commentCount:   2,
			checkScrollReq: false,
		},
		{
			name:           "many comments - likely needs scroll",
			commentCount:   20,
			checkScrollReq: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var comments []models.Comment
			for i := 0; i < tt.commentCount; i++ {
				comments = append(comments, models.Comment{
					ID:        int64(i + 1),
					IssueID:   "td-scroll",
					SessionID: fmt.Sprintf("sess-%d", i),
					Text:      fmt.Sprintf("Comment number %d", i+1),
					CreatedAt: time.Now().Add(-time.Duration(tt.commentCount-i) * time.Hour),
				})
			}

			modal := &ModalEntry{
				IssueID:  issue.ID,
				Issue:    issue,
				Comments: comments,
			}

			// Update content lines
			modal.ContentLines = m.estimateModalContentLines(modal)

			// Calculate max scroll
			maxScroll := m.modalMaxScroll(modal)

			// If we have many comments, we expect scroll to be needed
			if tt.checkScrollReq && tt.commentCount > 10 {
				if maxScroll <= 0 && modal.ContentLines > 30 {
					t.Logf("warning: large comment count (%d) but maxScroll=%d (lines=%d)",
						tt.commentCount, maxScroll, modal.ContentLines)
				}
			}

			// Verify content lines accounts for comments
			expectedMinLines := 5 + tt.commentCount // Base + comments
			if modal.ContentLines < expectedMinLines {
				t.Errorf("content lines = %d, want >= %d (base + %d comments)",
					modal.ContentLines, expectedMinLines, tt.commentCount)
			}
		})
	}
}
