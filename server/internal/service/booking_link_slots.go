package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/XiovV/calich/server/internal/availability"
	"github.com/XiovV/calich/server/internal/recurrence"
	"github.com/XiovV/calich/server/internal/repository"
)

// ErrInvalidMonth is returned by DeriveSlotsForMonth when month is outside
// 1-12.
var ErrInvalidMonth = fmt.Errorf("month must be between 1 and 12")

// DeriveSlotsForMonth answers "when is this person free" for one Booking
// Link, scoped to one calendar month (#323) — the endpoint the eventual
// public page (#324) will read with no Session at all. Derivation itself
// (internal/availability.DeriveSlots) is pure; this method is the seam that
// assembles its inputs from the database: the Availability Schedule's own
// ranges and timezone, and the Conflict set expanded into Busy intervals.
// now is passed in, not read from time.Now() here, so a caller (a test, or
// the eventual public handler) controls what "now" means for minimum
// notice and the booking horizon.
func (s *BookingLinkService) DeriveSlotsForMonth(ctx context.Context, userID, workspaceID, id int64, year int, month time.Month, now time.Time) ([]time.Time, error) {
	// Validated before the repository lookup below, not after: this
	// preserves DeriveSlotsForMonth's original priority (a malformed month
	// is rejected before ever costing a query) now that the lookup and the
	// derivation it feeds live in two separate functions.
	if month < 1 || month > 12 {
		return nil, ErrInvalidMonth
	}

	link, err := s.links.GetByID(ctx, id, userID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("get booking link: %w", err)
	}

	return s.DeriveSlotsForLinkAndMonth(ctx, link, year, month, now)
}

// DeriveSlotsForLinkAndMonth is DeriveSlotsForMonth's own body, split out so
// a caller that has already resolved a Booking Link some other way — the
// public page (#324), which resolves one by (Handle, Slug) rather than by
// (id, userID, workspaceID) — can reuse the exact same derivation instead of
// a second implementation that could drift from this one.
func (s *BookingLinkService) DeriveSlotsForLinkAndMonth(ctx context.Context, link repository.BookingLink, year int, month time.Month, now time.Time) ([]time.Time, error) {
	schedule, err := s.schedules.GetByID(ctx, link.AvailabilityScheduleID, link.UserID)
	if err != nil {
		return nil, fmt.Errorf("get availability schedule: %w", err)
	}
	return s.deriveSlotsWith(ctx, s.eventRepo, s.taskRepo, schedule, link, year, month, now)
}

// SlotAvailable reports whether start is still exactly one of link's
// bookable slots for the calendar month it falls in (in schedule's own
// zone), re-derived through events/tasks rather than trusted from an
// earlier read (#326, ADR-0087). tx, when non-nil, re-derives against that
// transaction's own tx-bound Event/Task repositories — built here, from
// BookingLinkService's own eventRepo/taskRepo fields, rather than handed in
// by the caller — so Booking's write path (the one caller that passes one)
// can run this check and the insert it guards against the same snapshot,
// inside the very transaction its insert is about to run in, without
// reaching into this service's internals to do it. Nil reuses the pooled
// repositories instead, same as DeriveSlotsForLinkAndMonth. Reuses
// deriveSlotsWith wholesale, the exact derivation /slots itself answers
// with, so a booking can never be accepted by a rule its own listing
// wouldn't have offered.
func (s *BookingLinkService) SlotAvailable(ctx context.Context, tx *sql.Tx, schedule repository.AvailabilitySchedule, link repository.BookingLink, start, now time.Time) (bool, error) {
	loc, err := time.LoadLocation(schedule.Tzid)
	if err != nil {
		return false, fmt.Errorf("load location %q: %w", schedule.Tzid, err)
	}
	local := start.In(loc)

	events, tasks := s.eventRepo, s.taskRepo
	if tx != nil {
		events, tasks = events.WithTx(tx), tasks.WithTx(tx)
	}

	slots, err := s.deriveSlotsWith(ctx, events, tasks, schedule, link, local.Year(), local.Month(), now)
	if err != nil {
		return false, err
	}
	for _, slot := range slots {
		if slot.Equal(start) {
			return true, nil
		}
	}
	return false, nil
}

// deriveSlotsWith is DeriveSlotsForLinkAndMonth's and SlotAvailable's shared
// body, taking the Event/Task repositories explicitly rather than reading
// s.eventRepo/s.taskRepo directly — the seam that lets SlotAvailable hand it
// tx-bound repositories instead of BookingLinkService's own pooled ones.
func (s *BookingLinkService) deriveSlotsWith(ctx context.Context, events *repository.EventRepository, tasks *repository.TaskRepository, schedule repository.AvailabilitySchedule, link repository.BookingLink, year int, month time.Month, now time.Time) ([]time.Time, error) {
	if month < 1 || month > 12 {
		return nil, ErrInvalidMonth
	}

	loc, err := time.LoadLocation(schedule.Tzid)
	if err != nil {
		return nil, fmt.Errorf("load location %q: %w", schedule.Tzid, err)
	}

	ranges := make([]availability.Range, len(schedule.Ranges))
	for i, r := range schedule.Ranges {
		ranges[i] = availability.Range{Weekday: r.Weekday, StartMinute: r.StartMinute, EndMinute: r.EndMinute}
	}

	// The same calendar-aware step DeriveSlots itself takes for the
	// horizon cutoff, computed here too so the Conflict set's Events are
	// queried over the identical window derivation will actually consider
	// — querying any narrower window could silently miss a Busy Occurrence
	// derivation would otherwise have found.
	horizonCutoff := now.AddDate(0, 0, link.BookingHorizonDays)
	busy, err := s.buildBusyIntervals(ctx, events, tasks, link, now, horizonCutoff)
	if err != nil {
		return nil, err
	}

	slots, err := availability.DeriveSlots(availability.Params{
		Ranges:               ranges,
		Tzid:                 schedule.Tzid,
		Busy:                 busy,
		DurationMinutes:      link.DurationMinutes,
		MinimumNoticeMinutes: link.MinimumNoticeMinutes,
		BookingHorizonDays:   link.BookingHorizonDays,
		Now:                  now,
	})
	if err != nil {
		return nil, err
	}

	filtered := make([]time.Time, 0, len(slots))
	for _, slot := range slots {
		local := slot.In(loc)
		if local.Year() == year && local.Month() == month {
			filtered = append(filtered, slot)
		}
	}
	return filtered, nil
}

// buildBusyIntervals assembles a Booking Link's Conflict set into
// availability.BusyInterval values over [from, to) (ADR-0087): Busy
// Occurrences from its Calendars, expanded with the same Go recurrence
// expander the reminder scheduler already uses, plus the host's incomplete
// Tasks' Time blocks when the Tasks row is set. A Deadline plays no part at
// all here, and a completed Task is already excluded by ListIncomplete
// itself — neither ever closes a slot. All-day Events need no special case:
// their Busy is read the same way a timed Event's is, and an all-day Event
// simply defaults to Free (ADR-0086), so only an explicitly Busy one ever
// contributes. Takes events/tasks explicitly — deriveSlotsWith's own reason
// for taking them, one layer up.
func (s *BookingLinkService) buildBusyIntervals(ctx context.Context, events *repository.EventRepository, tasks *repository.TaskRepository, link repository.BookingLink, from, to time.Time) ([]availability.BusyInterval, error) {
	rows, err := events.ListByCalendarIDs(ctx, link.ConflictCalendarIDs, &from, &to)
	if err != nil {
		return nil, fmt.Errorf("list conflict set events: %w", err)
	}

	overridesByParent := make(map[string][]repository.Event)
	var masters []repository.Event
	for _, e := range rows {
		if e.ParentID != nil {
			overridesByParent[*e.ParentID] = append(overridesByParent[*e.ParentID], e)
			continue
		}
		masters = append(masters, e)
	}

	var busy []availability.BusyInterval
	for _, master := range masters {
		occurrences, err := expandBusyOccurrences(master, overridesByParent[master.ID], from, to)
		if err != nil {
			return nil, err
		}
		busy = append(busy, occurrences...)
	}

	if link.TasksInConflictSet {
		incomplete, err := tasks.ListIncomplete(ctx, link.UserID, link.WorkspaceID)
		if err != nil {
			return nil, fmt.Errorf("list incomplete tasks: %w", err)
		}
		for _, task := range incomplete {
			if task.Start == nil || task.DurationMinutes == nil {
				continue
			}
			busy = append(busy, availability.BusyInterval{
				Start: *task.Start,
				End:   task.Start.Add(time.Duration(*task.DurationMinutes) * time.Minute),
			})
		}
	}

	return busy, nil
}

// expandBusyOccurrences returns master's own Occurrences overlapping
// [from, to) that are Busy, each replaced by its own Override when one
// exists for that Occurrence (an Override carries its own Busy
// independently of its parent's, ADR-0086) — mirroring
// event_series_read.go's own findOverrideForOccurrence rather than a
// second implementation of "does this Occurrence have an Override".
func expandBusyOccurrences(master repository.Event, overrides []repository.Event, from, to time.Time) ([]availability.BusyInterval, error) {
	masterDuration := master.End.Sub(master.Start)

	// recurrence.ExpandOccurrences reports an Occurrence only when its own
	// *start* falls in the window handed to it — correct for its other
	// callers (CalDAV's time-range filter cares about the same thing), but
	// an Occurrence already in progress at derivation time (started before
	// from, still running) must still close a slot here. Widening the
	// expansion window's lower bound by this Master's own duration, then
	// filtering the result by genuine [start, end) overlap with the true
	// [from, to) below, catches it without changing what "in the window"
	// means for anyone else.
	starts, err := recurrence.ExpandOccurrences(master.Rrule, master.Tzid, master.Start, from.Add(-masterDuration), to)
	if err != nil {
		return nil, fmt.Errorf("expand occurrences for event %s: %w", master.ID, err)
	}

	var busy []availability.BusyInterval
	for _, start := range starts {
		if isExdate(master.Exdates, start) {
			continue
		}

		busyStart, busyEnd, isBusy := start, start.Add(masterDuration), master.Busy
		if override, ok := findOverrideForOccurrence(overrides, start); ok {
			busyStart, busyEnd, isBusy = override.Start, override.End, override.Busy
		}
		if !isBusy {
			continue
		}
		// The widened lower bound above can surface an Occurrence that
		// finished before the true window even starts; recurrence.
		// ExpandOccurrences' own to-side exclusivity still guards the
		// far edge, so only this near-edge check is needed here.
		if !busyEnd.After(from) {
			continue
		}
		busy = append(busy, availability.BusyInterval{Start: busyStart, End: busyEnd})
	}
	return busy, nil
}

func isExdate(exdates []time.Time, start time.Time) bool {
	for _, exdate := range exdates {
		if exdate.Equal(start) {
			return true
		}
	}
	return false
}
