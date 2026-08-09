package db

import (
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMigration00038_BackfillsWorkspaceIDFromOwnersWorkspace covers
// 00038_add_workspace_id_to_calendars.sql's backfill (#155, ADR-0045):
// every pre-existing Calendar (which has no workspace_id column before this
// migration runs) moves into the Workspace already owned by its owner,
// joining on workspaces.owner_user_id = calendars.user_id. Migrates only up
// to version 37 first, seeds a User/Workspace/Calendar by hand against the
// pre-38 schema, then runs migration 38 and asserts the backfill landed
// each Calendar in its owner's Workspace — including a User who owns more
// than one Calendar, which must all land in the same Workspace.
func TestMigration00038_BackfillsWorkspaceIDFromOwnersWorkspace(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(1)

	goose.SetBaseFS(migrations)
	defer goose.SetBaseFS(nil)

	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set migration dialect: %v", err)
	}

	if err := goose.UpTo(sqlDB, "migrations", 37); err != nil {
		t.Fatalf("migrate up to version 37: %v", err)
	}

	// A User who owns a single Calendar.
	res, err := sqlDB.Exec(`INSERT INTO users (username, password_hash) VALUES (?, ?)`, "alice", "hash")
	if err != nil {
		t.Fatalf("insert alice: %v", err)
	}
	aliceID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("get alice's id: %v", err)
	}

	res, err = sqlDB.Exec(`INSERT INTO workspaces (name, owner_user_id) VALUES (?, ?)`, "Alice's Workspace", aliceID)
	if err != nil {
		t.Fatalf("insert alice's workspace: %v", err)
	}
	aliceWorkspaceID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("get alice's workspace id: %v", err)
	}

	res, err = sqlDB.Exec(`INSERT INTO calendars (id, user_id, name, color) VALUES (?, ?, ?, ?)`,
		"cal-alice-1", aliceID, "Personal", "#12809CFF")
	if err != nil {
		t.Fatalf("insert alice's calendar: %v", err)
	}
	if _, err := res.RowsAffected(); err != nil {
		t.Fatalf("check alice's calendar insert: %v", err)
	}

	// A second User who owns more than one Calendar — both must land in the
	// same Workspace after the backfill.
	res, err = sqlDB.Exec(`INSERT INTO users (username, password_hash) VALUES (?, ?)`, "bob", "hash")
	if err != nil {
		t.Fatalf("insert bob: %v", err)
	}
	bobID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("get bob's id: %v", err)
	}

	res, err = sqlDB.Exec(`INSERT INTO workspaces (name, owner_user_id) VALUES (?, ?)`, "Bob's Workspace", bobID)
	if err != nil {
		t.Fatalf("insert bob's workspace: %v", err)
	}
	bobWorkspaceID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("get bob's workspace id: %v", err)
	}

	if _, err := sqlDB.Exec(`INSERT INTO calendars (id, user_id, name, color) VALUES (?, ?, ?, ?)`,
		"cal-bob-1", bobID, "Work", "#E2483DFF"); err != nil {
		t.Fatalf("insert bob's first calendar: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO calendars (id, user_id, name, color) VALUES (?, ?, ?, ?)`,
		"cal-bob-2", bobID, "Family", "#6B9071FF"); err != nil {
		t.Fatalf("insert bob's second calendar: %v", err)
	}

	if err := goose.UpTo(sqlDB, "migrations", 38); err != nil {
		t.Fatalf("migrate up to version 38: %v", err)
	}

	assertCalendarWorkspace := func(calendarID string, wantWorkspaceID int64) {
		t.Helper()
		var gotWorkspaceID int64
		if err := sqlDB.QueryRow(`SELECT workspace_id FROM calendars WHERE id = ?`, calendarID).Scan(&gotWorkspaceID); err != nil {
			t.Fatalf("query workspace_id for calendar %q: %v", calendarID, err)
		}
		if gotWorkspaceID != wantWorkspaceID {
			t.Fatalf("calendar %q workspace_id = %d, want %d", calendarID, gotWorkspaceID, wantWorkspaceID)
		}
	}

	assertCalendarWorkspace("cal-alice-1", aliceWorkspaceID)
	assertCalendarWorkspace("cal-bob-1", bobWorkspaceID)
	assertCalendarWorkspace("cal-bob-2", bobWorkspaceID)
}
