package notes

import (
	"testing"

	"github.com/marcus/td/internal/db"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Initialize(t.TempDir())
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return &Store{db: database}
}

func TestStoreCRUDRestorePinArchive(t *testing.T) {
	s := openTestStore(t)

	n, err := s.Create("Hello", "body")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if n.ID == "" || n.Title != "Hello" {
		t.Fatalf("created: %+v", n)
	}

	got, err := s.Get(n.ID)
	if err != nil || got.Content != "body" {
		t.Fatalf("Get: %+v %v", got, err)
	}

	updated, err := s.Update(n.ID, "Hello", "body2")
	if err != nil || updated.Content != "body2" {
		t.Fatalf("Update: %+v %v", updated, err)
	}

	if err := s.Pin(n.ID); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if err := s.Archive(n.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if err := s.Delete(n.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(n.ID); err == nil {
		t.Fatal("Get after delete should fail")
	}
	any, err := s.GetAny(n.ID)
	if err != nil || any.DeletedAt == nil {
		t.Fatalf("GetAny deleted: %+v %v", any, err)
	}

	restored, err := s.Restore(n.ID)
	if err != nil || restored.DeletedAt != nil {
		t.Fatalf("Restore: %+v %v", restored, err)
	}
	if _, err := s.Get(n.ID); err != nil {
		t.Fatalf("Get after restore: %v", err)
	}

	var logs int
	if err := s.db.Conn().QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_type = 'note' AND entity_id = ?`, n.ID,
	).Scan(&logs); err != nil {
		t.Fatalf("count action_log: %v", err)
	}
	// create, update, pin, archive, delete, restore
	if logs < 6 {
		t.Fatalf("action_log rows = %d, want at least 6", logs)
	}
}

func TestListUnlimited(t *testing.T) {
	s := openTestStore(t)
	for i := 0; i < 55; i++ {
		if _, err := s.Create("n", "b"); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	all, err := s.List(ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 55 {
		t.Fatalf("List unlimited = %d, want 55", len(all))
	}
	capped, err := s.List(ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List limit: %v", err)
	}
	if len(capped) != 10 {
		t.Fatalf("List limit 10 = %d", len(capped))
	}
}
