package themecheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanMonitorDetectsFrozenAndRawColors(t *testing.T) {
	root := fixture(t, `package monitor

import "charm.land/lipgloss/v2"

func derived() lipgloss.Style { return lipgloss.NewStyle() }

var frozen = derived()

func render() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
}
`)
	findings, err := ScanMonitor(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := formatFindings(findings)
	for _, want := range []string{"frozen theme", "raw color"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("guard missed %q: %s", want, joined)
		}
	}
}

func TestScanMonitorAllowsRuntimeThemeAndDefaultDefinitions(t *testing.T) {
	root := fixture(t, `package monitor

import "charm.land/lipgloss/v2"

type Theme struct { Error string }

func render(theme Theme) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Error))
}
`)
	if err := os.WriteFile(filepath.Join(root, "pkg", "monitor", "theme.go"), []byte(`package monitor

import "charm.land/lipgloss/v2"

const defaultError = "19" + "6"

func defaultColor(value string) lipgloss.Color { return lipgloss.Color(value) }
func DefaultTheme() lipgloss.Color { return defaultColor(defaultError) }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := ScanMonitor(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("guard rejected runtime/default derivation: %s", formatFindings(findings))
	}
}

func TestScanMonitorRejectsStaticAndWrapperRawColorBypasses(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "constant expression",
			source: `package monitor

import "charm.land/lipgloss/v2"

const red = "19" + "6"

func render() lipgloss.Color { return lipgloss.Color(red) }
`,
		},
		{
			name: "local constant",
			source: `package monitor

import "charm.land/lipgloss/v2"

func render() lipgloss.Color {
	const red = "196"
	return lipgloss.Color(red)
}
`,
		},
		{
			name: "color wrapper",
			source: `package monitor

import "charm.land/lipgloss/v2"

func color(value string) lipgloss.Color { return lipgloss.Color(value) }
func render() lipgloss.Color { return color("196") }
`,
		},
		{
			name: "transitive wrapper and static function",
			source: `package monitor

import "charm.land/lipgloss/v2"

const base = "196"

func raw() string { return base }
func color(value string) lipgloss.Color { return lipgloss.Color(value) }
func semantic(value string) lipgloss.Color { return color(value) }
func render() lipgloss.Color { return semantic(raw()) }
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings, err := ScanMonitor(fixture(t, tt.source))
			if err != nil {
				t.Fatal(err)
			}
			if joined := formatFindings(findings); !strings.Contains(joined, "raw color") {
				t.Fatalf("guard missed raw-color bypass:\n%s\nfindings:\n%s", tt.source, joined)
			}
		})
	}
}

func fixture(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "pkg", "monitor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "view.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func formatFindings(findings []Finding) string {
	parts := make([]string, len(findings))
	for i := range findings {
		parts[i] = findings[i].String()
	}
	return strings.Join(parts, "\n")
}
