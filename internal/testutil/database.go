package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"grabarr/internal/repository"
)

// SetupTestDB creates a throwaway SQLite database for testing.
//
// It is file-backed rather than ":memory:" because the repository runs a
// connection pool, and every connection to ":memory:" gets its own private,
// empty database — so any code that queries from a second goroutine sees no
// tables at all. The file lives in the test's temp dir and goes away with it.
func SetupTestDB(t *testing.T) *repository.Repository {
	t.Helper()

	repo, err := repository.New(filepath.Join(t.TempDir(), "grabarr-test.db"))
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
