package monitor

// Keep the dormant notes modal scaffolding reachable until the feature is wired
// back into the active monitor flows.
var (
	_ = Model.openNotesModal
	_ = (*Model).closeNotesModal
	_ = Model.fetchNotes
	_ = Model.fetchNotesWithArchived
	_ = Model.renderNoteMarkdownAsync
	_ = (*Model).createNotesListModal
	_ = (*Model).createNoteDetailModal
	_ = (*Model).createNoteEditModal
	_ = (*Model).createNoteDeleteConfirmModal
	_ = Model.handleNotesAction
	_ = Model.openNoteCreator
	_ = Model.openNoteEditor
	_ = Model.saveNote
	_ = Model.cancelNoteEdit
	_ = Model.toggleNotePin
	_ = Model.toggleNoteArchive
	_ = formatNoteListItem
	_ = formatNoteMeta
	_ = formatNoteAge
	_ = Model.renderNotesModal
	_ = Model.wrapSimpleModal
	_ = modalBorderStyle
)
