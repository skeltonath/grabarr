package repository

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migsqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrateUp applies any pending migrations and returns how many ran.
func migrateUp(db *sql.DB) (int, error) {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return 0, fmt.Errorf("migration source: %w", err)
	}
	driver, err := migsqlite.WithInstance(db, &migsqlite.Config{})
	if err != nil {
		return 0, fmt.Errorf("migration driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", driver)
	if err != nil {
		return 0, fmt.Errorf("migrate init: %w", err)
	}

	before, _, _ := m.Version()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 0, fmt.Errorf("migrate up: %w", err)
	}
	after, _, _ := m.Version()
	if after <= before {
		return 0, nil
	}
	return int(after - before), nil
}
