package modal

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/pkg/monitor/ansifill"

	"github.com/marcus/td/pkg/monitor/mouse"
)

func TestNew(t *testing.T) {
	m := New("Test Modal")
	if m.title != "Test Modal" {
		t.Errorf("expected title 'Test Modal', got %q", m.title)
	}
	if m.width != DefaultWidth {
		t.Errorf("expected default width %d, got %d", DefaultWidth, m.width)
	}
	if m.variant != VariantDefault {
		t.Errorf("expected VariantDefault, got %v", m.variant)
	}
	if !m.closeOnBackdrop {
		t.Errorf("expected closeOnBackdrop true, got %v", m.closeOnBackdrop)
	}
}

func TestNewWithOptions(t *testing.T) {
	m := New("Test",
		WithWidth(60),
		WithVariant(VariantDanger),
		WithHints(false),
		WithPrimaryAction("submit"),
		WithCloseOnBackdropClick(false),
	)

	if m.width != 60 {
		t.Errorf("expected width 60, got %d", m.width)
	}
	if m.variant != VariantDanger {
		t.Errorf("expected VariantDanger, got %v", m.variant)
	}
	if m.showHints != false {
		t.Errorf("expected showHints false, got %v", m.showHints)
	}
	if m.primaryAction != "submit" {
		t.Errorf("expected primaryAction 'submit', got %q", m.primaryAction)
	}
	if m.closeOnBackdrop {
		t.Errorf("expected closeOnBackdrop false, got %v", m.closeOnBackdrop)
	}
}

func TestDefaultThemePreservesStandaloneStyles(t *testing.T) {
	m := New("Default")
	wantBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("212")).
		Background(lipgloss.Color("235")).
		Padding(1, 2).
		Width(DefaultWidth)
	if got, want := m.modalStyle(DefaultWidth).Render("body"), wantBox.Render("body"); got != want {
		t.Fatalf("default modal box appearance changed:\n got %q\nwant %q", got, want)
	}
	if got, want := m.renderTitleLine("Default"), lipgloss.NewStyle().Bold(true).Render("Default"); got != want {
		t.Fatalf("default modal title appearance changed: got %q, want %q", got, want)
	}
	if got, want := m.styles.body.Render("body"), "body"; got != want {
		t.Fatalf("default body appearance changed: got %q, want %q", got, want)
	}
}

func TestInstanceThemeCoversVariantsAndBuiltInSections(t *testing.T) {
	theme := Theme{
		Primary: "#110001", Warning: "#220002", Error: "#330003", Info: "#440004",
		TextPrimary: "#550005", TextSecondary: "#660006", TextMuted: "#770007",
		TextSelection: "#880008", OnPrimary: "#990009", OnError: "#aa000a",
		Surface: "#bb000b", SurfaceRaised: "#cc000c", Selection: "#dd000d",
		Border: "#ee000e", BorderMuted: "#ff000f",
	}
	variants := []struct {
		name    string
		variant Variant
		color   string
	}{
		{"default", VariantDefault, theme.Primary},
		{"warning", VariantWarning, theme.Warning},
		{"danger", VariantDanger, theme.Error},
		{"info", VariantInfo, theme.Info},
	}
	for _, tt := range variants {
		t.Run(tt.name, func(t *testing.T) {
			cursor := 0
			m := New("Themed", WithTheme(theme), WithVariant(tt.variant)).
				AddSection(Text("body")).
				AddSection(List("items", []ListItem{{ID: "one", Label: "selected"}}, &cursor)).
				AddSection(Buttons(Btn(" Delete ", "delete", BtnDanger())))
			// The renderer paints the surface behind every cell; strip it so the
			// themed fragments below can be matched verbatim.
			got := strings.ReplaceAll(m.Render(80, 24, nil), ansifill.Code(theme.Surface), "")
			for name, want := range map[string]string{
				"variant": lipgloss.NewStyle().Foreground(lipgloss.Color(tt.color)).Render("X"),
				"body":    m.styles.body.Render("body"),
				"list":    m.styles.listItemFocused.Render("selected"),
			} {
				prefix := strings.Split(want, "X")[0]
				if name != "variant" {
					prefix = want
				}
				if !strings.Contains(got, prefix) {
					t.Errorf("render missing themed %s sequence %q: %q", name, prefix, got)
				}
			}
			buttonCore := lipgloss.NewStyle().
				Foreground(lipgloss.Color(theme.TextSecondary)).
				Background(lipgloss.Color(theme.SurfaceRaised)).
				Render(" Delete ")
			if !strings.Contains(got, buttonCore) {
				t.Errorf("render missing themed danger button core %q: %q", buttonCore, got)
			}
		})
	}
}

func TestModalThemesAreIsolatedAndRethemePreservesState(t *testing.T) {
	input := textinput.New()
	input.SetValue("draft")
	first := New("First", WithTheme(Theme{Primary: "#120001", Surface: "#120002"}), WithHints(false)).
		AddSection(Input("name", &input)).
		AddSection(Buttons(Btn(" Save ", "save")))
	first.Render(60, 16, nil)
	first.SetFocus("save")
	first.Scroll(4)
	beforeFocus, beforeScroll := first.FocusedID(), first.ScrollOffset()

	second := New("Second", WithTheme(Theme{Primary: "#210001", Surface: "#210002"}), WithHints(false)).
		AddSection(Text("body"))
	firstBefore := first.Render(60, 16, nil)
	first.Scroll(4)
	beforeScroll = first.ScrollOffset()
	secondOutput := second.Render(60, 16, nil)
	if firstBefore == secondOutput {
		t.Fatal("two modal instances with different themes rendered identically")
	}

	first.SetTheme(Theme{Primary: "#310001", Surface: "#310002", TextPrimary: "#310003"})
	if got := first.InputValue("name"); got != "draft" {
		t.Fatalf("retheme changed input value: %q", got)
	}
	if first.FocusedID() != beforeFocus || first.ScrollOffset() != beforeScroll {
		t.Fatalf("retheme changed modal state: focus %q->%q scroll %d->%d", beforeFocus, first.FocusedID(), beforeScroll, first.ScrollOffset())
	}
	firstAfter := first.Render(60, 16, nil)
	if firstAfter == firstBefore {
		t.Fatal("SetTheme did not repaint the existing modal")
	}
	if got := second.Render(60, 16, nil); got != secondOutput {
		t.Fatal("retheming first modal contaminated second modal")
	}
}

func TestHostChromeContractMatchesSidecarGeometryAndHitRegions(t *testing.T) {
	theme := Theme{Primary: "#110001", TextPrimary: "#220002", Surface: "#330003"}
	var gotWidth, gotHeight, gotContentWidth, gotContentHeight int
	renderer := func(content string, width, height int) string {
		gotWidth, gotHeight = width, height
		gotContentWidth, gotContentHeight = lipgloss.Width(content), lipgloss.Height(content)
		// Sidecar-equivalent geometry: outer dimensions include one border cell
		// and the host adds padding=1 around the supplied content.
		lines := strings.Split(content, "\n")
		innerHeight := height - 2
		contentWidth := width - 4
		var out strings.Builder
		out.WriteString("+" + strings.Repeat("-", width-2) + "+\n")
		for i := 0; i < innerHeight; i++ {
			line := ""
			if i < len(lines) {
				line = lines[i]
			}
			lineWidth := lipgloss.Width(line)
			if lineWidth < contentWidth {
				line += strings.Repeat(" ", contentWidth-lineWidth)
			}
			out.WriteString("| " + line + " |")
			if i < innerHeight-1 {
				out.WriteByte('\n')
			}
		}
		out.WriteString("\n+" + strings.Repeat("-", width-2) + "+")
		return out.String()
	}

	input := textinput.New()
	m := New("Hosted", WithWidth(50), WithHints(false), WithTheme(theme), WithChromeRenderer(renderer)).
		AddSection(Custom(func(contentWidth int, focusID, hoverID string) RenderedSection {
			return RenderedSection{Content: "RAW CUSTOM"}
		}, nil)).
		AddSection(Input("name", &input))
	handler := mouse.NewHandler()
	got := m.Render(100, 30, handler)
	if lipgloss.Width(got) != 50 || gotWidth != 50 {
		t.Fatalf("hosted WithWidth geometry = output %d callback %d, want 50", lipgloss.Width(got), gotWidth)
	}
	if lipgloss.Height(got) != gotHeight || gotContentWidth != 46 || gotContentHeight != gotHeight-2 {
		t.Fatalf("host geometry mismatch: output h=%d callback=%dx%d content=%dx%d", lipgloss.Height(got), gotWidth, gotHeight, gotContentWidth, gotContentHeight)
	}
	surfaceFragment := "48;2;51;0;3"
	if !strings.Contains(got, surfaceFragment) || !strings.Contains(got, "RAW CUSTOM") {
		t.Fatalf("host lost td-owned Surface/custom content styling: %q", got)
	}
	for _, line := range strings.Split(ansi.Strip(got), "\n") {
		if strings.Contains(line, "RAW CUSTOM") && strings.Index(line, "RAW CUSTOM") != 3 {
			t.Fatalf("custom content starts at column %d, want 3 (border + host padding + td padding): %q", strings.Index(line, "RAW CUSTOM"), line)
		}
	}

	modalX := (100 - 50) / 2
	var body, field *mouse.Region
	for _, region := range handler.HitMap.Regions() {
		switch region.ID {
		case "modal-body":
			copy := region
			body = &copy
		case "name":
			copy := region
			field = &copy
		}
	}
	if body == nil || body.Rect.X != modalX || body.Rect.W != 50 || body.Rect.H != gotHeight {
		t.Fatalf("modal body region not aligned with host output: %#v", body)
	}
	if field == nil || field.Rect.X != modalX+3 || !body.Rect.Contains(field.Rect.X, field.Rect.Y) {
		t.Fatalf("input region not aligned with content inset/body: body=%#v input=%#v", body, field)
	}
	if clicked := handler.HandleClick(field.Rect.X, field.Rect.Y).Region; clicked == nil || clicked.ID != "name" {
		t.Fatalf("click at rendered input did not hit input region: %#v", clicked)
	}
}

func TestThemedCustomReceivesLiveInstanceTheme(t *testing.T) {
	m := New("Custom", WithTheme(Theme{Primary: "#110001"}), WithHints(false)).
		AddSection(ThemedCustom(func(contentWidth int, focusID, hoverID string, theme Theme) RenderedSection {
			return RenderedSection{Content: lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Primary)).Render("custom")}
		}, nil))
	before := m.Render(50, 16, nil)
	m.SetTheme(Theme{Primary: "#220002"})
	after := m.Render(50, 16, nil)
	if before == after || !strings.Contains(after, "38;2;34;0;2") || strings.Contains(after, "38;2;17;0;1") {
		t.Fatalf("themed custom section did not receive live theme:\n before %q\n after %q", before, after)
	}
}

func TestAddSection(t *testing.T) {
	m := New("Test").
		AddSection(Text("Hello")).
		AddSection(Spacer()).
		AddSection(Text("World"))

	if len(m.sections) != 3 {
		t.Errorf("expected 3 sections, got %d", len(m.sections))
	}
}

func TestTextSection(t *testing.T) {
	s := Text("Hello World")
	res := s.Render(80, "", "")

	if !strings.Contains(res.Content, "Hello World") {
		t.Errorf("expected content to contain 'Hello World', got %q", res.Content)
	}
	if len(res.Focusables) != 0 {
		t.Errorf("expected no focusables, got %d", len(res.Focusables))
	}
}

func TestSpacerSection(t *testing.T) {
	s := Spacer()
	res := s.Render(80, "", "")

	if res.Content != " " {
		t.Errorf("expected spacer content to be a single space, got %q", res.Content)
	}
}

func TestButtonsSection(t *testing.T) {
	s := Buttons(
		Btn(" Confirm ", "confirm"),
		Btn(" Cancel ", "cancel"),
	)
	res := s.Render(80, "confirm", "")

	if !strings.Contains(res.Content, "Confirm") {
		t.Errorf("expected content to contain 'Confirm', got %q", res.Content)
	}
	if len(res.Focusables) != 2 {
		t.Errorf("expected 2 focusables, got %d", len(res.Focusables))
	}

	// Check focusable IDs
	if res.Focusables[0].ID != "confirm" {
		t.Errorf("expected first focusable ID 'confirm', got %q", res.Focusables[0].ID)
	}
	if res.Focusables[1].ID != "cancel" {
		t.Errorf("expected second focusable ID 'cancel', got %q", res.Focusables[1].ID)
	}
}

func TestButtonsDanger(t *testing.T) {
	s := Buttons(
		Btn(" Delete ", "delete", BtnDanger()),
	)
	res := s.Render(80, "delete", "")

	// Should render with danger style
	if !strings.Contains(res.Content, "Delete") {
		t.Errorf("expected content to contain 'Delete', got %q", res.Content)
	}
}

func TestCheckboxSection(t *testing.T) {
	checked := false
	s := Checkbox("agree", "I agree", &checked)

	res := s.Render(80, "agree", "")
	if !strings.Contains(res.Content, "[ ]") {
		t.Errorf("expected unchecked box '[ ]', got %q", res.Content)
	}

	// Toggle via Update
	s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, "agree")
	if !checked {
		t.Errorf("expected checked to be true after Enter")
	}

	res = s.Render(80, "agree", "")
	if !strings.Contains(res.Content, "[x]") {
		t.Errorf("expected checked box '[x]', got %q", res.Content)
	}
}

func TestWhenSection(t *testing.T) {
	show := false
	s := When(func() bool { return show }, Text("Conditional"))

	// When false
	res := s.Render(80, "", "")
	if res.Content != "" {
		t.Errorf("expected empty when condition is false, got %q", res.Content)
	}

	// When true
	show = true
	res = s.Render(80, "", "")
	if !strings.Contains(res.Content, "Conditional") {
		t.Errorf("expected 'Conditional' when condition is true, got %q", res.Content)
	}
}

func TestWhenSectionNoSpacerLine(t *testing.T) {
	m := New("Test", WithHints(false)).
		AddSection(Custom(func(contentWidth int, focusID, hoverID string) RenderedSection {
			return RenderedSection{
				Content: "First",
				Focusables: []FocusableInfo{{
					ID:      "first",
					OffsetX: 0,
					OffsetY: 0,
					Width:   5,
					Height:  1,
				}},
			}
		}, nil)).
		AddSection(When(func() bool { return false }, Text("Hidden"))).
		AddSection(Custom(func(contentWidth int, focusID, hoverID string) RenderedSection {
			return RenderedSection{
				Content: "Second",
				Focusables: []FocusableInfo{{
					ID:      "second",
					OffsetX: 0,
					OffsetY: 0,
					Width:   6,
					Height:  1,
				}},
			}
		}, nil))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	regions := handler.HitMap.Regions()
	var first, second *mouse.Region
	for i := range regions {
		switch regions[i].ID {
		case "first":
			first = &regions[i]
		case "second":
			second = &regions[i]
		}
	}

	if first == nil || second == nil {
		t.Fatalf("expected both 'first' and 'second' regions to be registered")
	}

	if second.Rect.Y-first.Rect.Y != 1 {
		t.Errorf("expected no spacer line between sections; got delta %d", second.Rect.Y-first.Rect.Y)
	}
}

func TestHandleKeyEsc(t *testing.T) {
	m := New("Test").
		AddSection(Buttons(Btn(" OK ", "ok")))

	// Render to populate focusIDs
	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	action, _ := m.HandleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if action != "cancel" {
		t.Errorf("expected 'cancel' on Esc, got %q", action)
	}
}

func TestHandleKeyTab(t *testing.T) {
	m := New("Test").
		AddSection(Buttons(
			Btn(" A ", "a"),
			Btn(" B ", "b"),
			Btn(" C ", "c"),
		))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	// Initial focus should be on first element
	if m.FocusedID() != "a" {
		t.Errorf("expected initial focus on 'a', got %q", m.FocusedID())
	}

	// Tab to next
	m.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.FocusedID() != "b" {
		t.Errorf("expected focus on 'b' after Tab, got %q", m.FocusedID())
	}

	// Tab again
	m.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.FocusedID() != "c" {
		t.Errorf("expected focus on 'c' after second Tab, got %q", m.FocusedID())
	}

	// Tab wraps around
	m.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.FocusedID() != "a" {
		t.Errorf("expected focus to wrap to 'a', got %q", m.FocusedID())
	}

	// Shift+Tab goes backward
	m.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.FocusedID() != "c" {
		t.Errorf("expected focus on 'c' after Shift+Tab, got %q", m.FocusedID())
	}
}

func TestHandleKeyEnter(t *testing.T) {
	m := New("Test").
		AddSection(Buttons(
			Btn(" OK ", "ok"),
			Btn(" Cancel ", "cancel"),
		))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	// Enter on focused button returns its ID
	action, _ := m.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action != "ok" {
		t.Errorf("expected 'ok' on Enter, got %q", action)
	}

	// Focus cancel and enter
	m.SetFocus("cancel")
	action, _ = m.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action != "cancel" {
		t.Errorf("expected 'cancel' on Enter, got %q", action)
	}
}

func TestHandleMouseClick(t *testing.T) {
	m := New("Test", WithWidth(40)).
		AddSection(Text("Click a button")).
		AddSection(Spacer()).
		AddSection(Buttons(
			Btn(" OK ", "ok"),
			Btn(" Cancel ", "cancel"),
		))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	// Find the "ok" button region
	regions := handler.HitMap.Regions()
	var okRegion *mouse.Region
	for i := range regions {
		if regions[i].ID == "ok" {
			okRegion = &regions[i]
			break
		}
	}

	if okRegion == nil {
		t.Fatal("expected 'ok' button region to be registered")
	}

	// Click on the OK button
	clickX := okRegion.Rect.X + okRegion.Rect.W/2
	clickY := okRegion.Rect.Y
	action := m.HandleMouse(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft}, handler)

	if action != "ok" {
		t.Errorf("expected 'ok' on click, got %q", action)
	}
}

func TestHandleMouseBackdropClick(t *testing.T) {
	m := New("Test", WithWidth(40)).
		AddSection(Text("Click outside"))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	action := m.HandleMouse(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft}, handler)
	if action != "cancel" {
		t.Errorf("expected 'cancel' on backdrop click, got %q", action)
	}

	m = New("Test", WithWidth(40), WithCloseOnBackdropClick(false)).
		AddSection(Text("Click outside"))
	handler = mouse.NewHandler()
	m.Render(80, 24, handler)

	action = m.HandleMouse(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft}, handler)
	if action != "" {
		t.Errorf("expected no action on backdrop click when disabled, got %q", action)
	}
}

func TestHandleMouseHover(t *testing.T) {
	m := New("Test", WithWidth(40)).
		AddSection(Buttons(Btn(" OK ", "ok")))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	// Find the button region
	regions := handler.HitMap.Regions()
	var okRegion *mouse.Region
	for i := range regions {
		if regions[i].ID == "ok" {
			okRegion = &regions[i]
			break
		}
	}

	if okRegion == nil {
		t.Fatal("expected 'ok' button region")
	}

	// Hover over button
	m.HandleMouse(tea.MouseMotionMsg{X: okRegion.Rect.X, Y: okRegion.Rect.Y}, handler)

	if m.HoveredID() != "ok" {
		t.Errorf("expected hoverID 'ok', got %q", m.HoveredID())
	}

	// Move away
	m.HandleMouse(tea.MouseMotionMsg{X: 0, Y: 0}, handler)

	if m.HoveredID() != "" {
		t.Errorf("expected empty hoverID, got %q", m.HoveredID())
	}
}

func TestMouseScrollModal(t *testing.T) {
	m := New("Test", WithWidth(40)).
		AddSection(Text("Line 1")).
		AddSection(Text("Line 2")).
		AddSection(Text("Line 3")).
		AddSection(Text("Line 4")).
		AddSection(Text("Line 5"))

	handler := mouse.NewHandler()
	m.Render(80, 10, handler) // Small height to enable scrolling

	// Scroll on backdrop should do nothing
	m.HandleMouse(tea.MouseWheelMsg{X: 0, Y: 0, Button: tea.MouseWheelDown}, handler)

	initialOffset := m.scrollOffset

	// Scroll on modal body should work
	bodyRegion := handler.HitMap.Test(40, 5) // Should hit modal-body
	if bodyRegion != nil && bodyRegion.ID == "modal-body" {
		m.HandleMouse(tea.MouseWheelMsg{X: 40, Y: 5, Button: tea.MouseWheelDown}, handler)
		// Scroll offset should increase (if content is scrollable)
		_ = initialOffset // May not change if content fits
	}
}

func TestInputSection(t *testing.T) {
	ti := textinput.New()
	ti.Placeholder = "Enter name"
	s := InputWithLabel("name", "Name:", &ti)

	res := s.Render(60, "name", "")

	if !strings.Contains(res.Content, "Name:") {
		t.Errorf("expected content to contain 'Name:', got %q", res.Content)
	}
	if len(res.Focusables) != 1 {
		t.Errorf("expected 1 focusable, got %d", len(res.Focusables))
	}
	if res.Focusables[0].ID != "name" {
		t.Errorf("expected focusable ID 'name', got %q", res.Focusables[0].ID)
	}
}

func TestModalFirstRenderFocusesInputBeforeFirstKey(t *testing.T) {
	ti := textinput.New()
	m := New("Input").AddSection(Input("name", &ti))
	handler := mouse.NewHandler()

	// One render is the runtime boundary between opening a modal and receiving
	// its first key. It must both discover and focus the input.
	m.Render(80, 24, handler)
	if m.FocusedID() != "name" {
		t.Fatalf("focused ID = %q, want name", m.FocusedID())
	}

	_, _ = m.HandleKey(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if got := ti.Value(); got != "b" {
		t.Fatalf("first key was discarded: input = %q, want b", got)
	}
}

func TestModalRoutesPasteToFocusedInput(t *testing.T) {
	ti := textinput.New()
	m := New("Input").AddSection(Input("name", &ti))
	m.Render(80, 24, mouse.NewHandler())

	_, _ = m.HandleMsg(tea.PasteMsg{Content: "pasted value"})
	if got := ti.Value(); got != "pasted value" {
		t.Fatalf("pasted input = %q, want pasted value", got)
	}
}

func TestListSection(t *testing.T) {
	selectedIdx := 0
	items := []ListItem{
		{ID: "item1", Label: "Item 1"},
		{ID: "item2", Label: "Item 2"},
		{ID: "item3", Label: "Item 3"},
	}
	s := List("my-list", items, &selectedIdx)

	// Render with list focused
	res := s.Render(60, "my-list", "")

	if !strings.Contains(res.Content, "Item 1") {
		t.Errorf("expected content to contain 'Item 1', got %q", res.Content)
	}
	// List now registers as a single focusable (the list itself, not each item)
	if len(res.Focusables) != 1 {
		t.Errorf("expected 1 focusable (the list), got %d", len(res.Focusables))
	}
	if res.Focusables[0].ID != "my-list" {
		t.Errorf("expected focusable ID 'my-list', got %q", res.Focusables[0].ID)
	}

	// Test navigation - pass the list's ID, not item ID
	s.Update(tea.KeyPressMsg{Code: tea.KeyDown}, "my-list")
	if selectedIdx != 1 {
		t.Errorf("expected selectedIdx 1 after down, got %d", selectedIdx)
	}

	// Test enter returns selected item ID
	action, _ := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, "my-list")
	if action != "item2" {
		t.Errorf("expected action 'item2' on enter, got %q", action)
	}
}

func TestHitRegionAccuracy(t *testing.T) {
	m := New("Test Modal", WithWidth(50)).
		AddSection(Text("Some text")).
		AddSection(Spacer()).
		AddSection(Buttons(
			Btn(" OK ", "ok"),
			Btn(" Cancel ", "cancel"),
		))

	handler := mouse.NewHandler()
	rendered := m.Render(80, 24, handler)

	// The modal should have rendered something
	if rendered == "" {
		t.Error("expected non-empty render")
	}

	// Check that hit regions are registered
	regions := handler.HitMap.Regions()
	foundBackdrop := false
	foundBody := false
	foundOK := false
	foundCancel := false

	for _, r := range regions {
		switch r.ID {
		case "modal-backdrop":
			foundBackdrop = true
		case "modal-body":
			foundBody = true
		case "ok":
			foundOK = true
		case "cancel":
			foundCancel = true
		}
	}

	if !foundBackdrop {
		t.Error("expected modal-backdrop region")
	}
	if !foundBody {
		t.Error("expected modal-body region")
	}
	if !foundOK {
		t.Error("expected ok button region")
	}
	if !foundCancel {
		t.Error("expected cancel button region")
	}
}

func TestMeasureHeight(t *testing.T) {
	cases := []struct {
		content  string
		expected int
	}{
		{"", 0},
		{"single line", 1},
		{"line 1\nline 2", 2},
		{"line 1\nline 2\nline 3", 3},
		{"with trailing\n", 1}, // Trailing newline trimmed
		{"\n", 0},              // Only newline = empty
	}

	for _, tc := range cases {
		got := measureHeight(tc.content)
		if got != tc.expected {
			t.Errorf("measureHeight(%q) = %d, want %d", tc.content, got, tc.expected)
		}
	}
}

func TestSliceLines(t *testing.T) {
	content := "line 0\nline 1\nline 2\nline 3\nline 4"

	cases := []struct {
		offset, height int
		padToHeight    bool
		want           string
	}{
		{0, 2, true, "line 0\nline 1"},
		{1, 2, true, "line 1\nline 2"},
		{3, 3, true, "line 3\nline 4\n"},                                  // Padded with empty
		{0, 10, true, "line 0\nline 1\nline 2\nline 3\nline 4\n\n\n\n\n"}, // Padded
		{3, 3, false, "line 3\nline 4"},
		{0, 10, false, "line 0\nline 1\nline 2\nline 3\nline 4"},
	}

	for _, tc := range cases {
		got := sliceLines(content, tc.offset, tc.height, tc.padToHeight)
		if got != tc.want {
			t.Errorf("sliceLines(offset=%d, height=%d, pad=%v) = %q, want %q", tc.offset, tc.height, tc.padToHeight, got, tc.want)
		}
	}
}

func TestReset(t *testing.T) {
	m := New("Test").
		AddSection(Buttons(Btn(" A ", "a"), Btn(" B ", "b")))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	// Change state
	m.focusIdx = 1
	m.hoverID = "a"
	m.scrollOffset = 5

	// Reset
	m.Reset()

	if m.focusIdx != 0 {
		t.Errorf("expected focusIdx 0, got %d", m.focusIdx)
	}
	if m.hoverID != "" {
		t.Errorf("expected empty hoverID, got %q", m.hoverID)
	}
	if m.scrollOffset != 0 {
		t.Errorf("expected scrollOffset 0, got %d", m.scrollOffset)
	}
}

// TestHitMapPriority verifies that later (topmost) regions take priority
func TestHitMapPriority(t *testing.T) {
	hm := mouse.NewHitMap()

	// Add overlapping regions - backdrop first (lowest priority)
	hm.AddRect("backdrop", 0, 0, 100, 100, nil)
	hm.AddRect("modal", 10, 10, 30, 30, nil)
	hm.AddRect("button", 15, 15, 10, 10, nil)

	// Test at button location - should hit button (added last)
	region := hm.Test(20, 20)
	if region == nil || region.ID != "button" {
		t.Errorf("expected hit on 'button', got %v", region)
	}

	// Test at modal location (outside button) - should hit modal
	region = hm.Test(12, 12)
	if region == nil || region.ID != "modal" {
		t.Errorf("expected hit on 'modal', got %v", region)
	}

	// Test at backdrop location (outside modal) - should hit backdrop
	region = hm.Test(5, 5)
	if region == nil || region.ID != "backdrop" {
		t.Errorf("expected hit on 'backdrop', got %v", region)
	}
}

// TestMouseHandler tests the mouse.Handler functionality
func TestMouseHandler(t *testing.T) {
	handler := mouse.NewHandler()

	// Add some regions
	handler.HitMap.AddRect("button1", 10, 10, 20, 5, "data1")
	handler.HitMap.AddRect("button2", 40, 10, 20, 5, "data2")

	// Test click detection
	action := handler.HandleMouse(tea.MouseClickMsg{X: 15, Y: 12, Button: tea.MouseLeft})

	if action.Type != mouse.ActionClick {
		t.Errorf("expected ActionClick, got %v", action.Type)
	}
	if action.Region == nil || action.Region.ID != "button1" {
		t.Errorf("expected region 'button1', got %v", action.Region)
	}

	// Test hover detection
	action = handler.HandleMouse(tea.MouseMotionMsg{X: 45, Y: 12})

	if action.Type != mouse.ActionHover {
		t.Errorf("expected ActionHover, got %v", action.Type)
	}
	if action.Region == nil || action.Region.ID != "button2" {
		t.Errorf("expected region 'button2', got %v", action.Region)
	}

	// Test scroll
	action = handler.HandleMouse(tea.MouseWheelMsg{X: 15, Y: 12, Button: tea.MouseWheelDown})

	if action.Type != mouse.ActionScrollDown {
		t.Errorf("expected ActionScrollDown, got %v", action.Type)
	}
}

func TestGettingStartedModalButtonClick(t *testing.T) {
	// Simulate the Getting Started modal structure (must fit on 80x24)
	m := New("", WithWidth(60), WithHints(false)).
		AddSection(CenteredTitle("Welcome to td!")).
		AddSection(CenteredMuted("Task management for AI agents.")).
		AddSection(Spacer()).
		AddSection(Text("To use td, just prompt your agent:")).
		AddSection(Text(`"Use td to plan my feature and implement it."`)).
		AddSection(Spacer()).
		AddSection(Text("Press I to add compact td guidance to AGENTS.md")).
		AddSection(Spacer()).
		AddSection(Buttons(
			Btn(" [I]nstall ", "install"),
			Btn(" Close ", "close"),
		)).
		AddSection(Spacer()).
		AddSection(CenteredMuted("Press ? for help · H to reopen this modal"))

	handler := mouse.NewHandler()
	m.Render(80, 24, handler)

	// Verify focus IDs were populated
	if len(m.focusIDs) != 2 {
		t.Fatalf("expected 2 focusable IDs (install, close), got %d", len(m.focusIDs))
	}

	// Find the button regions
	regions := handler.HitMap.Regions()
	var installRegion, closeRegion *mouse.Region
	for i := range regions {
		if regions[i].ID == "install" {
			installRegion = &regions[i]
		}
		if regions[i].ID == "close" {
			closeRegion = &regions[i]
		}
	}

	if installRegion == nil {
		t.Fatal("expected 'install' button region to be registered")
	}
	if closeRegion == nil {
		t.Fatal("expected 'close' button region to be registered")
	}

	t.Logf("Install button at (%d, %d) size %dx%d", installRegion.Rect.X, installRegion.Rect.Y, installRegion.Rect.W, installRegion.Rect.H)
	t.Logf("Close button at (%d, %d) size %dx%d", closeRegion.Rect.X, closeRegion.Rect.Y, closeRegion.Rect.W, closeRegion.Rect.H)

	// Click the Install button
	action := m.HandleMouse(tea.MouseClickMsg{X: installRegion.Rect.X + 1, Y: installRegion.Rect.Y, Button: tea.MouseLeft}, handler)

	if action != "install" {
		t.Errorf("expected 'install' on click, got %q", action)
	}

	// Click the Close button
	action = m.HandleMouse(tea.MouseClickMsg{X: closeRegion.Rect.X + 1, Y: closeRegion.Rect.Y, Button: tea.MouseLeft}, handler)

	if action != "close" {
		t.Errorf("expected 'close' on click, got %q", action)
	}
}
