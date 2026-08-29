// Package db owns the SQLite connection and schema lifecycle.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pressly/goose/v3"
	"github.com/timdumol/fitsync/migrations"
	_ "modernc.org/sqlite"
)

const BaseDSN = "file:/data/fitness.db?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"

// DSN returns the required SQLite URI for a database path.
func DSN(path string) string {
	if strings.HasPrefix(path, "file:") {
		return path
	}
	if path == "" {
		return BaseDSN
	}
	return "file:" + filepath.ToSlash(path) + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
}

// Open opens a bounded SQLite pool. All connection-level pragmas are carried
// in the URI, so newly-created pooled connections receive the same settings.
func Open(path string) (*sql.DB, error) {
	database, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	database.SetMaxOpenConns(8)
	database.SetMaxIdleConns(8)
	if err := database.Ping(); err != nil {
		database.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return database, nil
}

// Migrate applies all embedded Goose migrations before the database is used.
func Migrate(ctx context.Context, database *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	if err := goose.UpContext(ctx, database, "."); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// OpenAndMigrate opens a database and applies the schema atomically from the
// service's perspective. The caller owns the returned pool.
func OpenAndMigrate(ctx context.Context, path string) (*sql.DB, error) {
	database, err := Open(path)
	if err != nil {
		return nil, err
	}
	if err := Migrate(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}
