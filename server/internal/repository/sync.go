package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// DeletedObject is a tombstone for a series deleted from a Calendar: the
// row a live Event would otherwise leave behind for sync-collection to
// diff against no longer exists once it's deleted, so its removal is
// recorded separately (ADR-0025).
type DeletedObject struct {
	CalendarID string
	UID        string
	ChangeSeq  int64
}

// SyncRepository owns the single global change_seq counter and the delete
// tombstones that back CalDAV's CTag and sync-collection REPORT (ADR-0025,
// #65).
type SyncRepository struct {
	db DBTX
}

func NewSyncRepository(db *sql.DB) *SyncRepository {
	return &SyncRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018).
func (r *SyncRepository) WithTx(tx *sql.Tx) *SyncRepository {
	return &SyncRepository{db: tx}
}

// NextChangeSeq bumps the single global change_seq counter and returns its
// new value. Every write to a series — a Master or Override create/update,
// a delete, an Exception, or a reparent — calls this exactly once per
// logical change (ADR-0025).
func (r *SyncRepository) NextChangeSeq(ctx context.Context) (int64, error) {
	var value int64
	err := r.db.QueryRowContext(ctx,
		`UPDATE change_sequence SET value = value + 1 WHERE id = 1 RETURNING value`,
	).Scan(&value)
	if err != nil {
		return 0, fmt.Errorf("bump change sequence: %w", err)
	}
	return value, nil
}

// Tombstone records a series' deletion under calendarID so a later
// sync-collection REPORT can report it as removed, even though no Event row
// survives the delete to read (ADR-0025).
func (r *SyncRepository) Tombstone(ctx context.Context, calendarID, uid string, changeSeq int64) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO deleted_objects (calendar_id, uid, change_seq) VALUES (?, ?, ?)`,
		calendarID, uid, changeSeq,
	); err != nil {
		return fmt.Errorf("insert tombstone: %w", err)
	}
	return nil
}

// CTag returns calendarID's CTag: the highest change_seq among its live
// Events and its tombstones, or 0 if it has never been written to
// (ADR-0025).
func (r *SyncRepository) CTag(ctx context.Context, calendarID string) (int64, error) {
	var ctag int64
	err := r.db.QueryRowContext(ctx, `
		SELECT MAX(seq) FROM (
			SELECT COALESCE(MAX(change_seq), 0) AS seq FROM events WHERE calendar_id = ?
			UNION ALL
			SELECT COALESCE(MAX(change_seq), 0) AS seq FROM deleted_objects WHERE calendar_id = ?
		)`, calendarID, calendarID,
	).Scan(&ctag)
	if err != nil {
		return 0, fmt.Errorf("compute ctag: %w", err)
	}
	return ctag, nil
}

// DeletedSince returns calendarID's tombstones recorded after since, ordered
// by change_seq — the removed half of a sync-collection REPORT's diff
// (ADR-0025).
func (r *SyncRepository) DeletedSince(ctx context.Context, calendarID string, since int64) ([]DeletedObject, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT calendar_id, uid, change_seq FROM deleted_objects WHERE calendar_id = ? AND change_seq > ? ORDER BY change_seq`,
		calendarID, since,
	)
	if err != nil {
		return nil, fmt.Errorf("list deleted objects: %w", err)
	}
	defer rows.Close()

	deleted := []DeletedObject{}
	for rows.Next() {
		var d DeletedObject
		if err := rows.Scan(&d.CalendarID, &d.UID, &d.ChangeSeq); err != nil {
			return nil, fmt.Errorf("scan deleted object: %w", err)
		}
		deleted = append(deleted, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deleted objects: %w", err)
	}
	return deleted, nil
}
