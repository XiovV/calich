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
	// WorkspaceID is the Workspace this Calendar belongs to (#155, ADR-0045)
	// — set once at creation time from whichever Workspace was active, and
	// never changes.
	WorkspaceID int64
	Name        string
	Color       string
	// SourceURL is non-nil only on a Subscribed Calendar (#83, ADR-0032):
	// the external .ics feed URL it was created from. Editable later via
	// UpdateSourceURL (#88), which also resets the conditional-GET
	// validators below since they were earned from the old URL.
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
	// FeedName and FeedColor are the last X-WR-CALNAME/
	// X-APPLE-CALENDAR-COLOR the feed actually supplied — not what's
	// displayed, which lives in Name/Color. A Refresh updates the displayed
	// value only while it still equals its shadow; nil means the feed has
	// never supplied one (#88, ADR-0032).
	FeedName, FeedColor *string
}

// CalendarFields are a calendar's writable columns, gathered into one value
// the same way EventFields already gathers an event's — so Create and Update
// take one argument each instead of separately threading every column.
//
// SourceURL is set on insert only — Update's SQL ignores it; a later change
// goes through the dedicated UpdateSourceURL instead (#88, ADR-0032).
type CalendarFields struct {
	Name      string
	Color     string
	SourceURL *string
	// KeepAlarms is set on insert only, like SourceURL — Update's SQL
	// ignores it too, since changing it later goes through the dedicated
	// UpdateKeepAlarms (#87, ADR-0032).
	KeepAlarms bool
	// FeedName and FeedColor are set on insert only too — the shadow of
	// whatever the feed supplied at Subscribe time, whether or not the
	// User's chosen Name/Color at that moment matched it. A later change
	// goes through RecordRefreshSuccess (#88, ADR-0032).
	FeedName, FeedColor *string
}

type CalendarRepository struct {
	db DBTX
}

func NewCalendarRepository(db *sql.DB) *CalendarRepository {
	return &CalendarRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018) —
// TransferOwnershipOne needs this to pair with UserRepository.Delete under
// the "transfer" disposition (ADR-0044).
func (r *CalendarRepository) WithTx(tx *sql.Tx) *CalendarRepository {
	return &CalendarRepository{db: tx}
}

func (r *CalendarRepository) Create(ctx context.Context, userID, workspaceID int64, id string, fields CalendarFields) (Calendar, error) {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO calendars (id, user_id, workspace_id, name, color, source_url, keep_alarms, feed_name, feed_color) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, workspaceID, fields.Name, fields.Color, fields.SourceURL, fields.KeepAlarms, fields.FeedName, fields.FeedColor,
	); err != nil {
		return Calendar{}, fmt.Errorf("insert calendar: %w", err)
	}

	return r.GetByID(ctx, userID, id)
}

const calendarColumns = `id, user_id, workspace_id, name, color, source_url, created_at, last_synced_at, etag, last_modified, content_hash, next_refresh_at, refresh_interval_seconds, failure_count, error_class, error_message, keep_alarms, feed_name, feed_color`

func (r *CalendarRepository) GetByID(ctx context.Context, userID int64, id string) (Calendar, error) {
	return r.scanCalendar(r.db.QueryRowContext(ctx,
		`SELECT `+calendarColumns+` FROM calendars WHERE user_id = ? AND id = ?`, userID, id,
	))
}

// GetByIDAny fetches id regardless of who owns it, unlike GetByID. Its only
// caller is the Access resolver (ADR-0034): every permission decision needs
// the row itself before it can ask whether userID may see it, so the check
// can no longer live in SQL's WHERE clause the way GetByID's does.
func (r *CalendarRepository) GetByIDAny(ctx context.Context, id string) (Calendar, error) {
	return r.scanCalendar(r.db.QueryRowContext(ctx,
		`SELECT `+calendarColumns+` FROM calendars WHERE id = ?`, id,
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

// ListByUserAndWorkspace returns a user's calendars scoped to workspaceID,
// ordered by creation time, oldest first — the workspace-scoped sibling of
// ListByUser (#155, ADR-0045), for call sites that need "everything this
// User owns inside the Workspace that's currently active" rather than
// everything they own across every Workspace they belong to.
func (r *CalendarRepository) ListByUserAndWorkspace(ctx context.Context, userID, workspaceID int64) ([]Calendar, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+calendarColumns+` FROM calendars WHERE user_id = ? AND workspace_id = ? ORDER BY created_at, id`, userID, workspaceID,
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

// CalendarWithRole pairs a Calendar with the Role a Share grants on it. Only
// produced by ListSharedWithUser, whose rows are all Shares by construction
// — there is no Owner case to represent, unlike ResolveAccess's role
// parameter which is nil for an Owner.
type CalendarWithRole struct {
	Calendar
	Role string
}

// sharedWithUserGrantsCTE unions calendar_shares' direct per-User grants
// with calendar_group_shares' Group grants resolved off userID's *current*
// group_members rows (ADR-0045) — never a snapshot taken at share time, so a
// Group membership change changes what this returns on the next call with
// no other write involved. Each Calendar appears at most once, carrying the
// most permissive Role among every grant that reaches it (editor beats
// viewer) — ResolveAccess's "max(direct share role, any group share role)"
// (ADR-0045), computed here in SQL rather than in Go so ListSharedWithUser
// and ListSharedWithUserAndWorkspace can both build on the same join.
const sharedWithUserGrantsCTE = `
	WITH grants AS (
		SELECT calendar_id, CASE role WHEN 'editor' THEN 2 ELSE 1 END AS role_rank
		FROM calendar_shares
		WHERE user_id = ?
		UNION ALL
		SELECT cgs.calendar_id, CASE cgs.role WHEN 'editor' THEN 2 ELSE 1 END AS role_rank
		FROM calendar_group_shares cgs
		JOIN group_members gm ON gm.group_id = cgs.group_id
		WHERE gm.user_id = ?
	)
`

// ListSharedWithUser returns every Calendar a direct or Group Share grants
// userID Access to — the other half of "which Calendars can this User see"
// alongside ListByUser's owned Calendars (ADR-0034, ADR-0045). Ordered by
// when each Calendar was created, matching ListByUser, so
// CalendarService.ListAccessible can merge the two without re-sorting by
// anything Share-specific. A Calendar userID owns is excluded even if some
// Group they belong to also holds a Share on it, since ListAccessible
// already lists it once via ListByUser with unclamped ownership.
func (r *CalendarRepository) ListSharedWithUser(ctx context.Context, userID int64) ([]CalendarWithRole, error) {
	rows, err := r.db.QueryContext(ctx,
		sharedWithUserGrantsCTE+
			`SELECT c.id, c.user_id, c.workspace_id, c.name, c.color, c.source_url, c.created_at, c.last_synced_at, c.etag, c.last_modified, c.content_hash,
			        c.next_refresh_at, c.refresh_interval_seconds, c.failure_count, c.error_class, c.error_message, c.keep_alarms, c.feed_name, c.feed_color,
			        MAX(g.role_rank)
			 FROM calendars c
			 JOIN grants g ON g.calendar_id = c.id
			 WHERE c.user_id != ?
			 GROUP BY c.id
			 ORDER BY c.created_at, c.id`,
		userID, userID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list shared calendars: %w", err)
	}
	defer rows.Close()

	calendars := []CalendarWithRole{}
	for rows.Next() {
		c, err := scanSharedCalendarRow(rows)
		if err != nil {
			return nil, err
		}
		calendars = append(calendars, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shared calendars: %w", err)
	}
	return calendars, nil
}

// ListSharedWithUserAndWorkspace returns every Calendar a direct or Group
// Share grants userID Access to, scoped to workspaceID — the
// workspace-scoped sibling of ListSharedWithUser (#155, ADR-0045).
func (r *CalendarRepository) ListSharedWithUserAndWorkspace(ctx context.Context, userID, workspaceID int64) ([]CalendarWithRole, error) {
	rows, err := r.db.QueryContext(ctx,
		sharedWithUserGrantsCTE+
			`SELECT c.id, c.user_id, c.workspace_id, c.name, c.color, c.source_url, c.created_at, c.last_synced_at, c.etag, c.last_modified, c.content_hash,
			        c.next_refresh_at, c.refresh_interval_seconds, c.failure_count, c.error_class, c.error_message, c.keep_alarms, c.feed_name, c.feed_color,
			        MAX(g.role_rank)
			 FROM calendars c
			 JOIN grants g ON g.calendar_id = c.id
			 WHERE c.user_id != ? AND c.workspace_id = ?
			 GROUP BY c.id
			 ORDER BY c.created_at, c.id`,
		userID, userID, userID, workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list shared calendars: %w", err)
	}
	defer rows.Close()

	calendars := []CalendarWithRole{}
	for rows.Next() {
		c, err := scanSharedCalendarRow(rows)
		if err != nil {
			return nil, err
		}
		calendars = append(calendars, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shared calendars: %w", err)
	}
	return calendars, nil
}

// scanSharedCalendarRow scans a ListSharedWithUser/ListSharedWithUserAndWorkspace
// row, translating the aggregated role_rank back into calendar_shares'
// stored Role strings ("editor"/"viewer").
func scanSharedCalendarRow(rows *sql.Rows) (CalendarWithRole, error) {
	var c CalendarWithRole
	var roleRank int
	if err := rows.Scan(&c.ID, &c.UserID, &c.WorkspaceID, &c.Name, &c.Color, &c.SourceURL, &c.CreatedAt, &c.LastSyncedAt, &c.ETag, &c.LastModified, &c.ContentHash,
		&c.NextRefreshAt, &c.RefreshIntervalSeconds, &c.FailureCount, &c.ErrorClass, &c.ErrorMessage, &c.KeepAlarms, &c.FeedName, &c.FeedColor,
		&roleRank); err != nil {
		return CalendarWithRole{}, fmt.Errorf("scan shared calendar: %w", err)
	}
	if roleRank >= 2 {
		c.Role = RoleEditor
	} else {
		c.Role = RoleViewer
	}
	return c, nil
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

// TransferOwnershipOne reassigns a single Calendar id, owned by fromUserID,
// to toUserID — the per-Calendar "transfer" disposition self-Delete requires
// (ADR-0044): unlike the retired Admin-driven Delete, which took one
// disposition for a whole account, a self-deleting User chooses transfer or
// delete independently for each Calendar they own. Existing Events and
// Shares on the Calendar are left exactly as they are; only ownership moves.
func (r *CalendarRepository) TransferOwnershipOne(ctx context.Context, fromUserID int64, id string, toUserID int64) error {
	res, err := r.db.ExecContext(ctx, `UPDATE calendars SET user_id = ? WHERE user_id = ? AND id = ?`, toUserID, fromUserID, id)
	if err != nil {
		return fmt.Errorf("transfer calendar ownership: %w", err)
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
	// Name and Color are what should be displayed after this attempt —
	// pass-through of the Calendar's existing values on a short-circuited
	// (NotModified) attempt, recomputed against the feed's own values
	// otherwise. FeedName and FeedColor are the shadow those displayed
	// values are compared against on the next attempt (#88, ADR-0032).
	Name, Color         string
	FeedName, FeedColor *string
}

// RecordRefreshSuccess records a Refresh's successful outcome, whether or
// not the feed had actually changed — a Refresh that found nothing new
// still counts as having synced successfully just now, and still reschedules.
func (r *CalendarRepository) RecordRefreshSuccess(ctx context.Context, userID int64, id string, s RefreshSuccess) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendars SET last_synced_at = ?, etag = ?, last_modified = ?, content_hash = ?,
			next_refresh_at = ?, refresh_interval_seconds = ?, failure_count = 0, error_class = NULL, error_message = NULL,
			name = ?, color = ?, feed_name = ?, feed_color = ?
		 WHERE user_id = ? AND id = ?`,
		s.SyncedAt, s.ETag, s.LastModified, s.ContentHash, s.NextRefreshAt, s.RefreshIntervalSeconds,
		s.Name, s.Color, s.FeedName, s.FeedColor, userID, id,
	)
	if err != nil {
		return fmt.Errorf("record refresh success: %w", err)
	}

	return requireAffected(res)
}

// UpdateSourceURL changes id's Subscription URL (#88, ADR-0032): a
// publisher moving their feed shouldn't force an unsubscribe-and-restart,
// which would discard the Subscription and recreate every Event. Resets
// the conditional-GET validators, since they were earned from the old
// URL's responses and sending them to whatever now answers the new one
// would be meaningless at best.
func (r *CalendarRepository) UpdateSourceURL(ctx context.Context, userID int64, id, url string) (Calendar, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE calendars SET source_url = ?, etag = NULL, last_modified = NULL, content_hash = NULL WHERE user_id = ? AND id = ?`,
		url, userID, id,
	)
	if err != nil {
		return Calendar{}, fmt.Errorf("update source_url: %w", err)
	}

	if err := requireAffected(res); err != nil {
		return Calendar{}, err
	}

	return r.GetByID(ctx, userID, id)
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
	err := row.Scan(&c.ID, &c.UserID, &c.WorkspaceID, &c.Name, &c.Color, &c.SourceURL, &c.CreatedAt, &c.LastSyncedAt, &c.ETag, &c.LastModified, &c.ContentHash,
		&c.NextRefreshAt, &c.RefreshIntervalSeconds, &c.FailureCount, &c.ErrorClass, &c.ErrorMessage, &c.KeepAlarms, &c.FeedName, &c.FeedColor)
	if err != nil {
		return Calendar{}, err
	}
	return c, nil
}
