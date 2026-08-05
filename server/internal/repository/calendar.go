package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Calendar struct {
	ID        string
	UserID    int64
	Name      string
	Color     string
	CreatedAt time.Time
}

type CalendarRepository struct {
	db *sql.DB
}

func NewCalendarRepository(db *sql.DB) *CalendarRepository {
	return &CalendarRepository{db: db}
}

func (r *CalendarRepository) Create(ctx context.Context, userID int64, id, name, color string) (Calendar, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO calendars (id, user_id, name, color) VALUES (?, ?, ?, ?)`,
		id, userID, name, color,
	); err != nil {
		return Calendar{}, fmt.Errorf("insert calendar: %w", err)
	}

	return r.GetByID(ctx, userID, id)
}

func (r *CalendarRepository) GetByID(ctx context.Context, userID int64, id string) (Calendar, error) {
	return r.scanCalendar(r.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, color, created_at FROM calendars WHERE user_id = ? AND id = ?`, userID, id,
	))
}

// ListByUser returns a user's calendars ordered by creation time, oldest first.
func (r *CalendarRepository) ListByUser(ctx context.Context, userID int64) ([]Calendar, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, name, color, created_at FROM calendars WHERE user_id = ? ORDER BY created_at, id`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}
	defer rows.Close()

	calendars := []Calendar{}
	for rows.Next() {
		var c Calendar
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.Color, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan calendar: %w", err)
		}
		calendars = append(calendars, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate calendars: %w", err)
	}

	return calendars, nil
}

func (r *CalendarRepository) Update(ctx context.Context, userID int64, id, name, color string) (Calendar, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendars SET name = ?, color = ? WHERE user_id = ? AND id = ?`,
		name, color, userID, id,
	)
	if err != nil {
		return Calendar{}, fmt.Errorf("update calendar: %w", err)
	}

	if err := requireAffected(res); err != nil {
		return Calendar{}, err
	}

	return r.GetByID(ctx, userID, id)
}

func (r *CalendarRepository) Delete(ctx context.Context, userID int64, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM calendars WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return fmt.Errorf("delete calendar: %w", err)
	}

	return requireAffected(res)
}

func (r *CalendarRepository) scanCalendar(row *sql.Row) (Calendar, error) {
	var c Calendar
	err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.Color, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Calendar{}, ErrNotFound
	}
	if err != nil {
		return Calendar{}, fmt.Errorf("scan calendar: %w", err)
	}
	return c, nil
}
