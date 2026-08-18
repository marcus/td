// Package monitor provides td's Bubble Tea task monitor for standalone and
// embedded use.
//
// Embedders should construct a model with NewEmbeddedWithOptions, provide a
// host-neutral Theme for the initial frame, and call Model.SetTheme from the
// same Bubble Tea goroutine when the host previews or applies a new palette.
// SetTheme changes presentation state in place; it does not restart polling,
// reopen the database, or reset navigation, forms, notes, or modal state.
//
// PanelRenderer and ModalRenderer remain optional host-owned chrome adapters.
// They can draw custom borders, while Theme owns all td content colors.
// MarkdownTheme is retained only as a deprecated compatibility fallback when
// EmbeddedOptions.Theme is empty.
package monitor
