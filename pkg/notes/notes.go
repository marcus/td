// Package notes is the public Go API for td notes.
//
// Sidecar and other in-process clients should open a Store and call these
// methods instead of speaking SQL against .todos/issues.db. Writes go through
// internal/db (withWriteLock + action_log), the same path as `td note`.
package notes

import (
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// Note is a freeform note stored in the project issues.db.
type Note struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Pinned    bool       `json:"pinned"`
	Archived  bool       `json:"archived"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// ListOptions filters List. Limit <= 0 means unlimited.
type ListOptions struct {
	Pinned         *bool
	Archived       *bool
	IncludeDeleted bool
	Search         string
	Limit          int
}

// Store is a long-lived handle on a project's notes.
type Store struct {
	db *db.DB
}

// Open opens the td database at baseDir (resolving .td-root) for notes access.
func Open(baseDir string) (*Store, error) {
	database, err := db.Open(baseDir)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	return &Store{db: database}, nil
}

// Init creates a new td database at baseDir and returns a notes Store.
// Production Sidecar should call Open; Init is for tests and first-run setups.
func Init(baseDir string) (*Store, error) {
	database, err := db.Initialize(baseDir)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	return &Store{db: database}, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Create inserts a note.
func (s *Store) Create(title, content string) (*Note, error) {
	n, err := s.db.CreateNote(title, content)
	return fromModel(n), err
}

// Get returns a live (not soft-deleted) note.
func (s *Store) Get(id string) (*Note, error) {
	n, err := s.db.GetNote(id)
	return fromModel(n), err
}

// GetAny returns a note including soft-deleted rows.
func (s *Store) GetAny(id string) (*Note, error) {
	n, err := s.db.GetNoteIncludingDeleted(id)
	return fromModel(n), err
}

// List returns notes matching opts. Limit <= 0 means unlimited.
func (s *Store) List(opts ListOptions) ([]Note, error) {
	rows, err := s.db.ListNotes(db.ListNotesOptions{
		Pinned:         opts.Pinned,
		Archived:       opts.Archived,
		IncludeDeleted: opts.IncludeDeleted,
		Search:         opts.Search,
		Limit:          opts.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Note, 0, len(rows))
	for i := range rows {
		if n := fromModel(&rows[i]); n != nil {
			out = append(out, *n)
		}
	}
	return out, nil
}

// Update changes title and content.
func (s *Store) Update(id, title, content string) (*Note, error) {
	n, err := s.db.UpdateNote(id, title, content)
	return fromModel(n), err
}

// Delete soft-deletes a note.
func (s *Store) Delete(id string) error {
	return s.db.DeleteNote(id)
}

// Restore undeletes a soft-deleted note.
func (s *Store) Restore(id string) (*Note, error) {
	n, err := s.db.RestoreNote(id)
	return fromModel(n), err
}

// Pin pins a note.
func (s *Store) Pin(id string) error { return s.db.PinNote(id) }

// Unpin unpins a note.
func (s *Store) Unpin(id string) error { return s.db.UnpinNote(id) }

// Archive archives a note.
func (s *Store) Archive(id string) error { return s.db.ArchiveNote(id) }

// Unarchive unarchives a note.
func (s *Store) Unarchive(id string) error { return s.db.UnarchiveNote(id) }

func fromModel(n *models.Note) *Note {
	if n == nil {
		return nil
	}
	out := &Note{
		ID:        n.ID,
		Title:     n.Title,
		Content:   n.Content,
		CreatedAt: n.CreatedAt,
		UpdatedAt: n.UpdatedAt,
		Pinned:    n.Pinned,
		Archived:  n.Archived,
	}
	if n.DeletedAt != nil {
		t := *n.DeletedAt
		out.DeletedAt = &t
	}
	return out
}
