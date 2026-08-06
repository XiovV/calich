package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Calendar struct {
	ID     string
	UserID int64
	Name   string
	Color  string
	// SourceURL is non-nil only on a Subscribed Calendar (#83, ADR-0032):
	// the external .ics feed URL it was created from. Set on insert only —
	// there is no rename-the-source flow, so Update never touches it.
	SourceURL *string
	CreatedAt time.Time
	// LastSyncedAt is when a Refresh last completed successfully against
	// this Calendar's SourceURL, whether or not it changed anything. Nil
	// until the first Refresh runs (#85, ADR-0033).
	LastSyncedAt *time.Time
	// ETag and LastModified are the feed's own response validators from its
	// last successful fetch, sent back as conditional-GET request headers on
	// the next Refresh. ContentHash is the fallback for a feed that sends
	// neither: a hash of the last fetched body, compared after a full fetch
	// since there's no validator to ask the server to compare for us. All
	// nil until the first Refresh runs, and only ever set on a Subscribed
	// Calendar (#85, ADR-0033).
	ETag, LastModified *string
	ContentHash        *string
	// NextRefreshAt is when the background poller should next attempt this
	// Calendar's Subscription — staggered at Subscribe time, recomputed
	// after every attempt. Nil on an ordinary Calendar (#86, ADR-0033).
	NextRefreshAt *time.Time
	// RefreshIntervalSeconds is the publisher's own stated poll cadence
	// (RFC 7986 REFRESH-INTERVAL or X-PUBLISHED-TTL), honoured only when
	// longer than the poller's own default. Nil when the feed states none,
	// or none has been observed yet.
	RefreshIntervalSeconds *int
	// FailureCount is how many consecutive Refresh attempts have failed
	// since the last success — drives exponential backoff, resets to 0 on
	// success. Never causes the Subscription to be disabled or deleted.
	FailureCount int
	// ErrorClass is "needs_attention" (a human must fix something: bad
	// auth, not found, an unparseable feed) or "retrying" (timeout, server
	// error, DNS — expected to clear on its own), nil while healthy.
	ErrorClass *string
	// ErrorMessage is the human-readable reason behind ErrorClass, nil
	// while healthy.
	ErrorMessage *string
	// KeepAlarms is a per-Subscription setting, off by default (#87,
	// ADR-0032): when false, a Refresh drops the feed's VALARMs; when true,
	// they become Reminders on both Channels, matching ICS import's
	// behaviour. Meaningless on an ordinary Calendar, which never sets it.
	KeepAlarms bool
}

// CalendarFields are a calendar's writable columns, gathered into one value
// the same way EventFields already gathers an event's — so Create and Update
// take one argument each instead of separately threading every column.
//
// SourceURL is set on insert only (like ParentID/RecurrenceID on
// EventFields) — Update's SQL ignores it, since a Calendar's source is
// fixed at subscribe time.
type CalendarFields struct {
	Name      string
	Color     string
	SourceURL *string
	// KeepAlarms is set on insert only, like SourceURL — Update's SQL
	// ignores it too, since changing it later goes through the dedicated
	// UpdateKeepAlarms (#87, ADR-0032).
	KeepAlarms bool
}

type CalendarRepository struct {
	db *sql.DB
}

func NewCalendarRepository(db *sql.DB) *CalendarRepository {
	return &CalendarRepository{db: db}
}

func (r *CalendarRepository) Create(ctx context.Context, userID int64, id string, fields CalendarFields) (Calendar, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO calendars (id, user_id, name, color, source_url, keep_alarms) VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, fields.Name, fields.Color, fields.SourceURL, fields.KeepAlarms,
	); err != nil {
		return Calendar{}, fmt.Errorf("insert calendar: %w", err)
	}

	return r.GetByID(ctx, userID, id)
}

const calendarColumns = `id, user_id, name, color, source_url, created_at, last_synced_at, etag, last_modified, content_hash, next_refresh_at, refresh_interval_seconds, failure_count, error_class, error_message, keep_alarms`

func (r *CalendarRepository) GetByID(ctx context.Context, userID int64, id string) (Calendar, error) {
	return r.scanCalendar(r.db.QueryRowContext(ctx,
		`SELECT `+calendarColumns+` FROM calendars WHERE user_id = ? AND id = ?`, userID, id,
	))
}

// ListByUser returns a user's calendars ordered by creation time, oldest first.
func (r *CalendarRepository) ListByUser(ctx context.Context, userID int64) ([]Calendar, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+calendarColumns+` FROM calendars WHERE user_id = ? ORDER BY created_at, id`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}
	defer rows.Close()

	calendars := []Calendar{}
	for rows.Next() {
		c, err := scanCalendarRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan calendar: %w", err)
		}
		calendars = append(calendars, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate calendars: %w", err)
	}

	return calendars, nil
}

// ListDueForRefresh returns every Subscribed Calendar (source_url set) whose
// next_refresh_at has come due — the background poller's read path (#86,
// ADR-0033). Ordinary Calendars (next_refresh_at NULL) never match.
func (r *CalendarRepository) ListDueForRefresh(ctx context.Context, now time.Time) ([]Calendar, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+calendarColumns+` FROM calendars WHERE source_url IS NOT NULL AND next_refresh_at IS NOT NULL AND next_refresh_at <= ? ORDER BY next_refresh_at, id`, now,
	)
	if err != nil {
		return nil, fmt.Errorf("list due subscriptions: %w", err)
	}
	defer rows.Close()

	calendars := []Calendar{}
	for rows.Next() {
		c, err := scanCalendarRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan calendar: %w", err)
		}
		calendars = append(calendars, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due subscriptions: %w", err)
	}

	return calendars, nil
}

func (r *CalendarRepository) Update(ctx context.Context, userID int64, id string, fields CalendarFields) (Calendar, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendars SET name = ?, color = ? WHERE user_id = ? AND id = ?`,
		fields.Name, fields.Color, userID, id,
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

// RefreshSuccess is what a completed, successful Refresh persists on a
// Subscribed Calendar (#85, #86, ADR-0033): the response validators (or
// content hash) to send back on the next conditional GET, when it
// completed, when the poller should attempt it next, and the publisher's
// own stated cadence if this fetch observed one. A success always resets
// FailureCount to 0 and clears any error, whatever it was — recovery is
// unconditional.
type RefreshSuccess struct {
	SyncedAt                        time.Time
	ETag, LastModified, ContentHash *string
	NextRefreshAt                   time.Time
	RefreshIntervalSeconds          *int
}

// RecordRefreshSuccess records a Refresh's successful outcome, whether or
// not the feed had actually changed — a Refresh that found nothing new
// still counts as having synced successfully just now, and still reschedules.
func (r *CalendarRepository) RecordRefreshSuccess(ctx context.Context, userID int64, id string, s RefreshSuccess) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendars SET last_synced_at = ?, etag = ?, last_modified = ?, content_hash = ?,
			next_refresh_at = ?, refresh_interval_seconds = ?, failure_count = 0, error_class = NULL, error_message = NULL
		 WHERE user_id = ? AND id = ?`,
		s.SyncedAt, s.ETag, s.LastModified, s.ContentHash, s.NextRefreshAt, s.RefreshIntervalSeconds, userID, id,
	)
	if err != nil {
		return fmt.Errorf("record refresh success: %w", err)
	}

	return requireAffected(res)
}

// RefreshFailure is what a failed Refresh persists (#86, ADR-0033): the
// classified reason, the new consecutive-failure count driving the next
// backoff, and when to retry. It deliberately never touches
// last_synced_at/etag/last_modified/content_hash — the Calendar's last good
// Events, and the validators that produced them, are left exactly as they
// were.
type RefreshFailure struct {
	ErrorClass, ErrorMessage string
	FailureCount             int
	NextRefreshAt            time.Time
}

// RecordRefreshFailure records a failed Refresh attempt. It never disables
// or deletes the Subscription — only the error/backoff columns move.
func (r *CalendarRepository) RecordRefreshFailure(ctx context.Context, userID int64, id string, f RefreshFailure) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendars SET failure_count = ?, error_class = ?, error_message = ?, next_refresh_at = ?
		 WHERE user_id = ? AND id = ?`,
		f.FailureCount, f.ErrorClass, f.ErrorMessage, f.NextRefreshAt, userID, id,
	)
	if err != nil {
		return fmt.Errorf("record refresh failure: %w", err)
	}

	return requireAffected(res)
}

// UpdateKeepAlarms changes id's keep_alarms setting alone (#87, ADR-0032) —
// the dedicated write path for a Subscription's later toggle, since the
// column is otherwise set on insert only and ignored by Update.
func (r *CalendarRepository) UpdateKeepAlarms(ctx context.Context, userID int64, id string, keepAlarms bool) (Calendar, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendars SET keep_alarms = ? WHERE user_id = ? AND id = ?`,
		keepAlarms, userID, id,
	)
	if err != nil {
		return Calendar{}, fmt.Errorf("update keep_alarms: %w", err)
	}

	if err := requireAffected(res); err != nil {
		return Calendar{}, err
	}

	return r.GetByID(ctx, userID, id)
}

// ScheduleNextRefresh sets next_refresh_at alone — used at Subscribe time to
// give a brand new Subscription its first, staggered due time before any
// Refresh has ever run against it (#86, ADR-0033).
func (r *CalendarRepository) ScheduleNextRefresh(ctx context.Context, userID int64, id string, nextRefreshAt time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendars SET next_refresh_at = ? WHERE user_id = ? AND id = ?`,
		nextRefreshAt, userID, id,
	)
	if err != nil {
		return fmt.Errorf("schedule next refresh: %w", err)
	}

	return requireAffected(res)
}

func (r *CalendarRepository) scanCalendar(row *sql.Row) (Calendar, error) {
	c, err := scanCalendarRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Calendar{}, ErrNotFound
	}
	if err != nil {
		return Calendar{}, fmt.Errorf("scan calendar: %w", err)
	}
	return c, nil
}

func scanCalendarRow(row rowScanner) (Calendar, error) {
	var c Calendar
	err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.Color, &c.SourceURL, &c.CreatedAt, &c.LastSyncedAt, &c.ETag, &c.LastModified, &c.ContentHash,
		&c.NextRefreshAt, &c.RefreshIntervalSeconds, &c.FailureCount, &c.ErrorClass, &c.ErrorMessage, &c.KeepAlarms)
	if err != nil {
		return Calendar{}, err
	}
	return c, nil
}
