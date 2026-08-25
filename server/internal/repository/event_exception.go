package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// EventExceptionRepository stores cancelled single Occurrences (iCalendar
// EXDATE) — the rule still generates that slot, but it is suppressed from
// expansion (ADR-0016).
type EventExceptionRepository struct {
	db DBTX
}

func NewEventExceptionRepository(db *sql.DB) *EventExceptionRepository {
	return &EventExceptionRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018).
func (r *EventExceptionRepository) WithTx(tx *sql.Tx) *EventExceptionRepository {
	return &EventExceptionRepository{db: tx}
}

func (r *EventExceptionRepository) Add(ctx context.Context, parentID string, occurrenceStart time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO event_exceptions (parent_id, occurrence_start) VALUES (?, ?)`,
		parentID, occurrenceStart.UTC(),
	)
	if err != nil {
		return fmt.Errorf("add exception: %w", err)
	}
	return nil
}

// ListByParentIDs returns each parent's Exception occurrence starts, keyed by
// parent_id. Parents with no Exceptions are simply absent from the map.
func (r *EventExceptionRepository) ListByParentIDs(ctx context.Context, parentIDs []string) (map[string][]time.Time, error) {
	result := make(map[string][]time.Time)
	if len(parentIDs) == 0 {
		return result, nil
	}

	query := `SELECT parent_id, occurrence_start FROM event_exceptions WHERE parent_id IN (` + placeholders(len(parentIDs)) + `)`
	args := make([]any, len(parentIDs))
	for i, id := range parentIDs {
		args[i] = id
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list exceptions: %w", err)
	}
	exceptions, err := collectRows(rows, scanExceptionRow)
	if err != nil {
		return nil, err
	}
	for _, e := range exceptions {
		result[e.ParentID] = append(result[e.ParentID], e.OccurrenceStart)
	}

	return result, nil
}

// exceptionRow is one ListByParentIDs row, grouped into its map after
// collectRows scans it.
type exceptionRow struct {
	ParentID        string
	OccurrenceStart time.Time
}

func scanExceptionRow(row rowScanner) (exceptionRow, error) {
	var e exceptionRow
	err := row.Scan(&e.ParentID, &e.OccurrenceStart)
	return e, err
}

// ReparentFrom moves every Exception of oldParentID at-or-after fromStart to
// belong to newParentID instead — the "this and following" split reparenting
// Exceptions at the boundary (ADR-0016).
func (r *EventExceptionRepository) ReparentFrom(ctx context.Context, oldParentID, newParentID string, fromStart time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE event_exceptions SET parent_id = ? WHERE parent_id = ? AND occurrence_start >= ?`,
		newParentID, oldParentID, fromStart.UTC(),
	)
	if err != nil {
		return fmt.Errorf("reparent exceptions: %w", err)
	}
	return nil
}

// DeleteByParentID deletes every Exception belonging to parentID. Used when a
// rule change forces the "All events" scope and discards per-Occurrence edits
// (ADR-0016).
func (r *EventExceptionRepository) DeleteByParentID(ctx context.Context, parentID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM event_exceptions WHERE parent_id = ?`, parentID)
	if err != nil {
		return fmt.Errorf("delete exceptions: %w", err)
	}
	return nil
}

func placeholders(n int) string {
	out := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '?')
	}
	return string(out)
}

// userFilter is the optional `user_id IN (...)` half of a Reminder-resolution
// read (ADR-0064): the three tables resolution reads all answer either for one
// User or for every User, and do it over one query each rather than a scoped
// and an unscoped variant of the same question. A nil userIDs means every
// User — an empty slice is not a way to ask for nobody, and no caller has one.
func userFilter(userIDs []int64) (clause string, args []any) {
	if len(userIDs) == 0 {
		return "", nil
	}
	args = make([]any, len(userIDs))
	for i, id := range userIDs {
		args[i] = id
	}
	return ` AND user_id IN (` + placeholders(len(userIDs)) + `)`, args
}
