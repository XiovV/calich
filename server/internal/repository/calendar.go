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
	CreatedAt   time.Time
	// Source is the external state marking this a Subscribed or Linked
	// Calendar (#284, ADR-0052) — nil for an ordinary Calendar. Never
	// populated by CalendarRepository itself (calendar_sources is a
	// separate table, looked up by CalendarID); CalendarService attaches it
	// after fetching the Calendar row, so every Calendar-reading path gets
	// it without every repository query growing a join.
	Source *Source
}

// CalendarFields are a calendar's writable columns, gathered into one value
// the same way EventFields already gathers an event's — so Create and Update
// take one argument each instead of separately threading every column.
type CalendarFields struct {
	Name  string
	Color string
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
		`INSERT INTO calendars (id, user_id, workspace_id, name, color) VALUES (?, ?, ?, ?, ?)`,
		id, userID, workspaceID, fields.Name, fields.Color,
	); err != nil {
		return Calendar{}, fmt.Errorf("insert calendar: %w", err)
	}

	return r.GetByID(ctx, userID, id)
}

const calendarColumns = `id, user_id, workspace_id, name, color, created_at`

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

// ListByIDsAny is GetByIDAny's batched sibling: every one of ids' Calendar
// rows, regardless of who owns them, in one query — CalendarService's
// AttendeeCalendarMeta fallback (ADR-0046) uses this to resolve every
// Attendee-only Event's Calendar name/color in a single round trip rather
// than one per Calendar. A missing id is simply absent from the result,
// never an error.
func (r *CalendarRepository) ListByIDsAny(ctx context.Context, ids []string) ([]Calendar, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+calendarColumns+` FROM calendars WHERE id IN (`+placeholders(len(ids))+`)`, args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list calendars by id: %w", err)
	}
	return collectRows(rows, scanCalendarRow)
}

// ListByUser returns a user's calendars ordered by creation time, oldest first.
func (r *CalendarRepository) ListByUser(ctx context.Context, userID int64) ([]Calendar, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+calendarColumns+` FROM calendars WHERE user_id = ? ORDER BY created_at, id`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}
	return collectRows(rows, scanCalendarRow)
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
	return collectRows(rows, scanCalendarRow)
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
			`SELECT c.id, c.user_id, c.workspace_id, c.name, c.color, c.created_at,
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
	return collectRows(rows, scanSharedCalendarRow)
}

// ListSharedWithUserAndWorkspace returns every Calendar a direct or Group
// Share grants userID Access to, scoped to workspaceID — the
// workspace-scoped sibling of ListSharedWithUser (#155, ADR-0045).
func (r *CalendarRepository) ListSharedWithUserAndWorkspace(ctx context.Context, userID, workspaceID int64) ([]CalendarWithRole, error) {
	rows, err := r.db.QueryContext(ctx,
		sharedWithUserGrantsCTE+
			`SELECT c.id, c.user_id, c.workspace_id, c.name, c.color, c.created_at,
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
	return collectRows(rows, scanSharedCalendarRow)
}

// scanSharedCalendarRow scans a ListSharedWithUser/ListSharedWithUserAndWorkspace
// row, translating the aggregated role_rank back into calendar_shares'
// stored Role strings ("editor"/"viewer").
func scanSharedCalendarRow(row rowScanner) (CalendarWithRole, error) {
	var c CalendarWithRole
	var roleRank int
	if err := row.Scan(&c.ID, &c.UserID, &c.WorkspaceID, &c.Name, &c.Color, &c.CreatedAt,
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
	err := row.Scan(&c.ID, &c.UserID, &c.WorkspaceID, &c.Name, &c.Color, &c.CreatedAt)
	if err != nil {
		return Calendar{}, err
	}
	return c, nil
}
