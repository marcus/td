package input

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadLinesFromReaderBasic tests reading basic lines
func TestReadLinesFromReaderBasic(t *testing.T) {
	r := strings.NewReader("line1\nline2\nline3")
	lines := ReadLinesFromReader(r)

	if len(lines) != 3 {
		t.Fatalf("Expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "line1" {
		t.Errorf("Expected 'line1', got %q", lines[0])
	}
	if lines[2] != "line3" {
		t.Errorf("Expected 'line3', got %q", lines[2])
	}
}

// TestReadLinesFromReaderEmpty tests reading from empty reader
func TestReadLinesFromReaderEmpty(t *testing.T) {
	r := strings.NewReader("")
	lines := ReadLinesFromReader(r)

	if len(lines) != 0 {
		t.Errorf("Expected 0 lines, got %d", len(lines))
	}
}

// TestReadLinesFromReaderSkipsEmptyLines tests that empty lines are skipped
func TestReadLinesFromReaderSkipsEmptyLines(t *testing.T) {
	r := strings.NewReader("line1\n\n\nline2\n\nline3")
	lines := ReadLinesFromReader(r)

	if len(lines) != 3 {
		t.Fatalf("Expected 3 lines (empty skipped), got %d", len(lines))
	}
}

// TestReadLinesFromReaderTrimsWhitespace tests whitespace trimming
func TestReadLinesFromReaderTrimsWhitespace(t *testing.T) {
	r := strings.NewReader("  line1  \n\tline2\t\n   line3   ")
	lines := ReadLinesFromReader(r)

	if len(lines) != 3 {
		t.Fatalf("Expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "line1" {
		t.Errorf("Expected trimmed 'line1', got %q", lines[0])
	}
	if lines[1] != "line2" {
		t.Errorf("Expected trimmed 'line2', got %q", lines[1])
	}
}

// TestReadLinesFromReaderWhitespaceOnlySkipped tests whitespace-only lines
func TestReadLinesFromReaderWhitespaceOnlySkipped(t *testing.T) {
	r := strings.NewReader("line1\n   \n\t\t\nline2")
	lines := ReadLinesFromReader(r)

	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines (whitespace-only skipped), got %d", len(lines))
	}
}

// TestExpandFlagValuesPassthrough tests regular value passthrough
func TestExpandFlagValuesPassthrough(t *testing.T) {
	values := []string{"value1", "value2", "value3"}
	result, stdinUsed := ExpandFlagValues(values, false)

	if stdinUsed {
		t.Error("stdin should not be marked as used")
	}
	if len(result) != 3 {
		t.Fatalf("Expected 3 values, got %d", len(result))
	}
	if result[0] != "value1" || result[1] != "value2" || result[2] != "value3" {
		t.Errorf("Values not passed through correctly: %v", result)
	}
}

// TestExpandFlagValuesFileExpansion tests @file expansion
func TestExpandFlagValuesFileExpansion(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	content := "file_line1\nfile_line2\nfile_line3"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	values := []string{"@" + filePath}
	result, stdinUsed := ExpandFlagValues(values, false)

	if stdinUsed {
		t.Error("stdin should not be marked as used")
	}
	if len(result) != 3 {
		t.Fatalf("Expected 3 lines from file, got %d", len(result))
	}
	if result[0] != "file_line1" {
		t.Errorf("Expected 'file_line1', got %q", result[0])
	}
}

// TestExpandFlagValuesMixedInput tests mixed regular and file values
func TestExpandFlagValuesMixedInput(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, []byte("from_file"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	values := []string{"regular", "@" + filePath, "another_regular"}
	result, _ := ExpandFlagValues(values, false)

	if len(result) != 3 {
		t.Fatalf("Expected 3 values, got %d", len(result))
	}
	if result[0] != "regular" {
		t.Errorf("Expected 'regular', got %q", result[0])
	}
	if result[1] != "from_file" {
		t.Errorf("Expected 'from_file', got %q", result[1])
	}
	if result[2] != "another_regular" {
		t.Errorf("Expected 'another_regular', got %q", result[2])
	}
}

// TestExpandFlagValuesMissingFile tests handling of missing file
func TestExpandFlagValuesMissingFile(t *testing.T) {
	values := []string{"@/nonexistent/path/to/file.txt"}
	result, _ := ExpandFlagValues(values, false)

	// Missing file should be skipped with warning
	if len(result) != 0 {
		t.Errorf("Expected 0 values (file not found), got %d", len(result))
	}
}

// TestExpandFlagValuesEmptyFile tests handling of empty file
func TestExpandFlagValuesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(filePath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	values := []string{"@" + filePath}
	result, _ := ExpandFlagValues(values, false)

	if len(result) != 0 {
		t.Errorf("Expected 0 values from empty file, got %d", len(result))
	}
}

// TestExpandFlagValuesFileWithEmptyLines tests file with empty lines
func TestExpandFlagValuesFileWithEmptyLines(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sparse.txt")
	content := "line1\n\n\nline2\n\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	values := []string{"@" + filePath}
	result, _ := ExpandFlagValues(values, false)

	if len(result) != 2 {
		t.Fatalf("Expected 2 lines (empty skipped), got %d", len(result))
	}
}

// TestExpandFlagValuesStdinAlreadyUsed tests stdin reuse prevention
func TestExpandFlagValuesStdinAlreadyUsed(t *testing.T) {
	values := []string{"-", "-"}
	result, stdinUsed := ExpandFlagValues(values, true) // stdin already used

	if !stdinUsed {
		t.Error("stdinUsed should remain true")
	}
	// Both stdin reads should be skipped since stdin was already used
	if len(result) != 0 {
		t.Errorf("Expected 0 values (stdin already used), got %d", len(result))
	}
}

// TestExpandFlagValuesPreservesStdinState tests stdin state preservation
func TestExpandFlagValuesPreservesStdinState(t *testing.T) {
	// When passing stdinUsed=true, it should stay true
	_, stdinUsed := ExpandFlagValues([]string{"regular"}, true)
	if !stdinUsed {
		t.Error("stdinUsed should remain true when passed as true")
	}

	// When passing stdinUsed=false and no stdin flag, it should stay false
	_, stdinUsed = ExpandFlagValues([]string{"regular"}, false)
	if stdinUsed {
		t.Error("stdinUsed should remain false when no stdin flag")
	}
}

// TestExpandFlagValuesMultipleFiles tests reading from multiple files
func TestExpandFlagValuesMultipleFiles(t *testing.T) {
	dir := t.TempDir()

	file1 := filepath.Join(dir, "file1.txt")
	file2 := filepath.Join(dir, "file2.txt")
	_ = os.WriteFile(file1, []byte("from_file1"), 0644)
	_ = os.WriteFile(file2, []byte("from_file2"), 0644)

	values := []string{"@" + file1, "@" + file2}
	result, _ := ExpandFlagValues(values, false)

	if len(result) != 2 {
		t.Fatalf("Expected 2 values from 2 files, got %d", len(result))
	}
	if result[0] != "from_file1" || result[1] != "from_file2" {
		t.Errorf("Values not correct: %v", result)
	}
}

// TestExpandFlagValuesAtSymbolInValue tests @ in regular value
func TestExpandFlagValuesAtSymbolInValue(t *testing.T) {
	// @ at the start is treated as file path
	values := []string{"email@example.com"} // This doesn't start with @
	result, _ := ExpandFlagValues(values, false)

	// Since it doesn't start with @, it should pass through
	if len(result) != 1 {
		t.Fatalf("Expected 1 value, got %d", len(result))
	}
	if result[0] != "email@example.com" {
		t.Errorf("Expected 'email@example.com', got %q", result[0])
	}
}

// TestExpandFlagValuesEmptySlice tests empty input
func TestExpandFlagValuesEmptySlice(t *testing.T) {
	result, stdinUsed := ExpandFlagValues([]string{}, false)

	if stdinUsed {
		t.Error("stdinUsed should be false")
	}
	if len(result) != 0 {
		t.Errorf("Expected 0 values, got %d", len(result))
	}
}

// TestExpandFlagValuesNilSlice tests nil input
func TestExpandFlagValuesNilSlice(t *testing.T) {
	result, stdinUsed := ExpandFlagValues(nil, false)

	if stdinUsed {
		t.Error("stdinUsed should be false")
	}
	if len(result) != 0 {
		t.Errorf("Expected nil or empty result, got %v", result)
	}
}

func TestReadTextFromFilePreservesExactContent(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "body.md")
	want := "Intro\n\n```go\nfmt.Println(\"hi\")\n```\n  indented\n"
	if err := os.WriteFile(filePath, []byte(want), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	got, stdinUsed, err := ReadText(filePath, strings.NewReader("unused"), false)
	if err != nil {
		t.Fatalf("ReadText failed: %v", err)
	}
	if stdinUsed {
		t.Fatal("stdinUsed should be false for file input")
	}
	if got != want {
		t.Fatalf("content mismatch\nwant: %q\ngot:  %q", want, got)
	}
}

func TestReadTextFromStdinPreservesExactContent(t *testing.T) {
	want := "# Title\n\n- item\n\n> quote\n"
	got, stdinUsed, err := ReadText("-", strings.NewReader(want), false)
	if err != nil {
		t.Fatalf("ReadText failed: %v", err)
	}
	if !stdinUsed {
		t.Fatal("stdinUsed should be true for stdin input")
	}
	if got != want {
		t.Fatalf("content mismatch\nwant: %q\ngot:  %q", want, got)
	}
}

func TestReadTextRejectsSecondStdinRead(t *testing.T) {
	_, stdinUsed, err := ReadText("-", strings.NewReader("ignored"), true)
	if !stdinUsed {
		t.Fatal("stdinUsed should remain true")
	}
	if !errors.Is(err, ErrStdinAlreadyUsed) {
		t.Fatalf("expected ErrStdinAlreadyUsed, got %v", err)
	}
}
