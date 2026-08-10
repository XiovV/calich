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
	// exactly-once key is (ReminderID, UserID, OccurrenceStart), since a
	// shared Calendar's Reminder fires independently per recipient
	// (ADR-0021, ADR-0036).
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

// representativeReminder is the single Reminder an offset override replaces
// an Event's whole Reminder set with (ADR-0036): "remind me two hours before
// this" is a statement about the Event, not a transformation applied to each
// of its Reminders, so an offset override must collapse to one trigger
// rather than one per underlying Reminder. Its id becomes that recipient's
// ledger key for the Event, so the choice must be stable across ticks and
// restarts regardless of query ordering — lowest id is arbitrary but fixed.
// It is not stable across an Event edit, which is fine: ADR-0020
// wholesale-replaces event_reminders on every edit anyway, so an edited
// Event's fired history restarts for everyone, override or not.
func representativeReminder(reminders []repository.Reminder) repository.Reminder {
	best := reminders[0]
	for _, r := range reminders[1:] {
		if r.ID < best.ID {
			best = r
		}
	}
	return best
}

// Due returns the due Reminders on event for each recipient whose trigger —
// an Occurrence's anchor, minus the effective offset — falls in the
// half-open window (from, to], matching the scheduler's "just-elapsed tick"
// semantics (ADR-0021). A recurring Event's RRULE is expanded (skipping any
// Exdated Occurrence); a non-recurring Event is checked as a series of one.
// Every User in event.RecipientUserIDs — the Calendar's Owner, every Shared
// Editor and Viewer, and every Attendee of the Event itself — is considered
// independently, so a Reminder fans out to everyone with Access to the
// Calendar unioned with everyone invited to the Event, regardless of Access
// (ADR-0036, ADR-0046). event.RecipientUserIDs is already deduplicated by
// its caller, so a User who is both an Access-holder and an Attendee is only
// considered once.
//
// A recipient's own entry in event.Overrides (ADR-0036) changes what they
// receive: a muted override drops them from every Reminder on this Event
// entirely. An offset override replaces the Event's whole Reminder set with
// one trigger at the override's offset — so a recipient with such an
// override gets exactly one DueReminder per Occurrence regardless of how
// many Reminders the Event carries — substituting the override's Channel too
// if it also sets one. A Channel-only override (no offset) instead
// re-channels every Reminder without collapsing their offsets, since nothing
// about the recipient's timing has changed. Because the effective offset can
// differ per recipient, the trigger window and its Occurrence search are
// computed once per (Reminder, recipient) pair rather than once per
// Reminder.
func Due(event repository.EventWithOwner, from, to time.Time) ([]DueReminder, error) {
	var due []DueReminder

	for _, userID := range event.RecipientUserIDs {
		override, hasOverride := event.Overrides[userID]
		if hasOverride && override.Muted {
			continue
		}

		reminders := event.Reminders
		if hasOverride && override.OffsetMinutes != nil && len(event.Reminders) > 0 {
			reminders = []repository.Reminder{representativeReminder(event.Reminders)}
		}

		for _, reminder := range reminders {
			offsetMinutes := reminder.OffsetMinutes
			channel := reminder.Channel
			if hasOverride {
				if override.OffsetMinutes != nil {
					offsetMinutes = *override.OffsetMinutes
				}
				if override.Channel != nil {
					channel = *override.Channel
				}
			}

			offset := time.Duration(offsetMinutes) * time.Minute
			triggerFrom := from.Add(offset)
			triggerTo := to.Add(offset)

			starts, err := occurrenceStarts(
				event.Event,
				triggerFrom.Add(-occurrenceSearchPad),
				triggerTo.Add(occurrenceSearchPad),
			)
			if err != nil {
				return nil, err
			}

			for _, start := range starts {
				at := anchor(event.Event, start)
				if at.After(triggerFrom) && !at.After(triggerTo) {
					due = append(due, DueReminder{
						EventID:         event.ID,
						UserID:          userID,
						Title:           event.Title,
						ReminderID:      reminder.ID,
						OccurrenceStart: start,
						OffsetMinutes:   offsetMinutes,
						Channel:         channel,
					})
				}
			}
		}
	}

	return due, nil
}

// overriddenRecurrenceIDs indexes every Override's recurrence id by its
// parent's id, so DueAll can keep a Master from firing a stale Reminder for
// an Occurrence that's since been overridden.
func overriddenRecurrenceIDs(events []repository.EventWithOwner) map[string][]time.Time {
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
func DueAll(events []repository.EventWithOwner, from, to time.Time) ([]DueReminder, error) {
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
