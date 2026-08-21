package monitor

import (
	"fmt"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// benchIssueDescription is a realistic issue body: headings, lists, code fence.
const benchIssueDescription = `## Summary

The **renderer** drops trailing cells when a panel is resized while a
modal is open. Reproduces on every terminal we tried.

## Steps

1. Open the monitor
2. Resize the window narrower than ` + "`MinWidth`" + `
3. Open any issue modal

## Notes

- Only happens when ` + "`Embedded`" + ` is true
- Scrollback seems unaffected

` + "```go\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n```\n"

const benchIssueAcceptance = `- [ ] No dropped cells at any width
- [ ] Modal stays centered
- [ ] Cursor returns to prior position`

// benchModel builds an embedded-style model over a temp database with a
// realistic amount of list content.
func benchModel(b *testing.B) Model {
	b.Helper()
	baseDir := b.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		b.Fatalf("db init: %v", err)
	}
	b.Cleanup(func() { _ = database.Close() })

	for i := 0; i < 30; i++ {
		status := models.StatusOpen
		switch i % 4 {
		case 1:
			status = models.StatusInProgress
		case 2:
			status = models.StatusInReview
		case 3:
			status = models.StatusBlocked
		}
		issue := &models.Issue{
			Title:       fmt.Sprintf("Fix renderer bug number %d in the compositor", i),
			Type:        models.TypeTask,
			Status:      status,
			Priority:    models.PriorityP1,
			Description: benchIssueDescription,
			Acceptance:  benchIssueAcceptance,
		}
		if err := database.CreateIssue(issue); err != nil {
			b.Fatalf("create issue: %v", err)
		}
	}

	searchInput := textinput.New()
	m := NewModel(database, "ses-bench", 10*time.Second, "", baseDir)
	m.Embedded = true
	m.SearchInput = searchInput
	m.Width = 200
	m.Height = 46
	m.updatePanelBounds()

	data := FetchDataWithSearchMode(database, m.SessionID, m.StartedAt, "", "auto", false, SortByPriority)
	m.FocusedIssue = data.FocusedIssue
	m.InProgress = data.InProgress
	m.Activity = data.Activity
	m.TaskList = data.TaskList
	m.RecentHandoffs = data.RecentHandoffs
	m.ActiveSessions = data.ActiveSessions
	m.HasIssues = data.HasIssues
	m.buildCurrentWorkRows()
	m.buildTaskListRows()
	return m
}

// benchOpenModal pushes an issue modal and feeds it fetched details plus a
// rendered markdown payload, mirroring what the real update loop does.
func benchOpenModal(b *testing.B, m Model) Model {
	b.Helper()
	issueID := ""
	if len(m.TaskListRows) > 0 {
		issueID = m.TaskListRows[0].Issue.ID
	} else if len(m.CurrentWorkRows) > 0 {
		issueID = m.CurrentWorkRows[0]
	}
	if issueID == "" {
		b.Fatal("no issues to open")
	}
	next, cmd := m.pushModal(issueID, PanelTaskList)
	m = next.(Model)
	// Drain the command chain: fetchIssueDetails -> IssueDetailsMsg ->
	// renderMarkdownAsync -> MarkdownRenderedMsg.
	for depth := 0; depth < 6 && cmd != nil; depth++ {
		msg := cmd()
		if msg == nil {
			break
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			var cmds []tea.Cmd
			for _, c := range batch {
				if c == nil {
					continue
				}
				if got := c(); got != nil {
					var c2 tea.Cmd
					m, c2 = feedMsg(m, got)
					cmds = append(cmds, c2)
				}
			}
			if len(cmds) > 0 {
				cmd = tea.Batch(cmds...)
				continue
			}
			break
		}
		m, cmd = feedMsg(m, msg)
	}
	if len(m.ModalStack) == 0 || m.ModalStack[0].Issue == nil || m.ModalStack[0].DescRender == "" {
		b.Fatal("expected modal to be loaded")
	}
	return m
}

func feedMsg(m Model, msg tea.Msg) (Model, tea.Cmd) {
	n2, c2 := m.Update(msg)
	return n2.(Model), c2
}

func BenchmarkViewModalClosed(b *testing.B) {
	m := benchModel(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.ViewString()
	}
}

func BenchmarkViewModalOpen(b *testing.B) {
	m := benchOpenModal(b, benchModel(b))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.ViewString()
	}
}

func BenchmarkRenderModalOnly(b *testing.B) {
	m := benchOpenModal(b, benchModel(b))
	b.ResetTimer()
	b.ReportAllocs()
	_ = m.renderModal() // warm the cache
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.renderModal()
	}
}

func BenchmarkOverlayModalOnly(b *testing.B) {
	m := benchOpenModal(b, benchModel(b))
	base := m.renderBaseView()
	modal := m.renderModal()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.overlayModal(base, modal)
	}
}
