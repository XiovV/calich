package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Notification is the persistent in-app record created when a
// Notification-Channel Reminder fires (ADR-0021). Title is a copy of the
// Event's title at fire time, not a live join, so a Notification keeps
// reading correctly even after the Event is later edited.
type Notification struct {
	ID              int64
	UserID          int64
	EventID         string
	OccurrenceStart time.Time
	Title           string
	FiredAt         time.Time
	Seen            bool
}

// NotificationRepository stores Notifications (ADR-0021) — the in-app feed's
// backing store, fed by the firing engine's Dispatcher seam.
type NotificationRepository struct {
	db *sql.DB
}

func NewNotificationRepository(db *sql.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// Insert records a newly-fired Notification for userID.
func (r *NotificationRepository) Insert(ctx context.Context, userID int64, eventID string, occurrenceStart time.Time, title string, firedAt time.Time) (Notification, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO notifications (user_id, event_id, occurrence_start, title, fired_at) VALUES (?, ?, ?, ?, ?)`,
		userID, eventID, occurrenceStart, title, firedAt,
	)
	if err != nil {
		return Notification{}, fmt.Errorf("insert notification: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return Notification{}, fmt.Errorf("get last insert id: %w", err)
	}

	return Notification{
		ID:              id,
		UserID:          userID,
		EventID:         eventID,
		OccurrenceStart: occurrenceStart,
		Title:           title,
		FiredAt:         firedAt,
		Seen:            false,
	}, nil
}

// ListRecentByUser returns userID's most recent Notifications, newest first,
// capped at limit.
func (r *NotificationRepository) ListRecentByUser(ctx context.Context, userID int64, limit int) ([]Notification, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, event_id, occurrence_start, title, fired_at, seen
		 FROM notifications WHERE user_id = ? ORDER BY fired_at DESC, id DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()

	notifications := []Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.EventID, &n.OccurrenceStart, &n.Title, &n.FiredAt, &n.Seen); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notifications: %w", err)
	}

	return notifications, nil
}

// MarkAllSeen marks every one of userID's currently-unseen Notifications as
// seen — opening the feed panel clears the unseen indicator wholesale.
func (r *NotificationRepository) MarkAllSeen(ctx context.Context, userID int64) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE notifications SET seen = 1 WHERE user_id = ? AND seen = 0`, userID); err != nil {
		return fmt.Errorf("mark notifications seen: %w", err)
	}
	return nil
}
