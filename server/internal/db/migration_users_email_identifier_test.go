package db

import (
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMigration00044_FailsLoudlyOnDuplicateEmail covers 00044's fail-loud
// decision (#169, ADR-0047): two pre-existing accounts sharing an email
// can't both satisfy the new email UNIQUE constraint, so the migration must
// abort rather than silently inventing a placeholder for either one.
func TestMigration00044_FailsLoudlyOnDuplicateEmail(t *testing.T) {
	sqlDB := openPreMigrationUsersDB(t)

	if _, err := sqlDB.Exec(`INSERT INTO users (username, password_hash, email) VALUES (?, ?, ?)`,
		"alice", "hash", "shared@example.com"); err != nil {
		t.Fatalf("insert alice: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO users (username, password_hash, email) VALUES (?, ?, ?)`,
		"bob", "hash", "shared@example.com"); err != nil {
		t.Fatalf("insert bob: %v", err)
	}

	if err := goose.UpTo(sqlDB, "migrations", 44); err == nil {
		t.Fatal("expected migration 44 to fail on a duplicate email, got nil error")
	}

	assertUsersTableUnmigrated(t, sqlDB)
}

// TestMigration00044_FailsLoudlyOnNullEmail covers the other fail-loud case:
// an account with no email at all (the pre-#169 norm — email was optional)
// can't satisfy the new NOT NULL constraint.
func TestMigration00044_FailsLoudlyOnNullEmail(t *testing.T) {
	sqlDB := openPreMigrationUsersDB(t)

	if _, err := sqlDB.Exec(`INSERT INTO users (username, password_hash) VALUES (?, ?)`,
		"alice", "hash"); err != nil {
		t.Fatalf("insert alice with no email: %v", err)
	}

	if err := goose.UpTo(sqlDB, "migrations", 44); err == nil {
		t.Fatal("expected migration 44 to fail on a null email, got nil error")
	}

	assertUsersTableUnmigrated(t, sqlDB)
}

// TestMigration00044_SucceedsWhenEveryAccountHasAUniqueEmail is the
// clean-data counterpart: once every row already satisfies NOT NULL UNIQUE,
// the rebuild succeeds and username becomes name.
func TestMigration00044_SucceedsWhenEveryAccountHasAUniqueEmail(t *testing.T) {
	sqlDB := openPreMigrationUsersDB(t)

	if _, err := sqlDB.Exec(`INSERT INTO users (username, password_hash, email) VALUES (?, ?, ?)`,
		"alice", "hash", "alice@example.com"); err != nil {
		t.Fatalf("insert alice: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO users (username, password_hash, email) VALUES (?, ?, ?)`,
		"bob", "hash", "bob@example.com"); err != nil {
		t.Fatalf("insert bob: %v", err)
	}

	if err := goose.UpTo(sqlDB, "migrations", 44); err != nil {
		t.Fatalf("migrate up to version 44: %v", err)
	}

	rows, err := sqlDB.Query(`SELECT name, email FROM users ORDER BY name`)
	if err != nil {
		t.Fatalf("query migrated users: %v", err)
	}
	defer rows.Close()

	type row struct{ name, email string }
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.name, &r.email); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		got = append(got, r)
	}

	want := []row{{"alice", "alice@example.com"}, {"bob", "bob@example.com"}}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// openPreMigrationUsersDB opens an in-memory database migrated up to just
// before 00044, so tests can seed rows against the old (username, nullable
// email) shape.
func openPreMigrationUsersDB(t *testing.T) *sql.DB {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)

	goose.SetBaseFS(migrations)
	t.Cleanup(func() { goose.SetBaseFS(nil) })

	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set migration dialect: %v", err)
	}

	if err := goose.UpTo(sqlDB, "migrations", 43); err != nil {
		t.Fatalf("migrate up to version 43: %v", err)
	}

	return sqlDB
}

// assertUsersTableUnmigrated checks the users table is still on the
// pre-00044 shape (a username column exists) — i.e. the failed migration's
// table-rebuild transaction rolled back cleanly rather than half-applying.
func assertUsersTableUnmigrated(t *testing.T, sqlDB *sql.DB) {
	t.Helper()

	var name string
	if err := sqlDB.QueryRow(`SELECT username FROM users LIMIT 1`).Scan(&name); err != nil {
		t.Fatalf("expected users.username to still exist after a rolled-back migration: %v", err)
	}
}
