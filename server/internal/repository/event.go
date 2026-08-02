package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Event struct {
	ID         string
	UserID     int64
	CalendarID string
	Title      string
	Start      time.Time
	End        time.Time
	CreatedAt  time.Time
}

type EventRepository struct {
	db *sql.DB
}

func NewEventRepository(db *sql.DB) *EventRepository {
	return &EventRepository{db: db}
}

func (r *EventRepository) Create(ctx context.Context, id string, userID int64, calendarID, title string, start, end time.Time) (Event, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO events (id, user_id, calendar_id, title, "start", "end") VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, calendarID, title, start, end,
	); err != nil {
		return Event{}, fmt.Errorf("insert event: %w", err)
	}

	return r.GetByID(ctx, userID, id)
}

func (r *EventRepository) GetByID(ctx context.Context, userID int64, id string) (Event, error) {
	return r.scanEvent(r.db.QueryRowContext(ctx,
		`SELECT id, user_id, calendar_id, title, "start", "end", created_at FROM events WHERE user_id = ? AND id = ?`,
		userID, id,
	))
}

// ListByUser returns a user's events ordered by start time. When from/to are
// non-nil, only events overlapping that half-open range are returned; either
// may be nil to leave that side of the range unbounded.
func (r *EventRepository) ListByUser(ctx context.Context, userID int64, from, to *time.Time) ([]Event, error) {
	query := `SELECT id, user_id, calendar_id, title, "start", "end", created_at FROM events WHERE user_id = ?`
	args := []any{userID}

	if from != nil {
		query += ` AND "end" > ?`
		args = append(args, *from)
	}
	if to != nil {
		query += ` AND "start" < ?`
		args = append(args, *to)
	}
	query += ` ORDER BY "start", id`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.UserID, &e.CalendarID, &e.Title, &e.Start, &e.End, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}

	return events, nil
}

func (r *EventRepository) Update(ctx context.Context, userID int64, id, calendarID, title string, start, end time.Time) (Event, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE events SET calendar_id = ?, title = ?, "start" = ?, "end" = ? WHERE user_id = ? AND id = ?`,
		calendarID, title, start, end, userID, id,
	)
	if err != nil {
		return Event{}, fmt.Errorf("update event: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return Event{}, fmt.Errorf("get rows affected: %w", err)
	}
	if affected == 0 {
		return Event{}, ErrNotFound
	}

	return r.GetByID(ctx, userID, id)
}

func (r *EventRepository) Delete(ctx context.Context, userID int64, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM events WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return fmt.Errorf("delete event: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *EventRepository) scanEvent(row *sql.Row) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.UserID, &e.CalendarID, &e.Title, &e.Start, &e.End, &e.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, fmt.Errorf("scan event: %w", err)
	}
	return e, nil
}
