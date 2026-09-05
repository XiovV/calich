// source.go implements calendar_sources (#284, ADR-0052): the mark on a
// Calendar meaning "these Events come from somewhere else, and this is not
// where they are written". A Calendar has zero or one Source, looked up by
// its own id — the table ADR-0032's fourteen columns bolted onto calendars
// moved into, so a second Source kind (a Connection-derived Linked Calendar)
// doesn't have to duplicate them.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SourceKind discriminates the two kinds of Source this app knows (#284,
// ADR-0052): a Subscription is bound to an external .ics feed (source_url);
// a Connection-derived Linked Calendar is mirrored from one calendar of a
// connections row (connection_id). Mutually exclusive per the
// calendar_sources CHECK constraint.
type SourceKind string

const (
	SourceKindSubscription SourceKind = "subscription"
	SourceKindConnection   SourceKind = "connection"
)

// SourceMode is what every Access and CalDAV-privilege clamp actually keys
// off (#284, ADR-0052) — never whether a Source merely exists. Every Source
// is SourceModeReadOnly today; the distinction is drawn ahead of write-back
// so "has a Source" and "may not be written here" don't fuse into one
// question across every guard that would otherwise have to unlearn it
// later.
type SourceMode string

const (
	SourceModeReadOnly SourceMode = "read_only"
	SourceModeWritable SourceMode = "writable"
)

// Source is one Calendar's external state (#284, ADR-0052) — nil on
// repository.Calendar for an ordinary Calendar, attached by CalendarService
// for one that carries a Source.
type Source struct {
	CalendarID   string
	Kind         SourceKind
	Mode         SourceMode
	ConnectionID *int64
	// SourceURL is set only on a Subscription (#83, ADR-0032): the external
	// .ics feed URL it was created from. Editable later via UpdateSourceURL
	// (#88), which also resets the conditional-GET validators below since
	// they were earned from the old URL.
	SourceURL *string
	// LastSyncedAt is when a Refresh last completed successfully against
	// this Source, whether or not it changed anything. Nil until the first
	// Refresh runs (#85, ADR-0033).
	LastSyncedAt *time.Time
	// ETag and LastModified are the feed's own response validators from its
	// last successful fetch, sent back as conditional-GET request headers on
	// the next Refresh. ContentHash is the fallback for a feed that sends
	// neither: a hash of the last fetched body, compared after a full fetch
	// since there's no validator to ask the server to compare for us. All
	// nil until the first Refresh runs (#85, ADR-0033).
	ETag, LastModified *string
	ContentHash        *string
	// NextRefreshAt is when the background poller should next attempt this
	// Source — staggered at creation time, recomputed after every attempt
	// (#86, ADR-0033).
	NextRefreshAt *time.Time
	// RefreshIntervalSeconds is the publisher's own stated poll cadence
	// (RFC 7986 REFRESH-INTERVAL or X-PUBLISHED-TTL), honoured only when
	// longer than the poller's own default. Nil when the feed states none,
	// or none has been observed yet.
	RefreshIntervalSeconds *int
	// FailureCount is how many consecutive Refresh attempts have failed
	// since the last success — drives exponential backoff, resets to 0 on
	// success. Never causes the Source to be disabled or deleted.
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
	// behaviour.
	KeepAlarms bool
	// FeedName and FeedColor are the last X-WR-CALNAME/
	// X-APPLE-CALENDAR-COLOR the feed actually supplied — not what's
	// displayed, which lives on the Calendar's own Name/Color. A Refresh
	// updates the displayed value only while it still equals its shadow;
	// nil means the feed has never supplied one (#88, ADR-0032).
	FeedName, FeedColor *string
}

// SourceFields are a Source's columns set at creation time — Create's one
// argument, the same shape CalendarFields already gathers a Calendar's own
// writable columns into.
type SourceFields struct {
	Kind         SourceKind
	Mode         SourceMode
	ConnectionID *int64
	SourceURL    *string
	KeepAlarms   bool
	FeedName     *string
	FeedColor    *string
}

// RefreshSuccess is what a completed, successful Refresh persists (#85, #86,
// ADR-0033): the response validators (or content hash) to send back on the
// next conditional GET, when it completed, when the poller should attempt
// it next, and the publisher's own stated cadence if this fetch observed
// one — all Source columns. Name and Color are the Calendar's own displayed
// values after this attempt, alongside FeedName/FeedColor, the shadow
// they're compared against on the next attempt (#88, ADR-0032):
// CalendarService.RecordRefreshSuccess is what actually splits this DTO
// across the calendar_sources and calendars writes it takes to persist, in
// one transaction, so a caller building it (SubscribeService.doRefresh)
// doesn't have to know the two tables exist. A success always resets
// FailureCount to 0 and clears any error, whatever it was — recovery is
// unconditional.
type RefreshSuccess struct {
	SyncedAt                        time.Time
	ETag, LastModified, ContentHash *string
	NextRefreshAt                   time.Time
	RefreshIntervalSeconds          *int
	Name, Color                     string
	FeedName, FeedColor             *string
}

// RefreshFailure is what a failed Refresh persists (#86, ADR-0033): the
// classified reason, the new consecutive-failure count driving the next
// backoff, and when to retry. It deliberately never touches
// last_synced_at/etag/last_modified/content_hash — the Events a prior
// success produced, and the validators that produced them, are left exactly
// as they were.
type RefreshFailure struct {
	ErrorClass, ErrorMessage string
	FailureCount             int
	NextRefreshAt            time.Time
}

// DueRefresh is one Source the background poller must attempt (#86,
// ADR-0033) — just enough to call SubscriptionRefresher.Refresh with:
// CalendarID and the Owner's UserID Refresh's own repository.Calendar
// lookups are scoped by.
type DueRefresh struct {
	CalendarID string
	UserID     int64
}

type SourceRepository struct {
	db DBTX
}

func NewSourceRepository(db *sql.DB) *SourceRepository {
	return &SourceRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018) —
// CalendarService.CreateSubscribed pairs this with CalendarRepository's own
// WithTx so a Calendar and its Source are created together or not at all.
func (r *SourceRepository) WithTx(tx *sql.Tx) *SourceRepository {
	return &SourceRepository{db: tx}
}

const sourceColumns = `calendar_id, kind, mode, connection_id, source_url, last_synced_at, etag, last_modified, content_hash, next_refresh_at, refresh_interval_seconds, failure_count, error_class, error_message, keep_alarms, feed_name, feed_color`

func (r *SourceRepository) Create(ctx context.Context, calendarID string, fields SourceFields) (Source, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO calendar_sources (calendar_id, kind, mode, connection_id, source_url, keep_alarms, feed_name, feed_color) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		calendarID, fields.Kind, fields.Mode, fields.ConnectionID, fields.SourceURL, fields.KeepAlarms, fields.FeedName, fields.FeedColor,
	); err != nil {
		return Source{}, fmt.Errorf("insert calendar source: %w", err)
	}

	return r.GetByCalendarID(ctx, calendarID)
}

// GetByCalendarID returns calendarID's Source, or ErrNotFound if it carries
// none — an ordinary Calendar.
func (r *SourceRepository) GetByCalendarID(ctx context.Context, calendarID string) (Source, error) {
	source, err := scanSourceRow(r.db.QueryRowContext(ctx,
		`SELECT `+sourceColumns+` FROM calendar_sources WHERE calendar_id = ?`, calendarID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return Source{}, ErrNotFound
	}
	if err != nil {
		return Source{}, fmt.Errorf("scan calendar source: %w", err)
	}
	return source, nil
}

// ListByCalendarIDs returns every one of ids' Sources, keyed by CalendarID —
// CalendarService's batched attach for a list of Calendars (List,
// ListAccessible's callers), so a caller listing many Calendars doesn't pay
// one query per row. A Calendar with no Source is simply absent from the
// result.
func (r *SourceRepository) ListByCalendarIDs(ctx context.Context, calendarIDs []string) (map[string]Source, error) {
	result := make(map[string]Source, len(calendarIDs))
	if len(calendarIDs) == 0 {
		return result, nil
	}

	args := make([]any, len(calendarIDs))
	for i, id := range calendarIDs {
		args[i] = id
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+sourceColumns+` FROM calendar_sources WHERE calendar_id IN (`+placeholders(len(calendarIDs))+`)`, args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list calendar sources: %w", err)
	}
	sources, err := collectRows(rows, scanSourceRow)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		result[source.CalendarID] = source
	}
	return result, nil
}

// ListDueForRefresh returns every Subscription (kind = 'subscription') whose
// next_refresh_at has come due — the background poller's read path (#86,
// ADR-0033). A Connection-derived Source never matches: its own poller is a
// later ticket's (#288).
func (r *SourceRepository) ListDueForRefresh(ctx context.Context, now time.Time) ([]DueRefresh, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT cs.calendar_id, c.user_id
		 FROM calendar_sources cs
		 JOIN calendars c ON c.id = cs.calendar_id
		 WHERE cs.kind = ? AND cs.next_refresh_at IS NOT NULL AND cs.next_refresh_at <= ?
		 ORDER BY cs.next_refresh_at, cs.calendar_id`,
		SourceKindSubscription, now,
	)
	if err != nil {
		return nil, fmt.Errorf("list due subscriptions: %w", err)
	}
	defer rows.Close()

	var due []DueRefresh
	for rows.Next() {
		var d DueRefresh
		if err := rows.Scan(&d.CalendarID, &d.UserID); err != nil {
			return nil, fmt.Errorf("scan due refresh: %w", err)
		}
		due = append(due, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list due subscriptions: %w", err)
	}
	return due, nil
}

// ownedSourceWhere scopes a calendar_sources write to a Source whose
// Calendar userID actually owns — calendar_sources itself carries no
// user_id, so ownership is checked via the Calendar it belongs to, the same
// defense-in-depth CalendarRepository's own `WHERE user_id = ?` gives its
// writes.
const ownedSourceWhere = `calendar_id = ? AND calendar_id IN (SELECT id FROM calendars WHERE user_id = ?)`

// UpdateSourceURL changes calendarID's Subscription URL (#88, ADR-0032): a
// publisher moving their feed shouldn't force an unsubscribe-and-restart,
// which would discard the Subscription and recreate every Event. Resets the
// conditional-GET validators, since they were earned from the old URL's
// responses and sending them to whatever now answers the new one would be
// meaningless at best.
func (r *SourceRepository) UpdateSourceURL(ctx context.Context, userID int64, calendarID, url string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendar_sources SET source_url = ?, etag = NULL, last_modified = NULL, content_hash = NULL WHERE `+ownedSourceWhere,
		url, calendarID, userID,
	)
	if err != nil {
		return fmt.Errorf("update source_url: %w", err)
	}
	return requireAffected(res)
}

// UpdateKeepAlarms changes calendarID's keep_alarms setting alone (#87,
// ADR-0032) — the dedicated write path for a Subscription's later toggle,
// since the column is otherwise set on insert only.
func (r *SourceRepository) UpdateKeepAlarms(ctx context.Context, userID int64, calendarID string, keepAlarms bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendar_sources SET keep_alarms = ? WHERE `+ownedSourceWhere,
		keepAlarms, calendarID, userID,
	)
	if err != nil {
		return fmt.Errorf("update keep_alarms: %w", err)
	}
	return requireAffected(res)
}

// ScheduleNextRefresh sets next_refresh_at alone — used at Subscribe time to
// give a brand new Source its first, staggered due time before any Refresh
// has ever run against it (#86, ADR-0033).
func (r *SourceRepository) ScheduleNextRefresh(ctx context.Context, userID int64, calendarID string, nextRefreshAt time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendar_sources SET next_refresh_at = ? WHERE `+ownedSourceWhere,
		nextRefreshAt, calendarID, userID,
	)
	if err != nil {
		return fmt.Errorf("schedule next refresh: %w", err)
	}
	return requireAffected(res)
}

// RecordRefreshSuccess records a completed Refresh's outcome on calendarID's
// Source, whether or not the feed had actually changed — a Refresh that
// found nothing new still counts as having synced successfully just now,
// and still reschedules.
func (r *SourceRepository) RecordRefreshSuccess(ctx context.Context, userID int64, calendarID string, s RefreshSuccess) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendar_sources SET last_synced_at = ?, etag = ?, last_modified = ?, content_hash = ?,
			next_refresh_at = ?, refresh_interval_seconds = ?, failure_count = 0, error_class = NULL, error_message = NULL,
			feed_name = ?, feed_color = ?
		 WHERE `+ownedSourceWhere,
		s.SyncedAt, s.ETag, s.LastModified, s.ContentHash, s.NextRefreshAt, s.RefreshIntervalSeconds,
		s.FeedName, s.FeedColor, calendarID, userID,
	)
	if err != nil {
		return fmt.Errorf("record refresh success: %w", err)
	}
	return requireAffected(res)
}

// RecordRefreshFailure records a failed Refresh attempt. It never disables
// or deletes the Source — only the error/backoff columns move.
func (r *SourceRepository) RecordRefreshFailure(ctx context.Context, userID int64, calendarID string, f RefreshFailure) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendar_sources SET failure_count = ?, error_class = ?, error_message = ?, next_refresh_at = ?
		 WHERE `+ownedSourceWhere,
		f.FailureCount, f.ErrorClass, f.ErrorMessage, f.NextRefreshAt, calendarID, userID,
	)
	if err != nil {
		return fmt.Errorf("record refresh failure: %w", err)
	}
	return requireAffected(res)
}

func scanSourceRow(row rowScanner) (Source, error) {
	var s Source
	err := row.Scan(&s.CalendarID, &s.Kind, &s.Mode, &s.ConnectionID, &s.SourceURL, &s.LastSyncedAt, &s.ETag, &s.LastModified, &s.ContentHash,
		&s.NextRefreshAt, &s.RefreshIntervalSeconds, &s.FailureCount, &s.ErrorClass, &s.ErrorMessage, &s.KeepAlarms, &s.FeedName, &s.FeedColor)
	if err != nil {
		return Source{}, err
	}
	return s, nil
}
