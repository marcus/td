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

func TestListNotesRecognizesHistoricalDeletedAt(t *testing.T) {
	database, err := Initialize(t.TempDir())
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	stamps := []string{
		"2026-02-03 20:43:14 +0000 UTC",
		"2026-08-09 15:06:18+00:00",
	}
	for i, stamp := range stamps {
		id := "nt-hist00" + string(rune('1'+i))
		_, err := database.Conn().Exec(`
			INSERT INTO notes (id, title, content, created_at, updated_at, pinned, archived, deleted_at)
			VALUES (?, ?, 'c', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 0, 0, ?)
		`, id, stamp, stamp)
		if err != nil {
			t.Fatalf("insert %s: %v", stamp, err)
		}
	}

	got, err := database.GetNoteIncludingDeleted("nt-hist001")
	if err != nil || got.DeletedAt == nil {
		t.Fatalf("GetNoteIncludingDeleted time.Time.String form: %+v %v", got, err)
	}
	if _, err := database.GetNote("nt-hist001"); err == nil {
		t.Fatal("GetNote should hide historically-stamped deleted notes")
	}

	listed, err := database.ListNotes(ListNotesOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	var deleted int
	for _, n := range listed {
		if n.DeletedAt != nil {
			deleted++
		}
	}
	if deleted != 2 {
		t.Fatalf("IncludeDeleted listed %d deleted, want 2 (got %+v)", deleted, listed)
	}
}
