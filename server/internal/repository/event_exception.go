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
	db *sql.DB
}

func NewEventExceptionRepository(db *sql.DB) *EventExceptionRepository {
	return &EventExceptionRepository{db: db}
}

func (r *EventExceptionRepository) Add(ctx context.Context, parentID string, occurrenceStart time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO event_exceptions (parent_id, occurrence_start) VALUES (?, ?)`,
		parentID, occurrenceStart,
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
	defer rows.Close()

	for rows.Next() {
		var parentID string
		var occurrenceStart time.Time
		if err := rows.Scan(&parentID, &occurrenceStart); err != nil {
			return nil, fmt.Errorf("scan exception: %w", err)
		}
		result[parentID] = append(result[parentID], occurrenceStart)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate exceptions: %w", err)
	}

	return result, nil
}

// ReparentFrom moves every Exception of oldParentID at-or-after fromStart to
// belong to newParentID instead — the "this and following" split reparenting
// Exceptions at the boundary (ADR-0016).
func (r *EventExceptionRepository) ReparentFrom(ctx context.Context, oldParentID, newParentID string, fromStart time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE event_exceptions SET parent_id = ? WHERE parent_id = ? AND occurrence_start >= ?`,
		newParentID, oldParentID, fromStart,
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
