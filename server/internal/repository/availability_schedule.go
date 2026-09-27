package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrScheduleReferenced is returned by Delete when id is still the
// AvailabilityScheduleID of some Booking Link (#322, ADR-0085) — deleting a
// Schedule still in use is refused, and the caller must repoint or delete
// that Booking Link first.
var ErrScheduleReferenced = errors.New("availability schedule still referenced by a booking link")

// AvailabilityRange is one weekly time range on an Availability Schedule
// (ADR-0085): a weekday (0-6, Sunday-Saturday, matching users.week_start's
// own convention) plus a start/end minute-of-day pair. Several per weekday
// are legal, so a split day is representable.
type AvailabilityRange struct {
	ID          int64
	ScheduleID  int64
	Weekday     int
	StartMinute int
	EndMinute   int
}

// AvailabilitySchedule is an Availability Schedule (ADR-0085): a named
// weekly pattern of time ranges belonging to one User, carrying its own IANA
// timezone. Deliberately not scoped to a Workspace — unlike a Calendar Set
// or a Task List, a Schedule is reusable across every Workspace a Booking
// Link of its owner's might write into.
type AvailabilitySchedule struct {
	ID        int64
	UserID    int64
	Name      string
	Tzid      string
	CreatedAt time.Time
	Ranges    []AvailabilityRange
}

type AvailabilityScheduleRepository struct {
	db DBTX
}

func NewAvailabilityScheduleRepository(db *sql.DB) *AvailabilityScheduleRepository {
	return &AvailabilityScheduleRepository{db: db}
}

// WithTx returns a copy of the repository bound to tx, for use inside
// repository.WithTx to make Create/Update's schedule-plus-ranges writes
// atomic (ADR-0018), mirroring TaskListRepository's own WithTx.
func (r *AvailabilityScheduleRepository) WithTx(tx *sql.Tx) *AvailabilityScheduleRepository {
	return &AvailabilityScheduleRepository{db: tx}
}

const availabilityScheduleColumns = `id, user_id, name, tzid, created_at`

// CreateSchedule inserts the schedule row alone, named name with timezone
// tzid, owned by userID. Its ranges are a separate insert (InsertRanges),
// left to the caller to run in the same transaction (AvailabilityScheduleService.Create
// does exactly this) — the two are never observed apart from each other in
// practice, but the split keeps this repository's methods single-table, the
// same shape as every other repository here.
func (r *AvailabilityScheduleRepository) CreateSchedule(ctx context.Context, userID int64, name, tzid string) (AvailabilitySchedule, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO availability_schedules (user_id, name, tzid) VALUES (?, ?, ?)`,
		userID, name, tzid,
	)
	if err != nil {
		return AvailabilitySchedule{}, fmt.Errorf("insert availability schedule: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return AvailabilitySchedule{}, fmt.Errorf("get inserted availability schedule id: %w", err)
	}

	return r.GetByID(ctx, id, userID)
}

// InsertRanges appends ranges to scheduleID, in the order given. A no-op
// against an empty slice — an empty Schedule is legal (ADR-0085).
func (r *AvailabilityScheduleRepository) InsertRanges(ctx context.Context, scheduleID int64, ranges []AvailabilityRange) error {
	for _, rng := range ranges {
		if _, err := r.db.ExecContext(ctx,
			`INSERT INTO availability_schedule_ranges (schedule_id, weekday, start_minute, end_minute) VALUES (?, ?, ?, ?)`,
			scheduleID, rng.Weekday, rng.StartMinute, rng.EndMinute,
		); err != nil {
			return fmt.Errorf("insert availability schedule range: %w", err)
		}
	}
	return nil
}

// DeleteRanges removes every range belonging to scheduleID — Update's first
// step before InsertRanges lays the replacement set back down, run in the
// same transaction as both (AvailabilityScheduleService.Update).
func (r *AvailabilityScheduleRepository) DeleteRanges(ctx context.Context, scheduleID int64) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM availability_schedule_ranges WHERE schedule_id = ?`, scheduleID); err != nil {
		return fmt.Errorf("delete availability schedule ranges: %w", err)
	}
	return nil
}

// UpdateFields renames id and/or changes its timezone, scoped to userID like
// GetByID.
func (r *AvailabilityScheduleRepository) UpdateFields(ctx context.Context, id, userID int64, name, tzid string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE availability_schedules SET name = ?, tzid = ? WHERE id = ? AND user_id = ?`,
		name, tzid, id, userID,
	)
	if err != nil {
		return fmt.Errorf("update availability schedule: %w", err)
	}
	return requireAffected(res)
}

// GetByID returns the Schedule identified by id, scoped to userID — a
// Schedule belonging to another User is ErrNotFound rather than any error
// naming the mismatch, the same posture as TaskListRepository.GetByID.
// Ranges are loaded and attached in Weekday, then start_minute order.
func (r *AvailabilityScheduleRepository) GetByID(ctx context.Context, id, userID int64) (AvailabilitySchedule, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+availabilityScheduleColumns+` FROM availability_schedules WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	schedule, err := scanAvailabilityScheduleRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AvailabilitySchedule{}, ErrNotFound
		}
		return AvailabilitySchedule{}, fmt.Errorf("scan availability schedule: %w", err)
	}

	ranges, err := r.listRanges(ctx, []int64{id})
	if err != nil {
		return AvailabilitySchedule{}, err
	}
	schedule.Ranges = ranges[id]

	return schedule, nil
}

// ListForUser returns every Schedule userID owns, ordered by when it was
// created, each carrying its own Ranges.
func (r *AvailabilityScheduleRepository) ListForUser(ctx context.Context, userID int64) ([]AvailabilitySchedule, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+availabilityScheduleColumns+` FROM availability_schedules WHERE user_id = ? ORDER BY created_at, id`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list availability schedules: %w", err)
	}
	schedules, err := collectRows(rows, scanAvailabilityScheduleRow)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, len(schedules))
	for i, s := range schedules {
		ids[i] = s.ID
	}
	rangesByID, err := r.listRanges(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range schedules {
		schedules[i].Ranges = rangesByID[schedules[i].ID]
	}

	return schedules, nil
}

// CountForUser returns how many Schedules userID owns — ensureDefault's
// lazy-creation check (ADR-0085): zero means none has been seeded yet.
func (r *AvailabilityScheduleRepository) CountForUser(ctx context.Context, userID int64) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM availability_schedules WHERE user_id = ?`, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count availability schedules: %w", err)
	}
	return count, nil
}

// Delete removes id outright, scoped to userID like GetByID. Its ranges
// cascade via ON DELETE CASCADE.
func (r *AvailabilityScheduleRepository) Delete(ctx context.Context, id, userID int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM availability_schedules WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		// A Booking Link's availability_schedule_id carries no ON DELETE
		// clause (00015_booking_links.sql), so SQLite's own FK enforcement
		// refuses this exactly as ADR-0085 requires ("deleting a Schedule
		// still referenced is refused") — translated into a typed sentinel
		// here rather than left as a raw SQL error, the same as every other
		// constraint violation this package catches.
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			return ErrScheduleReferenced
		}
		return fmt.Errorf("delete availability schedule: %w", err)
	}
	return requireAffected(res)
}

func scanAvailabilityScheduleRow(row rowScanner) (AvailabilitySchedule, error) {
	var s AvailabilitySchedule
	err := row.Scan(&s.ID, &s.UserID, &s.Name, &s.Tzid, &s.CreatedAt)
	return s, err
}

// listRanges returns every range belonging to any of scheduleIDs, grouped by
// schedule_id, ordered by weekday then start_minute. A single query
// regardless of how many schedules are asked for, so ListForUser stays O(1)
// round-trips rather than N+1.
func (r *AvailabilityScheduleRepository) listRanges(ctx context.Context, scheduleIDs []int64) (map[int64][]AvailabilityRange, error) {
	result := make(map[int64][]AvailabilityRange, len(scheduleIDs))
	if len(scheduleIDs) == 0 {
		return result, nil
	}

	placeholders := make([]any, len(scheduleIDs))
	query := `SELECT id, schedule_id, weekday, start_minute, end_minute FROM availability_schedule_ranges WHERE schedule_id IN (`
	for i, id := range scheduleIDs {
		if i > 0 {
			query += `, `
		}
		query += `?`
		placeholders[i] = id
	}
	query += `) ORDER BY weekday, start_minute`

	rows, err := r.db.QueryContext(ctx, query, placeholders...)
	if err != nil {
		return nil, fmt.Errorf("list availability schedule ranges: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var rng AvailabilityRange
		if err := rows.Scan(&rng.ID, &rng.ScheduleID, &rng.Weekday, &rng.StartMinute, &rng.EndMinute); err != nil {
			return nil, fmt.Errorf("scan availability schedule range: %w", err)
		}
		result[rng.ScheduleID] = append(result[rng.ScheduleID], rng)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate availability schedule ranges: %w", err)
	}

	return result, nil
}
