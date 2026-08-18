package monitor

import "github.com/marcus/td/pkg/monitor/modal"

func (m Model) themeOrDefault() Theme {
	if themeIsZero(m.theme) {
		return DefaultTheme()
	}
	return m.theme
}

func modalTheme(theme Theme) modal.Theme {
	if theme == DefaultTheme() {
		return modal.DefaultTheme()
	}
	return modal.Theme{
		Primary:       theme.Primary,
		Secondary:     theme.Secondary,
		Accent:        theme.Accent,
		Success:       theme.Success,
		Warning:       theme.Warning,
		Error:         theme.Error,
		Info:          theme.Info,
		ReadyToClose:  theme.ReadyToClose,
		PendingReview: theme.PendingReview,
		PendingOther:  theme.PendingOther,
		TextPrimary:   theme.TextPrimary,
		TextSecondary: theme.TextSecondary,
		TextMuted:     theme.TextMuted,
		TextSubtle:    theme.TextSubtle,
		TextSelection: theme.TextSelection,
		OnPrimary:     theme.OnPrimary,
		OnWarning:     theme.OnWarning,
		OnError:       theme.OnError,
		Background:    theme.Background,
		Surface:       theme.Surface,
		SurfaceRaised: theme.SurfaceRaised,
		Selection:     theme.Selection,
		Border:        theme.Border,
		BorderMuted:   theme.BorderMuted,
		BorderActive:  theme.BorderActive,
		Link:          theme.Link,
	}
}

func monitorTheme(theme modal.Theme) Theme {
	return Theme{
		Primary: theme.Primary, Secondary: theme.Secondary, Accent: theme.Accent,
		Success: theme.Success, Warning: theme.Warning, Error: theme.Error, Info: theme.Info,
		ReadyToClose: theme.ReadyToClose, PendingReview: theme.PendingReview, PendingOther: theme.PendingOther,
		TextPrimary: theme.TextPrimary, TextSecondary: theme.TextSecondary, TextMuted: theme.TextMuted,
		TextSubtle: theme.TextSubtle, TextSelection: theme.TextSelection,
		OnPrimary: theme.OnPrimary, OnWarning: theme.OnWarning, OnError: theme.OnError,
		Background: theme.Background, Surface: theme.Surface, SurfaceRaised: theme.SurfaceRaised,
		Selection: theme.Selection, Border: theme.Border, BorderMuted: theme.BorderMuted,
		BorderActive: theme.BorderActive, Link: theme.Link,
	}
}

func (m *Model) newModal(title string, modalType ModalType, opts ...modal.Option) *modal.Modal {
	theme := m.theme
	if themeIsZero(theme) {
		theme = DefaultTheme()
	}
	opts = append(opts, modal.WithTheme(modalTheme(theme)))
	if m.ModalRenderer != nil {
		opts = append(opts, modal.WithChromeRenderer(func(content string, width, height int) string {
			return m.ModalRenderer(content, width, height, modalType, 1)
		}))
	}
	return modal.New(title, opts...)
}

// rethemeDeclarativeModals updates every potentially open declarative modal
// in place. Modal-owned interaction and child input state are not rebuilt.
func (m *Model) rethemeDeclarativeModals(theme Theme) {
	value := modalTheme(theme)
	modals := []*modal.Modal{
		m.TDQHelpModal,
		m.DeleteConfirmModal,
		m.CloseConfirmModal,
		m.SelfReviewConfirmModal,
		m.RecordReviewModal,
		m.StatsModal,
		m.HandoffsModal,
		m.ActivityDetailModal,
		m.NotesModal,
		m.GettingStartedModal,
		m.SyncPromptModal,
		m.BoardPickerModal,
		m.BoardEditorModal,
	}
	for _, current := range modals {
		if current != nil {
			current.SetTheme(value)
		}
	}
}
