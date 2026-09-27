package service

import (
	"context"
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
	if month < 1 || month > 12 {
		return nil, ErrInvalidMonth
	}

	link, err := s.links.GetByID(ctx, id, userID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("get booking link: %w", err)
	}

	schedule, err := s.schedules.GetByID(ctx, link.AvailabilityScheduleID, userID)
	if err != nil {
		return nil, fmt.Errorf("get availability schedule: %w", err)
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
	busy, err := s.buildBusyIntervals(ctx, link, now, horizonCutoff)
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
// contributes.
func (s *BookingLinkService) buildBusyIntervals(ctx context.Context, link repository.BookingLink, from, to time.Time) ([]availability.BusyInterval, error) {
	events, err := s.eventRepo.ListByCalendarIDs(ctx, link.ConflictCalendarIDs, &from, &to)
	if err != nil {
		return nil, fmt.Errorf("list conflict set events: %w", err)
	}

	overridesByParent := make(map[string][]repository.Event)
	var masters []repository.Event
	for _, e := range events {
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
		tasks, err := s.taskRepo.ListIncomplete(ctx, link.UserID, link.WorkspaceID)
		if err != nil {
			return nil, fmt.Errorf("list incomplete tasks: %w", err)
		}
		for _, task := range tasks {
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
