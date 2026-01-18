package monitor

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

// MarkdownThemeConfig configures markdown rendering theme.
// Used by embedders (like sidecar) to share their theme with td.
type MarkdownThemeConfig struct {
	// SyntaxTheme is a Chroma theme name (e.g., "monokai", "dracula", "github-dark").
	// When set, uses glamour's built-in style with this syntax theme.
	// See https://xyproto.github.io/splash/docs/ for available themes.
	SyntaxTheme string

	// MarkdownTheme is a glamour base theme ("dark" or "light").
	// Only used when SyntaxTheme is set.
	MarkdownTheme string

	// Colors provides explicit hex colors for custom styling.
	// When set (Primary != ""), builds a custom style from these colors.
	// Takes precedence over SyntaxTheme/MarkdownTheme.
	Colors *MarkdownColorPalette
}

// MarkdownColorPalette holds hex colors for markdown styling.
// Colors should be in #RRGGBB format.
type MarkdownColorPalette struct {
	Primary   string // Keywords, functions, links - e.g., "#7C3AED"
	Secondary string // Strings, secondary headers - e.g., "#3B82F6"
	Success   string // Comments - e.g., "#10B981"
	Warning   string // Numbers, literals, inline code - e.g., "#F59E0B"
	Error     string // Errors, deleted - e.g., "#EF4444"
	Muted     string // Punctuation, subtle text - e.g., "#6B7280"
	Text      string // Primary text - e.g., "#F9FAFB"
	BgCode    string // Code block background - e.g., "#374151"
}

// Default hex colors for td monitor palette (used when no theme config).
// These are the hex equivalents of the ANSI 256 colors used in styles.go.
const (
	colorPrimary   = "#FF87D7" // Purple/magenta (ANSI 212) - keywords, functions
	colorSecondary = "#AF87FF" // Light purple (ANSI 141) - strings
	colorSuccess   = "#00D787" // Green (ANSI 42) - comments
	colorWarning   = "#FFAF00" // Orange (ANSI 214) - numbers, literals
	colorError     = "#FF0000" // Red (ANSI 196) - errors
	colorMuted     = "#626262" // Gray (ANSI 241) - punctuation
	colorCyan      = "#00D7FF" // Cyan (ANSI 45) - types, names
	colorWhite     = "#EEEEEE" // White (ANSI 255) - text
	colorBg        = "#3A3A3A" // Dark gray (ANSI 237) - background
)

// ptrString returns a pointer to the given string.
func ptrString(s string) *string { return &s }

// ptrBool returns a pointer to the given bool.
func ptrBool(b bool) *bool { return &b }

// buildChromaStyle creates a glamour Chroma style using ANSI 256 colors (td default).
func buildChromaStyle() *ansi.Chroma {
	return &ansi.Chroma{
		// Keywords: purple/magenta (primary)
		Keyword:          ansi.StylePrimitive{Color: ptrString(colorPrimary), Bold: ptrBool(true)},
		KeywordReserved:  ansi.StylePrimitive{Color: ptrString(colorPrimary), Bold: ptrBool(true)},
		KeywordNamespace: ansi.StylePrimitive{Color: ptrString(colorPrimary)},
		KeywordType:      ansi.StylePrimitive{Color: ptrString(colorCyan)},

		// Strings: light purple (secondary)
		LiteralString:       ansi.StylePrimitive{Color: ptrString(colorSecondary)},
		LiteralStringEscape: ansi.StylePrimitive{Color: ptrString(colorWarning)},

		// Numbers: orange (warning)
		LiteralNumber: ansi.StylePrimitive{Color: ptrString(colorWarning)},
		LiteralDate:   ansi.StylePrimitive{Color: ptrString(colorWarning)},
		Literal:       ansi.StylePrimitive{Color: ptrString(colorWarning)},

		// Comments: green (success)
		Comment:        ansi.StylePrimitive{Color: ptrString(colorSuccess), Italic: ptrBool(true)},
		CommentPreproc: ansi.StylePrimitive{Color: ptrString(colorSuccess)},

		// Names/identifiers: cyan and white
		Name:          ansi.StylePrimitive{Color: ptrString(colorWhite)},
		NameBuiltin:   ansi.StylePrimitive{Color: ptrString(colorCyan)},
		NameClass:     ansi.StylePrimitive{Color: ptrString(colorCyan), Bold: ptrBool(true)},
		NameConstant:  ansi.StylePrimitive{Color: ptrString(colorWarning)},
		NameDecorator: ansi.StylePrimitive{Color: ptrString(colorPrimary)},
		NameException: ansi.StylePrimitive{Color: ptrString(colorError)},
		NameFunction:  ansi.StylePrimitive{Color: ptrString(colorPrimary)},
		NameTag:       ansi.StylePrimitive{Color: ptrString(colorPrimary)},
		NameAttribute: ansi.StylePrimitive{Color: ptrString(colorCyan)},
		NameOther:     ansi.StylePrimitive{Color: ptrString(colorWhite)},

		// Operators and punctuation: muted
		Operator:    ansi.StylePrimitive{Color: ptrString(colorWhite)},
		Punctuation: ansi.StylePrimitive{Color: ptrString(colorMuted)},

		// Errors: red
		Error: ansi.StylePrimitive{Color: ptrString(colorError), Bold: ptrBool(true)},

		// Generic diffs
		GenericDeleted:  ansi.StylePrimitive{Color: ptrString(colorError)},
		GenericInserted: ansi.StylePrimitive{Color: ptrString(colorSuccess)},
		GenericEmph:     ansi.StylePrimitive{Italic: ptrBool(true)},
		GenericStrong:   ansi.StylePrimitive{Bold: ptrBool(true)},

		// Text and background
		Text:       ansi.StylePrimitive{Color: ptrString(colorWhite)},
		Background: ansi.StylePrimitive{BackgroundColor: ptrString(colorBg)},
	}
}

// buildChromaStyleFromPalette creates a glamour Chroma style using hex colors.
func buildChromaStyleFromPalette(p *MarkdownColorPalette) *ansi.Chroma {
	return &ansi.Chroma{
		// Keywords: primary color
		Keyword:          ansi.StylePrimitive{Color: ptrString(p.Primary), Bold: ptrBool(true)},
		KeywordReserved:  ansi.StylePrimitive{Color: ptrString(p.Primary), Bold: ptrBool(true)},
		KeywordNamespace: ansi.StylePrimitive{Color: ptrString(p.Primary)},
		KeywordType:      ansi.StylePrimitive{Color: ptrString(p.Secondary)},

		// Strings: secondary color
		LiteralString:       ansi.StylePrimitive{Color: ptrString(p.Secondary)},
		LiteralStringEscape: ansi.StylePrimitive{Color: ptrString(p.Warning)},

		// Numbers: warning color
		LiteralNumber: ansi.StylePrimitive{Color: ptrString(p.Warning)},
		LiteralDate:   ansi.StylePrimitive{Color: ptrString(p.Warning)},
		Literal:       ansi.StylePrimitive{Color: ptrString(p.Warning)},

		// Comments: success color
		Comment:        ansi.StylePrimitive{Color: ptrString(p.Success), Italic: ptrBool(true)},
		CommentPreproc: ansi.StylePrimitive{Color: ptrString(p.Success)},

		// Names/identifiers
		Name:          ansi.StylePrimitive{Color: ptrString(p.Text)},
		NameBuiltin:   ansi.StylePrimitive{Color: ptrString(p.Secondary)},
		NameClass:     ansi.StylePrimitive{Color: ptrString(p.Secondary), Bold: ptrBool(true)},
		NameConstant:  ansi.StylePrimitive{Color: ptrString(p.Warning)},
		NameDecorator: ansi.StylePrimitive{Color: ptrString(p.Primary)},
		NameException: ansi.StylePrimitive{Color: ptrString(p.Error)},
		NameFunction:  ansi.StylePrimitive{Color: ptrString(p.Primary)},
		NameTag:       ansi.StylePrimitive{Color: ptrString(p.Primary)},
		NameAttribute: ansi.StylePrimitive{Color: ptrString(p.Secondary)},
		NameOther:     ansi.StylePrimitive{Color: ptrString(p.Text)},

		// Operators and punctuation
		Operator:    ansi.StylePrimitive{Color: ptrString(p.Text)},
		Punctuation: ansi.StylePrimitive{Color: ptrString(p.Muted)},

		// Errors
		Error: ansi.StylePrimitive{Color: ptrString(p.Error), Bold: ptrBool(true)},

		// Generic diffs
		GenericDeleted:  ansi.StylePrimitive{Color: ptrString(p.Error)},
		GenericInserted: ansi.StylePrimitive{Color: ptrString(p.Success)},
		GenericEmph:     ansi.StylePrimitive{Italic: ptrBool(true)},
		GenericStrong:   ansi.StylePrimitive{Bold: ptrBool(true)},

		// Text and background
		Text:       ansi.StylePrimitive{Color: ptrString(p.Text)},
		Background: ansi.StylePrimitive{BackgroundColor: ptrString(p.BgCode)},
	}
}

// buildGlamourStyle creates a complete glamour StyleConfig for modal markdown rendering.
func buildGlamourStyle() ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: ptrString(colorWhite),
			},
			Margin: uintPtr(0),
		},
		BlockQuote: ansi.StyleBlock{
			Indent:      uintPtr(1),
			IndentToken: ptrString("│ "),
			StylePrimitive: ansi.StylePrimitive{
				Color:  ptrString(colorMuted),
				Italic: ptrBool(true),
			},
		},
		Paragraph: ansi.StyleBlock{},
		List: ansi.StyleList{
			LevelIndent: 2,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:       ptrString(colorWhite),
				Bold:        ptrBool(true),
				BlockSuffix: "\n",
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           ptrString(colorWhite),
				BackgroundColor: ptrString(colorPrimary),
				Bold:            ptrBool(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
				Color:  ptrString(colorPrimary),
				Bold:   ptrBool(true),
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
				Color:  ptrString(colorSecondary),
				Bold:   ptrBool(true),
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "#### ",
				Color:  ptrString(colorCyan),
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "##### ",
				Color:  ptrString(colorMuted),
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "###### ",
				Color:  ptrString(colorMuted),
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: ptrBool(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: ptrBool(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: ptrBool(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  ptrString(colorMuted),
			Format: "\n────────\n",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     ptrString(colorCyan),
			Underline: ptrBool(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: ptrString(colorPrimary),
			Bold:  ptrBool(true),
		},
		Image: ansi.StylePrimitive{
			Color:     ptrString(colorPrimary),
			Underline: ptrBool(true),
		},
		ImageText: ansi.StylePrimitive{
			Color:  ptrString(colorSecondary),
			Format: "Image: {{.text}}",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           ptrString(colorWarning),
				BackgroundColor: ptrString(colorBg),
				Prefix:          " ",
				Suffix:          " ",
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: ptrString(colorWhite),
				},
				Margin: uintPtr(0),
			},
			Chroma: buildChromaStyle(),
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
			CenterSeparator: ptrString("┼"),
			ColumnSeparator: ptrString("│"),
			RowSeparator:    ptrString("─"),
		},
		DefinitionTerm: ansi.StylePrimitive{
			Bold: ptrBool(true),
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "  ",
		},
	}
}

// buildGlamourStyleFromPalette creates a glamour StyleConfig using hex colors.
func buildGlamourStyleFromPalette(p *MarkdownColorPalette) ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: ptrString(p.Text),
			},
			Margin: uintPtr(0),
		},
		BlockQuote: ansi.StyleBlock{
			Indent:      uintPtr(1),
			IndentToken: ptrString("│ "),
			StylePrimitive: ansi.StylePrimitive{
				Color:  ptrString(p.Muted),
				Italic: ptrBool(true),
			},
		},
		Paragraph: ansi.StyleBlock{},
		List: ansi.StyleList{
			LevelIndent: 2,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:       ptrString(p.Text),
				Bold:        ptrBool(true),
				BlockSuffix: "\n",
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           ptrString(p.Text),
				BackgroundColor: ptrString(p.Primary),
				Bold:            ptrBool(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
				Color:  ptrString(p.Primary),
				Bold:   ptrBool(true),
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
				Color:  ptrString(p.Secondary),
				Bold:   ptrBool(true),
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "#### ",
				Color:  ptrString(p.Secondary),
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "##### ",
				Color:  ptrString(p.Muted),
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "###### ",
				Color:  ptrString(p.Muted),
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: ptrBool(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: ptrBool(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: ptrBool(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  ptrString(p.Muted),
			Format: "\n────────\n",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     ptrString(p.Secondary),
			Underline: ptrBool(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: ptrString(p.Primary),
			Bold:  ptrBool(true),
		},
		Image: ansi.StylePrimitive{
			Color:     ptrString(p.Primary),
			Underline: ptrBool(true),
		},
		ImageText: ansi.StylePrimitive{
			Color:  ptrString(p.Secondary),
			Format: "Image: {{.text}}",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           ptrString(p.Warning),
				BackgroundColor: ptrString(p.BgCode),
				Prefix:          " ",
				Suffix:          " ",
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: ptrString(p.Text),
				},
				Margin: uintPtr(0),
			},
			Chroma: buildChromaStyleFromPalette(p),
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
			CenterSeparator: ptrString("┼"),
			ColumnSeparator: ptrString("│"),
			RowSeparator:    ptrString("─"),
		},
		DefinitionTerm: ansi.StylePrimitive{
			Bold: ptrBool(true),
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "  ",
		},
	}
}

// uintPtr returns a pointer to the given uint.
func uintPtr(u uint) *uint { return &u }

// getGlamourOptions returns glamour renderer options with custom td monitor styling.
// Uses default td palette (ANSI 256 colors).
func getGlamourOptions(width int) []glamour.TermRendererOption {
	return []glamour.TermRendererOption{
		glamour.WithStyles(buildGlamourStyle()),
		glamour.WithWordWrap(width),
	}
}

// getGlamourOptionsWithTheme returns glamour renderer options using the provided theme config.
// Priority: Colors > SyntaxTheme/MarkdownTheme > default td palette.
func getGlamourOptionsWithTheme(width int, theme *MarkdownThemeConfig) []glamour.TermRendererOption {
	// No theme config: use default td palette
	if theme == nil {
		return getGlamourOptions(width)
	}

	// Explicit colors provided: build custom style from palette
	if theme.Colors != nil && theme.Colors.Primary != "" {
		return []glamour.TermRendererOption{
			glamour.WithStyles(buildGlamourStyleFromPalette(theme.Colors)),
			glamour.WithWordWrap(width),
		}
	}

	// Chroma theme name provided: use named Chroma style for syntax highlighting
	if theme.SyntaxTheme != "" {
		// Get base style (dark or light) and modify CodeBlock.Theme
		var style ansi.StyleConfig
		if theme.MarkdownTheme == "light" {
			style = styles.LightStyleConfig
		} else {
			style = styles.DarkStyleConfig
		}
		// Set Chroma theme name and clear embedded Chroma so Theme takes effect
		style.CodeBlock.Theme = theme.SyntaxTheme
		style.CodeBlock.Chroma = nil
		return []glamour.TermRendererOption{
			glamour.WithStyles(style),
			glamour.WithWordWrap(width),
		}
	}

	// Fallback to default td palette
	return getGlamourOptions(width)
}
