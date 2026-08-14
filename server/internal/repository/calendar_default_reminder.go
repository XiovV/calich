package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// CalendarDefaultReminderRepository stores a User's own Default reminders on
// a Calendar (ADR-0064): two independent lists per (Calendar, User) — timed
// and all-day — that a Reminder resolves to wherever that User has saved no
// list of their own on an Event. Same wholesale-replace-per-write shape as
// EventReminderRepository, keyed on (calendar_id, user_id, all_day) instead
// of (event_id, user_id).
type CalendarDefaultReminderRepository struct {
	db DBTX
}

func NewCalendarDefaultReminderRepository(db *sql.DB) *CalendarDefaultReminderRepository {
	return &CalendarDefaultReminderRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make a multi-table write atomic (ADR-0018).
func (r *CalendarDefaultReminderRepository) WithTx(tx *sql.Tx) *CalendarDefaultReminderRepository {
	return &CalendarDefaultReminderRepository{db: tx}
}

// ReplaceByCalendarID discards userID's existing default Reminders on
// calendarID for the allDay list (timed or all-day) and inserts reminders in
// their place — retuning a default is a replace, not a merge, mirroring
// EventReminderRepository.ReplaceByEventID. Never touches the other list
// (timed vs all-day) or another User's rows.
func (r *CalendarDefaultReminderRepository) ReplaceByCalendarID(ctx context.Context, userID int64, calendarID string, allDay bool, reminders []Reminder) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM calendar_default_reminders WHERE calendar_id = ? AND user_id = ? AND all_day = ?`,
		calendarID, userID, allDay,
	); err != nil {
		return fmt.Errorf("delete calendar default reminders: %w", err)
	}

	for _, reminder := range reminders {
		if _, err := r.db.ExecContext(ctx,
			`INSERT INTO calendar_default_reminders (calendar_id, user_id, all_day, offset_minutes, channel) VALUES (?, ?, ?, ?, ?)`,
			calendarID, userID, allDay, reminder.OffsetMinutes, reminder.Channel,
		); err != nil {
			return fmt.Errorf("insert calendar default reminder: %w", err)
		}
	}
	return nil
}

// ListByCalendarID returns userID's own default Reminders on calendarID,
// split into the timed and all-day lists — the Calendar edit modal's read
// path. A thin single-Calendar, single-User projection of ListByCalendarIDs,
// since one Calendar is just that method's batch of one.
func (r *CalendarDefaultReminderRepository) ListByCalendarID(ctx context.Context, userID int64, calendarID string) (timed, allDay []Reminder, err error) {
	timedByCalendar, allDayByCalendar, err := r.ListByCalendarIDs(ctx, []string{calendarID}, []int64{userID})
	if err != nil {
		return nil, nil, err
	}
	return timedByCalendar[calendarID][userID], allDayByCalendar[calendarID][userID], nil
}

// ListByCalendarIDs returns userIDs' default Reminders across calendarIDs,
// keyed by calendar id and then by the User whose rows they are, split into
// timed/all-day — the shape resolution answers in (ADR-0064), whether it
// answers for one viewer or for every User the firing engine must resolve
// independently (ADR-0021). A nil userIDs asks for every User; a (Calendar,
// User) pair with no default on that list is simply absent.
func (r *CalendarDefaultReminderRepository) ListByCalendarIDs(ctx context.Context, calendarIDs []string, userIDs []int64) (timedByCalendarUser, allDayByCalendarUser map[string]map[int64][]Reminder, err error) {
	timedByCalendarUser = make(map[string]map[int64][]Reminder)
	allDayByCalendarUser = make(map[string]map[int64][]Reminder)
	if len(calendarIDs) == 0 {
		return timedByCalendarUser, allDayByCalendarUser, nil
	}

	users, userArgs := userFilter(userIDs)
	query := `SELECT id, calendar_id, user_id, all_day, offset_minutes, channel FROM calendar_default_reminders WHERE calendar_id IN (` + placeholders(len(calendarIDs)) + `)` + users + ` ORDER BY id`
	args := make([]any, 0, len(calendarIDs)+len(userArgs))
	for _, id := range calendarIDs {
		args = append(args, id)
	}
	args = append(args, userArgs...)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list calendar default reminders: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var calendarID string
		var userID int64
		var isAllDay bool
		var reminder Reminder
		if err := rows.Scan(&reminder.ID, &calendarID, &userID, &isAllDay, &reminder.OffsetMinutes, &reminder.Channel); err != nil {
			return nil, nil, fmt.Errorf("scan calendar default reminder: %w", err)
		}
		byCalendarUser := timedByCalendarUser
		if isAllDay {
			byCalendarUser = allDayByCalendarUser
		}
		if byCalendarUser[calendarID] == nil {
			byCalendarUser[calendarID] = make(map[int64][]Reminder)
		}
		byCalendarUser[calendarID][userID] = append(byCalendarUser[calendarID][userID], reminder)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate calendar default reminders: %w", err)
	}
	return timedByCalendarUser, allDayByCalendarUser, nil
}

// CalendarIDsWithAny returns the distinct set of Calendar ids carrying at
// least one User's default Reminder, of either list — the firing engine's
// fast-path check (ListAllWithReminders): when this is empty, no Event
// anywhere can resolve a default, so the whole widened candidate scan can be
// skipped.
func (r *CalendarDefaultReminderRepository) CalendarIDsWithAny(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT calendar_id FROM calendar_default_reminders`)
	if err != nil {
		return nil, fmt.Errorf("list calendars with default reminders: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan calendar id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate calendars with default reminders: %w", err)
	}
	return ids, nil
}

// DeleteByUserAndCalendar clears both of userID's default Reminder lists on
// calendarID — a User losing Access to the Calendar (a Share revoked or
// renounced) takes their defaults with them (ADR-0064), mirroring
// EventReminderRepository.DeleteByUserAndCalendar, so regaining Access later
// starts fresh rather than silently reactivating a stale default.
func (r *CalendarDefaultReminderRepository) DeleteByUserAndCalendar(ctx context.Context, userID int64, calendarID string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM calendar_default_reminders WHERE user_id = ? AND calendar_id = ?`,
		userID, calendarID,
	); err != nil {
		return fmt.Errorf("delete calendar default reminders: %w", err)
	}
	return nil
}
