package monitor

import "github.com/marcus/td/pkg/monitor/keymap"

// FocusStopID is a stable host-facing identity for one root monitor panel.
type FocusStopID string

const (
	FocusStopCurrentWork FocusStopID = "current-work"
	FocusStopTaskList    FocusStopID = "task-list"
	FocusStopActivity    FocusStopID = "activity"
)

// FocusStop describes one visible root panel at its current rendered bounds.
type FocusStop struct {
	ID     FocusStopID
	Bounds Rect
}

var rootFocusStops = [...]struct {
	id    FocusStopID
	panel Panel
}{
	{FocusStopCurrentWork, PanelCurrentWork},
	{FocusStopTaskList, PanelTaskList},
	{FocusStopActivity, PanelActivity},
}

// VisibleFocusStops returns root panels with non-empty current bounds in
// visual order. The IDs remain stable when layout geometry changes.
func (m Model) VisibleFocusStops() []FocusStop {
	// Compact and error views replace the root panel layout entirely. Bounds
	// may still describe the previous/full layout after a resize or error, but
	// they are not visible focus targets in either replacement view.
	if m.Width < MinWidth || m.Height < MinHeight || m.Err != nil {
		return nil
	}
	stops := make([]FocusStop, 0, len(rootFocusStops))
	for _, candidate := range rootFocusStops {
		bounds, ok := m.PanelBounds[candidate.panel]
		if !ok || bounds.W <= 0 || bounds.H <= 0 {
			continue
		}
		stops = append(stops, FocusStop{ID: candidate.id, Bounds: bounds})
	}
	return stops
}

// CurrentFocusStop reports the stable ID of the active root panel. It returns
// the empty ID only if ActivePanel contains an invalid value.
func (m Model) CurrentFocusStop() FocusStopID {
	return focusStopForPanel(m.ActivePanel)
}

// SetFocusStop focuses a root panel directly. Invalid IDs are refused without
// changing state. The selected panel's cursor and scroll are normalized using
// the same path as standalone Tab and Shift+Tab navigation.
func (m *Model) SetFocusStop(id FocusStopID) bool {
	panel, ok := panelForFocusStop(id)
	if !ok {
		return false
	}
	return m.setFocusPanel(panel)
}

// TabOwnsFocus reports whether the monitor's current input or overlay context
// must receive Tab itself. At the root main and board contexts, an embedding
// host may compose the monitor's panel stops into its outer focus ring.
func (m Model) TabOwnsFocus() bool {
	// These declarative overlays predate dedicated keymap contexts, but each
	// owns Tab for its controls just like the other modal contexts do.
	if m.SelfReviewConfirmOpen || m.RecordReviewOpen || m.ActivityDetailOpen {
		return true
	}
	switch m.currentContext() {
	case keymap.ContextMain, keymap.ContextBoard:
		return false
	default:
		return true
	}
}

func (m *Model) setFocusPanel(panel Panel) bool {
	if m == nil || focusStopForPanel(panel) == "" {
		return false
	}
	if m.Cursor == nil {
		m.Cursor = make(map[Panel]int)
	}
	if m.ScrollOffset == nil {
		m.ScrollOffset = make(map[Panel]int)
	}
	m.ActivePanel = panel
	m.clampCursor(panel)
	m.ensureCursorVisible(panel)
	return true
}

func (m *Model) cycleFocusPanel(delta int) bool {
	if m == nil {
		return false
	}
	next := (int(m.ActivePanel) + delta) % len(rootFocusStops)
	if next < 0 {
		next += len(rootFocusStops)
	}
	return m.SetFocusStop(rootFocusStops[next].id)
}

func focusStopForPanel(panel Panel) FocusStopID {
	for _, candidate := range rootFocusStops {
		if candidate.panel == panel {
			return candidate.id
		}
	}
	return ""
}

func panelForFocusStop(id FocusStopID) (Panel, bool) {
	for _, candidate := range rootFocusStops {
		if candidate.id == id {
			return candidate.panel, true
		}
	}
	return 0, false
}
