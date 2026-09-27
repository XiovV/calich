package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BookingLink is a Booking Link (#322, ADR-0084, ADR-0087): a named,
// slug-addressed offer to be booked, belonging to one User within one
// Workspace, scoped and cascaded like a Task List. ConflictCalendarIDs is
// attached by GetByID/ListForUser (booking_link_conflict_calendars is a
// separate table), never populated by Create/Update themselves.
type BookingLink struct {
	ID                     int64
	UserID                 int64
	WorkspaceID            int64
	Title                  string
	Slug                   string
	DurationMinutes        int
	Visibility             string
	AvailabilityScheduleID int64
	BookIntoCalendarID     string
	Location               string
	Description            string
	MinimumNoticeMinutes   int
	BookingHorizonDays     int
	// TasksInConflictSet is the Conflict set's "single row standing for
	// Tasks" (CONTEXT.md) — a plain column here, unlike the Calendar half
	// of the Conflict set, since Tasks has no calendar_id to put in
	// booking_link_conflict_calendars.
	TasksInConflictSet  bool
	CreatedAt           time.Time
	ConflictCalendarIDs []string
}

// BookingLinkFields are a Booking Link's writable columns, gathered into
// one value the same way CalendarFields already gathers a Calendar's — so
// Create and Update take one argument each instead of separately threading
// every column.
type BookingLinkFields struct {
	Title                  string
	Slug                   string
	DurationMinutes        int
	Visibility             string
	AvailabilityScheduleID int64
	BookIntoCalendarID     string
	Location               string
	Description            string
	MinimumNoticeMinutes   int
	BookingHorizonDays     int
	TasksInConflictSet     bool
}

// ErrSlugTaken is returned by Create and Update when (userID, slug) is
// already in use — a Slug is unique per User, not instance-wide (ADR-0084:
// "two people may both publish intro-call").
var ErrSlugTaken = errors.New("slug already taken")

type BookingLinkRepository struct {
	db DBTX
}

func NewBookingLinkRepository(db *sql.DB) *BookingLinkRepository {
	return &BookingLinkRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make Create's row-plus-conflict-set writes atomic
// (ADR-0018), mirroring AvailabilityScheduleRepository's own WithTx.
func (r *BookingLinkRepository) WithTx(tx *sql.Tx) *BookingLinkRepository {
	return &BookingLinkRepository{db: tx}
}

const bookingLinkColumns = `id, user_id, workspace_id, title, slug, duration_minutes, visibility, availability_schedule_id, book_into_calendar_id, location, description, minimum_notice_minutes, booking_horizon_days, tasks_in_conflict_set, created_at`

// Create inserts a new Booking Link row alone, owned by userID inside
// workspaceID. Its Conflict set is a separate insert (AddConflictCalendar),
// left to the caller to run in the same transaction
// (BookingLinkService.Create does exactly this).
func (r *BookingLinkRepository) Create(ctx context.Context, userID, workspaceID int64, fields BookingLinkFields) (BookingLink, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO booking_links (user_id, workspace_id, title, slug, duration_minutes, visibility, availability_schedule_id, book_into_calendar_id, location, description, minimum_notice_minutes, booking_horizon_days, tasks_in_conflict_set)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, workspaceID, fields.Title, fields.Slug, fields.DurationMinutes, fields.Visibility, fields.AvailabilityScheduleID, fields.BookIntoCalendarID, fields.Location, fields.Description, fields.MinimumNoticeMinutes, fields.BookingHorizonDays, fields.TasksInConflictSet,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return BookingLink{}, ErrSlugTaken
		}
		return BookingLink{}, fmt.Errorf("insert booking link: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return BookingLink{}, fmt.Errorf("get inserted booking link id: %w", err)
	}

	return r.GetByID(ctx, id, userID, workspaceID)
}

// Update replaces id's writable columns wholesale, scoped to userID and
// workspaceID like GetByID. Never touches the Conflict set — that's
// AddConflictCalendar/RemoveConflictCalendar's job, edited with immediate
// effect and no save step, the same split CalendarSetService draws between
// Rename and AddCalendar/RemoveCalendar.
func (r *BookingLinkRepository) Update(ctx context.Context, id, userID, workspaceID int64, fields BookingLinkFields) (BookingLink, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE booking_links SET title = ?, slug = ?, duration_minutes = ?, visibility = ?, availability_schedule_id = ?, book_into_calendar_id = ?, location = ?, description = ?, minimum_notice_minutes = ?, booking_horizon_days = ?, tasks_in_conflict_set = ?
		 WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		fields.Title, fields.Slug, fields.DurationMinutes, fields.Visibility, fields.AvailabilityScheduleID, fields.BookIntoCalendarID, fields.Location, fields.Description, fields.MinimumNoticeMinutes, fields.BookingHorizonDays, fields.TasksInConflictSet,
		id, userID, workspaceID,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return BookingLink{}, ErrSlugTaken
		}
		return BookingLink{}, fmt.Errorf("update booking link: %w", err)
	}
	if err := requireAffected(res); err != nil {
		return BookingLink{}, err
	}
	return r.GetByID(ctx, id, userID, workspaceID)
}

// GetByID returns the Booking Link identified by id, scoped to userID and
// workspaceID together — one belonging to another User, or to a different
// Workspace, is ErrNotFound rather than any error naming the mismatch, the
// same posture as TaskListRepository.GetByID. Its Conflict set's Calendar
// half is loaded and attached.
func (r *BookingLinkRepository) GetByID(ctx context.Context, id, userID, workspaceID int64) (BookingLink, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+bookingLinkColumns+` FROM booking_links WHERE id = ? AND user_id = ? AND workspace_id = ?`,
		id, userID, workspaceID,
	)
	link, err := scanBookingLinkRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BookingLink{}, ErrNotFound
		}
		return BookingLink{}, fmt.Errorf("scan booking link: %w", err)
	}

	ids, err := r.listConflictCalendarIDs(ctx, []int64{id})
	if err != nil {
		return BookingLink{}, err
	}
	link.ConflictCalendarIDs = ids[id]

	return link, nil
}

// GetBySlug resolves the Booking Link identified by (userID, slug) — an
// exact-match lookup relying on the column's own COLLATE NOCASE for
// case-insensitivity, the same as UserRepository.GetByHandle. Used by
// BookingLinkService's slug-disambiguation loops (Duplicate); the public
// router (#324) will use it too, scoped instead by (handle-resolved userID,
// slug).
func (r *BookingLinkRepository) GetBySlug(ctx context.Context, userID int64, slug string) (BookingLink, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+bookingLinkColumns+` FROM booking_links WHERE user_id = ? AND slug = ?`,
		userID, slug,
	)
	link, err := scanBookingLinkRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BookingLink{}, ErrNotFound
		}
		return BookingLink{}, fmt.Errorf("scan booking link: %w", err)
	}
	return link, nil
}

// ListForUser returns every Booking Link userID owns inside workspaceID,
// ordered by when it was created, each carrying its own Conflict set.
func (r *BookingLinkRepository) ListForUser(ctx context.Context, userID, workspaceID int64) ([]BookingLink, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+bookingLinkColumns+` FROM booking_links WHERE user_id = ? AND workspace_id = ? ORDER BY created_at, id`,
		userID, workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list booking links: %w", err)
	}
	links, err := collectRows(rows, scanBookingLinkRow)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, len(links))
	for i, l := range links {
		ids[i] = l.ID
	}
	idsByLink, err := r.listConflictCalendarIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range links {
		links[i].ConflictCalendarIDs = idsByLink[links[i].ID]
	}

	return links, nil
}

// CountForUser returns how many Booking Links userID owns across every
// Workspace — deliberately not scoped to one Workspace, since "a User's
// first Booking Link" (the Handle auto-claim trigger, ADR-0084) is a
// User-wide count, on the same terms as the Handle it claims.
func (r *BookingLinkRepository) CountForUser(ctx context.Context, userID int64) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM booking_links WHERE user_id = ?`, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count booking links: %w", err)
	}
	return count, nil
}

// Delete removes id outright, scoped to userID and workspaceID like
// GetByID. Its Conflict set rows cascade via ON DELETE CASCADE.
func (r *BookingLinkRepository) Delete(ctx context.Context, id, userID, workspaceID int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM booking_links WHERE id = ? AND user_id = ? AND workspace_id = ?`, id, userID, workspaceID)
	if err != nil {
		return fmt.Errorf("delete booking link: %w", err)
	}
	return requireAffected(res)
}

// DeleteAllForUser removes every Booking Link userID owns, across every
// Workspace. AccountService.Delete calls this, tx-bound, before it deletes
// or transfers any of userID's own Calendars — a Booking Link has no
// transfer semantics of its own, and book_into_calendar_id's own FK
// (00015_booking_links.sql, no ON DELETE clause) would otherwise refuse a
// DispositionDelete on a Calendar this User's own Booking Link still
// points at, since that Calendar's owning User hasn't been deleted yet at
// that point in the transaction — user_id's ON DELETE CASCADE only fires
// once the users row itself goes, later in the same transaction.
func (r *BookingLinkRepository) DeleteAllForUser(ctx context.Context, userID int64) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM booking_links WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete all booking links for user: %w", err)
	}
	return nil
}

// AddConflictCalendar puts calendarID into id's Conflict set. Idempotent: a
// Calendar already in the set is not an error, matching
// CalendarSetRepository.AddCalendar's own INSERT OR IGNORE.
func (r *BookingLinkRepository) AddConflictCalendar(ctx context.Context, linkID int64, calendarID string) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO booking_link_conflict_calendars (link_id, calendar_id) VALUES (?, ?)`,
		linkID, calendarID,
	); err != nil {
		return fmt.Errorf("add conflict calendar: %w", err)
	}
	return nil
}

// RemoveConflictCalendar takes calendarID out of id's Conflict set.
// Refusing to remove the Book-into Calendar is BookingLinkService's job,
// checked before this is ever called — this method itself doesn't inspect
// which calendar_id is pinned.
func (r *BookingLinkRepository) RemoveConflictCalendar(ctx context.Context, linkID int64, calendarID string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM booking_link_conflict_calendars WHERE link_id = ? AND calendar_id = ?`,
		linkID, calendarID,
	); err != nil {
		return fmt.Errorf("remove conflict calendar: %w", err)
	}
	return nil
}

func scanBookingLinkRow(row rowScanner) (BookingLink, error) {
	var l BookingLink
	err := row.Scan(
		&l.ID, &l.UserID, &l.WorkspaceID, &l.Title, &l.Slug, &l.DurationMinutes, &l.Visibility,
		&l.AvailabilityScheduleID, &l.BookIntoCalendarID, &l.Location, &l.Description,
		&l.MinimumNoticeMinutes, &l.BookingHorizonDays, &l.TasksInConflictSet, &l.CreatedAt,
	)
	return l, err
}

// listConflictCalendarIDs returns every Calendar id in any of linkIDs'
// Conflict sets, grouped by link_id. A single query regardless of how many
// links are asked for, so ListForUser stays O(1) round-trips rather than
// N+1, mirroring AvailabilityScheduleRepository.listRanges.
func (r *BookingLinkRepository) listConflictCalendarIDs(ctx context.Context, linkIDs []int64) (map[int64][]string, error) {
	result := make(map[int64][]string, len(linkIDs))
	if len(linkIDs) == 0 {
		return result, nil
	}

	placeholders := make([]any, len(linkIDs))
	query := `SELECT link_id, calendar_id FROM booking_link_conflict_calendars WHERE link_id IN (`
	for i, id := range linkIDs {
		if i > 0 {
			query += `, `
		}
		query += `?`
		placeholders[i] = id
	}
	query += `) ORDER BY calendar_id`

	rows, err := r.db.QueryContext(ctx, query, placeholders...)
	if err != nil {
		return nil, fmt.Errorf("list booking link conflict calendars: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var linkID int64
		var calendarID string
		if err := rows.Scan(&linkID, &calendarID); err != nil {
			return nil, fmt.Errorf("scan booking link conflict calendar: %w", err)
		}
		result[linkID] = append(result[linkID], calendarID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate booking link conflict calendars: %w", err)
	}

	return result, nil
}
