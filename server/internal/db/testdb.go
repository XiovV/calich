package db

import "database/sql"

// OpenInMemory opens a fresh in-memory SQLite database with all migrations
// applied. Intended for use in repository tests.
func OpenInMemory() (*sql.DB, error) {
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}

	// An in-memory SQLite database is scoped to a single connection; without
	// this, database/sql may open a second connection that sees an empty
	// (unmigrated) database.
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
