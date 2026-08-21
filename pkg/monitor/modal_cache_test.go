package monitor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// modalCacheTestModel builds a model with an open, fully loaded issue modal.
func modalCacheTestModel(t *testing.T) Model {
	t.Helper()
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	issue := &models.Issue{
		Title:       "Cache subject",
		Type:        models.TypeTask,
		Status:      models.StatusInProgress,
		Priority:    models.PriorityP1,
		Description: benchIssueDescription,
		Acceptance:  benchIssueAcceptance,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	m := NewModel(database, "ses-cache", 0, "", baseDir)
	m.Embedded = true
	m.Width = 200
	m.Height = 46
	m.updatePanelBounds()
	next, cmd := m.pushModal(issue.ID, PanelTaskList)
	m = next.(Model)
	for depth := 0; depth < 6 && cmd != nil; depth++ {
		msg := cmd()
		if msg == nil {
			break
		}
		n2, c2 := m.Update(msg)
		m = n2.(Model)
		cmd = c2
	}
	if len(m.ModalStack) == 0 || m.ModalStack[0].Issue == nil {
		t.Fatal("modal not loaded")
	}
	return m
}

func TestRenderModalCacheHits(t *testing.T) {
	m := modalCacheTestModel(t)
	first := m.renderModal()
	key := m.modalRenderKeyFor(m.CurrentModal())
	if m.modalRender == nil {
		t.Fatal("cache not initialized")
	}
	if !key.equal(m.modalRender.key) {
		t.Fatal("cache key mismatch after render")
	}
	if second := m.renderModal(); first != second {
		t.Fatal("cached render differs from fresh render")
	}
}

func TestRenderModalInvalidatesOnScroll(t *testing.T) {
	m := modalCacheTestModel(t)
	before := m.renderModal()
	entry := m.CurrentModal()
	entry.Scroll = 1
	if after := m.renderModal(); before == after {
		t.Fatal("scroll change did not invalidate the cached render")
	}
}

func TestRenderModalInvalidatesOnThemeRevision(t *testing.T) {
	m := modalCacheTestModel(t)
	_ = m.renderModal()

	// SetTheme swaps m.styles and bumps themeRevision in one step. The
	// revision is the only key input that moves, so it must invalidate the
	// cached render even when every other fingerprinted input stands still.
	m.themeRevision++
	if after := m.modalRenderKeyFor(m.CurrentModal()); after.equal(m.modalRender.key) {
		t.Fatal("theme revision bump did not invalidate the cache key")
	}
}

func TestRenderModalInvalidatesOnNewDetails(t *testing.T) {
	m := modalCacheTestModel(t)
	before := m.renderModal()

	// Simulate a reactive refresh that changed the issue title.
	entry := m.CurrentModal()
	updated := *entry.Issue
	updated.Title = "Changed title"
	next, cmd := m.Update(IssueDetailsMsg{IssueID: entry.IssueID, Issue: &updated})
	m = next.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			n2, _ := m.Update(msg)
			m = n2.(Model)
		}
	}
	if after := m.renderModal(); before == after {
		t.Fatal("new issue details did not invalidate the cached render")
	}
}

var _ = tea.Batch // keep import if handlers change
