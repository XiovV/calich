package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Notification kind values (ADR-0061): what produced this record —
// KindReminder for a fired Notification-Channel Reminder, KindInvite for
// being made an Attendee of an Event, KindWriteBackFailed for a Write-back
// push that permanently failed on a writable Linked Calendar (#299,
// ADR-0081) — coalesced on the Source rather than raised per Event, so a
// dead grant failing dozens of queued pushes at once still raises exactly
// one.
const (
	KindReminder        = "reminder"
	KindInvite          = "invite"
	KindWriteBackFailed = "writeback_failed"
)

// Notification is the persistent in-app record of something that concerns
// one User (ADR-0021, ADR-0061). Title is a copy of the Event's (or, for
// KindWriteBackFailed, the Calendar's) title at write time, not a live join,
// so a Notification keeps reading correctly even after that row is later
// edited. EventID is nil only for KindWriteBackFailed, which concerns a
// Calendar rather than any one Event; CalendarID is nil for every other
// kind. OccurrenceStart is meaningful only for Kind == KindReminder — every
// other kind concerns a whole series or a whole Calendar, not one
// Occurrence, so it's nil there.
type Notification struct {
	ID              int64
	UserID          int64
	EventID         *string
	CalendarID      *string
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
		EventID:         &eventID,
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
		EventID: &eventID,
		Kind:    KindInvite,
		Title:   title,
		FiredAt: invitedAt,
		Seen:    false,
	}, nil
}

// UpsertWriteBackFailure records that userID's Write-back to calendarID's
// Provider has permanently failed (#299, ADR-0081, ADR-0079's own thesis
// applied to a phone with no protocol affordance to be told later). Coalesced
// on (user_id, calendar_id) via the partial unique index migration 00008
// creates: a Source whose grant is dead fails every one of its queued pushes
// at once, so re-raising the same failure upserts the existing row — fresh
// title, fresh firedAt, seen reset to false — rather than adding a second
// one that would make "how many things are wrong" indistinguishable from
// "how many pushes failed".
func (r *NotificationRepository) UpsertWriteBackFailure(ctx context.Context, userID int64, calendarID, title string, firedAt time.Time) (Notification, error) {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO notifications (user_id, calendar_id, kind, title, fired_at, seen)
		 VALUES (?, ?, ?, ?, ?, 0)
		 ON CONFLICT (user_id, calendar_id) WHERE kind = 'writeback_failed'
		 DO UPDATE SET title = excluded.title, fired_at = excluded.fired_at, seen = 0`,
		userID, calendarID, KindWriteBackFailed, title, firedAt.UTC(),
	)
	if err != nil {
		return Notification{}, fmt.Errorf("upsert write-back failure notification: %w", err)
	}

	n, err := scanNotificationRow(r.db.QueryRowContext(ctx,
		`SELECT id, user_id, event_id, calendar_id, kind, occurrence_start, title, fired_at, seen
		 FROM notifications WHERE user_id = ? AND calendar_id = ? AND kind = ?`,
		userID, calendarID, KindWriteBackFailed,
	))
	if err != nil {
		return Notification{}, fmt.Errorf("get upserted write-back failure notification: %w", err)
	}
	return n, nil
}

// ListRecentByUser returns userID's most recent Notifications, newest first,
// capped at limit.
func (r *NotificationRepository) ListRecentByUser(ctx context.Context, userID int64, limit int) ([]Notification, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, event_id, calendar_id, kind, occurrence_start, title, fired_at, seen
		 FROM notifications WHERE user_id = ? ORDER BY fired_at DESC, id DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	return collectRows(rows, scanNotificationRow)
}

func scanNotificationRow(row rowScanner) (Notification, error) {
	var n Notification
	var eventID, calendarID sql.NullString
	var occurrenceStart sql.NullTime
	if err := row.Scan(&n.ID, &n.UserID, &eventID, &calendarID, &n.Kind, &occurrenceStart, &n.Title, &n.FiredAt, &n.Seen); err != nil {
		return Notification{}, err
	}
	if eventID.Valid {
		n.EventID = &eventID.String
	}
	if calendarID.Valid {
		n.CalendarID = &calendarID.String
	}
	if occurrenceStart.Valid {
		n.OccurrenceStart = &occurrenceStart.Time
	}
	return n, nil
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
