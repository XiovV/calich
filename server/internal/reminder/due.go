// Package reminder is the server-side firing engine (ADR-0021): pure
// occurrence-expansion and due-Reminder computation, plus a scheduler that
// ticks it against a fired-ledger and a dispatch seam. This reverses
// ADR-0016's frontend-only recurrence expansion for the firing path only —
// rendering still expands on the frontend.
package reminder

import (
	"time"

	"github.com/XiovV/calendar/server/internal/repository"
)

// DueReminder is one Reminder that becomes due within a window, paired with
// the specific Occurrence start it fires relative to.
type DueReminder struct {
	EventID string
	UserID  int64
	// Title is the Event's own title at the moment it fires — carried here
	// so a Notification-channel dispatch can copy it into its persisted
	// record without a second lookup (ADR-0021).
	Title string
	// ReminderID identifies the fired Reminder's own row — the ledger's
	// exactly-once key is (ReminderID, OccurrenceStart), ADR-0021.
	ReminderID      int64
	OccurrenceStart time.Time
	OffsetMinutes   int
	Channel         string
}

// anchor is the instant a Reminder's offset counts back from: an
// Occurrence's own start for a timed Event, or 09:00 on the Occurrence's own
// date for an all-day Event — never midnight, so an all-day Reminder never
// fires overnight (ADR-0020).
func anchor(event repository.Event, occurrenceStart time.Time) time.Time {
	if !event.AllDay {
		return occurrenceStart
	}
	y, m, d := occurrenceStart.UTC().Date()
	return time.Date(y, m, d, 9, 0, 0, 0, time.UTC)
}

// occurrenceSearchPad is generous slack added around a Reminder's
// trigger-window when searching for candidate Occurrence starts, since an
// all-day Reminder's anchor sits up to 9 hours after its Occurrence's stored
// (midnight) start. The exact anchor is recomputed and checked precisely
// below, so this only needs to be wide enough to not miss a candidate.
const occurrenceSearchPad = 24 * time.Hour

// Due returns every Reminder on event whose trigger — its Occurrence's
// anchor, minus the Reminder's own offset — falls in the half-open window
// (from, to], matching the scheduler's "just-elapsed tick" semantics
// (ADR-0021). A recurring Event's RRULE is expanded (skipping any Exdated
// Occurrence); a non-recurring Event is checked as a series of one.
func Due(event repository.Event, from, to time.Time) ([]DueReminder, error) {
	var due []DueReminder

	for _, reminder := range event.Reminders {
		offset := time.Duration(reminder.OffsetMinutes) * time.Minute
		triggerFrom := from.Add(offset)
		triggerTo := to.Add(offset)

		starts, err := occurrenceStarts(
			event,
			triggerFrom.Add(-occurrenceSearchPad),
			triggerTo.Add(occurrenceSearchPad),
		)
		if err != nil {
			return nil, err
		}

		for _, start := range starts {
			at := anchor(event, start)
			if at.After(triggerFrom) && !at.After(triggerTo) {
				due = append(due, DueReminder{
					EventID:         event.ID,
					UserID:          event.UserID,
					Title:           event.Title,
					ReminderID:      reminder.ID,
					OccurrenceStart: start,
					OffsetMinutes:   reminder.OffsetMinutes,
					Channel:         reminder.Channel,
				})
			}
		}
	}

	return due, nil
}

// overriddenRecurrenceIDs indexes every Override's recurrence id by its
// parent's id, so DueAll can keep a Master from firing a stale Reminder for
// an Occurrence that's since been overridden.
func overriddenRecurrenceIDs(events []repository.Event) map[string][]time.Time {
	byParent := make(map[string][]time.Time)
	for _, event := range events {
		if event.ParentID != nil && event.RecurrenceID != nil {
			byParent[*event.ParentID] = append(byParent[*event.ParentID], *event.RecurrenceID)
		}
	}
	return byParent
}

// DueAll runs Due across every event, in order — the scheduler's per-tick
// entry point over the full set of events with Reminders (ADR-0021).
//
// A Master's own RRULE keeps generating an Occurrence slot that's since been
// replaced by an Override — creating an Override never adds an Exdate
// (unlike an Exception/cancelled Occurrence) — so without this, the Master
// would still fire its (now stale) Reminders for that slot alongside the
// Override's own. DueAll excludes every Override's recurrence id from its
// Master's expansion the same way an Exdate does, so only the Override's own
// Event (found elsewhere in events, with its own Reminders and its own,
// possibly-moved start) fires for that Occurrence — mirroring the frontend's
// expandOccurrences.ts substitution (ADR-0016).
func DueAll(events []repository.Event, from, to time.Time) ([]DueReminder, error) {
	overriddenByParent := overriddenRecurrenceIDs(events)

	var due []DueReminder
	for _, event := range events {
		if overridden := overriddenByParent[event.ID]; len(overridden) > 0 {
			event.Exdates = append(append([]time.Time{}, event.Exdates...), overridden...)
		}

		eventDue, err := Due(event, from, to)
		if err != nil {
			return nil, err
		}
		due = append(due, eventDue...)
	}
	return due, nil
}
