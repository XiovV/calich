package service

import (
	"context"
	"fmt"

	"github.com/XiovV/calich/server/internal/repository"
)

// everyUser is the audience a resolution names when it answers for all of
// them — the firing engine's case (ADR-0021), where who gets a Reminder is
// what the resolution is being asked, not something the caller already knows.
// The viewer read names one User instead. Nobody names an empty audience.
var everyUser []int64 = nil

// reminderResolver answers ADR-0064's resolution rule, and is the only place
// it is stated: which Reminders a set of Users have on a set of Events — their
// own rows if they have any, else nothing at all if they have explicitly saved
// an empty list on that Event, else the Event's Calendar's Default reminders
// for its timed or all-day kind. Nothing is ever copied onto an Event;
// resolution happens where Reminders are read.
//
// One module rather than two implementations (#216): the viewer read the web
// app, CalDAV and ICS export share, and the firing engine's every-User read,
// come from this one answer, so what the grid shows and what the scheduler
// fires on cannot drift.
//
// Its repositories are the pooled ones, so it must not be called from inside a
// withTx body — see txRepos, whose rule this is.
type reminderResolver struct {
	reminders        *repository.EventReminderRepository
	explicit         *repository.EventReminderExplicitRepository
	calendarDefaults *repository.CalendarDefaultReminderRepository
}

// Resolve answers (events, users) → reminders. userIDs names the Users to
// resolve for — one for a viewer read, everyUser for the firing engine's.
// A (Event, User) pair appears in the result only when at least one Reminder
// resolved for it, so an absent pair and an empty one are the same answer.
//
// Costs one query per table regardless of how many Events, Calendars or Users
// are being resolved.
func (r *reminderResolver) Resolve(ctx context.Context, events []repository.Event, userIDs []int64) (repository.ResolvedReminders, error) {
	ids := eventIDs(events)

	resolved, err := r.reminders.ListByEventIDs(ctx, ids, userIDs)
	if err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}
	timedByCalendar, allDayByCalendar, err := r.calendarDefaults.ListByCalendarIDs(ctx, uniqueCalendarIDs(events), userIDs)
	if err != nil {
		return nil, fmt.Errorf("list calendar default reminders: %w", err)
	}
	markers, err := r.explicit.ListByEventIDs(ctx, ids, userIDs)
	if err != nil {
		return nil, fmt.Errorf("list explicit reminder markers: %w", err)
	}

	for _, e := range events {
		defaults := timedByCalendar[e.CalendarID]
		if e.AllDay {
			defaults = allDayByCalendar[e.CalendarID]
		}
		for userID, userDefaults := range defaults {
			if len(resolved[e.ID][userID]) > 0 {
				// Their own rows on this Event win over their Default.
				continue
			}
			if markers[e.ID][userID] {
				// They saved an empty list here on purpose: no Reminders,
				// and their Default stops applying to this one Event.
				continue
			}
			if resolved[e.ID] == nil {
				resolved[e.ID] = make(map[int64][]repository.Reminder)
			}
			resolved[e.ID][userID] = projectDefaults(userDefaults)
		}
	}

	return resolved, nil
}

// projectDefaults turns a Calendar's Default reminders into the Reminders they
// resolve to on an Event: the same offset and Channel, carrying the default
// row's own id so the firing ledger can key an exactly-once fire on it
// (ADR-0064), and no Reminder id of their own — they were never rows on the
// Event they resolved onto.
func projectDefaults(defaults []repository.Reminder) []repository.Reminder {
	resolved := make([]repository.Reminder, len(defaults))
	for i, d := range defaults {
		resolved[i] = repository.Reminder{DefaultReminderID: d.ID, OffsetMinutes: d.OffsetMinutes, Channel: d.Channel}
	}
	return resolved
}

// uniqueCalendarIDs returns the distinct Calendar ids events span, in first-
// seen order — a resolution's batched Default lookup needs each Calendar once,
// regardless of how many Events on it are being resolved.
func uniqueCalendarIDs(events []repository.Event) []string {
	seen := make(map[string]bool, len(events))
	ids := make([]string, 0, len(events))
	for _, e := range events {
		if seen[e.CalendarID] {
			continue
		}
		seen[e.CalendarID] = true
		ids = append(ids, e.CalendarID)
	}
	return ids
}
