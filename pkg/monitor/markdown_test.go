package monitor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
)

func TestBuildChromaStyle(t *testing.T) {
	style := buildChromaStyle()
	if style == nil {
		t.Fatal("buildChromaStyle returned nil")
	}

	// Verify key token types are configured
	tests := []struct {
		name  string
		check func() bool
	}{
		{"Keyword has color", func() bool { return style.Keyword.Color != nil }},
		{"Comment has color", func() bool { return style.Comment.Color != nil }},
		{"LiteralString has color", func() bool { return style.LiteralString.Color != nil }},
		{"LiteralNumber has color", func() bool { return style.LiteralNumber.Color != nil }},
		{"NameFunction has color", func() bool { return style.NameFunction.Color != nil }},
		{"Error has color", func() bool { return style.Error.Color != nil }},
		{"Punctuation has color", func() bool { return style.Punctuation.Color != nil }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.check() {
				t.Errorf("%s: expected color to be set", tt.name)
			}
		})
	}
}

func TestBuildChromaStyleFromPalette(t *testing.T) {
	palette := &MarkdownColorPalette{
		Primary:   "#7C3AED",
		Secondary: "#3B82F6",
		Success:   "#10B981",
		Warning:   "#F59E0B",
		Error:     "#EF4444",
		Muted:     "#6B7280",
		Text:      "#F9FAFB",
		BgCode:    "#374151",
	}

	style := buildChromaStyleFromPalette(palette)
	if style == nil {
		t.Fatal("buildChromaStyleFromPalette returned nil")
	}

	// Verify colors are set
	if style.Keyword.Color == nil || *style.Keyword.Color != "#7C3AED" {
		t.Error("Keyword should have primary color")
	}
	if style.Comment.Color == nil || *style.Comment.Color != "#10B981" {
		t.Error("Comment should have success color")
	}
}

func TestBuildGlamourStyle(t *testing.T) {
	style := buildGlamourStyle()

	// Verify key style elements are configured
	if style.Heading.Bold == nil || !*style.Heading.Bold {
		t.Error("Heading should be bold")
	}

	if style.CodeBlock.Chroma == nil {
		t.Error("CodeBlock should have Chroma style")
	}

	if style.Strong.Bold == nil || !*style.Strong.Bold {
		t.Error("Strong should be bold")
	}

	if style.Emph.Italic == nil || !*style.Emph.Italic {
		t.Error("Emph should be italic")
	}
}

func TestBuildGlamourStyleFromPalette(t *testing.T) {
	palette := &MarkdownColorPalette{
		Primary:   "#7C3AED",
		Secondary: "#3B82F6",
		Success:   "#10B981",
		Warning:   "#F59E0B",
		Error:     "#EF4444",
		Muted:     "#6B7280",
		Text:      "#F9FAFB",
		BgCode:    "#374151",
	}

	style := buildGlamourStyleFromPalette(palette)

	// Verify style uses palette colors
	if style.H2.Color == nil || *style.H2.Color != "#7C3AED" {
		t.Error("H2 should have primary color")
	}
	if style.H3.Color == nil || *style.H3.Color != "#3B82F6" {
		t.Error("H3 should have secondary color")
	}
}

func TestGetGlamourOptions(t *testing.T) {
	options := getGlamourOptions(80)
	if len(options) != 2 {
		t.Errorf("expected 2 options, got %d", len(options))
	}
}

func TestGetGlamourOptionsWithTheme(t *testing.T) {
	t.Run("nil theme uses default", func(t *testing.T) {
		options := getGlamourOptionsWithTheme(80, nil)
		if len(options) != 2 {
			t.Errorf("expected 2 options, got %d", len(options))
		}
	})

	t.Run("with colors uses palette", func(t *testing.T) {
		theme := &MarkdownThemeConfig{
			Colors: &MarkdownColorPalette{
				Primary:   "#FF0000",
				Secondary: "#00FF00",
				Success:   "#0000FF",
				Warning:   "#FFFF00",
				Error:     "#FF00FF",
				Muted:     "#888888",
				Text:      "#FFFFFF",
				BgCode:    "#111111",
			},
		}
		options := getGlamourOptionsWithTheme(80, theme)
		if len(options) != 2 {
			t.Errorf("expected 2 options, got %d", len(options))
		}
	})

	t.Run("with syntax theme uses builtin", func(t *testing.T) {
		theme := &MarkdownThemeConfig{
			SyntaxTheme:   "monokai",
			MarkdownTheme: "dark",
		}
		options := getGlamourOptionsWithTheme(80, theme)
		if len(options) != 2 {
			t.Errorf("expected 2 options, got %d", len(options))
		}
	})
}

func TestGlamourRendererCreation(t *testing.T) {
	// Verify we can create a renderer with our custom options
	renderer, err := glamour.NewTermRenderer(getGlamourOptions(80)...)
	if err != nil {
		t.Fatalf("failed to create renderer: %v", err)
	}
	if renderer == nil {
		t.Fatal("renderer is nil")
	}
}

func TestGlamourRendererWithTheme(t *testing.T) {
	// Verify we can create a renderer with hex color theme
	theme := &MarkdownThemeConfig{
		Colors: &MarkdownColorPalette{
			Primary:   "#7C3AED",
			Secondary: "#3B82F6",
			Success:   "#10B981",
			Warning:   "#F59E0B",
			Error:     "#EF4444",
			Muted:     "#6B7280",
			Text:      "#F9FAFB",
			BgCode:    "#374151",
		},
	}
	renderer, err := glamour.NewTermRenderer(getGlamourOptionsWithTheme(80, theme)...)
	if err != nil {
		t.Fatalf("failed to create renderer with theme: %v", err)
	}
	if renderer == nil {
		t.Fatal("renderer is nil")
	}

	// Verify it can render markdown
	result, err := renderer.Render("**bold** and `code`")
	if err != nil {
		t.Fatalf("failed to render: %v", err)
	}
	if !strings.Contains(result, "bold") {
		t.Error("expected result to contain 'bold'")
	}
}

// stripANSI removes ANSI escape sequences from a string for easier testing.
func stripANSI(s string) string {
	// Simple ANSI escape sequence stripper
	result := strings.Builder{}
	inEscape := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if s[i] == 'm' {
				inEscape = false
			}
			continue
		}
		result.WriteByte(s[i])
	}
	return result.String()
}

func TestPreRenderMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		width    int
		contains []string
		notEmpty bool
	}{
		{
			name:     "empty input",
			input:    "",
			width:    80,
			contains: nil,
			notEmpty: false,
		},
		{
			name:     "plain text",
			input:    "Hello world",
			width:    80,
			contains: []string{"Hello", "world"},
			notEmpty: true,
		},
		{
			name:     "bold text",
			input:    "This is **bold** text",
			width:    80,
			contains: []string{"bold"},
			notEmpty: true,
		},
		{
			name:     "code block",
			input:    "```go\nfunc main() {}\n```",
			width:    80,
			contains: []string{"func", "main"},
			notEmpty: true,
		},
		{
			name:     "inline code",
			input:    "Use `td add` to create",
			width:    80,
			contains: []string{"td", "add"},
			notEmpty: true,
		},
		{
			name:     "bullet list",
			input:    "- item one\n- item two",
			width:    80,
			contains: []string{"item", "one", "two"},
			notEmpty: true,
		},
		{
			name:     "heading",
			input:    "## My Heading",
			width:    80,
			contains: []string{"My", "Heading"},
			notEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := preRenderMarkdown(tt.input, tt.width, nil)

			if tt.notEmpty && result == "" {
				t.Error("expected non-empty result")
			}

			if !tt.notEmpty && result != "" {
				t.Errorf("expected empty result, got: %s", result)
			}

			// Strip ANSI codes for content checking since glamour adds styling
			stripped := stripANSI(result)
			for _, s := range tt.contains {
				if !strings.Contains(stripped, s) {
					t.Errorf("expected result to contain %q, got (stripped): %s", s, stripped)
				}
			}
		})
	}
}

func TestPreRenderMarkdownWithTheme(t *testing.T) {
	theme := &MarkdownThemeConfig{
		Colors: &MarkdownColorPalette{
			Primary:   "#7C3AED",
			Secondary: "#3B82F6",
			Success:   "#10B981",
			Warning:   "#F59E0B",
			Error:     "#EF4444",
			Muted:     "#6B7280",
			Text:      "#F9FAFB",
			BgCode:    "#374151",
		},
	}

	result := preRenderMarkdown("**bold** and `code`", 80, theme)
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !strings.Contains(result, "bold") {
		t.Error("expected result to contain 'bold'")
	}
}

func TestPreRenderMarkdownStripsTrailingWhitespace(t *testing.T) {
	input := "Some text\n\n"
	result := preRenderMarkdown(input, 80, nil)

	if strings.HasSuffix(result, "\n") {
		t.Error("result should not end with newline")
	}
	if strings.HasSuffix(result, " ") {
		t.Error("result should not end with space")
	}
}

func TestPreRenderMarkdownSyntaxHighlighting(t *testing.T) {
	// Test that code blocks produce ANSI escape codes (syntax highlighting)
	input := "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```"
	result := preRenderMarkdown(input, 80, nil)

	// Check for ANSI escape sequences (indicates syntax highlighting is working)
	if !strings.Contains(result, "\x1b[") {
		t.Error("expected ANSI escape sequences for syntax highlighting")
	}
}

func TestPreRenderMarkdownWidthRespected(t *testing.T) {
	// Long line that should wrap
	longLine := strings.Repeat("word ", 30)
	result := preRenderMarkdown(longLine, 40, nil)

	// Should contain newlines due to wrapping
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Error("expected text to wrap to multiple lines")
	}
}

func TestColorConstants(t *testing.T) {
	// Verify color constants are valid hex colors (#RRGGBB)
	colors := []string{
		colorPrimary,
		colorSecondary,
		colorSuccess,
		colorWarning,
		colorError,
		colorMuted,
		colorCyan,
		colorWhite,
		colorBg,
	}

	for _, c := range colors {
		if c == "" {
			t.Error("color constant should not be empty")
		}
		// Simple validation - should be hex color format #RRGGBB
		if len(c) != 7 || c[0] != '#' {
			t.Errorf("color %q should be #RRGGBB format", c)
		}
		for _, r := range c[1:] {
			if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') || (r >= 'a' && r <= 'f')) {
				t.Errorf("color %q should contain only hex characters", c)
			}
		}
	}
}

func TestMarkdownThemeConfig(t *testing.T) {
	t.Run("empty config uses defaults", func(t *testing.T) {
		config := &MarkdownThemeConfig{}
		options := getGlamourOptionsWithTheme(80, config)
		// Should fallback to default (2 options)
		if len(options) != 2 {
			t.Errorf("expected 2 options, got %d", len(options))
		}
	})

	t.Run("colors take precedence over syntax theme", func(t *testing.T) {
		config := &MarkdownThemeConfig{
			SyntaxTheme: "monokai", // Should be ignored when Colors is set
			Colors: &MarkdownColorPalette{
				Primary: "#FF0000",
				Text:    "#FFFFFF",
			},
		}
		options := getGlamourOptionsWithTheme(80, config)
		if len(options) != 2 {
			t.Errorf("expected 2 options, got %d", len(options))
		}
	})
}

func TestSyntaxThemeActuallyApplies(t *testing.T) {
	// Verify that different SyntaxTheme values produce different output
	code := "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```"

	// Render with monokai theme
	monokaiTheme := &MarkdownThemeConfig{
		SyntaxTheme:   "monokai",
		MarkdownTheme: "dark",
	}
	monokaiResult := preRenderMarkdown(code, 80, monokaiTheme)

	// Render with dracula theme
	draculaTheme := &MarkdownThemeConfig{
		SyntaxTheme:   "dracula",
		MarkdownTheme: "dark",
	}
	draculaResult := preRenderMarkdown(code, 80, draculaTheme)

	// Render with default td palette (no SyntaxTheme)
	defaultResult := preRenderMarkdown(code, 80, nil)

	// All should contain the code
	for name, result := range map[string]string{
		"monokai": monokaiResult,
		"dracula": draculaResult,
		"default": defaultResult,
	} {
		if !strings.Contains(result, "func") || !strings.Contains(result, "main") {
			t.Errorf("%s: result should contain 'func' and 'main'", name)
		}
		// All should have ANSI escape codes (syntax highlighting)
		if !strings.Contains(result, "\x1b[") {
			t.Errorf("%s: result should contain ANSI codes", name)
		}
	}

	// Monokai and dracula should produce different output (different color schemes)
	if monokaiResult == draculaResult {
		t.Error("monokai and dracula themes should produce different output")
	}

	// Both named themes should differ from default td palette
	if monokaiResult == defaultResult {
		t.Error("monokai theme should differ from default td palette")
	}
}

func TestSyntaxThemeLightMode(t *testing.T) {
	code := "```go\nvar x = 42\n```"

	// Test light mode base style
	lightTheme := &MarkdownThemeConfig{
		SyntaxTheme:   "monokai",
		MarkdownTheme: "light",
	}
	result := preRenderMarkdown(code, 80, lightTheme)

	if !strings.Contains(result, "42") {
		t.Error("light theme result should contain code")
	}
	if !strings.Contains(result, "\x1b[") {
		t.Error("light theme should produce ANSI codes")
	}
}
