package monitor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/internal/models"
)

// paintedSwimlaneCategories is every category BuildSwimlaneRows can emit.
// A missing header or tag here is the blank-gap / unprefixed-row paint bug.
func paintedSwimlaneCategories() []TaskListCategory {
	return []TaskListCategory{
		CategoryReviewable,
		CategoryReadyToClose,
		CategoryNeedsRework,
		CategoryInProgress,
		CategoryReady,
		CategoryPendingReview,
		CategoryPendingOther,
		CategoryBlocked,
		CategoryClosed,
	}
}

func TestCategoryPaintCoversEverySwimlaneBucket(t *testing.T) {
	m := Model{
		TaskList: TaskListData{
			Reviewable:    []models.Issue{{ID: "rev"}},
			ReadyToClose:  []models.Issue{{ID: "rtc"}},
			NeedsRework:   []models.Issue{{ID: "rwk"}},
			InProgress:    []models.Issue{{ID: "wip"}},
			Ready:         []models.Issue{{ID: "rdy"}},
			PendingReview: []models.Issue{{ID: "prv"}},
			PendingOther:  []models.Issue{{ID: "oth"}},
			Blocked:       []models.Issue{{ID: "blk"}},
			Closed:        []models.Issue{{ID: "cls"}},
		},
		BoardMode: BoardMode{
			SwimlaneData: TaskListData{
				Reviewable:    []models.Issue{{ID: "rev"}},
				ReadyToClose:  []models.Issue{{ID: "rtc"}},
				NeedsRework:   []models.Issue{{ID: "rwk"}},
				InProgress:    []models.Issue{{ID: "wip"}},
				Ready:         []models.Issue{{ID: "rdy"}},
				PendingReview: []models.Issue{{ID: "prv"}},
				PendingOther:  []models.Issue{{ID: "oth"}},
				Blocked:       []models.Issue{{ID: "blk"}},
				Closed:        []models.Issue{{ID: "cls"}},
			},
		},
	}

	want := map[TaskListCategory]struct {
		header string
		tag    string
	}{
		CategoryReviewable:    {header: "REVIEWABLE", tag: "[REV]"},
		CategoryReadyToClose:  {header: "READY TO CLOSE", tag: "[RTC]"},
		CategoryNeedsRework:   {header: "NEEDS REWORK", tag: "[RWK]"},
		CategoryInProgress:    {header: "IN PROGRESS", tag: "[WIP]"},
		CategoryReady:         {header: "READY", tag: "[RDY]"},
		CategoryPendingReview: {header: "PENDING REVIEW", tag: "[PRV]"},
		CategoryPendingOther:  {header: "PENDING OTHER", tag: "[OTH]"},
		CategoryBlocked:       {header: "BLOCKED", tag: "[BLK]"},
		CategoryClosed:        {header: "CLOSED", tag: "[CLS]"},
	}

	for _, cat := range paintedSwimlaneCategories() {
		expect, ok := want[cat]
		if !ok {
			t.Fatalf("paintedSwimlaneCategories includes %q with no expected paint", cat)
		}

		swimHeader := ansi.Strip(m.formatSwimlaneCategoryHeader(cat))
		listHeader := ansi.Strip(m.formatCategoryHeader(cat))
		tag := ansi.Strip(m.formatCategoryTag(cat))

		if swimHeader == "" || listHeader == "" || tag == "" {
			t.Errorf("%s: empty paint swim=%q list=%q tag=%q", cat, swimHeader, listHeader, tag)
		}
		if !strings.Contains(swimHeader, expect.header) {
			t.Errorf("%s swimlane header %q missing %q", cat, swimHeader, expect.header)
		}
		if !strings.Contains(listHeader, expect.header) {
			t.Errorf("%s task-list header %q missing %q", cat, listHeader, expect.header)
		}
		if tag != expect.tag {
			t.Errorf("%s tag = %q, want %q", cat, tag, expect.tag)
		}
	}
}

func TestBuildSwimlaneRowsCategoriesArePainted(t *testing.T) {
	// Guard the seam: every category BuildSwimlaneRows can emit must have
	// paint coverage above. Adding a bucket without a header/tag is what
	// produced the blank gap above td-7dd76f.
	data := TaskListData{
		Reviewable:    []models.Issue{{ID: "rev"}},
		ReadyToClose:  []models.Issue{{ID: "rtc"}},
		NeedsRework:   []models.Issue{{ID: "rwk"}},
		InProgress:    []models.Issue{{ID: "wip"}},
		Ready:         []models.Issue{{ID: "rdy"}},
		PendingReview: []models.Issue{{ID: "prv"}},
		PendingOther:  []models.Issue{{ID: "oth"}},
		Blocked:       []models.Issue{{ID: "blk"}},
		Closed:        []models.Issue{{ID: "cls"}},
	}
	seen := map[TaskListCategory]bool{}
	for _, row := range BuildSwimlaneRows(data) {
		seen[row.Category] = true
	}

	allowed := map[TaskListCategory]bool{}
	for _, cat := range paintedSwimlaneCategories() {
		allowed[cat] = true
	}
	for cat := range seen {
		if !allowed[cat] {
			t.Errorf("BuildSwimlaneRows emitted unpainted category %q", cat)
		}
	}
	for _, cat := range paintedSwimlaneCategories() {
		if !seen[cat] {
			t.Errorf("painted category %q is never emitted by BuildSwimlaneRows", cat)
		}
	}
}
