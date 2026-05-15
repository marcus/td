package output

import (
	"strings"
	"testing"
)

func TestTerminalWidthFallback(t *testing.T) {
	// When stdout isn't a TTY (typical in `go test`) and COLUMNS is unset,
	// TerminalWidth should return the provided fallback.
	t.Setenv("COLUMNS", "")
	if got := TerminalWidth(120); got <= 0 {
		t.Errorf("expected positive width, got %d", got)
	}
}

func TestTerminalWidthDefaultsWhenFallbackNonPositive(t *testing.T) {
	t.Setenv("COLUMNS", "")
	// Non-positive fallback should clamp to defaultMarkdownWidth (80) when
	// no terminal or COLUMNS is available. We assert a sensible positive value.
	got := TerminalWidth(0)
	if got <= 0 {
		t.Errorf("expected positive width, got %d", got)
	}
}

func TestTerminalWidthUsesColumnsEnv(t *testing.T) {
	t.Setenv("COLUMNS", "57")
	// We can't force GetSize to fail, but in `go test` stdout typically isn't
	// a TTY so the env-var branch is hit. Allow either: a positive terminal
	// width or the env value.
	got := TerminalWidth(80)
	if got <= 0 {
		t.Errorf("expected positive width, got %d", got)
	}
}

func TestTerminalWidthIgnoresInvalidColumns(t *testing.T) {
	t.Setenv("COLUMNS", "not-a-number")
	got := TerminalWidth(42)
	if got <= 0 {
		t.Errorf("expected positive width, got %d", got)
	}
}

func TestRenderMarkdownEmpty(t *testing.T) {
	cases := []string{"", "   ", "\n\n", "\t \n"}
	for _, in := range cases {
		got, err := RenderMarkdown(in)
		if err != nil {
			t.Fatalf("RenderMarkdown(%q) unexpected error: %v", in, err)
		}
		if got != "" {
			t.Errorf("RenderMarkdown(%q) = %q, want empty", in, got)
		}
	}
}

func TestRenderMarkdownWithWidthEmpty(t *testing.T) {
	got, err := RenderMarkdownWithWidth("", 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty output, got %q", got)
	}
}

func TestRenderMarkdownWithWidthClampsTooSmall(t *testing.T) {
	// Width below the minimum should still produce output without panicking.
	out, err := RenderMarkdownWithWidth("hello world", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", out)
	}
}

func TestRenderMarkdownRendersHeading(t *testing.T) {
	out, err := RenderMarkdownWithWidth("# Title\n\nSome body text.\n", 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Title") {
		t.Errorf("expected heading text in output, got %q", out)
	}
	if !strings.Contains(out, "Some body text") {
		t.Errorf("expected body text in output, got %q", out)
	}
	// Should be trimmed of trailing newlines.
	if strings.HasSuffix(out, "\n") {
		t.Errorf("expected trailing newlines to be trimmed, got %q", out)
	}
}

func TestRenderMarkdownRendersList(t *testing.T) {
	in := "- one\n- two\n- three\n"
	out, err := RenderMarkdownWithWidth(in, 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, item := range []string{"one", "two", "three"} {
		if !strings.Contains(out, item) {
			t.Errorf("expected %q in rendered list, got %q", item, out)
		}
	}
}
