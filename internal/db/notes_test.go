package db

import "testing"

func TestRestoreNoteAndFlagActionLog(t *testing.T) {
	database, err := Initialize(t.TempDir())
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	note, err := database.CreateNote("t", "c")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := database.PinNote(note.ID); err != nil {
		t.Fatalf("PinNote: %v", err)
	}
	if err := database.ArchiveNote(note.ID); err != nil {
		t.Fatalf("ArchiveNote: %v", err)
	}
	if err := database.DeleteNote(note.ID); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}
	if _, err := database.RestoreNote(note.ID); err != nil {
		t.Fatalf("RestoreNote: %v", err)
	}

	var n int
	if err := database.Conn().QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND entity_type = 'note'`, note.ID,
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n < 5 {
		t.Fatalf("action_log = %d, want create+pin+archive+delete+restore", n)
	}

	live, err := database.GetNote(note.ID)
	if err != nil || live.DeletedAt != nil {
		t.Fatalf("GetNote after restore: %+v %v", live, err)
	}
}
