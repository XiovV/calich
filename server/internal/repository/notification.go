package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Notification kind values (ADR-0061): what produced this record —
// KindReminder for a fired Notification-Channel Reminder, KindInvite for
// being made an Attendee of an Event.
const (
	KindReminder = "reminder"
	KindInvite   = "invite"
)

// Notification is the persistent in-app record of something that concerns
// one User (ADR-0021, ADR-0061). Title is a copy of the Event's title at
// write time, not a live join, so a Notification keeps reading correctly
// even after the Event is later edited. OccurrenceStart is meaningful only
// for Kind == KindReminder — an invite concerns the Event series rather
// than one Occurrence, so it's nil there.
type Notification struct {
	ID              int64
	UserID          int64
	EventID         string
	Kind            string
	OccurrenceStart *time.Time
	Title           string
	FiredAt         time.Time
	Seen            bool
}

// NotificationRepository stores Notifications (ADR-0021, ADR-0061) — the
// in-app feed's backing store, fed by the firing engine's Dispatcher seam
// for reminders and by EventService's Attendee-invite seam for invites.
type NotificationRepository struct {
	db DBTX
}

func NewNotificationRepository(db *sql.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx — used by the
// Attendee-invite write path (EventService.txRepos), which writes the
// attendees row and the invite Notification atomically.
func (r *NotificationRepository) WithTx(tx *sql.Tx) *NotificationRepository {
	return &NotificationRepository{db: tx}
}

// Insert records a newly-fired Notification-Channel Reminder for userID
// (ADR-0021). The Notification-channel dispatch seam's only write.
func (r *NotificationRepository) Insert(ctx context.Context, userID int64, eventID string, occurrenceStart time.Time, title string, firedAt time.Time) (Notification, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO notifications (user_id, event_id, kind, occurrence_start, title, fired_at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, eventID, KindReminder, occurrenceStart.UTC(), title, firedAt.UTC(),
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
		Kind:            KindReminder,
		OccurrenceStart: &occurrenceStart,
		Title:           title,
		FiredAt:         firedAt,
		Seen:            false,
	}, nil
}

// InsertInvite records that userID was made an Attendee of eventID
// (ADR-0061) — written once per Attendee row, never per Occurrence, since
// an invite concerns the whole series rather than any one Occurrence.
func (r *NotificationRepository) InsertInvite(ctx context.Context, userID int64, eventID string, title string, invitedAt time.Time) (Notification, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO notifications (user_id, event_id, kind, occurrence_start, title, fired_at) VALUES (?, ?, ?, NULL, ?, ?)`,
		userID, eventID, KindInvite, title, invitedAt.UTC(),
	)
	if err != nil {
		return Notification{}, fmt.Errorf("insert invite notification: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return Notification{}, fmt.Errorf("get last insert id: %w", err)
	}

	return Notification{
		ID:      id,
		UserID:  userID,
		EventID: eventID,
		Kind:    KindInvite,
		Title:   title,
		FiredAt: invitedAt,
		Seen:    false,
	}, nil
}

// ListRecentByUser returns userID's most recent Notifications, newest first,
// capped at limit.
func (r *NotificationRepository) ListRecentByUser(ctx context.Context, userID int64, limit int) ([]Notification, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, event_id, kind, occurrence_start, title, fired_at, seen
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
		var occurrenceStart sql.NullTime
		if err := rows.Scan(&n.ID, &n.UserID, &n.EventID, &n.Kind, &occurrenceStart, &n.Title, &n.FiredAt, &n.Seen); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		if occurrenceStart.Valid {
			n.OccurrenceStart = &occurrenceStart.Time
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notifications: %w", err)
	}

	return notifications, nil
}

// MarkAllSeen marks every one of userID's currently-unseen Notifications as
// seen — opening the feed panel clears the unseen indicator wholesale,
// invite and reminder Notifications alike.
func (r *NotificationRepository) MarkAllSeen(ctx context.Context, userID int64) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE notifications SET seen = 1 WHERE user_id = ? AND seen = 0`, userID); err != nil {
		return fmt.Errorf("mark notifications seen: %w", err)
	}
	return nil
}
