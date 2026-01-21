package output

import (
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

const (
	defaultMarkdownWidth = 80
	minMarkdownWidth     = 20
)

// TerminalWidth returns the current terminal width or a fallback when unavailable.
func TerminalWidth(fallback int) int {
	if fallback <= 0 {
		fallback = defaultMarkdownWidth
	}

	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 {
		return width
	}

	if cols := os.Getenv("COLUMNS"); cols != "" {
		if parsed, err := strconv.Atoi(cols); err == nil && parsed > 0 {
			return parsed
		}
	}

	return fallback
}

// RenderMarkdown renders markdown using Glamour with terminal-aware wrapping.
func RenderMarkdown(text string) (string, error) {
	return RenderMarkdownWithWidth(text, TerminalWidth(defaultMarkdownWidth))
}

// RenderMarkdownWithWidth renders markdown using Glamour with explicit wrapping.
func RenderMarkdownWithWidth(text string, width int) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	if width < minMarkdownWidth {
		width = minMarkdownWidth
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}

	rendered, err := renderer.Render(text)
	if err != nil {
		return "", err
	}

	return strings.TrimRight(rendered, "\n"), nil
}
