package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens (creating if necessary) the SQLite database at $dataDir/calich.db
// and runs any pending migrations against it.
func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	// Attachments' bytes (#132, ADR-0040) live under here too, so a backup
	// of dataDir now needs more than calich.db — see the README.
	if err := os.MkdirAll(filepath.Join(dataDir, "attachments"), 0o755); err != nil {
		return nil, fmt.Errorf("create attachments dir: %w", err)
	}

	// SQLite disables foreign key enforcement per-connection by default —
	// without this, the ON DELETE CASCADE constraints in our schema (e.g.
	// events cascading off their calendar) would silently do nothing.
	dsn := filepath.Join(dataDir, "calich.db") + "?_pragma=foreign_keys(1)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	goose.SetBaseFS(migrations)
	defer goose.SetBaseFS(nil)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}

	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}
