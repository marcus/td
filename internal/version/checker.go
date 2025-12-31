package version

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// UpdateAvailableMsg is sent when a new version is available.
type UpdateAvailableMsg struct {
	CurrentVersion string
	LatestVersion  string
	UpdateCommand  string
}

// CheckAsync returns a Bubble Tea command that checks for updates in background.
func CheckAsync(currentVersion string) tea.Cmd {
	return func() tea.Msg {
		// Check cache first
		if cached, err := LoadCache(); err == nil && IsCacheValid(cached, currentVersion) {
			if cached.HasUpdate {
				return UpdateAvailableMsg{
					CurrentVersion: currentVersion,
					LatestVersion:  cached.LatestVersion,
					UpdateCommand:  UpdateCommand(cached.LatestVersion),
				}
			}
			return nil // up-to-date, cached
		}

		// Cache miss or invalid, fetch from GitHub
		result := Check(currentVersion)

		// Only cache successful checks (don't cache network errors)
		if result.Error == nil {
			_ = SaveCache(&CacheEntry{
				LatestVersion:  result.LatestVersion,
				CurrentVersion: currentVersion,
				CheckedAt:      time.Now(),
				HasUpdate:      result.HasUpdate,
			})
		}

		if result.HasUpdate {
			return UpdateAvailableMsg{
				CurrentVersion: currentVersion,
				LatestVersion:  result.LatestVersion,
				UpdateCommand:  UpdateCommand(result.LatestVersion),
			}
		}

		return nil
	}
}
