package testutil

import (
	"grabarr/internal/repository"
	"os"
	"testing"
)

// SetupTestDB creates an in-memory SQLite database for testing
func SetupTestDB(t *testing.T) *repository.Repository {
	t.Helper()

	// Use in-memory database
	repo, err := repository.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}

	t.Cleanup(func() {
		repo.Close()
	})

	return repo
}

// SetupTestDBWithFile creates a temporary file-based SQLite database for testing
func SetupTestDBWithFile(t *testing.T) (*repository.Repository, string) {
	t.Helper()

	// Create temporary database file
	tmpFile, err := os.CreateTemp("", "grabarr-test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp database file: %v", err)
	}
	dbPath := tmpFile.Name()
	tmpFile.Close()

	repo, err := repository.New(dbPath)
	if err != nil {
		os.Remove(dbPath)
		t.Fatalf("failed to create test database: %v", err)
	}

	t.Cleanup(func() {
		repo.Close()
		os.Remove(dbPath)
	})

	return repo, dbPath
}