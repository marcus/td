package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestInitCreatesTodosDirectory tests that init creates the .todos directory
func TestInitCreatesTodosDirectory(t *testing.T) {
	dir := t.TempDir()

	// Initialize database
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Check that .todos directory exists
	todosPath := filepath.Join(dir, ".todos")
	if info, err := os.Stat(todosPath); err != nil || !info.IsDir() {
		t.Errorf("Expected .todos directory to exist at %s", todosPath)
	}
}

// TestInitCreatesSQLiteDatabase tests that init creates the SQLite database
func TestInitCreatesSQLiteDatabase(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Check that database file exists
	dbPath := filepath.Join(dir, ".todos", "issues.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("Expected issues.db to exist at %s", dbPath)
	}
}

// TestInitIdempotent tests that init can be called multiple times safely
func TestInitIdempotent(t *testing.T) {
	dir := t.TempDir()

	// First init
	database1, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("First Initialize failed: %v", err)
	}
	database1.Close()

	// Second init (should not fail)
	database2, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Second Initialize failed: %v", err)
	}
	defer database2.Close()

	// Check both succeeded
	todosPath := filepath.Join(dir, ".todos")
	if _, err := os.Stat(todosPath); err != nil {
		t.Error("Expected .todos directory to still exist")
	}
}

// TestInitWithExistingStructure tests init with existing directory structure
func TestInitWithExistingStructure(t *testing.T) {
	dir := t.TempDir()

	// Create .todos directory first
	todosPath := filepath.Join(dir, ".todos")
	if err := os.MkdirAll(todosPath, 0755); err != nil {
		t.Fatalf("Failed to create .todos directory: %v", err)
	}

	// Initialize should still work
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize with existing .todos failed: %v", err)
	}
	defer database.Close()

	// Verify it worked
	if _, err := os.Stat(todosPath); err != nil {
		t.Error(".todos directory should exist")
	}
}

// TestInitSetup tests basic database setup
func TestInitSetup(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Verify database is usable by creating an issue
	issue := &models.Issue{Title: "Test Setup"}
	if err := database.CreateIssue(issue); err != nil {
		t.Errorf("Database operation failed: %v", err)
	}
}

// TestInitPermissions tests that created directories have proper permissions
func TestInitPermissions(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	todosPath := filepath.Join(dir, ".todos")
	info, err := os.Stat(todosPath)
	if err != nil {
		t.Fatalf("Failed to stat .todos: %v", err)
	}

	// Check that directory is readable and writable
	if (info.Mode() & 0700) == 0 {
		t.Error("Expected .todos directory to be readable/writable")
	}
}
