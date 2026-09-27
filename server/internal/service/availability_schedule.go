package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

var (
	// ErrInvalidScheduleName is returned by Create and Update when name is
	// empty.
	ErrInvalidScheduleName = errors.New("availability schedule name must not be empty")

	// ErrInvalidTimezone is returned by Create and Update when tzid is not a
	// loadable IANA zone (ADR-0085 — a Schedule's zone is an Anchor zone in
	// everything but name, so it must resolve the same way an Event's does).
	ErrInvalidTimezone = errors.New("invalid IANA timezone")

	// ErrInvalidAvailabilityRange is returned by Create and Update when a
	// range's weekday is outside 0-6, or its start/end minute-of-day pair is
	// out of bounds or non-increasing.
	ErrInvalidAvailabilityRange = errors.New("invalid availability schedule range")
)

const (
	minWeekday     = 0
	maxWeekday     = 6
	maxRangeMinute = 24 * 60

	// defaultScheduleName and fallbackTzid seed the first Schedule an ensureDefault
	// auto-creates (ADR-0085).
	defaultScheduleName = "Default"
	fallbackTzid        = "Etc/UTC"

	defaultWorkingHoursStart = 9 * 60
	defaultWorkingHoursEnd   = 17 * 60
)

// seedWeekdays is Mon-Fri (1-5, matching users.week_start's Sunday=0
// convention) — the days ADR-0085's seeding rule shades, whether copied from
// Working hours or falling back to 09:00-17:00.
var seedWeekdays = []int{1, 2, 3, 4, 5}

// AvailabilityScheduleService creates, lists, updates and deletes
// Availability Schedules (ADR-0085). Private to the caller's own userID —
// unlike CalendarSetService and TaskListService there is no Workspace
// scoping at all, since a Schedule belongs to the User outright and is
// reusable across every Workspace a Booking Link of theirs might write
// into. Every method resolves by (id, userID) at the repository layer and
// surfaces a mismatch as repository.ErrNotFound, never a distinguishable
// "forbidden".
type AvailabilityScheduleService struct {
	db        *sql.DB
	schedules *repository.AvailabilityScheduleRepository
	users     *repository.UserRepository
}

func NewAvailabilityScheduleService(db *sql.DB, schedules *repository.AvailabilityScheduleRepository, users *repository.UserRepository) *AvailabilityScheduleService {
	return &AvailabilityScheduleService{db: db, schedules: schedules, users: users}
}

// ListForUser returns every Schedule userID owns, first making sure at least
// one exists (ensureDefault) — the lazy creation ADR-0085 describes as
// happening "at that User's first Booking Link" happens here instead, since
// nothing in this codebase creates a Booking Link yet (#322): the first time
// a caller asks to see their Schedules is the earliest available trigger.
// tzHint is the caller's own browser-detected IANA zone, used only to seed
// the Default Schedule's timezone the one time it doesn't exist yet;
// already-existing Schedules are never touched by it.
func (s *AvailabilityScheduleService) ListForUser(ctx context.Context, userID int64, tzHint string) ([]repository.AvailabilitySchedule, error) {
	if err := s.ensureDefault(ctx, userID, tzHint); err != nil {
		return nil, fmt.Errorf("ensure default availability schedule: %w", err)
	}

	schedules, err := s.schedules.ListForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list availability schedules: %w", err)
	}
	return schedules, nil
}

// ensureDefault seeds userID's first Schedule, named "Default", from their
// Working hours Preference — Mon-Fri at its start/end when set, Mon-Fri
// 09:00-17:00 otherwise (ADR-0085) — the moment they have none at all.
// Seeded once: a second call is a no-op, so editing Working hours afterwards
// never reaches a Schedule, ever.
func (s *AvailabilityScheduleService) ensureDefault(ctx context.Context, userID int64, tzHint string) error {
	count, err := s.schedules.CountForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("count availability schedules: %w", err)
	}
	if count > 0 {
		return nil
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	start, end := defaultWorkingHoursStart, defaultWorkingHoursEnd
	if user.WorkingHoursStart != nil && user.WorkingHoursEnd != nil {
		start, end = *user.WorkingHoursStart, *user.WorkingHoursEnd
	}

	// tzHint is trusted only if it actually resolves — an empty or garbled
	// browser-detected zone falls back to Etc/UTC rather than seeding a
	// Schedule that can never derive a slot (#323).
	tzid := fallbackTzid
	if strings.TrimSpace(tzHint) != "" {
		if _, err := time.LoadLocation(tzHint); err == nil {
			tzid = tzHint
		}
	}

	ranges := make([]repository.AvailabilityRange, len(seedWeekdays))
	for i, weekday := range seedWeekdays {
		ranges[i] = repository.AvailabilityRange{Weekday: weekday, StartMinute: start, EndMinute: end}
	}

	return repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		txSchedules := s.schedules.WithTx(tx)
		schedule, err := txSchedules.CreateSchedule(ctx, userID, defaultScheduleName, tzid)
		if err != nil {
			return err
		}
		return txSchedules.InsertRanges(ctx, schedule.ID, ranges)
	})
}

// Create makes a new Schedule named name, in timezone tzid, owned by userID,
// with ranges as its initial weekly pattern — an empty slice is legal
// (ADR-0085's "an empty Schedule is legal" rule).
func (s *AvailabilityScheduleService) Create(ctx context.Context, userID int64, name, tzid string, ranges []repository.AvailabilityRange) (repository.AvailabilitySchedule, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return repository.AvailabilitySchedule{}, ErrInvalidScheduleName
	}
	if err := validateTzid(tzid); err != nil {
		return repository.AvailabilitySchedule{}, err
	}
	if err := validateRanges(ranges); err != nil {
		return repository.AvailabilitySchedule{}, err
	}

	var created repository.AvailabilitySchedule
	err := repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		txSchedules := s.schedules.WithTx(tx)

		schedule, err := txSchedules.CreateSchedule(ctx, userID, name, tzid)
		if err != nil {
			return err
		}
		if err := txSchedules.InsertRanges(ctx, schedule.ID, ranges); err != nil {
			return err
		}

		created, err = txSchedules.GetByID(ctx, schedule.ID, userID)
		return err
	})
	if err != nil {
		return repository.AvailabilitySchedule{}, fmt.Errorf("create availability schedule: %w", err)
	}
	return created, nil
}

// Update replaces id's name, timezone and entire weekly range set at once,
// scoped to userID. Ranges are wholesale-replaced (delete then re-insert in
// the same transaction) rather than diffed — the Settings Section edits a
// Schedule as one form, so there is no partial-range update to preserve.
func (s *AvailabilityScheduleService) Update(ctx context.Context, userID, id int64, name, tzid string, ranges []repository.AvailabilityRange) (repository.AvailabilitySchedule, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return repository.AvailabilitySchedule{}, ErrInvalidScheduleName
	}
	if err := validateTzid(tzid); err != nil {
		return repository.AvailabilitySchedule{}, err
	}
	if err := validateRanges(ranges); err != nil {
		return repository.AvailabilitySchedule{}, err
	}

	var updated repository.AvailabilitySchedule
	err := repository.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		txSchedules := s.schedules.WithTx(tx)

		if err := txSchedules.UpdateFields(ctx, id, userID, name, tzid); err != nil {
			return err
		}
		if err := txSchedules.DeleteRanges(ctx, id); err != nil {
			return err
		}
		if err := txSchedules.InsertRanges(ctx, id, ranges); err != nil {
			return err
		}

		var err error
		updated, err = txSchedules.GetByID(ctx, id, userID)
		return err
	})
	if err != nil {
		return repository.AvailabilitySchedule{}, fmt.Errorf("update availability schedule: %w", err)
	}
	return updated, nil
}

// Delete removes id outright, scoped to userID.
func (s *AvailabilityScheduleService) Delete(ctx context.Context, userID, id int64) error {
	if err := s.schedules.Delete(ctx, id, userID); err != nil {
		return fmt.Errorf("delete availability schedule: %w", err)
	}
	return nil
}

// validateTzid requires tzid to be a loadable IANA zone — validated eagerly
// at this boundary (a User picking one in Settings) since nothing else in
// this codebase checks it before it is stored.
func validateTzid(tzid string) error {
	if strings.TrimSpace(tzid) == "" {
		return ErrInvalidTimezone
	}
	if _, err := time.LoadLocation(tzid); err != nil {
		return ErrInvalidTimezone
	}
	return nil
}

func validateRanges(ranges []repository.AvailabilityRange) error {
	for _, rng := range ranges {
		if rng.Weekday < minWeekday || rng.Weekday > maxWeekday {
			return ErrInvalidAvailabilityRange
		}
		if rng.StartMinute < 0 || rng.StartMinute >= maxRangeMinute {
			return ErrInvalidAvailabilityRange
		}
		if rng.EndMinute <= rng.StartMinute || rng.EndMinute > maxRangeMinute {
			return ErrInvalidAvailabilityRange
		}
	}
	return nil
}
