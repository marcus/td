package ansifill

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestLinesKeepsSurfaceSolidBehindNestedStyles(t *testing.T) {
	bg := Code("#101020")
	nested := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Render("RED")
	got := Lines(nested+" plain", bg, 0)

	if !strings.HasPrefix(got, bg) {
		t.Fatalf("line does not start on the surface: %q", got)
	}
	// The nested style's reset must be followed by the surface again, so the
	// text after it is not painted on the terminal default background.
	reset := strings.Index(got, "\x1b[m")
	if reset < 0 {
		t.Fatalf("expected nested reset in %q", got)
	}
	if !strings.HasPrefix(got[reset:], "\x1b[m"+bg) {
		t.Fatalf("surface not restored after nested reset: %q", got)
	}
}

func TestLinesPreservesNestedBackgrounds(t *testing.T) {
	bg := Code("#101020")
	selected := lipgloss.NewStyle().
		Background(lipgloss.Color("#445566")).
		Foreground(lipgloss.Color("#ffffff")).
		Render("selected")
	got := Lines(selected, bg, 0)

	if strings.Contains(got, selectionSGR(selected)+bg) {
		t.Fatalf("surface clobbered the nested background: %q", got)
	}
	if !strings.Contains(got, "48;2;68;85;102") {
		t.Fatalf("nested background lost: %q", got)
	}
}

// selectionSGR returns the leading SGR sequence of a rendered fragment.
func selectionSGR(rendered string) string {
	end := strings.Index(rendered, "m")
	return rendered[:end+1]
}

func TestLinesPadsToWidthWithSurface(t *testing.T) {
	bg := Code("#101020")
	got := Lines("hi", bg, 10)
	if width := ansi.StringWidth(got); width != 10 {
		t.Fatalf("padded width = %d, want 10", width)
	}
	if !strings.HasSuffix(got, "        \x1b[0m") {
		t.Fatalf("padding not inside the surface run: %q", got)
	}
}

func TestLinesWithoutBackgroundIsIdentity(t *testing.T) {
	if got := Lines("plain", "", 20); got != "plain" {
		t.Fatalf("Lines with no background changed content: %q", got)
	}
}

func TestResetsBackgroundIgnoresForegroundComponents(t *testing.T) {
	// 38;2;44;0;5 is a truecolor foreground whose green component looks like
	// the SGR code for a green background.
	if !resetsBackground("\x1b[38;2;44;0;5m") {
		t.Fatal("truecolor foreground misread as setting a background")
	}
	if resetsBackground("\x1b[48;5;237m") {
		t.Fatal("256-color background misread as clearing the background")
	}
}
