package monitor

import (
	"path/filepath"

	"github.com/marcus/td/pkg/monitor/modal"
)

// createGettingStartedModal builds the Getting Started modal.
// Call this when opening the modal (not at init time) so it reflects current state.
// Content is kept compact to fit on 80x24 terminals.
func (m *Model) createGettingStartedModal() *modal.Modal {
	// Determine which file to suggest
	fileName := "AGENTS.md"
	if m.AgentFilePath != "" {
		fileName = filepath.Base(m.AgentFilePath)
	}

	md := m.newModal("Welcome to td!", ModalTypeHelp, modal.WithWidth(60), modal.WithHints(false))

	md.AddSection(modal.Text("Task management for AI agents."))
	md.AddSection(modal.Spacer())

	if m.AgentFileTDNeedsUpdate {
		md.AddSection(modal.Text("Updated td guidance is available for " + fileName))
	} else if m.AgentFileHasTD {
		md.AddSection(modal.Text("\u2713 td guidance installed"))
	} else {
		md.AddSection(modal.Text("Press I to add compact td guidance to " + fileName))
	}
	md.AddSection(modal.Spacer())

	md.AddSection(modal.Text("PROMPT: \"Use td to plan my feature and implement it.\""))
	md.AddSection(modal.Spacer())

	md.AddSection(modal.Text("Press ? for help · H to reopen this modal"))
	md.AddSection(modal.Spacer())

	// Only show Install button if not already installed
	if m.AgentFileHasTD && !m.AgentFileTDNeedsUpdate {
		md.AddSection(modal.Buttons(
			modal.Btn(" Close ", "close"),
		))
	} else {
		actionLabel := " [I]nstall "
		if m.AgentFileTDNeedsUpdate {
			actionLabel = " [I] Update "
		}
		md.AddSection(modal.Buttons(
			modal.Btn(actionLabel, "install"),
			modal.Btn(" Close ", "close"),
		))
	}

	return md
}
