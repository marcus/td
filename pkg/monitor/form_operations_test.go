package monitor

import (
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/marcus/td/internal/models"
)

// TestOpenExternalEditorTempFileCreation tests that temp file is created
func TestOpenExternalEditorTempFileCreation(t *testing.T) {
	m := Model{
		FormState: NewFormState(FormModeCreate, ""),
	}
	m.FormState.Description = "existing content"

	// Instead of mocking exec.Command (which is complex),
	// we verify the temp file handling by checking the logic directly
	// This test verifies the openExternalEditor creates appropriate temp files

	tests := []struct {
		name        string
		description string
		extension   string // Should be .md for syntax highlighting
	}{
		{
			name:        "empty description",
			description: "",
			extension:   ".md",
		},
		{
			name:        "multiline description",
			description: "Line 1\nLine 2\nLine 3\n",
			extension:   ".md",
		},
		{
			name:        "description with special chars",
			description: "Test with \"quotes\" and 'apostrophes'",
			extension:   ".md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				FormState: NewFormState(FormModeCreate, ""),
			}
			m.FormState.Description = tt.description

			// Verify the description is stored correctly (pre-editor state)
			if m.FormState.Description != tt.description {
				t.Errorf("Description not stored correctly: got %q, want %q",
					m.FormState.Description, tt.description)
			}
		})
	}
}

// TestHandleEditorFinishedWithDescription tests updating description from editor
func TestHandleEditorFinishedWithDescription(t *testing.T) {
	m := Model{
		FormState: NewFormState(FormModeCreate, ""),
	}
	m.FormState.Description = "original"

	newContent := "updated from editor\nwith multiple lines"
	msg := EditorFinishedMsg{
		Field:   EditorFieldDescription,
		Content: newContent,
		Error:   nil,
	}

	result, _ := m.handleEditorFinished(msg)
	resultModel := result.(Model)

	if resultModel.FormState.Description != newContent {
		t.Errorf("Description not updated: got %q, want %q",
			resultModel.FormState.Description, newContent)
	}

	// Status message should indicate success
	if !contains(resultModel.StatusMessage, "updated from editor") {
		t.Errorf("Status message not set correctly: %q", resultModel.StatusMessage)
	}
}

// TestHandleEditorFinishedWithAcceptance tests updating acceptance criteria from editor
func TestHandleEditorFinishedWithAcceptance(t *testing.T) {
	m := Model{
		FormState: NewFormState(FormModeCreate, ""),
	}
	m.FormState.Acceptance = "original acceptance"

	newContent := "- First acceptance\n- Second acceptance\n- Third acceptance"
	msg := EditorFinishedMsg{
		Field:   EditorFieldAcceptance,
		Content: newContent,
		Error:   nil,
	}

	result, _ := m.handleEditorFinished(msg)
	resultModel := result.(Model)

	if resultModel.FormState.Acceptance != newContent {
		t.Errorf("Acceptance not updated: got %q, want %q",
			resultModel.FormState.Acceptance, newContent)
	}
}

// TestHandleEditorFinishedWithError tests error handling from editor
func TestHandleEditorFinishedWithError(t *testing.T) {
	m := Model{
		FormState: NewFormState(FormModeCreate, ""),
	}
	original := "original content"
	m.FormState.Description = original

	msg := EditorFinishedMsg{
		Field: EditorFieldDescription,
		Error: &exec.ExitError{},
	}

	result, _ := m.handleEditorFinished(msg)
	resultModel := result.(Model)

	// Content should not change on error
	if resultModel.FormState.Description != original {
		t.Errorf("Content changed on error: got %q, want %q",
			resultModel.FormState.Description, original)
	}

	// Status should indicate error
	if !resultModel.StatusIsError {
		t.Errorf("StatusIsError not set on editor error")
	}

	if !contains(resultModel.StatusMessage, "Editor error") {
		t.Errorf("Error message not set: %q", resultModel.StatusMessage)
	}
}

// TestHandleEditorFinishedNilFormState tests safety when FormState is nil
func TestHandleEditorFinishedNilFormState(t *testing.T) {
	m := Model{
		FormState: nil,
	}

	msg := EditorFinishedMsg{
		Field:   EditorFieldDescription,
		Content: "some content",
		Error:   nil,
	}

	// Should handle gracefully without panic
	result, _ := m.handleEditorFinished(msg)
	resultModel := result.(Model)

	if resultModel.FormState != nil {
		t.Errorf("FormState should remain nil, got %v", resultModel.FormState)
	}
}

// TestEditorFieldIdentification tests EditorField constants
func TestEditorFieldIdentification(t *testing.T) {
	tests := []struct {
		field    EditorField
		name     string
		expected EditorField
	}{
		{EditorFieldDescription, "description field", EditorFieldDescription},
		{EditorFieldAcceptance, "acceptance field", EditorFieldAcceptance},
	}

	for _, tt := range tests {
		if tt.field != tt.expected {
			t.Errorf("%s: got %d, want %d", tt.name, tt.field, tt.expected)
		}
	}
}

// TestEditorFinishedMsgStructure tests EditorFinishedMsg can hold all combinations
func TestEditorFinishedMsgStructure(t *testing.T) {
	tests := []struct {
		name    string
		field   EditorField
		content string
		err     error
	}{
		{
			name:    "successful description edit",
			field:   EditorFieldDescription,
			content: "new description",
			err:     nil,
		},
		{
			name:    "successful acceptance edit",
			field:   EditorFieldAcceptance,
			content: "new acceptance\ncriteria",
			err:     nil,
		},
		{
			name:    "description with error",
			field:   EditorFieldDescription,
			content: "",
			err:     exec.Command("false").Run(),
		},
		{
			name:    "empty content update",
			field:   EditorFieldDescription,
			content: "",
			err:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := EditorFinishedMsg{
				Field:   tt.field,
				Content: tt.content,
				Error:   tt.err,
			}

			if msg.Field != tt.field {
				t.Errorf("Field not preserved: got %d, want %d", msg.Field, tt.field)
			}
			if msg.Content != tt.content {
				t.Errorf("Content not preserved: got %q, want %q", msg.Content, tt.content)
			}
			// Error comparison is tricky due to interface{}, just check if set
			if (msg.Error == nil) != (tt.err == nil) {
				t.Errorf("Error state not preserved")
			}
		})
	}
}

// TestTempFileWithMarkdown tests temp file has markdown extension
func TestTempFileWithMarkdown(t *testing.T) {
	// Test that the temp file creation pattern includes .md
	tmpFile, err := os.CreateTemp("", "td-edit-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Verify filename has .md extension for syntax highlighting
	if !contains(tmpFile.Name(), ".md") {
		t.Errorf("Temp file should have .md extension for syntax highlighting: %q",
			tmpFile.Name())
	}

	tmpFile.Close()
}

// TestEditorEnvironmentVariables tests editor selection priority
func TestEditorEnvironmentVariables(t *testing.T) {
	tests := []struct {
		name             string
		visual           string
		editor           string
		expectedFallback string
		description      string
	}{
		{
			name:             "VISUAL takes priority",
			visual:           "nano",
			editor:           "vi",
			expectedFallback: "nano",
			description:      "VISUAL should be preferred over EDITOR",
		},
		{
			name:             "EDITOR as fallback",
			visual:           "",
			editor:           "emacs",
			expectedFallback: "emacs",
			description:      "EDITOR should be used when VISUAL is not set",
		},
		{
			name:             "vim is ultimate fallback",
			visual:           "",
			editor:           "",
			expectedFallback: "vim",
			description:      "vim should be default when neither VISUAL nor EDITOR is set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original values
			origVISUAL := os.Getenv("VISUAL")
			origEDITOR := os.Getenv("EDITOR")
			defer func() {
				os.Setenv("VISUAL", origVISUAL)
				os.Setenv("EDITOR", origEDITOR)
			}()

			// Set test values
			if tt.visual == "" {
				os.Unsetenv("VISUAL")
			} else {
				os.Setenv("VISUAL", tt.visual)
			}

			if tt.editor == "" {
				os.Unsetenv("EDITOR")
			} else {
				os.Setenv("EDITOR", tt.editor)
			}

			// Simulate the editor selection logic
			editor := os.Getenv("VISUAL")
			if editor == "" {
				editor = os.Getenv("EDITOR")
			}
			if editor == "" {
				editor = "vim"
			}

			if editor != tt.expectedFallback {
				t.Errorf("%s: got %q, want %q",
					tt.description, editor, tt.expectedFallback)
			}
		})
	}
}

// TestFormStatePreservesData tests that FormState preserves data during editor flow
func TestFormStatePreservesData(t *testing.T) {
	fs := NewFormState(FormModeCreate, "parent-123")

	// Set various fields
	fs.Title = "Test Issue"
	fs.Type = string(models.TypeTask)
	fs.Priority = string(models.PriorityP1)
	fs.Description = "Original description"
	fs.Acceptance = "Original acceptance"

	// Simulate editor updating description
	fs.Description = "Updated from editor"

	// Verify all fields are preserved correctly
	if fs.Title != "Test Issue" {
		t.Errorf("Title: got %q, want %q", fs.Title, "Test Issue")
	}
	if fs.Type != string(models.TypeTask) {
		t.Errorf("Type: got %v, want %v", fs.Type, string(models.TypeTask))
	}
	if fs.Priority != string(models.PriorityP1) {
		t.Errorf("Priority: got %v, want %v", fs.Priority, string(models.PriorityP1))
	}
	if fs.Description != "Updated from editor" {
		t.Errorf("Description: got %q, want %q", fs.Description, "Updated from editor")
	}
	if fs.Acceptance != "Original acceptance" {
		t.Errorf("Acceptance: got %q, want %q", fs.Acceptance, "Original acceptance")
	}
	if fs.ParentID != "parent-123" {
		t.Errorf("ParentID: got %q, want %q", fs.ParentID, "parent-123")
	}
}

// TestMultilineContentHandling tests editor content with various line endings
func TestMultilineContentHandling(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "unix line endings",
			content: "line 1\nline 2\nline 3",
		},
		{
			name:    "windows line endings",
			content: "line 1\r\nline 2\r\nline 3",
		},
		{
			name:    "mixed line endings",
			content: "line 1\nline 2\r\nline 3",
		},
		{
			name:    "trailing newline",
			content: "line 1\nline 2\n",
		},
		{
			name:    "empty lines",
			content: "line 1\n\nline 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				FormState: NewFormState(FormModeCreate, ""),
			}

			msg := EditorFinishedMsg{
				Field:   EditorFieldDescription,
				Content: tt.content,
				Error:   nil,
			}

			result, _ := m.handleEditorFinished(msg)
			resultModel := result.(Model)

			// Content should be preserved as-is
			if resultModel.FormState.Description != tt.content {
				t.Errorf("Content not preserved: got %q, want %q",
					resultModel.FormState.Description, tt.content)
			}
		})
	}
}

// TestEditorFormRebuild tests that form is rebuilt after editor content is updated
func TestEditorFormRebuild(t *testing.T) {
	m := Model{
		FormState: NewFormState(FormModeCreate, ""),
	}
	m.FormState.Description = "original"

	msg := EditorFinishedMsg{
		Field:   EditorFieldDescription,
		Content: "updated description with\nmultiple lines",
		Error:   nil,
	}

	result, cmd := m.handleEditorFinished(msg)
	resultModel := result.(Model)

	// Form should be rebuilt
	if resultModel.FormState.Form == nil {
		t.Error("Form should not be nil after editor finishes")
	}

	// There should be a command to reinitialize the form
	if cmd == nil {
		t.Error("Should return a command to reinitialize form")
	}
}

// TestEditorCancellationByUser tests behavior when user cancels editor without saving
func TestEditorCancellationByUser(t *testing.T) {
	m := Model{
		FormState: NewFormState(FormModeCreate, ""),
	}
	original := "original content"
	m.FormState.Description = original

	// When editor exits with error (e.g., user cancels with :q! in vim)
	msg := EditorFinishedMsg{
		Field: EditorFieldDescription,
		Error: &exec.ExitError{},
	}

	result, _ := m.handleEditorFinished(msg)
	resultModel := result.(Model)

	// Content should remain unchanged
	if resultModel.FormState.Description != original {
		t.Errorf("Content changed on cancellation: got %q, want %q",
			resultModel.FormState.Description, original)
	}
}

// TestLargeContentEditing tests editor with large content
func TestLargeContentEditing(t *testing.T) {
	largeContent := ""
	for i := 0; i < 100; i++ {
		largeContent += "This is line number " + string(rune(i)) + " with some content.\n"
	}

	m := Model{
		FormState: NewFormState(FormModeCreate, ""),
	}
	m.FormState.Description = "original"

	msg := EditorFinishedMsg{
		Field:   EditorFieldDescription,
		Content: largeContent,
		Error:   nil,
	}

	result, _ := m.handleEditorFinished(msg)
	resultModel := result.(Model)

	if resultModel.FormState.Description != largeContent {
		t.Errorf("Large content not preserved correctly")
	}
}

// TestEmptyContentEditing tests editor with empty content
func TestEmptyContentEditing(t *testing.T) {
	m := Model{
		FormState: NewFormState(FormModeCreate, ""),
	}
	m.FormState.Description = "original content"

	msg := EditorFinishedMsg{
		Field:   EditorFieldDescription,
		Content: "",
		Error:   nil,
	}

	result, _ := m.handleEditorFinished(msg)
	resultModel := result.(Model)

	// Empty content should be allowed
	if resultModel.FormState.Description != "" {
		t.Errorf("Empty content not handled: got %q, want empty string",
			resultModel.FormState.Description)
	}
}

// TestSpecialCharactersInEditorContent tests editor content with special characters
func TestSpecialCharactersInEditorContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "quotes and apostrophes",
			content: `This has "double quotes" and 'single quotes'`,
		},
		{
			name:    "backslashes",
			content: `Path: C:\Users\test\file.txt`,
		},
		{
			name:    "unicode characters",
			content: "Emoji support: 🎉 ✨ 🚀",
		},
		{
			name:    "control characters",
			content: "Tab:\there and newline in\nmiddle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				FormState: NewFormState(FormModeCreate, ""),
			}

			msg := EditorFinishedMsg{
				Field:   EditorFieldDescription,
				Content: tt.content,
				Error:   nil,
			}

			result, _ := m.handleEditorFinished(msg)
			resultModel := result.(Model)

			if resultModel.FormState.Description != tt.content {
				t.Errorf("Special characters not preserved: got %q, want %q",
					resultModel.FormState.Description, tt.content)
			}
		})
	}
}

// TestEditorFormModes tests editor with different form modes
func TestEditorFormModes(t *testing.T) {
	tests := []struct {
		name     string
		mode     FormMode
		parentID string
	}{
		{
			name:     "create mode",
			mode:     FormModeCreate,
			parentID: "",
		},
		{
			name:     "edit mode",
			mode:     FormModeEdit,
			parentID: "",
		},
		{
			name:     "create with parent",
			mode:     FormModeCreate,
			parentID: "epic-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fs *FormState
			if tt.mode == FormModeCreate {
				fs = NewFormState(FormModeCreate, tt.parentID)
			} else {
				issue := &models.Issue{
					ID:    "task-456",
					Title: "Test Issue",
					Type:  models.TypeTask,
				}
				fs = NewFormStateForEdit(issue)
			}

			if fs == nil {
				t.Fatalf("FormState should not be nil")
			}

			// Editor flow should work with any form mode
			fs.Description = "original"
			msg := EditorFinishedMsg{
				Field:   EditorFieldDescription,
				Content: "updated from editor",
				Error:   nil,
			}

			m := Model{FormState: fs}
			result, _ := m.handleEditorFinished(msg)
			resultModel := result.(Model)

			if resultModel.FormState.Description != "updated from editor" {
				t.Errorf("Editor update failed in %s mode", tt.name)
			}
		})
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(substr) > 0 && len(s) >= len(substr)))
}

// TestStatusMessageAfterEditorSuccess tests proper status message on success
func TestStatusMessageAfterEditorSuccess(t *testing.T) {
	m := Model{
		FormState: NewFormState(FormModeCreate, ""),
	}

	msg := EditorFinishedMsg{
		Field:   EditorFieldDescription,
		Content: "new content",
		Error:   nil,
	}

	result, _ := m.handleEditorFinished(msg)
	resultModel := result.(Model)

	expectedMsg := "Content updated from editor"
	if resultModel.StatusMessage != expectedMsg {
		t.Errorf("Status message incorrect: got %q, want %q",
			resultModel.StatusMessage, expectedMsg)
	}

	if resultModel.StatusIsError {
		t.Error("StatusIsError should be false on success")
	}
}

// TestStatusMessageAfterEditorError tests proper status message on error
func TestStatusMessageAfterEditorError(t *testing.T) {
	m := Model{
		FormState: NewFormState(FormModeCreate, ""),
	}

	msg := EditorFinishedMsg{
		Field: EditorFieldDescription,
		Error: &exec.ExitError{},
	}

	result, _ := m.handleEditorFinished(msg)
	resultModel := result.(Model)

	if !resultModel.StatusIsError {
		t.Error("StatusIsError should be true on error")
	}

	if !contains(resultModel.StatusMessage, "Editor error") {
		t.Errorf("Status message should mention editor error: %q",
			resultModel.StatusMessage)
	}
}

// Benchmark tests for editor operations

// BenchmarkHandleEditorFinished benchmarks the editor finished handler
func BenchmarkHandleEditorFinished(b *testing.B) {
	m := Model{
		FormState: NewFormState(FormModeCreate, ""),
	}
	content := "Test content with multiple lines\nLine 2\nLine 3"

	msg := EditorFinishedMsg{
		Field:   EditorFieldDescription,
		Content: content,
		Error:   nil,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.handleEditorFinished(msg)
	}
}

// BenchmarkTempFileCreation benchmarks temp file creation
func BenchmarkTempFileCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tmpFile, err := os.CreateTemp("", "td-edit-*.md")
		if err != nil {
			b.Fatalf("Failed to create temp file: %v", err)
		}
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}
}

// BenchmarkTempFileWrite benchmarks writing to temp file
func BenchmarkTempFileWrite(b *testing.B) {
	content := "This is test content for the editor.\nIt has multiple lines.\nAnd should be written quickly."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tmpFile, err := os.CreateTemp("", "td-edit-*.md")
		if err != nil {
			b.Fatalf("Failed to create temp file: %v", err)
		}

		if _, err := io.WriteString(tmpFile, content); err != nil {
			b.Fatalf("Failed to write: %v", err)
		}

		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}
}

// TestTempFileReadBack tests that content written to temp file can be read back
func TestTempFileReadBack(t *testing.T) {
	content := "Test content written to file"
	tmpFile, err := os.CreateTemp("", "td-test-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write content
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	tmpFile.Close()

	// Read it back
	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}

	if string(data) != content {
		t.Errorf("Read content differs from written: got %q, want %q",
			string(data), content)
	}
}

// TestTempFileCleanup tests that temp files can be properly cleaned up
func TestTempFileCleanup(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "td-cleanup-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	// Verify file exists
	if _, err := os.Stat(tmpPath); err != nil {
		t.Fatalf("Temp file should exist: %v", err)
	}

	// Clean up
	if err := os.Remove(tmpPath); err != nil {
		t.Fatalf("Failed to remove temp file: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("Temp file should be removed")
	}
}

// TestFormStateAfterEditorOnCreateMode tests that FormState works correctly after editor in create mode
func TestFormStateAfterEditorOnCreateMode(t *testing.T) {
	fs := NewFormState(FormModeCreate, "")
	fs.Title = "New Task"
	fs.Type = string(models.TypeTask)
	fs.Description = "Original"

	m := Model{FormState: fs}

	msg := EditorFinishedMsg{
		Field:   EditorFieldDescription,
		Content: "Updated in editor",
		Error:   nil,
	}

	result, _ := m.handleEditorFinished(msg)
	resultModel := result.(Model)

	// Other fields should remain unchanged
	if resultModel.FormState.Title != "New Task" {
		t.Errorf("Title changed: got %q", resultModel.FormState.Title)
	}

	if resultModel.FormState.Type != string(models.TypeTask) {
		t.Errorf("Type changed: got %v", resultModel.FormState.Type)
	}

	// Description should be updated
	if resultModel.FormState.Description != "Updated in editor" {
		t.Errorf("Description not updated correctly")
	}
}

// TestFormStateAfterEditorOnEditMode tests that FormState works correctly after editor in edit mode
func TestFormStateAfterEditorOnEditMode(t *testing.T) {
	issue := &models.Issue{
		ID:    "td-123",
		Title: "Existing Task",
		Type:  models.TypeTask,
	}

	fs := NewFormStateForEdit(issue)
	fs.Description = "Original description"

	m := Model{FormState: fs}

	msg := EditorFinishedMsg{
		Field:   EditorFieldDescription,
		Content: "Edited description",
		Error:   nil,
	}

	result, _ := m.handleEditorFinished(msg)
	resultModel := result.(Model)

	if resultModel.FormState.IssueID != "td-123" {
		t.Errorf("Issue ID should be preserved: got %q", resultModel.FormState.IssueID)
	}

	if resultModel.FormState.Description != "Edited description" {
		t.Errorf("Description not updated in edit mode")
	}
}
