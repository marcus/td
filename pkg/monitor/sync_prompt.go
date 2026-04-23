package monitor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/marcus/td/internal/syncconfig"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/pkg/monitor/modal"
	"github.com/marcus/td/pkg/monitor/mouse"
)

// Sync prompt phases
const (
	syncPromptPhaseList   = iota // show project list
	syncPromptPhaseCreate        // show name input for new project
)

// buildSyncPromptListModal builds the list-phase modal showing remote projects.
func (m *Model) buildSyncPromptListModal(projects []syncclient.ProjectResponse) *modal.Modal {
	md := modal.New("SYNC THIS PROJECT?",
		modal.WithWidth(55),
		modal.WithVariant(modal.VariantInfo),
	)

	// Description text
	md.AddSection(modal.Text("You have remote sync projects. Link this project to sync with your team."))
	md.AddSection(modal.Spacer())

	// Build list items from projects
	items := make([]modal.ListItem, 0, len(projects))
	for i, p := range projects {
		label := fmt.Sprintf("%s (%s)", p.Name, p.ID)
		items = append(items, modal.ListItem{
			ID:    fmt.Sprintf("select_%d", i),
			Label: label,
			Data:  i,
		})
	}

	// Calculate max visible items
	maxVisible := 8
	if maxVisible > len(items) {
		maxVisible = len(items)
	}
	if maxVisible < 3 {
		maxVisible = 3
	}

	md.AddSection(modal.List("sync-projects-list", items, &m.SyncPromptCursor, modal.WithMaxVisible(maxVisible)))

	// Buttons
	md.AddSection(modal.Spacer())
	md.AddSection(modal.Buttons(
		modal.Btn(" Create New ", "create_new"),
		modal.Btn(" Skip ", "skip"),
	))

	md.Reset()
	return md
}

// buildSyncPromptCreateModal builds the create-phase modal with a name input.
func (m *Model) buildSyncPromptCreateModal() *modal.Modal {
	md := modal.New("CREATE SYNC PROJECT",
		modal.WithWidth(55),
		modal.WithVariant(modal.VariantInfo),
		modal.WithPrimaryAction("create_confirm"),
	)

	// Initialize the name input if needed
	if m.SyncPromptNameInput == nil {
		ti := textinput.New()
		ti.Placeholder = "Project name"
		ti.CharLimit = 100
		ti.Width = 40
		m.SyncPromptNameInput = &ti
	}

	md.AddSection(modal.Input("sync-project-name", m.SyncPromptNameInput, modal.WithSubmitAction("create_confirm")))

	// Buttons
	md.AddSection(modal.Spacer())
	md.AddSection(modal.Buttons(
		modal.Btn(" Create ", "create_confirm"),
		modal.Btn(" Back ", "back"),
	))

	md.Reset()
	return md
}

// handleSyncPromptAction handles actions from the sync prompt modal.
func (m *Model) handleSyncPromptAction(action string) tea.Cmd {
	switch {
	case strings.HasPrefix(action, "select_"):
		// Parse index from action like "select_0"
		var idx int
		if _, err := fmt.Sscanf(action, "select_%d", &idx); err != nil {
			return nil
		}
		if idx < 0 || idx >= len(m.SyncPromptProjects) {
			return nil
		}
		project := m.SyncPromptProjects[idx]
		m.closeSyncPromptModal()

		// Async command to link the project
		db := m.DB
		return func() tea.Msg {
			err := db.SetSyncState(project.ID)
			if err != nil {
				return SyncPromptLinkResultMsg{Success: false, ProjectName: project.Name, Error: err}
			}
			return SyncPromptLinkResultMsg{Success: true, ProjectName: project.Name}
		}

	case action == "create_new":
		// Switch to create phase
		m.SyncPromptPhase = syncPromptPhaseCreate
		m.SyncPromptNameInput = nil // Reset input
		m.SyncPromptModal = m.buildSyncPromptCreateModal()
		m.SyncPromptMouse = mouse.NewHandler()
		return nil

	case action == "create_confirm":
		// Get the input value
		if m.SyncPromptNameInput == nil {
			return nil
		}
		name := strings.TrimSpace(m.SyncPromptNameInput.Value())
		if name == "" {
			return nil
		}
		m.closeSyncPromptModal()

		// Async command to create project and link
		db := m.DB
		return func() tea.Msg {
			// Build sync client from auth config
			apiKey := syncconfig.GetAPIKey()
			serverURL := syncconfig.GetServerURL()
			deviceID, err := syncconfig.GetDeviceID()
			if err != nil {
				return SyncPromptCreateResultMsg{Success: false, ProjectName: name, Error: err}
			}

			client := syncclient.New(serverURL, apiKey, deviceID)
			project, err := client.CreateProject(name, "")
			if err != nil {
				return SyncPromptCreateResultMsg{Success: false, ProjectName: name, Error: err}
			}

			// Link the project
			if err := db.SetSyncState(project.ID); err != nil {
				return SyncPromptCreateResultMsg{Success: false, ProjectName: name, Error: err}
			}

			return SyncPromptCreateResultMsg{Success: true, ProjectName: name}
		}

	case action == "skip":
		m.closeSyncPromptModal()
		return nil

	case action == "cancel":
		// Esc key
		if m.SyncPromptPhase == syncPromptPhaseCreate {
			// Go back to list
			m.SyncPromptPhase = syncPromptPhaseList
			m.SyncPromptModal = m.buildSyncPromptListModal(m.SyncPromptProjects)
			m.SyncPromptMouse = mouse.NewHandler()
			return nil
		}
		// From list phase, close
		m.closeSyncPromptModal()
		return nil

	case action == "back":
		// Go back to list from create
		m.SyncPromptPhase = syncPromptPhaseList
		m.SyncPromptCursor = 0
		m.SyncPromptModal = m.buildSyncPromptListModal(m.SyncPromptProjects)
		m.SyncPromptMouse = mouse.NewHandler()
		return nil
	}

	return nil
}

// closeSyncPromptModal closes the sync prompt modal and clears state.
func (m *Model) closeSyncPromptModal() {
	m.SyncPromptOpen = false
	m.SyncPromptPhase = syncPromptPhaseList
	m.SyncPromptProjects = nil
	m.SyncPromptModal = nil
	m.SyncPromptMouse = nil
	m.SyncPromptNameInput = nil
	m.SyncPromptCursor = 0
}
