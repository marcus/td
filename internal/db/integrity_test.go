package db

import "testing"

func TestQuickCheckHealthyDatabase(t *testing.T) {
	database, err := Initialize(t.TempDir())
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer func() { _ = database.Close() }()

	if err := database.QuickCheck(); err != nil {
		t.Fatalf("QuickCheck: %v", err)
	}
}
