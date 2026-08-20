package monitor

import (
	"errors"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/td/internal/models"
)

func TestVisibleFocusStopsUseStableOrderAndCurrentBounds(t *testing.T) {
	m := Model{Width: 80, Height: 30, PanelBounds: map[Panel]Rect{
		PanelCurrentWork: {X: 1, Y: 2, W: 80, H: 7},
		PanelTaskList:    {X: 3, Y: 9, W: 76, H: 0},
		PanelActivity:    {X: 5, Y: 14, W: 70, H: 6},
	}}
	want := []FocusStop{
		{ID: FocusStopCurrentWork, Bounds: Rect{X: 1, Y: 2, W: 80, H: 7}},
		{ID: FocusStopActivity, Bounds: Rect{X: 5, Y: 14, W: 70, H: 6}},
	}
	if got := m.VisibleFocusStops(); !reflect.DeepEqual(got, want) {
		t.Fatalf("VisibleFocusStops() = %#v, want %#v", got, want)
	}

	m.PanelBounds[PanelTaskList] = Rect{X: 3, Y: 9, W: 76, H: 5}
	if got := m.VisibleFocusStops(); len(got) != 3 || got[1].ID != FocusStopTaskList || got[1].Bounds != m.PanelBounds[PanelTaskList] {
		t.Fatalf("VisibleFocusStops() after layout change = %#v", got)
	}
}

func TestVisibleFocusStopsHideReplacementViews(t *testing.T) {
	m := Model{
		PaneHeights: defaultPaneHeights(),
		PanelBounds: make(map[Panel]Rect),
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 39, Height: 30})
	m = updated.(Model)
	if bounds := m.PanelBounds[PanelCurrentWork]; bounds.W <= 0 || bounds.H <= 0 {
		t.Fatalf("39x30 resize did not retain the stale-bounds reproduction: %#v", m.PanelBounds)
	}
	if got := m.VisibleFocusStops(); len(got) != 0 {
		t.Fatalf("compact 39x30 view exposed root panel stops: %#v", got)
	}

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m = updated.(Model)
	m.Err = errors.New("database unavailable")
	if got := m.VisibleFocusStops(); len(got) != 0 {
		t.Fatalf("error view exposed stale root panel stops: %#v", got)
	}
}

func TestCurrentAndDirectFocusStopClampAndEnsureVisible(t *testing.T) {
	m := Model{
		Height:            30,
		PaneHeights:       defaultPaneHeights(),
		ActivePanel:       PanelCurrentWork,
		Cursor:            map[Panel]int{PanelTaskList: 9},
		ScrollOffset:      map[Panel]int{PanelTaskList: 9},
		TaskListRows:      []TaskListRow{{Issue: models.Issue{ID: "td-one"}}, {Issue: models.Issue{ID: "td-two"}}},
		PanelBounds:       make(map[Panel]Rect),
		ScrollIndependent: make(map[Panel]bool),
	}
	if got := m.CurrentFocusStop(); got != FocusStopCurrentWork {
		t.Fatalf("CurrentFocusStop() = %q", got)
	}
	if !m.SetFocusStop(FocusStopTaskList) {
		t.Fatal("SetFocusStop(task-list) refused")
	}
	if m.CurrentFocusStop() != FocusStopTaskList || m.Cursor[PanelTaskList] != 1 || m.ScrollOffset[PanelTaskList] != 0 {
		t.Fatalf("direct focus state: stop=%q cursor=%d scroll=%d", m.CurrentFocusStop(), m.Cursor[PanelTaskList], m.ScrollOffset[PanelTaskList])
	}

	beforePanel, beforeCursor, beforeScroll := m.ActivePanel, m.Cursor[PanelTaskList], m.ScrollOffset[PanelTaskList]
	if m.SetFocusStop(FocusStopID("missing")) {
		t.Fatal("invalid focus stop was accepted")
	}
	if m.ActivePanel != beforePanel || m.Cursor[PanelTaskList] != beforeCursor || m.ScrollOffset[PanelTaskList] != beforeScroll {
		t.Fatal("invalid focus stop mutated model state")
	}
}

func TestTabOwnsFocusOnlyOutsideRootContexts(t *testing.T) {
	tests := []struct {
		name string
		set  func(*Model)
		want bool
	}{
		{name: "main", want: false},
		{name: "board", set: func(m *Model) { m.ActivePanel = PanelTaskList; m.TaskListMode = TaskListModeBoard }, want: false},
		{name: "search", set: func(m *Model) { m.SearchMode = true }, want: true},
		{name: "help", set: func(m *Model) { m.HelpOpen = true }, want: true},
		{name: "confirmation", set: func(m *Model) { m.ConfirmOpen = true }, want: true},
		{name: "self-review confirmation", set: func(m *Model) { m.SelfReviewConfirmOpen = true }, want: true},
		{name: "record review", set: func(m *Model) { m.RecordReviewOpen = true }, want: true},
		{name: "activity detail", set: func(m *Model) { m.ActivityDetailOpen = true }, want: true},
		{name: "kanban", set: func(m *Model) { m.KanbanOpen = true }, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{}
			if tt.set != nil {
				tt.set(&m)
			}
			if got := m.TabOwnsFocus(); got != tt.want {
				t.Fatalf("TabOwnsFocus() = %v, want %v (context %v)", got, tt.want, m.currentContext())
			}
		})
	}
}

func TestStandaloneNextPreviousPanelUseDirectFocusPath(t *testing.T) {
	tests := []struct {
		name  string
		key   tea.KeyPressMsg
		delta int
	}{
		{name: "next", key: tea.KeyPressMsg{Code: tea.KeyTab}, delta: 1},
		{name: "previous", key: tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, delta: -1},
	}
	for _, tt := range tests {
		for start := PanelCurrentWork; start <= PanelActivity; start++ {
			t.Run(tt.name+"-from-"+string(focusStopForPanel(start)), func(t *testing.T) {
				m := Model{
					Height:          30,
					PaneHeights:     defaultPaneHeights(),
					ActivePanel:     start,
					Cursor:          map[Panel]int{PanelCurrentWork: 9, PanelTaskList: 9, PanelActivity: 9},
					ScrollOffset:    map[Panel]int{PanelCurrentWork: 9, PanelTaskList: 9, PanelActivity: 9},
					CurrentWorkRows: []string{"cw-one", "cw-two"},
					TaskListRows:    []TaskListRow{{Issue: models.Issue{ID: "td-one"}}, {Issue: models.Issue{ID: "td-two"}}},
					Activity:        []ActivityItem{{IssueID: "td-one"}, {IssueID: "td-two"}},
					Keymap:          newTestKeymap(),
				}
				target := Panel((int(start) + tt.delta + len(rootFocusStops)) % len(rootFocusStops))
				want := m
				want.SetFocusStop(focusStopForPanel(target))
				updated, _ := m.handleKey(tt.key)
				got := updated.(Model)
				if got.ActivePanel != want.ActivePanel || got.Cursor[target] != want.Cursor[target] || got.ScrollOffset[target] != want.ScrollOffset[target] {
					t.Fatalf("navigation state: panel=%v cursor=%d scroll=%d; want panel=%v cursor=%d scroll=%d",
						got.ActivePanel, got.Cursor[target], got.ScrollOffset[target], want.ActivePanel, want.Cursor[target], want.ScrollOffset[target])
				}
			})
		}
	}
}
