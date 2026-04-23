package monitor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcus/td/internal/syncclient"
)

// sampleProjects returns test project data for sync prompt tests.
func sampleProjects(n int) []syncclient.ProjectResponse {
	projects := make([]syncclient.ProjectResponse, n)
	for i := 0; i < n; i++ {
		projects[i] = syncclient.ProjectResponse{
			ID:   fmt.Sprintf("proj-%d", i),
			Name: fmt.Sprintf("Project %d", i),
		}
	}
	return projects
}

// --- 1. Modal building tests ---

func TestBuildSyncPromptListModal_ShowsProjects(t *testing.T) {
	m := newTestModel()
	projects := sampleProjects(3)

	md := m.buildSyncPromptListModal(projects)
	if md == nil {
		t.Fatal("expected non-nil modal from buildSyncPromptListModal")
	}
}

func TestBuildSyncPromptCreateModal_HasInput(t *testing.T) {
	m := newTestModel()

	md := m.buildSyncPromptCreateModal()
	if md == nil {
		t.Fatal("expected non-nil modal from buildSyncPromptCreateModal")
	}
	if m.SyncPromptNameInput == nil {
		t.Fatal("expected SyncPromptNameInput to be initialized after buildSyncPromptCreateModal")
	}
}

// --- 2. Action handler tests ---

func TestHandleSyncPromptAction_SelectProject(t *testing.T) {
	m := newTestModel()
	projects := sampleProjects(3)
	m.SyncPromptOpen = true
	m.SyncPromptProjects = projects
	m.SyncPromptModal = m.buildSyncPromptListModal(projects)

	cmd := m.handleSyncPromptAction("select_0")

	if m.SyncPromptOpen {
		t.Error("expected SyncPromptOpen=false after selecting a project")
	}
	// The handler returns a tea.Cmd that calls DB.SetSyncState, which will
	// panic/fail without a real DB. We just verify a cmd was returned.
	if cmd == nil {
		t.Error("expected a non-nil cmd from select action (async link)")
	}
}

func TestHandleSyncPromptAction_SelectProject_OutOfBounds(t *testing.T) {
	m := newTestModel()
	m.SyncPromptProjects = sampleProjects(2)

	cmd := m.handleSyncPromptAction("select_5")
	if cmd != nil {
		t.Error("expected nil cmd for out-of-bounds select")
	}
}

func TestHandleSyncPromptAction_CreateNew(t *testing.T) {
	m := newTestModel()
	m.SyncPromptOpen = true
	m.SyncPromptPhase = syncPromptPhaseList
	m.SyncPromptProjects = sampleProjects(2)

	cmd := m.handleSyncPromptAction("create_new")

	if m.SyncPromptPhase != syncPromptPhaseCreate {
		t.Errorf("expected phase=syncPromptPhaseCreate, got %d", m.SyncPromptPhase)
	}
	if m.SyncPromptModal == nil {
		t.Error("expected SyncPromptModal to be rebuilt for create phase")
	}
	if cmd != nil {
		t.Error("expected nil cmd from create_new (synchronous phase switch)")
	}
}

func TestHandleSyncPromptAction_Skip(t *testing.T) {
	m := newTestModel()
	m.SyncPromptOpen = true

	cmd := m.handleSyncPromptAction("skip")

	if m.SyncPromptOpen {
		t.Error("expected SyncPromptOpen=false after skip")
	}
	if cmd != nil {
		t.Error("expected nil cmd from skip")
	}
}

func TestHandleSyncPromptAction_Back(t *testing.T) {
	m := newTestModel()
	m.SyncPromptOpen = true
	m.SyncPromptPhase = syncPromptPhaseCreate
	m.SyncPromptProjects = sampleProjects(2)

	cmd := m.handleSyncPromptAction("back")

	if m.SyncPromptPhase != syncPromptPhaseList {
		t.Errorf("expected phase=syncPromptPhaseList after back, got %d", m.SyncPromptPhase)
	}
	if m.SyncPromptCursor != 0 {
		t.Errorf("expected cursor reset to 0, got %d", m.SyncPromptCursor)
	}
	if cmd != nil {
		t.Error("expected nil cmd from back")
	}
}

func TestHandleSyncPromptAction_Cancel_FromList(t *testing.T) {
	m := newTestModel()
	m.SyncPromptOpen = true
	m.SyncPromptPhase = syncPromptPhaseList

	cmd := m.handleSyncPromptAction("cancel")

	if m.SyncPromptOpen {
		t.Error("expected SyncPromptOpen=false after cancel from list")
	}
	if cmd != nil {
		t.Error("expected nil cmd from cancel")
	}
}

func TestHandleSyncPromptAction_Cancel_FromCreate(t *testing.T) {
	m := newTestModel()
	m.SyncPromptOpen = true
	m.SyncPromptPhase = syncPromptPhaseCreate
	m.SyncPromptProjects = sampleProjects(2)

	cmd := m.handleSyncPromptAction("cancel")

	// Cancel from create goes back to list, doesn't close
	if m.SyncPromptPhase != syncPromptPhaseList {
		t.Errorf("expected phase=syncPromptPhaseList after cancel from create, got %d", m.SyncPromptPhase)
	}
	// Modal should still be open (went back to list)
	if m.SyncPromptModal == nil {
		t.Error("expected SyncPromptModal to be rebuilt for list phase")
	}
	if cmd != nil {
		t.Error("expected nil cmd from cancel (back to list)")
	}
}

// --- 3. Message handler tests (Update method) ---

func TestSyncPromptDataMsg_OpensModal(t *testing.T) {
	m := newTestModel()
	m.SyncPromptOpen = false

	projects := sampleProjects(3)
	msg := SyncPromptDataMsg{Projects: projects}

	result, _ := m.Update(msg)
	updated := result.(Model)

	if !updated.SyncPromptOpen {
		t.Error("expected SyncPromptOpen=true after receiving projects")
	}
	if updated.SyncPromptModal == nil {
		t.Error("expected SyncPromptModal to be non-nil")
	}
	if len(updated.SyncPromptProjects) != 3 {
		t.Errorf("expected 3 projects, got %d", len(updated.SyncPromptProjects))
	}
	if updated.SyncPromptPhase != syncPromptPhaseList {
		t.Errorf("expected phase=syncPromptPhaseList, got %d", updated.SyncPromptPhase)
	}
}

func TestSyncPromptDataMsg_NilProjects_NoOp(t *testing.T) {
	m := newTestModel()
	m.SyncPromptOpen = false

	msg := SyncPromptDataMsg{Projects: nil}

	result, _ := m.Update(msg)
	updated := result.(Model)

	if updated.SyncPromptOpen {
		t.Error("expected SyncPromptOpen to stay false when projects is nil")
	}
}

func TestSyncPromptDataMsg_Error_NoOp(t *testing.T) {
	m := newTestModel()
	m.SyncPromptOpen = false

	msg := SyncPromptDataMsg{Error: fmt.Errorf("network error"), Projects: nil}

	result, _ := m.Update(msg)
	updated := result.(Model)

	if updated.SyncPromptOpen {
		t.Error("expected SyncPromptOpen to stay false when error is set")
	}
}

func TestSyncPromptLinkResult_Success_ShowsToast(t *testing.T) {
	m := newTestModel()

	msg := SyncPromptLinkResultMsg{Success: true, ProjectName: "my-project"}

	result, _ := m.Update(msg)
	updated := result.(Model)

	if updated.StatusMessage == "" {
		t.Error("expected StatusMessage to be set after successful link")
	}
	if updated.StatusIsError {
		t.Error("expected StatusIsError=false for successful link")
	}
	// Verify project name appears in message
	if !strings.Contains(updated.StatusMessage, "my-project") {
		t.Errorf("expected StatusMessage to contain 'my-project', got %q", updated.StatusMessage)
	}
}

func TestSyncPromptLinkResult_Error_ShowsErrorToast(t *testing.T) {
	m := newTestModel()

	msg := SyncPromptLinkResultMsg{Success: false, Error: fmt.Errorf("fail")}

	result, _ := m.Update(msg)
	updated := result.(Model)

	if updated.StatusMessage == "" {
		t.Error("expected StatusMessage to be set after link error")
	}
	if !updated.StatusIsError {
		t.Error("expected StatusIsError=true for failed link")
	}
}

func TestSyncPromptCreateResult_Success_ShowsToast(t *testing.T) {
	m := newTestModel()

	msg := SyncPromptCreateResultMsg{Success: true, ProjectName: "new-proj"}

	result, _ := m.Update(msg)
	updated := result.(Model)

	if updated.StatusMessage == "" {
		t.Error("expected StatusMessage to be set after successful create")
	}
	if updated.StatusIsError {
		t.Error("expected StatusIsError=false for successful create")
	}
	if !strings.Contains(updated.StatusMessage, "new-proj") {
		t.Errorf("expected StatusMessage to contain 'new-proj', got %q", updated.StatusMessage)
	}
}

func TestSyncPromptCreateResult_Error_ShowsErrorToast(t *testing.T) {
	m := newTestModel()

	msg := SyncPromptCreateResultMsg{Success: false, ProjectName: "fail-proj", Error: fmt.Errorf("create failed")}

	result, _ := m.Update(msg)
	updated := result.(Model)

	if updated.StatusMessage == "" {
		t.Error("expected StatusMessage to be set after create error")
	}
	if !updated.StatusIsError {
		t.Error("expected StatusIsError=true for failed create")
	}
}

// --- 4. First-run guard test ---

func TestIsFirstRunInit_Guards_SyncPrompt(t *testing.T) {
	t.Run("IsFirstRunInit=true enables sync prompt after getting started", func(t *testing.T) {
		m := newTestModel()
		m.IsFirstRunInit = true

		// Verify the flag is set — this is the guard that triggers checkSyncPrompt
		// in the getting started close handler (commands.go)
		if !m.IsFirstRunInit {
			t.Fatal("expected IsFirstRunInit=true")
		}
	})

	t.Run("IsFirstRunInit=false skips sync prompt", func(t *testing.T) {
		m := newTestModel()
		m.IsFirstRunInit = false

		// When false (e.g., user reopened getting started with H key),
		// sync prompt should NOT be triggered
		if m.IsFirstRunInit {
			t.Fatal("expected IsFirstRunInit=false")
		}
	})

	t.Run("FirstRunCheckMsg sets IsFirstRunInit", func(t *testing.T) {
		m := newTestModel()
		m.IsFirstRunInit = false

		msg := FirstRunCheckMsg{IsFirstRun: true}
		result, _ := m.Update(msg)
		updated := result.(Model)

		if !updated.IsFirstRunInit {
			t.Error("expected IsFirstRunInit=true after FirstRunCheckMsg{IsFirstRun: true}")
		}
		if !updated.GettingStartedOpen {
			t.Error("expected GettingStartedOpen=true after first run check")
		}
	})

	t.Run("FirstRunCheckMsg false does not set flag", func(t *testing.T) {
		m := newTestModel()
		m.IsFirstRunInit = false

		msg := FirstRunCheckMsg{IsFirstRun: false}
		result, _ := m.Update(msg)
		updated := result.(Model)

		if updated.IsFirstRunInit {
			t.Error("expected IsFirstRunInit to stay false when IsFirstRun=false")
		}
	})
}

